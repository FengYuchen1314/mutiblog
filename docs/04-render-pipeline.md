# 04 · 渲染管线规格

> 系统最核心的子系统。分两半：**Go 侧编排**（决定渲染什么、何时渲染、写到哪）与 **Node 侧执行**（Markdown → HTML → React SSR）。

---

## 1. 概念：RenderUnit

一个 RenderUnit = 磁盘上一个输出文件。这是渲染系统的最小调度单位。

```go
type RenderUnit struct {
    Key        string          // 全局唯一，作为队列去重键
    Kind       UnitKind
    Locale     model.Locale
    OutputPath string          // 相对 generated/public 的路径
    Params     map[string]any  // 如 {"articleID":"...", "page":2, "categoryID":"linux"}
    Priority   int
}

type UnitKind string
const (
    UnitPost     UnitKind = "post"
    UnitPage     UnitKind = "page"
    UnitHome     UnitKind = "home"       // 含分页
    UnitCategory UnitKind = "category"   // 含分页
    UnitCategoryList UnitKind = "category_list"
    UnitTag      UnitKind = "tag"
    UnitTagList  UnitKind = "tag_list"
    UnitArchive  UnitKind = "archive"    // 索引 / 年 / 年月
    UnitLinks    UnitKind = "links"
    UnitSearch   UnitKind = "search"     // 搜索页 HTML
    UnitSearchIndex UnitKind = "search_index"  // JSON
    UnitRSS      UnitKind = "rss"
    UnitSitemap  UnitKind = "sitemap"
    UnitSitemapIndex UnitKind = "sitemap_index"
    UnitRobots   UnitKind = "robots"
    Unit404      UnitKind = "not_found"
)
```

### 1.1 Key 与输出路径对照表

| Kind | Key 格式 | 输出路径 |
|---|---|---|
| post | `post:<locale>:<articleID>` | `<prefix>/posts/<slug>/index.html` |
| page | `page:<locale>:<articleID>` | `<prefix>/<slug>/index.html` |
| home | `home:<locale>:<page>` | `<prefix>/index.html` / `<prefix>/page/<n>/index.html` |
| category | `category:<locale>:<catID>:<page>` | `<prefix>/categories/<slug>/index.html` / `.../page/<n>/index.html` |
| category_list | `category_list:<locale>` | `<prefix>/categories/index.html` |
| tag | `tag:<locale>:<tagID>:<page>` | `<prefix>/tags/<slug>/index.html` |
| tag_list | `tag_list:<locale>` | `<prefix>/tags/index.html` |
| archive | `archive:<locale>:<year>:<month>` | `<prefix>/archive/index.html`, `/archive/2026/index.html`, `/archive/2026/08/index.html` |
| links | `links:<locale>` | `<prefix>/links/index.html` |
| search | `search:<locale>` | `<prefix>/search/index.html` |
| search_index | `search_index:<locale>` | `<prefix>/search-index.json` |
| rss | `rss:<locale>` | `<prefix>/rss.xml` |
| sitemap | `sitemap:<locale>` | `sitemap-<prefix>.xml` |
| sitemap_index | `sitemap_index` | `sitemap.xml` |
| robots | `robots` | `robots.txt` |
| not_found | `404:<locale>` | `<prefix>/404.html` |

`<prefix>` = 该 locale 的 `urlPrefix`，如 `zh-cn`、`en`。

---

## 2. 增量依赖计算

**不做通用依赖追踪图**（复杂且易错）。改为显式的 `AffectedUnits(change) []RenderUnit` 纯函数——输入变更描述 + 当前索引，输出受影响单元列表。可单元测试，可推理。

```go
package render

func (o *Orchestrator) AffectedUnits(ch Change) []RenderUnit
```

### 2.1 各类变更的影响面（必须完整实现）

#### 文章发布 / 更新 / 取消发布 / 删除

设文章 A，语言 L，分类集合 C（含祖先），标签集合 T，日期 Y-M。

```
1.  post:L:A                                    （删除时改为「删文件」而非渲染）
2.  post:L:prev(A)  与  post:L:next(A)          ← 相邻文章的 prev/next 导航变了
       · 取变更前和变更后的相邻文章并集（顺序可能改变）
3.  home:L:k .. home:L:lastPage                  ← 见下方「分页的正确算法」
4.  对每个 c ∈ C:  category:L:c:k..last  +  category_list:L
5.  对每个 t ∈ T:  tag:L:t:k..last        +  tag_list:L
6.  archive:L::  、archive:L:Y:  、archive:L:Y:M
7.  rss:L
8.  sitemap:L  +  sitemap_index
9.  search_index:L
10. 若 A 的其他语言版本存在 → 它们的 post 页也需重渲染（hreflang 与语言切换器变了）
       · 仅当「语言集合发生增减」时才需要；纯内容更新不需要
       · ★ 必须使用 30s 滑动合并窗口，见下方
```

#### ⚠️ hreflang 重渲染必须走合并窗口（[docs/13 P21](13-first-run-walkthrough.md)）

hreflang 要求双向对称，所以新增一个语言版本要重渲染其他所有语言的页面。而翻译是**依次完成**的：

```
en 完成    → 重渲染 zh-CN                 (1)
ja 完成    → 重渲染 zh-CN, en             (2)
de 完成    → 重渲染 zh-CN, en, ja         (3)
zh-TW 完成 → 重渲染 zh-CN, en, ja, de     (4)
                          合计 10 次兄弟页渲染 + 5 次自身 = 15 次
```

单篇 5 语言 = 15 次而非 5 次；**批量翻译 100 篇 = 1500 次而非 500 次**，在 1C1G 上是几十分钟的差距。

**正确做法**：

```
翻译完成 → 「该语言自身的页面」立即入队（priority 10，用户要马上看到）
        → 「兄弟语言的 hreflang 更新」入队时 run_after = now+30s
           新任务到来时把 run_after 刷新为 now+30s（滑动窗口）
           dedupe_key 保证同一页面只有一个待处理任务
```

