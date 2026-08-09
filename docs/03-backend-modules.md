# 03 · Go 后端模块规格

> 每个包的职责、公开类型、关键算法。实现者按此逐包实现，**不要在包之间引入反向依赖**。

## 0. 依赖清单（go.mod）

```
go 1.23

require (
    github.com/go-chi/chi/v5              // 路由
    github.com/go-chi/cors                // CORS（dev）
    github.com/google/uuid                // UUIDv7: uuid.NewV7()
    github.com/fsnotify/fsnotify          // 文件监听
    gopkg.in/yaml.v3                      // YAML（用 yaml.Node 控制顺序/注释）
    github.com/yuin/goldmark              // 仅用于翻译分段的 AST，不产出 HTML
    github.com/yuin/goldmark/extension    //
    golang.org/x/crypto                   // argon2
    golang.org/x/image                    // draw（缩略图）
    golang.org/x/text                     // language.ParseAcceptLanguage、NFKC
    modernc.org/sqlite                    // 纯 Go SQLite，无 cgo
    github.com/BurntSushi/toml            // 仅导入器解析 Hugo TOML front matter
)
```

**禁止**引入：任何需要 cgo 的库、任何 ORM、任何 web 框架（chi 只是 mux）。

构建：`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" ./cmd/blog`

---

## 1. `internal/model` — 纯数据类型

无内部依赖。所有跨包传递的类型定义在此。

```go
package model

type Locale string          // 规范形式 "zh-CN"
type ArticleID string       // UUIDv7
type ContentType string     // "post" | "page"

type Status string
const (
    StatusDraft       Status = "draft"
    StatusPublished   Status = "published"
    StatusUnpublished Status = "unpublished"
    StatusTrashed     Status = "trashed"
)

type TranslationStatus string
const (
    TSOriginal    TranslationStatus = "original"
    TSPending     TranslationStatus = "pending"
    TSTranslating TranslationStatus = "translating"
    TSCompleted   TranslationStatus = "completed"
    TSFailed      TranslationStatus = "failed"
    TSOutdated    TranslationStatus = "outdated"
    TSManual      TranslationStatus = "manual"
)

// LocalizedString: YAML 中可以是 string 或 map[locale]string，统一成 map
type LocalizedString map[Locale]string

func (l LocalizedString) Get(loc, fallback Locale) string
func (l *LocalizedString) UnmarshalYAML(n *yaml.Node) error  // 兼容纯 string
func (l LocalizedString) MarshalYAML() (any, error)          // 单键且为 fallback 时输出纯 string

// ── Article ───────────────────────────────────────────────
type Article struct {
    ID        ArticleID
    Type      ContentType
    BundleDir string                       // 相对 content/ 的目录，如 "posts/2026/my-server"
    Source    Locale
    SourceRev int
    CreatedAt time.Time
    Versions  map[Locale]*ArticleVersion   // 各语言正文 + FrontMatter
    Trans     map[Locale]*TranslationState
    Broken    bool
    BrokenErr string
}

type ArticleVersion struct {
    Locale      Locale
    FilePath    string      // 相对 content/
    Front       FrontMatter
    Body        string      // 原始 Markdown 正文（不含 front matter）
    BodyHash    string      // sha256(Body)，用于判断是否真的变了
    FileModTime time.Time
    Rev         int
}

type FrontMatter struct {
    ID          ArticleID `yaml:"id"`
    Title       string    `yaml:"title"`
    Slug        string    `yaml:"slug"`
    Description string    `yaml:"description,omitempty"`
    Date        time.Time `yaml:"date"`
    Updated     *time.Time `yaml:"updated,omitempty"`
    Status      Status    `yaml:"status"`
    Categories  []string  `yaml:"categories,omitempty"`
    Tags        []string  `yaml:"tags,omitempty"`
    Cover       string    `yaml:"cover,omitempty"`
    Author      string    `yaml:"author"`
    SourceLocale Locale   `yaml:"sourceLocale"`
    Locale      Locale    `yaml:"locale"`
    Pinned      bool      `yaml:"pinned,omitempty"`
    TOC         *bool     `yaml:"toc,omitempty"`
    Comments    *bool     `yaml:"comments,omitempty"`
    SEO         *SEO      `yaml:"seo,omitempty"`
    // page only
    Template    string    `yaml:"template,omitempty"`
    Order       int       `yaml:"order,omitempty"`
    ShowInMenu  bool      `yaml:"showInMenu,omitempty"`
    Extra       map[string]any `yaml:",inline"`   // 保留未知字段，写回时不丢失
}

type TranslationState struct {
    Status                TranslationStatus
    TranslatedFromRevision int
    Revision              int
    ManualEdited          bool
    ManualEditedAt        *time.Time
    Provider, Model       string
    UpdatedAt             *time.Time
    TokensUsed            int
    Error                 string
    FailedAt              *time.Time
    Attempts              int
    SourceDrift           int  `yaml:"-"`   // 运行时计算，不落盘
}

// Category / Tag / Link / LinkGroup / Menu / MenuItem / User / MediaMeta
// 字段与 docs/02 的 YAML 一一对应，此处省略，实现时照抄。
```