`jobs` 表已有 `dedupe_key` + `run_after`，无需新增字段。上例从 15 次降到 9 次，批量场景下所有翻译跑完后统一刷一遍，收益更大。

#### ⚠️ 分页的正确算法（原"重渲染前 3 页"的写法是错的）

原规格写的"MVP 简化：重渲染首页前 3 页 + 页数变化时的最后一页"**不成立**。

考虑：第 1 页删掉一篇文章 → 第 2 页的第一篇会前移到第 1 页末尾 → 第 3 页的第一篇前移到第 2 页末尾 → **每一页的内容都变了**。只渲染前 3 页会让第 4 页往后长期错乱，而且**不报错、不告警**，用户可能几个月都发现不了。

正确算法：**从该文章所在的页码开始，重渲染到最后一页。**

```go
// 列表 L 已按 pinned desc, date desc 排好序（内存索引维护）
func affectedPages(list []ArticleID, id ArticleID, perPage int,
                   before, after *ArticleSnapshot) (from, to int) {
    // 取变更前后位置的较小者——新增/删除/位置移动都要覆盖
    posBefore := indexOf(listBefore, id)   // -1 表示原先不在列表中
    posAfter  := indexOf(listAfter, id)    // -1 表示现在不在列表中（草稿/删除）

    minPos := minNonNegative(posBefore, posAfter)
    if minPos < 0 { return 0, 0 }          // 前后都不在列表，无影响

    from = minPos/perPage + 1
    to   = maxPages(len(listBefore), len(listAfter), perPage)   // 取两者较大
    return from, to
}
```

要点：
- `to` 取**变更前后页数的较大值**——文章数减少导致末页消失时，要能删掉多余的 `page/N/` 输出
- 置顶（`pinned`）变更会让文章跳到列表首位，`minPos` 自然覆盖
- 纯内容更新（位置不变）时 `from == to == 该文章所在页`，只渲染 1 页，代价很低
- 只有"新增/删除/置顶/改日期"这类**改变排序位置**的操作才会触发大范围重渲染，这是必要代价

**成本评估**：1000 篇 / perPage=10 = 100 页。删除第 1 页的一篇文章 = 重渲染 100 页。看起来多，但列表页渲染很轻（无 Markdown 解析），且这类操作低频。**正确性优先于此处的性能**——真需要优化时，第二阶段可以给列表页做"数据驱动的客户端分页"，但那是另一个设计。

**验证**：这条规则的正确性由 `blog-server verify`（[docs/14 §4](14-cli-and-lifecycle.md)）兜底。修改 `AffectedUnits` 后必须跑 verify。

#### 旧值处理

**旧分类/标签的处理**：更新文章时若移除了分类 `x`，必须把 `x` 的分类页也加入受影响集合。因此 `Change` 必须携带**变更前的快照**：

```go
type Change struct {
    Op        ChangeOp     // publish|update|unpublish|delete|restore
    ArticleID model.ArticleID
    Type      model.ContentType
    Locales   []model.Locale
    Before    *ArticleSnapshot   // nil 表示新建
    After     *ArticleSnapshot   // nil 表示删除
}
type ArticleSnapshot struct {
    Slug string; Status model.Status; Categories, Tags []string
    Date time.Time; Locales []model.Locale; Pinned bool
}
```

#### 分类变更

```
created            → category_list:*, 若有 parent 则父分类页（子分类列表变了）
updated (仅名称)    → category:*:<id>:*, category_list:*, 所有引用该分类的文章页（面包屑/标签显示）
updated (slug 变)  → 上述 + 删除旧路径的输出目录 + 所有引用文章页（链接变了）
updated (parent 变)→ 全量重建（分类继承关系改变，影响面难以精确计算）
deleted            → 删除输出 + category_list:* + 所有曾引用的文章页
```

#### 标签变更
类似分类，但无继承，影响面较小。

#### 友链 / 菜单变更
```
links:*  +  （菜单出现在所有页面的 header/footer）→ 全量重建
```
**菜单变更 = 全量重建**。这是合理的（菜单不常改），不要试图优化。

#### 站点设置 / 主题设置 / 主题切换 / 主题代码变更
```
→ 全量重建
```

#### UI 文案 `data/translations/*.yaml` 变更
```
→ 该 locale 的全量重建
```

### 2.2 全量重建

```go
func (o *Orchestrator) AllUnits() []RenderUnit
```

遍历所有 locale × 所有实体，生成完整单元列表。典型规模：1000 篇 × 4 语言 + 列表页 ≈ 4500 个单元。

**全量重建走 Release 模式**（见 §5.2），增量走就地原子写。

---

## 3. Go ↔ Node 渲染协议

### 3.1 传输

Unix Domain Socket 上的 HTTP/1.1（`keep-alive`）。Go 侧：

```go
tr := &http.Transport{
    DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
        return (&net.Dialer{}).DialContext(ctx, "unix", cfg.WorkerSocket)
    },
    MaxIdleConnsPerHost: 4,
}
client := &http.Client{Transport: tr, Timeout: cfg.Timeout}
// 请求 URL 用占位 host：http://render/render
```

**为什么不用 stdio JSON-RPC**：HTTP 天然支持并发、超时、backpressure、健康检查，且 Node 侧用 `node:http` 零依赖。

### 3.2 端点

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/health` | 返回 `{"ok":true,"theme":"default","uptime":123}` |
| POST | `/render` | 渲染一个单元，返回 HTML |
| POST | `/render/batch` | 批量渲染（同一模板多个单元，复用 highlighter） |
| POST | `/markdown` | 只做 Markdown → HTML（供后台实时预览用） |
| POST | `/reload` | 清空模块缓存重载主题 SSR bundle（dev / 主题切换） |

### 3.3 `POST /render` 请求体

```jsonc
{
  "unit": {
    "key": "post:zh-CN:019fd210-...",
    "kind": "post",
    "locale": "zh-CN",
    "outputPath": "zh-cn/posts/my-server/index.html"
  },
  "theme": {
    "name": "default",
    "dir": "/app/themes/default",
    "settings": { "showAvatar": true, "accentColor": "#3b82f6" },
    "manifest": {
      "client": ["/assets/client-a1b2c3.js"],
      "css": ["/assets/theme-d4e5f6.css"]
    }
  },
  "site": {
    "title": "My Blog",
    "description": "...",
    "baseURL": "https://example.com",
    "logo": "/media/logo.png",
    "copyright": "© 2026",
    "locale": "zh-CN",
    "urlPrefix": "zh-cn",
    "locales": [
      { "code":"zh-CN","name":"简体中文","urlPrefix":"zh-cn","url":"/zh-cn/posts/my-server/" },
      { "code":"en","name":"English","urlPrefix":"en","url":"/en/posts/my-server/" }
    ],
    "menus": { "header": [...], "footer": [...] },
    "i18n": { "readMore": "阅读全文", "publishedAt": "发布于" },
    "comments": { "provider": "giscus", "options": {...} },
    "search": { "enabled": true, "indexURL": "/zh-cn/search-index.json" }
  },
  "markdown": {
    "shiki": { "themes": {"light":"github-light","dark":"github-dark"},
               "langs": ["go","bash"], "lineNumbers": false, "copyButton": true },
    "katex": true, "mermaid": true,
    "externalLinksNewTab": true, "headingAnchors": true,
    "tocMinDepth": 2, "tocMaxDepth": 3,
    "assetBaseURL": "/media/_bundles/019fd210-.../",
    "mediaDimensions": {
      "/media/2026/08/cover.png": { "w": 2560, "h": 1440 },
      "/media/_bundles/019fd210-.../diagram.png": { "w": 800, "h": 600 }
    }
  },
  "props": { /* 见 §3.5 PageProps */ },
  "seo": {
    "title": "我的服务器搭建记录 | My Blog",
    "description": "...",
    "canonical": "https://example.com/zh-cn/posts/my-server/",
    "ogImage": "https://example.com/media/2026/08/cover.png",
    "type": "article",
    "alternates": [
      { "hreflang": "zh-CN", "href": "https://example.com/zh-cn/posts/my-server/" },
      { "hreflang": "en",    "href": "https://example.com/en/posts/my-server/" },
      { "hreflang": "x-default", "href": "https://example.com/zh-cn/posts/my-server/" }
    ],
    "jsonLD": { "@context":"https://schema.org", "@type":"BlogPosting", "...": "..." },
    "noindex": false
  }
}
```

### 3.4 响应体

```jsonc
{
  "ok": true,
  "html": "<!doctype html><html lang=\"zh-CN\">...</html>",
  "meta": {
    "plainText": "正文纯文本，供搜索索引…",
    "excerpt": "自动摘要…",
    "toc": [ {"depth":2,"id":"docker","text":"Docker","children":[]} ],
    "wordCount": 1234,
    "readingMinutes": 5,
    "islands": ["ThemeToggle","CopyCode","Mermaid"],
    "usedLangs": ["go","bash"]
  },
  "warnings": ["unknown language 'foo' in code block at line 42"],
  "durationMs": 38
}
```

失败时：`{"ok":false,"error":"...","stack":"..."}` + HTTP 500。

**关键设计**：Node 返回 `meta.plainText` / `toc` / `excerpt` / `wordCount`，Go **不自己解析 Markdown 生成这些**。这样 Markdown 语义只有一处实现，永不分歧。

### 3.5 PageProps（各 Kind 的 props 形状）

```ts
// frontend/shared/src/types.ts —— Go 侧用等价 struct，两边手工保持同步

interface PostProps {
  article: {
    id: string; title: string; slug: string; description: string;
    date: string; updated?: string; author: Author;
    cover?: string; pinned: boolean; toc: boolean; comments: boolean;
    locale: string; sourceLocale: string;
    url: string;                       // "/zh-cn/posts/my-server/"
    markdown: string;                  // ★ 原始正文，Node 负责转 HTML
    categories: CategoryRef[];         // 含祖先链，用于面包屑
    tags: TagRef[];
    translationStatus: string;         // "original" | "completed" | ...
  };
  prev?: PostSummary; next?: PostSummary;
  related?: PostSummary[];             // ⚠️ 见下方说明
}

interface HomeProps {
  posts: PostSummary[];                // 当前页的文章
  pagination: Pagination;
  pinned: PostSummary[];               // 仅第 1 页
}

interface CategoryProps { category: CategoryDetail; children: CategoryRef[];
  posts: PostSummary[]; pagination: Pagination; breadcrumb: CategoryRef[]; }

interface TagProps { tag: TagDetail; posts: PostSummary[]; pagination: Pagination; }

interface ArchiveProps {
  scope: "all" | "year" | "month";
  year?: number; month?: number;
  years: { year: number; count: number;
           months: { month: number; count: number }[] }[];
  posts: PostSummary[];                // scope=year/month 时为该范围文章；scope=all 时为全部（按年月分组）
}

interface LinksProps { groups: { id:string; name:string; links: LinkItem[] }[]; }

interface PageProps_ { page: { /* 同 article，但 template/order */ } }

interface SearchProps { /* 空，纯壳，索引由 island 拉取 */ }

interface PostSummary {
  id: string; title: string; slug: string; url: string;
  description: string; date: string; updated?: string;
  cover?: string; author: Author;
  categories: CategoryRef[]; tags: TagRef[];
  readingMinutes: number; pinned: boolean;
}