**`Extra map[string]any` 内联字段是必须的**：用户或第三方可能在 Front Matter 里加自定义字段，写回时必须原样保留。

---

## 2. `internal/fsutil` — 文件系统原语

```go
package fsutil

// AtomicWrite: 写 tmp → fsync(file) → close → rename → fsync(dir)
// tmp 文件与目标同目录（保证同一文件系统），名为 ".<base>.tmp<rand>"
// 写入前调用 suppressor（若注册）通知 watcher 忽略即将发生的事件
func AtomicWrite(path string, data []byte, perm os.FileMode) error

// AtomicWriteDir: 批量写入，全部成功才 rename；任一失败则清理所有 tmp
func AtomicWriteBatch(files map[string][]byte, perm os.FileMode) error

// SafeJoin: 防路径穿越。base 必须是绝对路径，rel 不得逃出 base
// 返回 (abs, error)；rel 含 ".." 或绝对路径或符号链接逃逸时报错
func SafeJoin(base, rel string) (string, error)

// EnsureDir: MkdirAll + 校验是目录
func EnsureDir(path string, perm os.FileMode) error

// AtomicSymlink: 用 tmp symlink + rename 原子切换 symlink 指向
func AtomicSymlink(target, linkPath string) error

// CopyFile / HardLinkOrCopy: 发布 bundle assets 时用，优先硬链接省空间
func HardLinkOrCopy(src, dst string) error

// KeyedMutex: 按 key 加锁，用于 bundle 级串行化
type KeyedMutex struct{ ... }
func (m *KeyedMutex) Lock(key string) func()   // 返回 unlock

// WriteSuppressor 接口，由 watcher 实现，fsutil 只持有接口
type WriteSuppressor interface{ Suppress(path string, d time.Duration) }
func SetSuppressor(s WriteSuppressor)
```

**AtomicWrite 的实现细节（必须完全按此写）**：

```go
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
    dir := filepath.Dir(path)
    if err := os.MkdirAll(dir, 0o755); err != nil { return err }
    if sup != nil { sup.Suppress(path, 2*time.Second) }

    f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp")
    if err != nil { return err }
    tmp := f.Name()
    defer func() { if err != nil { f.Close(); os.Remove(tmp) } }()

    if _, err = f.Write(data); err != nil { return err }
    if err = f.Sync(); err != nil { return err }          // 数据落盘
    if err = f.Chmod(perm); err != nil { return err }
    if err = f.Close(); err != nil { return err }
    if err = os.Rename(tmp, path); err != nil { return err }

    // rename 本身也需要持久化目录项
    if d, e := os.Open(dir); e == nil { d.Sync(); d.Close() }
    return nil
}
```

---

## 3. `internal/content` — Bundle 读写