interface Pagination {
  page: number; perPage: number; total: number; totalPages: number;
  prevURL?: string; nextURL?: string;
  pages: { n: number; url: string; current: boolean }[];  // 已计算好的页码窗口
}
```

**摘要 (`description`) 的来源**：优先 Front Matter 的 `description`；为空时由 Go 从 Markdown 粗略截断（去掉标题/代码块后取前 N 字）。列表页不做完整 Markdown 渲染。

#### ⚠️ `related` 的一致性限制（必须写进主题文档）

「相关文章」是 **N:N 依赖**：A 的相关列表里有 B，B 改了标题或被删除，A 的页面就陈旧了。精确计算这个影响面要求维护反向引用图，成本远高于收益。

**明确的取舍**：

| 场景 | related 是否准确 |
|---|---|
| 全量重建后 | ✅ 准确 |
| 该文章自身被重渲染时 | ✅ 准确 |
| 被引用的文章改了标题/封面 | ❌ **可能陈旧**，直到下次该页面因其他原因重渲染 |
| 被引用的文章被删除 | ⚠️ 会留下死链 |

**处理**：
1. `related` 中的每一项**只使用极稳定的字段**（title、url、cover），主题不得依赖其 date/tags 等易变字段。
2. 死链防护：渲染 `related` 时校验目标文章仍存在且已发布，不存在则跳过该项（列表可能短于 5 项，主题必须容忍）。
3. 定期全量重建（推荐每周一次，配合 `verify`）修正陈旧。
4. **主题文档必须写明这个限制**，让主题作者知道 related 是"尽力而为"而非强一致。

若某个主题不能接受这个限制，正确做法是把相关文章做成客户端 island（运行时从 `search-index.json` 现算），而不是要求渲染系统提供强一致的 N:N 依赖追踪。

---

## 4. Node 渲染器实现

### 4.1 目录与依赖

```
frontend/renderer/
├── package.json
└── src/
    ├── server.ts        # node:http on unix socket
    ├── markdown.ts      # unified 管线（含缓存的 Shiki highlighter）
    ├── render.ts        # React SSR + islands 两遍渲染
    ├── html.ts          # 文档外壳组装 + minify
    ├── themeLoader.ts   # 动态 import 主题 SSR bundle，支持 reload
    └── types.ts         # 从 ../shared 复制/软链
```

依赖（**版本必须钉死**，见 [docs/15 §0](15-implementation-handoff.md)）：
```
react@^19            react-dom@^19
unified@^11          remark-parse@^11    remark-gfm@^4      remark-math@^6
remark-rehype@^11    rehype-raw@^7       rehype-slug@^6
rehype-autolink-headings@^7             rehype-katex@^7
rehype-external-links@^3                rehype-sanitize@^6   ← 评论渲染与可选的正文清洗
rehype-stringify@^10
hast-util-to-text@^4  unist-util-visit@^5
shiki@^3             @shikijs/rehype@^3  @shikijs/transformers@^3
katex@^0.16
```

`rehype-external-links` 与 `unist-util-visit` 在早先版本的管线代码里被使用但漏列了依赖；`rehype-sanitize` 是评论渲染（[docs/16 §2.3](16-social-features.md)）的必需项。

### 4.2 Markdown 管线

```ts
// markdown.ts
let highlighter: Highlighter | null = null;

async function getHighlighter(cfg: ShikiConfig) {
  if (!highlighter) {
    highlighter = await createHighlighter({
      themes: [cfg.themes.light, cfg.themes.dark],
      langs: cfg.langs,
    });
  }
  return highlighter;
}