```go
package content

type Store struct { root string; mu *fsutil.KeyedMutex }

// 读
func (s *Store) LoadBundle(bundleDir string) (*model.Article, error)
func (s *Store) ScanAll(ctx context.Context) ([]*model.Article, []ScanError, error)  // 并发扫描

// 写
func (s *Store) SaveVersion(a *model.Article, loc model.Locale, front model.FrontMatter, body string, opts SaveOpts) error
func (s *Store) SaveMetadata(a *model.Article) error
func (s *Store) CreateBundle(typ model.ContentType, loc model.Locale, front model.FrontMatter, body string) (*model.Article, error)
func (s *Store) DeleteBundle(id model.ArticleID) error       // 硬删除（回收站清空时）
func (s *Store) RenameBundle(id model.ArticleID, newDir string) error

// 草稿
func (s *Store) SaveDraft(id model.ArticleID, loc model.Locale, front model.FrontMatter, body string) error
func (s *Store) LoadDraft(id model.ArticleID, loc model.Locale) (*Draft, error)
func (s *Store) DiscardDraft(id model.ArticleID, loc model.Locale) error

// 修订
func (s *Store) SnapshotRevision(a *model.Article, loc model.Locale) (rev int, err error)
func (s *Store) ListRevisions(id model.ArticleID, loc model.Locale) ([]RevisionInfo, error)
func (s *Store) ReadRevision(id model.ArticleID, loc model.Locale, rev int) (front model.FrontMatter, body string, err error)

type SaveOpts struct {
    BumpSourceRevision bool   // 发布源语言时 true
    Snapshot           bool   // 是否快照修订
    MarkManualEdit     bool   // 编辑派生语言时 true
    MirrorAuthoritative bool  // 保存派生语言时，用源的权威字段覆写
}
```

### 3.1 Front Matter 解析器

```go
// SplitFrontMatter 返回 (yamlBytes, body, error)
// 处理：UTF-8 BOM、\r\n、开头空行容忍、第二个 "---" 必须独占一行
func SplitFrontMatter(raw []byte) ([]byte, string, error)

// Serialize 按固定字段顺序输出，保证 git diff 稳定
func Serialize(front model.FrontMatter, body string) ([]byte, error)
```

**Serialize 的顺序**（硬编码，见 docs/02 §1.3 字段表顺序）：
`id, title, slug, description, date, updated, status, categories, tags, cover, author, sourceLocale, locale, pinned, toc, comments, template, order, showInMenu, seo, <Extra 按键名字典序>`

正文输出规则：Front Matter 后接**一个空行**再接正文；正文末尾保证恰好一个 `\n`。

### 3.2 权威字段镜像

保存派生语言时（`MirrorAuthoritative=true`）：

```go
func mirrorFromSource(src, dst *model.FrontMatter) {
    dst.ID = src.ID
    dst.Date = src.Date
    dst.Status = src.Status
    dst.Categories = slices.Clone(src.Categories)
    dst.Tags = slices.Clone(src.Tags)
    dst.Author = src.Author
    dst.Pinned = src.Pinned
    dst.Cover = src.Cover
    dst.SourceLocale = src.SourceLocale
    dst.Comments = src.Comments
    // title/description/slug/updated/toc/seo 保持 dst 自己的
}
```

源语言发布时，必须**同步刷新所有派生语言文件**的权威字段（因为分类/状态可能变了），并触发它们的重渲染。

---

## 4. `internal/index` — 内存索引

系统的读路径核心。所有列表查询都从这里出，绝不遍历文件系统。

```go
package index

type Index struct {
    mu sync.RWMutex

    articles   map[model.ArticleID]*model.Article
    // 排序视图：按 type + locale 分别维护，已按 pinned desc, date desc 排好
    byTypeLoc  map[typeLocKey][]model.ArticleID
    // slug → id，按 type + locale
    slugIdx    map[slugKey]model.ArticleID
    // 分类/标签倒排
    byCategory map[string][]model.ArticleID     // 含继承：文章属于子分类时也计入祖先
    byTag      map[string][]model.ArticleID
    // 归档：locale → year → month → ids
    archive    map[model.Locale]map[int]map[int][]model.ArticleID

    categories map[string]*model.Category
    catTree    []*CategoryNode
    tags       map[string]*model.Tag
    links      map[string]*model.Link
    linkGroups []*model.LinkGroup
    menus      map[string]*model.Menu
    users      map[string]*model.User

    errors     []ScanError
    stats      Stats
    generation uint64      // 每次变更 +1，用于 ETag / 缓存失效
}
```

### 4.1 查询 API

```go
func (ix *Index) Article(id model.ArticleID) (*model.Article, bool)
func (ix *Index) BySlug(typ model.ContentType, loc model.Locale, slug string) (*model.Article, bool)

type ListQuery struct {
    Type      model.ContentType
    Locale    model.Locale
    Status    []model.Status     // 空 = 全部
    Category  string             // 含子分类
    Tag       string
    Author    string
    Year, Month int
    Search    string             // 后台列表用的简单子串匹配（前台搜索走静态索引）
    TransStatus model.TranslationStatus
    Sort      string             // date_desc | date_asc | updated_desc | title_asc
    Page, PerPage int
}
func (ix *Index) List(q ListQuery) (items []*model.Article, total int)

// 渲染用：只取已发布的
func (ix *Index) PublishedPosts(loc model.Locale) []*model.Article
func (ix *Index) AdjacentPosts(loc model.Locale, id model.ArticleID) (prev, next *model.Article)
func (ix *Index) CategoryTree(loc model.Locale) []*CategoryNode
func (ix *Index) Stats() Stats
```

### 4.2 变更 API（写路径调用）

```go
func (ix *Index) UpsertArticle(a *model.Article)   // 内部重建受影响的排序视图
func (ix *Index) RemoveArticle(id model.ArticleID)
func (ix *Index) ReloadTaxonomy(kind string) error // categories|tags|links|menus|users
func (ix *Index) RebuildAll(ctx context.Context) error
```

**性能约束**：`UpsertArticle` 不得触发全量重排。做法：`byTypeLoc` 用有序切片，插入时二分查找位置 `slices.Insert`；更新时先移除旧位置再插入。1000 篇文章下单次 Upsert < 100µs。

### 4.3 分类继承

文章标 `categories: [linux]`，而 `linux.parent = technology`，则该文章**同时**出现在 `technology` 分类页。实现：构建 `byCategory` 时对每个文章分类沿 parent 链向上传播。分类树变更时必须重建整个 `byCategory`。

### 4.4 内存预算（★ 已修订，原方案会在真实数据下爆内存）

**原先的估算是错的**：我按"平均正文 8KB"算出 1000 篇 × 4 语言 = 32MB。但 8KB 是随手拍的——技术博客单篇动辄两三万字，即 **60~90KB**。真实场景：

```
1000 篇 × 5 语言 × 70KB ≈ 350MB       ← 在 512MB 预算里直接撑爆
```

而且原方案把"是否改惰性加载"推迟到 M7 压测后决定，这是把一个**会推翻内存模型的架构决定**放到项目后期——真要改，`index` 包和所有调用方都得动。

#### 修正后的方案：M1 就实现惰性 Body，按字节数而非文章数触发

```go
type Index struct {
    // ...
    bodyMode   BodyMode          // resident | lazy
    bodyBytes  int64             // 常驻模式下已占用的字节数
    bodyCache  *lru.Cache        // lazy 模式下的 LRU，容量 index.bodyCacheMB
}

type BodyMode int
const (
    BodyResident BodyMode = iota  // Body 全部常驻内存
    BodyLazy                      // 只存 FrontMatter + BodyHash，Body 按需读盘 + LRU
)
```

**切换规则（按字节，不按篇数）**：

```
构建索引时累加 len(Body)：
  总字节 <= index.bodyResidentLimitMB (默认 64MB)  → 保持 resident
  超过阈值 → 就地降级为 lazy：释放所有 Body，改走 LRU
             日志: "正文总量 320MB 超过常驻上限 64MB，已切换为按需加载模式"
```

配置项（新增到 `config.yaml`）：

```yaml
index:
  bodyResidentLimitMB: 64      # 超过则降级为 lazy
  bodyCacheMB: 32              # lazy 模式下的 LRU 容量
  forceMode: ""                # "" | resident | lazy（调试用，强制指定）
```

**关键设计**：
- `ArticleVersion.Body` 字段改为**私有 + 通过方法访问**：`func (ix *Index) Body(id, locale) (string, error)`。两种模式对调用方完全透明。
- `BodyHash` **永远常驻**（32 字节 × N，可忽略），用于判断内容是否真变。
- 列表查询、分类倒排、归档索引**从不需要 Body** —— 只有渲染和翻译需要。所以 lazy 模式对读路径几乎无影响。
- lazy 模式下的读盘走 `os.ReadFile` + 解析 Front Matter 后取正文，单次约 0.1ms（页缓存命中），可接受。