export async function renderMarkdown(md: string, cfg: MarkdownConfig) {
  const file = await unified()
    .use(remarkParse)
    .use(remarkGfm)                                    // 表格/删除线/任务列表/脚注/自动链接
    .use(cfg.katex ? remarkMath : noop)
    .use(remarkMermaidToIsland, { enabled: cfg.mermaid })   // 自定义：见 §4.3
    .use(remarkRewriteAssets, { base: cfg.assetBaseURL })   // 自定义：相对图片路径重写
    .use(remarkRehype, { allowDangerousHtml: true })
    .use(rehypeRaw)                                    // 允许正文内嵌 HTML
    .use(rehypeSlug)
    .use(cfg.headingAnchors ? rehypeAutolinkHeadings : noop,
         { behavior: 'append', properties: { class: 'heading-anchor', ariaHidden: true } })
    .use(cfg.katex ? rehypeKatex : noop, { throwOnError: false, strict: false })
    .use(rehypeShiki, {
      themes: cfg.shiki.themes,           // 双主题 → CSS 变量，配合 .dark 选择器
      defaultColor: false,
      transformers: [
        transformerNotationHighlight(),   // // [!code highlight]
        transformerNotationDiff(),        // // [!code ++] / [!code --]
        transformerNotationFocus(),
        transformerMetaHighlight(),       // ```go {2,4-6}
        transformerAddLangLabel(),        // 自定义，见下
      ],
      fallbackLanguage: 'text',
      onError: (e) => warnings.push(String(e)),
    })
    .use(rehypeExternalLinks, { target: '_blank', rel: ['noopener','noreferrer'] })
    .use(rehypeStringify, { allowDangerousHtml: true })
    .process(md);

  return {
    html: String(file),
    toc: extractTOC(file, cfg.tocMinDepth, cfg.tocMaxDepth),
    plainText: toText(file),      // hast-util-to-text，排除 pre/code 中的部分
    wordCount: countWords(plainText),
  };
}
```

#### ⚠️ 勘误：复制按钮与行号不是 Shiki transformer

本文档早先版本写了 `transformerCopyButton()` 与 `transformerLineNumbers()`——**这两个 API 在 `@shikijs/transformers` 中不存在**，是我凭印象编造的。实现者若照抄会直接编译失败。

`@shikijs/transformers` 实际导出的全部内容：

```
transformerNotationDiff          transformerNotationHighlight
transformerNotationWordHighlight transformerNotationFocus
transformerNotationErrorLevel    transformerMetaHighlight
transformerMetaWordHighlight     transformerRenderWhitespace
transformerCompactLineOptions    transformerRemoveLineBreak
transformerRemoveNotationEscape  transformerStyleToClass
```

正确做法：

| 功能 | 实现方式 |
|---|---|
| **复制按钮** | **客户端 island**（`CopyCode`，已在 island 清单中）。扫描 `pre.shiki` 注入按钮，用 `navigator.clipboard.writeText`。零 SSR 成本，且 `markdown.shiki.copyButton` 只作为主题开关传给 island |
| **行号** | **纯 CSS**，无需 JS 也无需 transformer：<br>`.shiki code { counter-reset: line }`<br>`.shiki .line::before { counter-increment: line; content: counter(line); ... }`<br>由 `markdown.shiki.lineNumbers` 决定是否给 `<pre>` 加 `.with-line-numbers` 类 |
| **语言标签** | 自定义 transformer（十几行）：从 `this.options.lang` 读语言名写到 `pre` 的 `data-lang` 属性，CSS 用 `::after` 显示 |

`transformerAddLangLabel` 是需要自己写的那一个：

```ts
function transformerAddLangLabel(): ShikiTransformer {
  return {
    name: 'add-lang-label',
    pre(node) { node.properties['data-lang'] = this.options.lang || 'text'; },
  };
}
```

#### 图片尺寸注入（防 CLS）

Markdown 的 `![](url)` 不带尺寸，而缺少 `width`/`height` 会导致累积布局偏移（CLS），直接拉低 Lighthouse 分数——docs/09 §6 要求「宽高属性防抖动」，但原先的 RenderRequest **根本没传这个数据**，无法实现。

**修正**：Go 在组装 RenderRequest 前，扫描该文档引用的所有站内图片路径，从媒体 sidecar（`media/.meta/<path>.json`）批量查出尺寸，放进 `markdown.mediaDimensions`。

```ts
// rehype 插件：为站内图片补 width/height
function rehypeImageDimensions(dims: Record<string, {w:number,h:number}>) {
  return (tree) => visit(tree, 'element', (node) => {
    if (node.tagName !== 'img') return;
    const d = dims[node.properties.src];
    if (d) { node.properties.width = d.w; node.properties.height = d.h; }
    node.properties.loading ??= 'lazy';
    node.properties.decoding ??= 'async';
  });
}
```

要点：
- 只处理**站内**路径（`/media/` 开头），外链图片拿不到尺寸，跳过
- sidecar 缺失时跳过该图（不阻塞渲染），并记录一条 warning
- 主题 CSS 必须配 `img { max-width: 100%; height: auto; }`，否则显式宽高会破坏响应式

**双主题输出**：Shiki 的 `themes: {light, dark}` + `defaultColor: false` 会输出 `style="--shiki-light:#...;--shiki-dark:#..."`。主题 CSS 里：

```css
.shiki span { color: var(--shiki-light); }
html.dark .shiki span { color: var(--shiki-dark); }
```

零运行时 JS，切换主题即时生效。

### 4.3 Mermaid 处理

Mermaid 需要 DOM，服务端渲染需 jsdom/puppeteer，成本不可接受。方案：编译成 island 容器。

````ts
// remarkMermaidToIsland: 把
//   ```mermaid
//   graph TD
//   A --> B
//   ```
// 转成 hast:
{
  type: 'element', tagName: 'div',
  properties: {
    className: ['mermaid-island'],
    dataIsland: 'Mermaid',
    dataProps: JSON.stringify({ code: '...' })   // 注意 HTML 转义
  },
  children: [
    // SSR 占位：显示原始代码（无 JS 时也能看到内容，渐进增强）
    { type:'element', tagName:'pre', properties:{className:['mermaid-source']},
      children:[{type:'text', value: code}] }
  ]
}
````

客户端 island 懒加载 `mermaid` ESM chunk，渲染成 SVG 后替换 `<pre>`。**只有包含 mermaid 的页面才会加载这个 chunk**（island runtime 按 `data-island` 值动态 import）。

### 4.4 React SSR + Islands 两遍渲染

**问题**：整页 `renderToStaticMarkup` 产出的标记无 hydration 元数据，`hydrateRoot` 会警告；整页 `renderToString` + 整页 hydrate 违背"尽量减少 Hydration"的要求。

**方案：两遍渲染**（Astro 同款思路）。

```tsx
// render.ts
export async function renderPage(req: RenderRequest): Promise<RenderResult> {
  const templates = await themeLoader.load(req.theme);
  const Template = templates[req.unit.kind];
  if (!Template) throw new Error(`theme has no template for kind=${req.unit.kind}`);

  // ── 第 1 遍：渲染外壳，island 只产出占位 token
  const collector: IslandRecord[] = [];
  const shell = renderToStaticMarkup(
    <RenderContext.Provider value={{ collector, mode: 'collect', ...ctx }}>
      <Template {...props} />
    </RenderContext.Provider>
  );

  // ── 第 2 遍：逐个 island 独立 renderToString
  let body = shell;
  for (const rec of collector) {
    const inner = renderToString(<rec.Component {...rec.props} />);
    body = body.replace(rec.token, inner);
  }

  const html = assembleDocument({ body, head: buildHead(req), req, islands: collector });
  return { ok: true, html, meta: {...} };
}
```

`<Island>` 组件在 collect 模式下的行为：

```tsx
export function Island({ name, props, children, hydrate = 'load' }: IslandProps) {
  const ctx = useContext(RenderContext);
  const token = `<!--island:${ctx.collector.length}-->`;
  ctx.collector.push({ name, props, Component: islandRegistry[name], token });
  return (
    <div
      data-island={name}
      data-island-hydrate={hydrate}          // load | idle | visible | media(...)
      data-island-props={JSON.stringify(props)}
      dangerouslySetInnerHTML={{ __html: token }}
    />
  );
}
```