**必须在 M1 实现两种模式并各自单测**，而不是留到 M7。M1 的验收增加一条：构造 200MB 正文的测试数据集，断言进程 RSS < 250MB。

---

## 5. `internal/watcher` — 文件监听

```go
package watcher

type Watcher struct { ... }

func New(paths []string, bus *events.Bus) (*Watcher, error)
func (w *Watcher) Start(ctx context.Context) error
func (w *Watcher) Suppress(path string, d time.Duration)   // 实现 fsutil.WriteSuppressor
```

**算法**：

```
1. fsnotify 递归监听 content/ data/ config/ themes/*/dist/
   （fsnotify 不递归，需自己 walk 并对每个子目录 Add；新建目录时动态 Add）
2. 收到事件 → 检查 suppressor：
     若 path 在 recentWrites 且未过期 → 丢弃
3. 过滤：
     忽略 .tmp* / .swp / ~ 结尾 / .DS_Store / 隐藏目录（.drafts 与 .revisions 也忽略）
4. 放入 pending set（按"受影响对象"归并，而非按文件）：
     content/posts/2026/x/index.en.md  → 对象 = bundle "posts/2026/x"
     data/categories/linux.yaml        → 对象 = taxonomy "categories"
     config/config.yaml                → 对象 = config
5. 300ms debounce（每次新事件重置定时器，最长强制 2s 触发一次）
6. 触发后按对象类型投递事件：
     BundleChanged{dir} / TaxonomyChanged{kind} / ConfigChanged / ThemeDistChanged
7. index 订阅这些事件 → 重载对应对象 → 再发 ArticleChanged 等业务事件 → render 订阅并入队
```

**边界情况**：
- 整个 bundle 目录被删除 → `BundleRemoved`，从索引移除并删除对应 HTML 输出。
- 大批量变更（如 `git checkout` 切换分支）→ 若 300ms 内事件数 > 200，直接触发 `RebuildAll` 而非逐个处理。
- macOS 上 fsnotify 有 fd 上限，超过 8000 个目录时降级为**轮询模式**（每 5s 扫描 mtime）。记录警告。

---

## 6. `internal/events` — 事件总线

同步进程内总线，作为未来插件系统的 Hook 点。

```go
package events

type Event interface{ Name() string }

type Bus struct{ mu sync.RWMutex; subs map[string][]Handler }
type Handler func(ctx context.Context, e Event) error

func (b *Bus) Subscribe(name string, h Handler)
func (b *Bus) Publish(ctx context.Context, e Event)          // 异步，错误只记日志
func (b *Bus) PublishSync(ctx context.Context, e Event) error // 同步，任一 handler 出错则中止
```

**必须实现的事件**（需求第 59 条）：

```go
ArticleCreated{ID, Type, Locale}
ArticleUpdated{ID, Locale, BodyChanged bool}
ArticlePublished{ID, Locale, FirstPublish bool}
ArticleUnpublished{ID, Locale}
ArticleDeleted{ID, Type}
ArticleRestored{ID}

CategoryChanged{ID, Op}   // Op: created|updated|deleted
TagChanged{ID, Op}
LinkChanged{ID, Op}
MenuChanged{ID}
SettingsChanged{Keys []string}
ThemeChanged{Name}
ThemeSettingsChanged{Name}

BeforeRender{Unit RenderUnit, Props *PageProps}   // PublishSync，允许修改 Props
AfterRender{Unit RenderUnit, HTML *string}        // PublishSync，允许修改 HTML
RenderCompleted{Units int, Duration time.Duration}

MediaUploaded{Path, Size}
MediaDeleted{Path}

TranslationStarted{ArticleID, Target Locale}
TranslationCompleted{ArticleID, Target Locale, Tokens int}
TranslationFailed{ArticleID, Target Locale, Err string}
```

`BeforeRender` / `AfterRender` 用 `PublishSync` 且允许 handler 修改传入的指针——这是第二阶段插件注入内容的接口。MVP 内部只用它注入 SEO 标签。

---

## 7. `internal/media` — 媒体与存储

### 7.1 Storage 接口

```go
package media

type Storage interface {
    Put(ctx context.Context, path string, r io.Reader, size int64, mime string) error
    Get(ctx context.Context, path string) (io.ReadCloser, error)
    Delete(ctx context.Context, path string) error
    Move(ctx context.Context, src, dst string) error
    List(ctx context.Context, dir string) ([]Entry, error)
    Stat(ctx context.Context, path string) (Entry, error)
    PublicURL(path string) string
    // 本地驱动返回真实路径供 Nginx 直服；对象存储返回 ""
    LocalPath(path string) string
}

type LocalStorage struct{ root, prefix string }
```

MVP 只实现 `LocalStorage`。S3/R2 在第二阶段加，**接口不得为了 local 而设计得无法适配对象存储**（所以有 `PublicURL` 和返回空的 `LocalPath`）。

### 7.2 上传流程

```
1. multipart 接收，流式写入临时文件（不整体读进内存）
2. 大小校验（config.storage.image.maxUploadSize）
3. MIME 嗅探：http.DetectContentType(前 512 字节)，与扩展名交叉校验
   —— 不信任客户端 Content-Type
4. 文件名规范化：转小写、空格→-、去除特殊字符、保留扩展名
5. 目标路径：<year>/<month>/<name>[-<n>].<ext>，重名时加序号
6. SVG 特殊处理：必须经过 SVG 清洗（移除 <script>/on*/xlink:href=javascript:）
   或在配置中禁用 SVG 上传（默认允许但清洗）
7. 图片：解码取宽高；stripEXIF=true 时重编码去除 EXIF
8. 生成缩略图（见下）
9. 写 sidecar .meta/<path>.json
10. 发 MediaUploaded 事件
```

### 7.3 图片处理（MVP 范围）

**明确取舍**：纯 Go 生态缺少可靠的有损 WebP / AVIF 编码器，引入 cgo 违反 A7。因此：

| 能力 | MVP | 实现 |
|---|---|---|
| 读取宽高 | ✅ | `image.DecodeConfig`（jpeg/png/gif/webp 用 `golang.org/x/image/webp` 解码） |
| 缩略图（同格式） | ✅ | `golang.org/x/image/draw` 的 `CatmullRom` 缩放，输出与源同格式 |
| EXIF 剥离 | ✅ | 重编码即可去除 |
| WebP/AVIF 转码 | ⛔ MVP | 检测 `cwebp`/`avifenc` 是否在 PATH，存在则调用外部命令；不存在则跳过并在后台提示 |
| SVG 清洗 | ✅ | 自实现白名单清洗器 |

缩略图命名：`<base>.<variant>.<ext>`，如 `cover.thumb.png`。

### 7.4 媒体库目录

支持虚拟目录（就是真实目录）。API 提供 `mkdir` / `rename` / `move`。所有路径操作必须过 `fsutil.SafeJoin`。

---

## 8. `internal/theme` — 主题管理

```go
package theme

type Theme struct {
    Name        string
    Meta        Meta            // theme.yaml
    Schema      SettingsSchema  // settings.schema.json 解析后
    Settings    map[string]any  // data/themes/<name>.settings.yaml 的值 + schema 默认值
    Manifest    Manifest        // dist/manifest.json：入口 js/css 及其 hash 文件名
    Templates   []string        // dist/ssr 声明支持的模板
    Dir         string
}

type Manager struct{ themes map[string]*Theme; active string }

func (m *Manager) Discover() error                     // 扫描 themes/*/theme.yaml
func (m *Manager) Active() *Theme
func (m *Manager) Activate(name string) error          // 校验 → 写 config → 触发全量重建
func (m *Manager) UpdateSettings(name string, vals map[string]any) error  // 按 schema 校验
func (m *Manager) ValidateSettings(t *Theme, vals map[string]any) []ValidationError
```

`settings.schema.json` 的支持类型见 [docs/09](09-theme-system.md)。后端只需**校验**与**存储**，UI 渲染由 Admin 前端完成。

---

## 9. `internal/jobs` — 任务队列

基于 SQLite 的持久队列 + 内存 worker pool。