`renderToStaticMarkup` 会原样输出 `<!--island:0-->`（因为走 `dangerouslySetInnerHTML`），第 2 遍字符串替换即可。

**为什么 `renderToString` 而不是 `renderToStaticMarkup` 渲染 island 内部**：`hydrateRoot` 需要 React 的 hydration 标记（如 `<!--$-->` for Suspense、文本节点分隔）才能无警告接管。island 内部用 `renderToString`，容器由 `hydrateRoot(container, <Component {...props}/>)` 接管，标记匹配。

### 4.5 客户端 Island 运行时

```ts
// themes/default/src/entry.client.ts
const registry: Record<string, () => Promise<{ default: ComponentType<any> }>> = {
  ThemeToggle:    () => import('./islands/ThemeToggle'),
  LocaleSwitcher: () => import('./islands/LocaleSwitcher'),
  Search:         () => import('./islands/Search'),
  CopyCode:       () => import('./islands/CopyCode'),
  Lightbox:       () => import('./islands/Lightbox'),
  Mermaid:        () => import('./islands/Mermaid'),
  Comments:       () => import('./islands/Comments'),
  TocScrollSpy:   () => import('./islands/TocScrollSpy'),
  BackToTop:      () => import('./islands/BackToTop'),
};

function hydrateIsland(el: HTMLElement) {
  const name = el.dataset.island!;
  const props = JSON.parse(el.dataset.islandProps || '{}');
  registry[name]?.().then(({ default: C }) => {
    hydrateRoot(el, createElement(C, props));
  });
}

for (const el of document.querySelectorAll<HTMLElement>('[data-island]')) {
  const strategy = el.dataset.islandHydrate ?? 'load';
  if (strategy === 'load') hydrateIsland(el);
  else if (strategy === 'idle') requestIdleCallback(() => hydrateIsland(el));
  else if (strategy === 'visible') {
    new IntersectionObserver((es, ob) => {
      if (es[0].isIntersecting) { ob.disconnect(); hydrateIsland(el); }
    }, { rootMargin: '200px' }).observe(el);
  }
}
```

**JS 预算**：入口 runtime 必须 < 3KB gzip。所有 island 走动态 import，按需加载。

**主题切换的 FOUC 防护**：`assembleDocument` 在 `<head>` 内联一段同步脚本（< 300B）读取 `localStorage.theme` / `prefers-color-scheme` 并给 `<html>` 加 `class="dark"`。这是唯一允许的阻塞脚本。

### 4.6 文档外壳

```ts
function assembleDocument({ body, req, islands }): string {
  const { seo, site, theme } = req;
  return `<!doctype html>
<html lang="${site.locale}"${/* dir for rtl */''}>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>${esc(seo.title)}</title>
<meta name="description" content="${esc(seo.description)}">
<link rel="canonical" href="${seo.canonical}">
${seo.noindex ? '<meta name="robots" content="noindex,nofollow">' : ''}
${seo.alternates.map(a => `<link rel="alternate" hreflang="${a.hreflang}" href="${a.href}">`).join('\n')}
<meta property="og:type" content="${seo.type}">
<meta property="og:title" content="${esc(seo.title)}">
<meta property="og:description" content="${esc(seo.description)}">
<meta property="og:url" content="${seo.canonical}">
<meta property="og:site_name" content="${esc(site.title)}">
<meta property="og:locale" content="${site.locale.replace('-','_')}">
${seo.ogImage ? `<meta property="og:image" content="${seo.ogImage}">` : ''}
<meta name="twitter:card" content="${seo.ogImage ? 'summary_large_image' : 'summary'}">
<link rel="alternate" type="application/rss+xml" href="/${site.urlPrefix}/rss.xml" title="${esc(site.title)}">
${theme.manifest.css.map(h => `<link rel="stylesheet" href="${h}">`).join('\n')}
${req.markdown.katex ? '<link rel="stylesheet" href="/assets/katex.min.css">' : ''}
<script>${THEME_INIT_SCRIPT}</script>
<script type="application/ld+json">${JSON.stringify(seo.jsonLD)}</script>
</head>
<body>
${body}
${islands.length ? theme.manifest.client.map(h => `<script type="module" src="${h}"></script>`).join('\n') : ''}
</body>
</html>`;
}
```

**注意**：`islands.length === 0` 时**不注入任何 JS**。纯文章页若无 island 就是零 JS 页面。

### 4.7 HTML 压缩

`config.render.minifyHTML=true` 时做保守压缩（自实现，不引入 html-minifier 这类重依赖）：
- 折叠标签之间的纯空白（`>\s+<` → `><`），但**跳过** `<pre>`、`<code>`、`<textarea>`、`<script>`、`<style>` 内部
- 移除 HTML 注释，但保留 `<!--$-->`/`<!--/$-->` 等 React hydration 标记与条件注释

实现用一个简单的状态机扫描器。**若不确定能正确处理，就把 minify 默认关掉**——正确性优先于几个 KB。

### 4.8 KaTeX 字体

`katex.min.css` 与字体文件在构建时从 `node_modules/katex/dist` 复制到 `generated/public/assets/`。字体用 `font-display: swap`。

---

## 5. Go 侧写盘与发布

### 5.1 增量：就地原子写

```go
func (o *Orchestrator) writeUnit(u RenderUnit, html []byte) error {
    abs, err := fsutil.SafeJoin(o.publicDir, u.OutputPath)
    if err != nil { return err }
    return fsutil.AtomicWrite(abs, html, 0o644)
}
```

`o.publicDir` 解析 symlink 后的真实 release 目录。

### 5.1.1 Release 指纹与自动重建（[docs/13 P26](13-first-run-walkthrough.md)）

每次全量重建时在 `generated/releases/<ts>/.release.json` 写入指纹：

```json
{
  "builtAt": "2026-08-09T12:00:00+08:00",
  "baseURL": "https://blog.example.com",
  "theme": "default",
  "themeSettingsHash": "sha256:…",
  "locales": ["zh-CN","en","ja","de","zh-TW"],
  "appVersion": "1.0.0"
}
```