```go
package jobs

type Job struct {
    ID int64; Kind string; DedupeKey string
    Payload json.RawMessage
    Priority int; Attempts, MaxAttempts int
    RunAfter time.Time
}

type Queue struct{ db *sql.DB }

// Enqueue：若存在同 (kind, dedupe_key) 的 pending 任务，则合并（更新 payload 与 priority 取更小值）
func (q *Queue) Enqueue(ctx context.Context, j Job) (int64, error)
func (q *Queue) Claim(ctx context.Context, kinds []string, workerID string) (*Job, error)
func (q *Queue) Complete(ctx context.Context, id int64) error
func (q *Queue) Fail(ctx context.Context, id int64, err error, retryIn time.Duration) error
func (q *Queue) Stats(ctx context.Context) (map[string]KindStats, error)

type Pool struct{ ... }
func NewPool(q *Queue, kinds []string, concurrency int, h HandlerFunc) *Pool
func (p *Pool) Start(ctx context.Context)
```

**Claim 的 SQL**（必须用事务 + `RETURNING` 保证原子领取）：

```sql
UPDATE jobs SET status='running', locked_by=?, locked_at=?, attempts=attempts+1, updated_at=?
WHERE id = (
  SELECT id FROM jobs
  WHERE status='pending' AND kind IN (...) AND run_after <= ?
  ORDER BY priority ASC, id ASC LIMIT 1
)
RETURNING id, kind, payload, attempts, max_attempts;
```

**心跳与僵死回收**：worker 在执行期间每 30s 更新一次 `locked_at`（心跳）。看门狗把 `status='running' AND locked_at < now-120s` 的任务重置为 `pending`。

```go
func (q *Queue) Heartbeat(ctx context.Context, id int64, workerID string) error
```

⚠️ **回收时不增加 `attempts`** —— 进程被 kill 或宿主重启不是任务本身的错，不应消耗重试配额。只有任务自身返回错误才 `attempts+1`。同理，熔断器打开期间被跳过的任务也不增加 `attempts`。详见 [docs/12 §3.1](12-reliability-ux.md)。

**退避**：`retryIn = min(2^attempts * 2s, 5min)` + 0~20% 抖动。

**毒丸隔离**：同一 `dedupe_key` 在 24 小时内失败超过 10 次 → 标记 `permanently_failed`，停止重试并告警，避免一个坏对象堵死队列。见 [docs/12 §4.1](12-reliability-ux.md)。

**定时发布调度器**：独立 goroutine，每 30s 查 `scheduled_publish WHERE status='scheduled' AND publish_at <= now`，执行发布。

---

## 10. `internal/state` — SQLite 封装

```go
package state

// DB 封装读写分离的双连接池
type DB struct {
    read  *sql.DB      // WAL 允许并发读，MaxOpenConns = min(4, NumCPU)
    write *sql.DB      // MaxOpenConns = 1，所有写串行化
}

func Open(path string) (*DB, error)  // WAL, busy_timeout=5000, foreign_keys=ON, synchronous=NORMAL
func (d *DB) Read() *sql.DB          // SELECT 用
func (d *DB) Write() *sql.DB         // INSERT/UPDATE/DELETE 用
func (d *DB) Tx(ctx, fn func(*sql.Tx) error) error   // 写事务，自动重试 SQLITE_BUSY
func Migrate(d *DB) error            // 顺序执行 embed 的 migrations/*.sql
func Checkpoint(d *DB) error         // 关闭前 PRAGMA wal_checkpoint(TRUNCATE)
```

#### 并发模型（★ 已修订，原先写"SetMaxOpenConns(1)"过于笼统）

原规格只说"只用一个 `*sql.DB`，`SetMaxOpenConns(1)`"。但系统里同时有：渲染 worker 更新 `render_units`、翻译 worker 更新 `translation_tasks`、看门狗每 30s 扫描、审计日志写入、SSE 查询状态、后台列表查询。**全部串行到一个连接上**，任何一个慢查询都会阻塞其余所有操作，包括只读的后台列表。

**正确做法：读写分离。**

| 池 | MaxOpenConns | 用途 | 理由 |
|---|---|---|---|
| `read` | `min(4, NumCPU)` | 所有 `SELECT` | WAL 模式下读不阻塞写，也不互相阻塞 |
| `write` | **1** | 所有写操作 | SQLite 本质单写者；串行化到 1 个连接可彻底避免 `SQLITE_BUSY` 竞争 |