**启动时比对当前配置与当前 release 的指纹，任一项不一致则自动触发全量重建**，并在日志与后台说明原因。

没有这个机制的后果：用户把 `baseURL` 从 `http://IP:8080` 改成 `https://blog.example.com` 并重启后，因为 `generated/public` 存在而不会重建，**全站的 canonical / hreflang / sitemap / RSS / og:url 全部停留在旧地址**，用户直到 Search Console 报错才会发现。

同样适用于：切换主题、修改主题设置、增删语言、升级应用版本。

### 5.2 全量：Release 切换

```
1.  ts := time.Now().Format("20060102-150405")
2.  newDir := generated/releases/<ts>/
3.  渲染全部单元 → 写入 newDir
4.  拷贝静态资产：
      · themes/<active>/dist/client/*  → newDir/assets/
      · katex 字体/css                 → newDir/assets/
      · bundle assets                  → newDir/media/_bundles/<id>/
5.  写 robots.txt / sitemap.xml
6.  校验：newDir 下 HTML 文件数 ≥ 预期 × 0.95，否则中止并保留旧 release
7.  fsutil.AtomicSymlink(newDir, generated/public)
8.  删除超出 keepReleases 的旧 release
9.  发 RenderCompleted 事件 → CachePurger.PurgeAll()
```

**Nginx 配合**：`root /app/generated/public;` + `disable_symlinks off;`（默认即 off）。切换 symlink 后新请求立即走新 release，已打开的 fd 继续读旧文件直到关闭——无中断。

**磁盘占用**：每个 release 约等于站点大小（1000 篇 ≈ 50MB），`keepReleases: 3` → 150MB。可接受。媒体文件**不复制进 release**（Nginx 单独 `location /media/`）。

### 5.3 删除输出

文章删除 / slug 变更时必须清理旧文件：

```go
func (o *Orchestrator) removeOutput(u RenderUnit) error {
    abs, _ := fsutil.SafeJoin(o.publicDir, u.OutputPath)
    os.Remove(abs)
    // pretty URL 模式下删空目录
    os.Remove(filepath.Dir(abs))   // 只在空时成功，正合适
    return nil
}
```

**slug 变更**必须携带旧 slug，否则会留下幽灵页面。`Change.Before.Slug` 就是为此存在。

---

## 6. 渲染队列与调度

```go
type Orchestrator struct {
    ix       *index.Index
    client   *WorkerClient
    queue    *jobs.Queue
    sem      chan struct{}     // 并发限制
    progress *ProgressBroadcaster  // SSE
}

func (o *Orchestrator) Schedule(ctx context.Context, ch Change) (queued int, err error)
func (o *Orchestrator) RebuildAll(ctx context.Context) error
func (o *Orchestrator) renderOne(ctx context.Context, u RenderUnit) error
```

**优先级**：

| 单元 | Priority |
|---|---|
| 刚发布的文章本身 | 10 |
| 同语言首页 / 相邻文章 | 20 |
| 分类 / 标签 / 归档 | 50 |
| RSS / Sitemap / 搜索索引 | 80 |
| 全量重建的单元 | 200 |

**去重**：`jobs` 表的 `UNIQUE INDEX ... WHERE status='pending'` 保证同一 unit key 只有一个待处理任务。5 秒内连按 3 次发布，只渲染一次。

**批处理优化**：`search_index` / `sitemap` / `rss` 这类"聚合型"单元用 250ms 的合并窗口——短时间内多次触发只在窗口结束时执行一次。

**进度广播**：每完成一个单元推 SSE `{type:"render", done:12, total:34, current:"zh-cn/posts/x/"}`。

---

## 7. RSS / Sitemap / 搜索索引

这三者由 **Go 直接生成**（不走 Node），因为是纯数据序列化，无需 React。

### 7.1 RSS `<prefix>/rss.xml`

RSS 2.0 + Atom self link。取该 locale 最近 `seo.rssItemCount` 篇已发布文章。

**`<description>` 内容**：用摘要（纯文本，CDATA 包裹），**不放全文 HTML**——避免为 RSS 单独渲染 Markdown。若用户要全文 RSS，第二阶段再做（届时可让 Node 在渲染文章时把 HTML 缓存到 `cache/html/<id>.<locale>.html` 供 RSS 复用）。

### 7.2 Sitemap

- `sitemap-<prefix>.xml`：该 locale 的所有 post/page/category/tag/archive/links URL，含 `<lastmod>`（取 `updated` 或 `date`）与 `xhtml:link` hreflang 交叉引用。
- `sitemap.xml`：sitemap index，列出各 locale 的 sitemap。
- 单文件 URL 数超过 45000 时自动分片为 `sitemap-en-1.xml` 等。

### 7.3 搜索索引 `<prefix>/search-index.json`

```json
{
  "version": 1,
  "locale": "zh-CN",
  "generatedAt": "2026-08-09T12:00:00+08:00",
  "docs": [
    {
      "i": "019fd210",
      "t": "我的服务器搭建记录",
      "d": "记录我的服务器搭建过程",
      "u": "/zh-cn/posts/my-server/",
      "c": ["Linux","服务器"],
      "g": ["Debian","Docker"],
      "p": "正文纯文本前 2000 字…",
      "dt": "2026-08-09"
    }
  ]
}
```

字段名用单字母压缩体积。`p` 取自 Node 返回的 `meta.plainText`，截断到 `config.search.bodyCharsPerDoc`。

#### plainText 的来源与缓存（★ 已修订，原方案有循环依赖）

原写法是"缓存缺失时只用 title+description 建索引，并投递一个低优先级任务补齐"——但**补齐任务是什么？重新渲染全站？那就是全量重建**。这个循环没闭合。而且 `cache/` 在备份中被排除，恢复备份后搜索会静默退化成只能搜标题。

**修正：plainText 落盘到 `generated/` 而非 `cache/`，与 HTML 同生共死。**

```
渲染文章单元时，Node 返回 meta.plainText
  → Go 写 HTML 的同时，写 <release>/.meta/<locale>/<articleID>.txt
  → 这份文件与 HTML 在同一个 release 内，生命周期一致
```

| 属性 | 说明 |
|---|---|
| 位置 | `generated/releases/<ts>/.meta/<locale>/<id>.txt`（`.` 开头，不被 Nginx 提供） |
| 生命周期 | 与 release 绑定。全量重建产生新 release 时一并重生成 |
| 冷启动 | 从当前 release 的 `.meta/` 直接读取，**无需重新渲染** |
| 备份 | 不需要备份（属于 `generated/`，是派生物） |
| 恢复备份后 | 触发全量重建 → `.meta/` 自然重建 → 搜索完整 |
| 内存 | 索引构建时读一次生成 JSON 后即释放，**不常驻内存** |

这样彻底消除了循环：**搜索索引的输入永远来自当前 release 自己的产物**。若 `.meta/` 缺失（例如手工删了），说明该 release 不完整，正确的响应是触发全量重建，而不是降级建索引。

**体积控制**：超过 `maxIndexSizeMB` 时，按 `bodyCharsPerDoc` 减半重试，仍超则丢弃 `p` 字段并在后台告警，提示改用第二阶段的分片索引。

前端使用：搜索 island 在**首次输入时**才 fetch 索引，`MiniSearch.loadJSON` 或 `addAll`，之后本地搜索。

---

## 8. 缓存刷新接口

```go
type CachePurger interface {
    PurgeURLs(ctx context.Context, urls []string) error
    PurgeAll(ctx context.Context) error
}

type NoopPurger struct{}        // MVP 默认
type CloudflarePurger struct{}  // 第二阶段
```

MVP 的 `NoopPurger` 只记日志。接口现在就定义好，第二阶段填 Cloudflare 实现（`POST /zones/<id>/purge_cache`，每次最多 30 个 URL，需分批）。

---

## 9. 后台实时预览

编辑器的预览面板走 `POST /api/admin/preview`，Go 转发到 Node 的 `/markdown`，只返回正文 HTML 片段（不含外壳），前端注入到预览容器并加载主题 CSS。

**防抖 400ms**，并发请求取消旧的（`AbortController`）。

若渲染器不可用，前端降级为客户端 `marked` 简易预览并显示"高亮/公式不可用"提示。

---

## 10. 验收测试

| # | 测试 | 期望 |
|---|---|---|
| R1 | 发布一篇含代码块/表格/公式/mermaid/脚注的文章 | HTML 中代码有 Shiki span、公式为 KaTeX HTML、mermaid 为 island 容器、脚注锚点正确 |
| R2 | 禁用 JS 访问该文章 | 排版、高亮、公式全部正常；mermaid 显示源码 |
| R3 | 修改文章分类 | 旧分类页与新分类页都被重渲染 |
| R4 | 修改文章 slug | 旧路径 HTML 被删除，新路径生成，站内链接更新 |
| R5 | 连续 5 次快速发布 | 每个单元只渲染一次（检查 render_units.attempts） |
| R6 | 删除 `generated/` 后重启 | 自动全量重建，站点完整恢复 |
| R7 | 全量重建过程中访问站点 | 旧 release 持续可访问，切换瞬间无 404 |
| R8 | 杀死 Node 渲染器 | Go 自动重启；期间发布任务排队；恢复后自动消化 |
| R9 | 全量重建性能（见下方修订） | 单单元 P95 < 200ms；内存峰值 < 400MB |
| R10 | 单篇文章页面 JS 体积 | 无 island 时 0 字节；有 island 时入口 < 3KB gzip |

### 10.1 关于"全量重建 < 5 分钟"（★ 修正一个没有依据的指标）

原验收标准写"1000 篇 × 4 语言全量重建 < 5 分钟"。**这个数字是拍脑袋的，且在单核机器上根本达不到。**

React SSR 是 CPU 密集且 Node 单线程。实测量级：简单文章约 30~50ms/单元，含大量代码块的长文经 Shiki 高亮后可达 200~500ms/单元。4500 个单元在**单核**上：

```
4500 × 150ms（乐观均值）≈ 11 分钟
4500 × 300ms（含长文）  ≈ 22 分钟
```

而目标环境正是 1C1G 的 VPS——**单核无法并行，这是物理限制，不是优化能解决的**。

**修正后的指标**（可验证、与硬件解耦）：

| 指标 | 目标 |
|---|---|
| 单单元渲染耗时 P95 | **< 200ms**（这是真正该优化的东西） |
| 全量重建吞吐 | ≥ 5 单元/秒/核 |
| 内存峰值 | < 400MB（含 Node） |
| 全量重建期间站点可用性 | 100%（旧 release 持续服务） |

按吞吐推算的参考时长写进文档，让用户有预期：

| 规模 | 1 核 | 2 核 | 4 核 |
|---|---|---|---|
| 100 篇 × 3 语言（≈450 单元） | ~1.5 分钟 | ~50 秒 | ~30 秒 |
| 1000 篇 × 4 语言（≈4500 单元） | ~15 分钟 | ~8 分钟 | ~4 分钟 |

**产品层面的应对**（比追求更快更重要）：
1. 全量重建**从不阻塞用户** —— 旧 release 持续服务，用户完全无感。
2. 后台显示真实进度与 ETA（[docs/12 §1](12-reliability-ux.md)）。
3. 日常操作走增量渲染（十几个单元，几秒完成），全量重建只在切主题、改 baseURL、恢复备份时发生 —— **都是低频操作**。
4. `render.workerCount > 1` 在多核机器上 spawn 多个 Node 进程线性提速；单核上保持 1。