```go
// Open 内部
read.SetMaxOpenConns(min(4, runtime.NumCPU()))
read.SetMaxIdleConns(2)
write.SetMaxOpenConns(1)
write.SetMaxIdleConns(1)
write.SetConnMaxLifetime(0)   // 长期复用，避免反复打开
```

**额外要求**：
- 写事务必须短。**绝不在事务里做 HTTP 调用、文件 IO 或渲染**——先在事务外完成慢操作，再开事务写结果。
- `Tx()` 对 `SQLITE_BUSY` / `SQLITE_LOCKED` 自动重试（最多 3 次，退避 50/200/500ms）。
- 审计日志与 `system_log` 走**异步缓冲写**（带 channel 的单 goroutine，每 200ms 或积满 50 条批量提交），避免高频小写拖慢主流程。缓冲丢失可接受（日志不是关键数据）。
- `modernc.org/sqlite` 是纯 Go 实现，性能低于 cgo 版约 2~3 倍。上述设计正是为了在这个前提下仍然够用。

迁移文件用 `//go:embed migrations/*.sql`，命名 `0001_init.sql`、`0002_xxx.sql`。**只增不改**。

**损坏恢复**：`Open` 后执行 `PRAGMA integrity_check`，非 `ok` 则关闭、重命名为 `state.db.corrupt.<unix>`、新建空库、返回 `ErrRecreated`，上层据此触发全量重建。

---

## 11. `internal/backup`

```go
func (s *Service) Create(ctx context.Context, opts Options) (path string, err error)
func (s *Service) List() ([]BackupInfo, error)
func (s *Service) Restore(ctx context.Context, path string, opts RestoreOptions) error
func (s *Service) ExportZIP(ctx context.Context, w io.Writer, opts Options) error
```

**备份内容**：`content/`（含 `.revisions/`，不含 `.drafts/`）、`data/`（含 `state.db` 的 `VACUUM INTO` 快照，不含 wal/shm）、`media/`（可选）、`config/`。
**不含**：`generated/`、`cache/`、`node_modules/`、`themes/*/dist/`。

格式：`backups/backup-<ts>.zip`，内含 `manifest.json`（版本、时间、文件数、各目录 sha256）。

**恢复流程**：解压到临时目录 → 校验 manifest → 停止 watcher 与 job pool → 把现有目录重命名为 `.bak.<ts>` → 移动新目录就位 → 重建索引 → 全量重建 → 恢复 watcher。任何一步失败则回滚。

---

## 12. 关键实现陷阱清单

实现者请逐条核对：

1. **`yaml.Marshal` 的 map 顺序不稳定** → 所有多语言 map 输出前按 locale 排序；`FrontMatter` 用手工构造的 `yaml.Node`。
2. **`time.Time` 的 YAML 序列化会丢时区** → 统一用 `string` 中转，格式 `time.RFC3339`，解析时保留原时区偏移。
3. **Windows 路径分隔符** → 所有存进 model 的路径用 `/`，只在真正 IO 时 `filepath.FromSlash`。
4. **fsnotify 在 rename 时先发 Create 再发 Rename/Chmod** → 必须靠 suppressor 而不是靠事件类型判断。
5. **同一 bundle 的并发写** → 必须过 `KeyedMutex`，key = bundleDir。
6. **索引写锁不能跨越 IO** → 先在锁外读文件解析，再拿写锁替换对象。
7. **UUIDv7 需要 `uuid.NewV7()`（google/uuid ≥ 1.6）**，不要用 v4。
8. **`slices.Insert` 后原切片可能被复用** → 排序视图更新后重新赋值回 map。
9. **SQLite 并发** → 读写分离双池（见 §10）：读池 `min(4,NumCPU)`，写池固定 1。**绝不在写事务里做 HTTP 调用、文件 IO 或渲染**——先在事务外完成慢操作再开事务写结果。审计日志走异步缓冲批量写。
10. **删除文章时必须同时删除 `generated/` 下所有 locale 的产物**，否则会留下幽灵页面。
11. **`Extra` 内联字段与已知字段重名** → `yaml:",inline"` 要求 map 中不含已定义字段，解析后需手工剔除。
12. **正文中的 `---` 分隔线** 可能被误判为 Front Matter 结束 → 必须只在文件**开头**匹配，且第二个 `---` 从第 2 行开始找。
