# 剩余工作任务表

> 审阅日期：2026-08-10 · 基于当前工作区实际代码
> 执行者：接手的编码模型 · 规格依据：`PLAN.md` + `docs/01`~`docs/16`
>
> ⚠️ **动工前必须先读 [docs/17-spec-vs-reality.md](docs/17-spec-vs-reality.md)**。
> 前 16 份规格描述的是设计意图，渲染层与前端的实现已经偏离。
> `docs/17` 记录了**真实契约**（只有 5 种 kind、响应只有 `{html}`）与每处偏差的处置决定。
> 不读它就动工，会去实现规格里写着但代码中不存在的接口。

---

## 一、当前状态快照

### ✅ 已完成且质量达标

| 模块 | 状态 | 证据 |
|---|---|---|
| Go 后端 | **24 个包，构建通过，测试全绿** | `cd backend && go build ./... && go test ./...` |
| 代码量 | ~14,700 行（含测试） | 26 个包，每包都有 `_test.go` |
| CLI | `doctor` `verify` `rebuild` `migrate` `admin *` `backup *` `import` `export` `config *` `index *` 均已实现 | `backend/cmd/blog/main.go` (1198 行) |
| REST API | 覆盖 auth / dashboard / posts / media / 分类法 / themes / settings / users / backup / import / translations / system | `backend/internal/httpserver/server.go` (2592 行) |
| Markdown 管线 | Shiki 双主题 + KaTeX + GFM + 标题锚点 + 外链处理 | `frontend/renderer/server.js` |
| 静态发布 | release 目录 + 原子切换 + sitemap + rss + robots 已产出 | `generated/releases/*/` |

**结论：后端可以认为是可用的基线，不要推倒重做。**

### ❌ 阻断性问题（必须先解决）

| # | 问题 | 影响 |
|---|---|---|
| **B1** | **代码被压成超长单行**：`main.tsx` 最长行 **5653 字符**、`server.js` **5040 字符**、`server.go` **393 字符** | 无法审查、无法安全修改；模型每改一次都可能整行写坏。**这是当前最大的技术债** |
| **B2** | **主题没有源码**：`themes/default/` 只有 13 行的 `dist/ssr/entry.js`，没有 `src/`、没有 React、没有 vite | 违反 `docs/09`；主题不可维护、不可扩展；11 个声明的模板只有 4 个分支 |
| **B3** | **Islands 是死的**：renderer 输出了 `data-island="Mermaid"` 容器，但**没有任何客户端 JS 去 hydrate 它** | Mermaid 图表永远不会渲染，只显示源码。整套 hydration 架构缺失 |
| **B4** | **渲染器内联了整套主题**：CSS 和 HTML 模板全写在 `server.js` 里 | 与主题系统职责重叠；主题只能覆盖 `<body>` 内容，改不了 `<head>` 和样式 |

### ⚠️ 功能缺口

| # | 缺口 | 规格位置 |
|---|---|---|
| G1 | 搜索是 `.includes()` 子串匹配，不是真索引 | `docs/04 §7.3` 要求 MiniSearch |
| G2 | renderer 不返回 `toc` / `plainText` / `wordCount` | `docs/04 §3.4` |
| G3 | 无 `rehype-sanitize` | `docs/10 §3.1` |
| G4 | 图片无 `width`/`height`（CLS 问题） | `docs/04 §4.2` |
| G5 | 无确定性渲染测试 | `docs/14 §4.3`，`verify` 的前提 |
| G6 | Admin 无路由（TanStack Router 已装未用） | `docs/08 §2` |
| G7 | Admin 缺页面：分类 / 标签 / 菜单 / 友链 / 独立页面 / 多语言矩阵 / 回收站 | `docs/08 §5` |
| G8 | 无 CI | `docs/15 §4.1` |

---

## 二、强制代码规范（**先读这一条，否则后面全部作废**）

当前代码把整个 React 组件、整个渲染函数压在一行里。**继续这样写，项目会在两周内变成没人能改的死代码。**

### 硬性规则

1. **单行不得超过 120 字符。** 无例外。
2. **一个文件只做一件事。** `main.tsx` 现在有 16 个组件挤在 63 行里 —— 必须拆成一个组件一个文件。
3. **禁止用 `;` 把多条语句写在同一行。**
4. **禁止把 JSX 压成单行。** 超过 2 个属性就换行。
5. **每个导出的函数/组件上方要有一句注释说明它做什么**（不是说明怎么做）。

### 落地方式（T0 任务）

```bash
# 前端：Prettier + ESLint
cd frontend/admin && pnpm add -D prettier eslint @typescript-eslint/parser \
  @typescript-eslint/eslint-plugin eslint-plugin-react-hooks
```

`.prettierrc`：
```json
{ "printWidth": 100, "semi": false, "singleQuote": true, "trailingComma": "all" }
```

`Makefile` 增加：
```make
fmt:
	cd backend && gofmt -w ./cmd ./internal
	cd frontend/admin && pnpm prettier --write "src/**/*.{ts,tsx,css}"
	cd frontend/renderer && npx prettier --write "src/**/*.{ts,js}"
	cd themes/default && npx prettier --write "src/**/*.{ts,tsx,css}"

lint:
	cd backend && gofmt -l ./cmd ./internal | (! grep .) && go vet ./...
	cd frontend/admin && pnpm eslint src --max-warnings 0
```

**验收**：`make lint` 退出码为 0，且全仓库无超过 120 字符的行：
```bash
find backend frontend themes -type f \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' \) \
  -not -path '*/node_modules/*' -not -path '*/dist/*' -not -name '*.d.ts' \
  | xargs awk 'length > 120 {print FILENAME":"FNR" ("length" 字符)"}' | head -20
```
输出为空才算通过。

---

## 三、任务表

优先级：**P0 阻断** > **P1 核心功能** > **P2 管理后台** > **P3 质量收尾**
每个任务标注：`[依赖]` · `预估人日` · `验收命令`

### P0 · 结构性重构（必须最先做，共 6 人日）

---

#### T0 · 代码规范基线
`[无依赖]` · **0.5 人日**

> ✅ 已完成（2026-08-10）：Prettier/ESLint/gofmt 就绪，全仓超长行拆分完毕，`make lint` 通过，Go 测试全绿。

**做什么**
1. 按上面 §二 装 Prettier + ESLint，写配置文件
2. `gofmt -w` 全部 Go 代码
3. 把 `backend/internal/httpserver/server.go` 中超过 120 字符的行拆开（393 字符那几行主要是 `json.Marshal(map[string]any{...})`，拆成多行字面量）
4. `Makefile` 加 `fmt` / `lint` 目标

**不要做**：这一步**只格式化，不改逻辑**。改完 `go test ./...` 必须仍然全绿。

**验收**
```bash
make lint && cd backend && go test ./...
```

---

#### T1 · 拆分 `frontend/admin/src/main.tsx`
`[T0]` · **1.5 人日**

> ✅ 已完成（2026-08-10）：main.tsx 拆分为 api/client+types、components/Layout+MarkdownEditor+UserContext、12 个 feature 页面与 router.tsx，启用 TanStack Router（URL 与页面一一对应）。构建/ESLint/行长检查通过；交互点击验收待浏览器环境确认。

现状：63 行、16 个组件、最长行 5653 字符。

**目标结构**
```
frontend/admin/src/
├── main.tsx                 # 只负责 createRoot + Router 挂载（< 30 行）
├── router.tsx               # TanStack Router 路由树
├── api/
│   ├── client.ts            # fetch 封装 + CSRF + 401 处理（从现有 api() 提取）
│   └── schema.d.ts          # 已存在，保持
├── components/
│   ├── Layout.tsx           # 侧边栏 + 顶栏
│   ├── MarkdownEditor.tsx   # CodeMirror（从现有提取）
│   └── ui/                  # Button / Input / Card / Table 等原子组件
└── features/
    ├── setup/SetupPage.tsx
    ├── auth/LoginPage.tsx
    ├── dashboard/DashboardPage.tsx
    ├── posts/PostListPage.tsx
    ├── posts/PostEditorPage.tsx
    ├── media/MediaPage.tsx
    ├── settings/SiteSettingsPage.tsx
    ├── settings/ThemeSettingsPage.tsx
    ├── system/SystemActivityPage.tsx
    ├── system/BackupPage.tsx
    ├── users/UserPage.tsx
    └── i18n/TranslationTasksPage.tsx
```

**要求**
- **逐个组件搬移，每搬一个就跑一次 `pnpm build` 确认没坏**
- 行为必须与现在完全一致（这是纯重构，不加功能）
- 搬完后启用 TanStack Router，URL 与页面对应（现在是 useState 切页，刷新就丢状态）

**验收**
```bash
cd frontend/admin && pnpm build && pnpm eslint src --max-warnings 0
# 手动：登录 → 每个页面点一遍 → 刷新浏览器，URL 应保持在当前页
```

---

#### T2 · 主题源码化
`[T0]` · **2 人日** · **最重要的一个任务**

> ✅ 已完成（2026-08-10）：themes/default 源码化（src/ + vite 双构建 + manifest.mjs），
> 实现 5 个可达 kind 的模板（post/page/collection/search/not_found），SSR bundle 自包含 react，
> 渲染器 themeMarkup 改为 createElement 渲染（修复 hooks 崩溃）。验收 grep 全部通过。

现状：`themes/default/` 只有 `dist/ssr/entry.js`（13 行手写字符串拼接）。

**目标结构**
```
themes/default/
├── theme.yaml               # 已存在，保持
├── settings.schema.json     # 已存在，保持
├── package.json             # 新建
├── vite.config.ts           # 新建：双构建 ssr + client
├── src/
│   ├── entry.ssr.tsx        # export const templates = { home, post, page, ... }
│   ├── entry.client.ts      # island 引导（见 T3）
│   ├── lib/
│   │   ├── Island.tsx       # <Island name props hydrate>
│   │   └── ctx.tsx          # useSite / useThemeSettings / useT / useURL
│   ├── templates/
│   │   ├── Home.tsx  Post.tsx  Page.tsx  Collection.tsx
│   │   ├── Search.tsx  NotFound.tsx
│   ├── components/
│   │   ├── Layout.tsx  Header.tsx  Footer.tsx
│   │   ├── PostCard.tsx  Pagination.tsx  LocaleSwitcher.tsx
│   │   ├── TOC.tsx  Prose.tsx
│   ├── islands/             # 见 T3
│   └── styles/
│       ├── main.css         # 从 server.js 里那坨内联 CSS 搬过来并展开
│       └── prose.css        # 正文排版 + Shiki 双主题 + KaTeX
└── dist/                    # 构建产物（gitignore）
    ├── ssr/entry.js
    ├── client/*.js
    ├── assets/*.css
    └── manifest.json
```

**关键约束**
1. **Go 侧实际只发送 5 种 `kind`**（已逐行核实 `render.go`）：

   | kind | 来源 | 覆盖的页面 |
   |---|---|---|
   | `post` | `string(article.Type)` | 文章 |
   | `page` | `string(article.Type)` | 独立页面 |
   | `collection` | `renderCollection()` | **首页 + 分类 + 标签 + 归档 + 友链，全部挤在这一个 kind 里** |
   | `search` | `RenderSearch()` | 搜索页 |
   | `not_found` | `RenderNotFound()` | 404 |

   ⚠️ **`home` 这个 kind 不存在**——`RenderHome()` 只是转调 `RenderCollection()`。
   `theme.yaml` 里声明的 11 个模板中，有 6 个当前拿不到对应的 kind。

   **T2 阶段先按这 5 个实现**，`templates` 的键必须是上面这 5 个。多余的模板文件可以先建但不会被调用。

2. ⚠️ **`collection` 的 payload 没有任何区分字段**。当前只有：
   `kind, title, locale, items, pagination, theme, themeName, themeDir, canonical, alternates`
   ——没有 category 对象、没有 tag 对象、没有归档分组、没有友链分组。
   **主题在 T2 阶段无法区分首页和分类页**，这是 Go 侧的限制，不是主题的问题。解决方案见 T8。

3. **T2 不要改 Go 侧**，先适配现有 props 契约。当前全部字段：
   `kind, title, description, body, locale, author, publishedAt, modifiedAt, theme, themeName, themeDir, markdown, canonical, alternates, items, pagination, searchIndexURL, message, homeLabel, prefix`
4. **renderer 已支持两种主题契约**（`templates[kind]` 组件 或 `render(props)` 函数）—— 用 `templates`。
5. `manifest.json` 格式见 `docs/09 §5.2`。

**`vite.config.ts` 要点**
```ts
// mode=ssr  → dist/ssr/entry.js  (format: esm, external react/react-dom)
// mode=client → dist/client/*.js + dist/assets/*.css (带 hash)
// 构建后跑 scripts/manifest.mjs 归一化产出 dist/manifest.json
```

**验收**
```bash
cd themes/default && npm install && npm run build
ls dist/ssr/entry.js dist/manifest.json dist/client/
cd ../.. && ./blog-server rebuild && \
  grep -q 'class="theme-default"' generated/public/zh-cn/index.html && echo "主题生效"
```

---

#### T3 · Islands 客户端运行时
`[T2]` · **1.5 人日**

> ✅ 已完成（2026-08-10）：8 个 island（Mermaid/CopyCode/ThemeToggle/LocaleSwitcher/Search/
> Lightbox/TocScrollSpy/BackToTop）按 load/idle/visible 策略挂载；引导包 1.5KB gzip；
> FOUC 内联脚本防闪烁；渲染器按页面是否含 data-island 注入 module 脚本；Go 侧发布主题资源。
> 静态验收全部通过；浏览器交互（mermaid SVG/复制按钮/深浅切换）需浏览器环境人工确认。

现状：renderer 输出 `data-island="Mermaid"` 容器，**但没有任何 JS 去 hydrate**，图表永远不显示。

**要实现**

`themes/default/src/lib/Island.tsx`（SSR 侧，两遍渲染，见 `docs/04 §4.4`）：
```tsx
// 第 1 遍：收集 island，输出 <!--island:N--> 占位
// 第 2 遍：对每个 island 单独 renderToString，字符串替换回去
```

`themes/default/src/entry.client.ts`（客户端引导，**必须 < 3KB gzip**）：
```ts
const registry = {
  ThemeToggle:    () => import('../islands/ThemeToggle'),
  LocaleSwitcher: () => import('../islands/LocaleSwitcher'),
  Search:         () => import('../islands/Search'),
  CopyCode:       () => import('../islands/CopyCode'),
  Lightbox:       () => import('../islands/Lightbox'),
  Mermaid:        () => import('../islands/Mermaid'),
  TocScrollSpy:   () => import('../islands/TocScrollSpy'),
  BackToTop:      () => import('../islands/BackToTop'),
}
// 扫描 [data-island]，按 data-island-hydrate 策略 (load|idle|visible) 动态 import + hydrateRoot
```

**8 个 island 的实现要求**

| island | hydrate 策略 | 说明 |
|---|---|---|
| `Mermaid` | **`visible`** | 懒加载 mermaid（~500KB gzip），**绝不能用 `load`**。渲染成 SVG 后替换 `<pre class="mermaid-source">` |
| `CopyCode` | `idle` | 扫描 `pre.shiki` 注入复制按钮，`navigator.clipboard.writeText` |
| `ThemeToggle` | `load` | 切换 `<html class="dark">` + 写 localStorage |
| `LocaleSwitcher` | `load` | 写 `preferred_locale` cookie 后跳转 |
| `Search` | `idle` | 见 T5（MiniSearch） |
| `Lightbox` | `visible` | 点击正文图片放大 |
| `TocScrollSpy` | `visible` | 滚动高亮当前章节 |
| `BackToTop` | `visible` | 回到顶部 |

**FOUC 防护**：`<head>` 内联一段 < 300B 的同步脚本，读 `localStorage.theme` / `prefers-color-scheme` 给 `<html>` 加 `dark` 类。这是**唯一允许的阻塞脚本**。

**验收**
```bash
# 发布一篇含 mermaid + 代码块的文章后：
grep -c 'data-island' generated/public/zh-cn/posts/*/index.html   # > 0
grep -c '<script type="module"' generated/public/zh-cn/posts/*/index.html  # = 1
# 浏览器打开：mermaid 显示为 SVG（不是源码）、代码块有复制按钮、主题切换生效
# 无 island 的页面必须 0 字节 JS：
grep -c '<script' generated/public/zh-cn/404.html   # 应为 0 或仅 FOUC 脚本
```

---

#### T4 · 渲染器瘦身与职责分离
`[T2, T3]` · **1 人日**

> ✅ 已完成（2026-08-10）：frontend/renderer 拆分 src/{server,markdown,render,html,theme}.js；
> 内联 CSS/模板/搜索 JS 全部移除，主题负责样式与页面结构，渲染器保留 Markdown 管线、
> SEO head 组装、主题加载缓存与单个内置兜底页。Go 侧 worker 路径更新，Dockerfile 同步。

现状：`server.js` 152 行，但 `render()` 是一个 5040 字符的单行函数，内含整套 CSS + 6 种页面的 HTML 模板。

**目标结构**
```
frontend/renderer/
├── package.json
└── src/
    ├── server.js       # 只做 HTTP + 路由分发（< 80 行）
    ├── markdown.js     # unified 管线（从现有提取，展开成多行）
    ├── render.js       # 加载主题 + 两遍 island 渲染
    ├── html.js         # 文档外壳组装（head/meta/seo/jsonld）
    └── theme.js        # 主题 SSR bundle 加载与缓存
```

**要删掉的**
- `render()` 里所有内联 HTML 模板（home/collection/search/not_found 分支）→ 全部交给主题
- 那坨内联 `<style>` → 搬到 `themes/default/src/styles/main.css`
- 内联的搜索 JS（`.includes()` 子串匹配）→ 由 Search island 取代

**保留的**
- Markdown 管线（Shiki / KaTeX / GFM / mermaid→island）
- SEO head 组装（canonical / hreflang / og / jsonld）
- 主题加载与 mtime 缓存

**降级**：主题不可用时输出一个极简的内置兜底页（**只保留这一个兜底，不是 6 个模板**）。

**验收**
```bash
awk 'length > 120' frontend/renderer/src/*.js | wc -l   # 必须为 0
node -e "import('./frontend/renderer/src/markdown.js').then(m=>m.renderMarkdown('# hi\n\`\`\`go\nfunc main(){}\n\`\`\`',{})).then(r=>console.log(r.html.includes('shiki')?'OK':'FAIL'))"
```

---

### P1 · 核心功能补齐（共 4 人日）

---

#### T5 · 真正的搜索索引
`[T3]` · **1 人日**

现状：renderer 内联 `.includes()` 子串匹配。

**做什么**
1. Go 侧生成 `<prefix>/search-index.json`，字段用单字母压缩（`docs/04 §7.3`）：
   `{i,t,d,u,c,g,p,dt}` = id/title/description/url/categories/tags/plainText/date
2. `p` 字段来自 renderer 返回的 `meta.plainText`（依赖 T6），截断到 `config.search.bodyCharsPerDoc`（默认 2000）
3. Search island 用 **MiniSearch**，**首次输入时才 fetch 索引**（不在页面加载时拉）
4. 体积护栏：超过 `maxIndexSizeMB`（默认 3MB）时减半重试，仍超则丢弃 `p` 并告警

**验收**
```bash
./blog-server rebuild
python3 -c "import json;d=json.load(open('generated/public/zh-cn/search-index.json'));print(len(d['docs']),'篇')"
# 浏览器：搜索框输入关键词能命中正文内容（不只是标题）
```

---

#### T6 · renderer 返回结构化元数据
`[T4]` · **0.5 人日**

renderer 的 `/render` 响应目前只有 `{html}`，需要补齐（`docs/04 §3.4`）：

```json
{
  "html": "...",
  "meta": {
    "plainText": "正文纯文本",
    "excerpt": "自动摘要",
    "toc": [{"depth":2,"id":"docker","text":"Docker","children":[]}],
    "wordCount": 1234,
    "readingMinutes": 5,
    "islands": ["Mermaid","CopyCode"]
  },
  "warnings": []
}
```

- `plainText` 用 `hast-util-to-text`（**排除 `<pre>` 内容**，避免代码污染搜索）
- `wordCount` 中英文分别统计：CJK 按字符数，拉丁按单词数
- Go 侧 `render.go` 接收后：`plainText` 写入 `<release>/.meta/<locale>/<id>.txt` 供搜索索引复用

**验收**
```bash
cd backend && go test ./internal/render/...
# 检查 .meta 目录已生成
ls generated/releases/*/.meta/zh-cn/ | head
```

---

#### T7 · 安全与质量补丁
`[T4]` · **1 人日**

| 子项 | 做什么 | 依据 |
|---|---|---|
| **rehype-sanitize** | 加入管线，受 `config.markdown.sanitize` 控制（默认 `false`，多作者站点开启）。**评论渲染必须强制开启**（预留 `preset: "comment"`） | `docs/10 §3.1` |
| **图片尺寸注入** | Go 侧扫描文档引用的站内图片，从 `media/.meta/*.json` 查尺寸，通过 `markdown.mediaDimensions` 传给 renderer；rehype 插件补 `width`/`height`/`loading="lazy"`/`decoding="async"` | `docs/04 §4.2` |
| **超大代码块防护** | 单个代码块 > 256KB 时跳过 Shiki 高亮，输出纯 `<pre>` 并记 warning（防正则回溯爆炸） | `docs/12 §2 #10` |
| **确定性渲染测试** | 同一篇文章连续渲染两次，断言输出**逐字节相同**。检查有无 `Date.now()` / 未排序 map | `docs/14 §4.3` |

**验收**
```bash
cd backend && go test ./internal/render/... -run Deterministic -v
./blog-server verify    # 退出码 0
```

---

#### T8 · 拆分 collection + 主题模板补全
`[T2]` · **2.5 人日** · ⚠️ **这是前后端联合任务，不是纯主题工作**

**背景**：首页 / 分类 / 标签 / 归档 / 友链目前全部走同一个 `kind: "collection"`，payload 里没有任何字段能区分。**主题在物理上无法为它们渲染不同布局。**

**第一步：Go 侧拆分 kind（先做，约 1 人日）**

改造 `backend/internal/render/render.go`：

```go
// 现状：RenderHome() → RenderCollection() → kind:"collection"
// 目标：各自发送独立 kind 与专属 props

RenderHome     → kind:"home"      props: + pinned[]
RenderCategory → kind:"category"  props: + category{id,name,slug,description}, breadcrumb[], children[]
RenderTag      → kind:"tag"       props: + tag{id,name,slug,description}
RenderArchive  → kind:"archive"   props: + scope("all"|"year"|"month"), year, month, years[{year,count,months[]}]
RenderLinks    → kind:"links"     props: + groups[{id,name,links[{name,url,logo,description}]}]
```

要求：
- 保留 `RenderCollection` 作为兜底（未知 kind 时降级）
- `render_test.go` 为每个新 kind 加一个测试
- **props 字段名与 `docs/04 §3.5` 的 TS 接口保持一致**

**第二步：主题模板（约 1.5 人日）**

| 模板 | 要求 |
|---|---|
| `Home` | 置顶区 + 文章流 + 分页 |
| `Category` / `Tag` | 面包屑 + 描述 + 子分类 + 文章列表 + 分页 |
| `Archive` | 按年/月分组的时间线 |
| `Links` | 按分组展示友链卡片（头像 + 名称 + 描述） |
| 全部 | 响应式 375px~2560px、深色模式、**禁用 JS 后完全可读** |

**验收**
```bash
cd backend && go test ./internal/render/...
./blog-server rebuild
# 五个页面的 HTML 结构应各不相同：
for p in "" categories/ tags/ archive/ links/; do
  echo "--- /zh-cn/$p"; grep -o 'class="[a-z-]*"' "generated/public/zh-cn/${p}index.html" | head -3
done
```
Lighthouse 文章页：Performance ≥ 90、Accessibility ≥ 95、SEO = 100。

**验收**
- Lighthouse 文章页：Performance ≥ 90、Accessibility ≥ 95、SEO = 100
- 禁用 JS 后：排版、代码高亮、公式、导航全部正常
- 375px 宽度无横向滚动

---

### P2 · 管理后台补全（共 5 人日）

> 全部依赖 T1（拆分完成）。每个页面**必须是独立文件**，禁止再往一个文件里塞。

---

#### T9 · 分类与标签管理
`[T1]` · **1 人日**

- 分类：左树右编辑面板，`@dnd-kit` 拖拽改父级与排序
- 多语言名称/描述按启用语言分 Tab
- 删除时若被引用 → 弹窗要求选择迁移目标（后端 `?migrateTo=` 已支持）
- 标签：列表 + 合并功能（`POST /tags/merge`）

**验收**：拖拽调层级后 `data/categories/*.yaml` 正确更新，前台分类页层级正确。

---

#### T10 · 菜单与友链管理
`[T1]` · **1 人日**

- 菜单：左侧「可添加项」（页面/文章/分类/标签/自定义），右侧拖拽树（最多 2 层）
- 友链：按分组看板，组内排序 + 跨组拖拽
- 均已有后端 API（`/menus` `/links` `/reorder` `/groups`）

---

#### T11 · 独立页面（Pages）管理
`[T1]` · **0.5 人日**

复用文章列表与编辑器组件，额外字段：`template`（从 `theme.yaml` 的 `pageTemplates` 读取）、`order`、`showInMenu`。

---

#### T12 · 多语言矩阵与翻译 UI
`[T1]` · **1.5 人日**

**这是本项目区别于普通 CMS 的核心界面，要做好。**

- **文章编辑页语言标签栏**：`[中文 原文] [English ✓] [日本語 ⚠过期] [+ 添加语言]`
- 编辑派生语言时：权威字段（分类/时间/作者/封面/置顶）**控件禁用 + 锁图标 + tooltip**
- 过期时顶部黄条：`源文章已更新到版本 21，此译文基于版本 17 · [更新翻译]`
- **翻译矩阵页**：行=文章、列=语言、格=状态徽章，支持筛选（过期/缺失/失败）+ 批量触发
- **批量翻译前必须弹成本确认框**（`docs/16 §7` 同款）：显示任务数、预估 token、预计耗时，勾选框确认后才能提交

**验收**：翻译一篇文章，矩阵徽章实时更新；批量选 20 篇时出现成本确认框。

---

#### T13 · 回收站与日志页
`[T1]` · **1 人日**

- 回收站：列表 + 恢复 + 永久删除（后端 `/restore` `/purge` 已有）
- 日志页：系统日志 + 审计日志双 Tab，按 level/component 筛选分页（后端 `/system/logs` `/system/audit` 已有）
- 文章列表补齐：状态 Tab、分类/标签/语言筛选、批量操作条

---

### P3 · 质量收尾（共 2 人日）

---

#### T14 · CI 与类型一致性
`[T0]` · **1 人日**

`.github/workflows/ci.yml`：
```yaml
- gofmt 检查 + go vet + go test ./...
- 行长检查（> 120 字符即失败）
- pnpm build (admin) + npm run build (theme)
- openapi → TS 类型重新生成后 git diff 必须为空
- ./blog-server verify --sample 50
```

**类型一致性**：`backend/api/openapi.yaml` 与 `frontend/admin/src/api/schema.d.ts` 必须由 `make api-types` 生成，CI 检查生成物已提交（`docs/15 §4.1`）。

---

#### T15 · 文档与部署校准
`[全部]` · **1 人日**

1. `README.md` 补：功能进度表、开发环境搭建、主题开发入门
2. `themes/README.md`：主题开发文档（目录约定、props 契约、Island 用法、构建命令）
3. 按 `docs/13` 的 W1~W11 走一遍全新部署验收
4. Dockerfile 增加主题构建阶段（当前只构建了 admin 和 renderer，**主题的 dist 是直接 COPY 的**，源码化后必须加构建步骤）

---

## 四、执行顺序与检查点

```
第 1 天  T0 ──────────────► 格式化基线（不改逻辑）
第 2-3 天 T1 ─────────────► admin 拆分 + 路由
第 4-5 天 T2 ─────────────► 主题源码化      ★ 关键里程碑
第 6 天   T3 ─────────────► islands 运行时  ★ 关键里程碑
第 7 天   T4 ─────────────► 渲染器瘦身
第 8 天   T5 T6 ──────────► 搜索 + 元数据
第 9 天    T7 ─────────────► 安全补丁
第 10-11 天 T8 ────────────► 拆分 collection + 模板补全（前后端联合）
第 12-16 天 T9~T13 ───────► 后台补全
第 17 天   T14 T15 ───────► CI + 文档
```

**每个任务完成后必须跑的回归**：
```bash
make lint
cd backend && go test ./...
cd frontend/admin && pnpm build
cd themes/default && npm run build
./blog-server rebuild && ./blog-server verify
git add -A && git commit -m "..."
```

**关键里程碑（T2/T3）完成后应该看到**：一个有真实样式、代码块带复制按钮、mermaid 能显示图表、能切换深浅色的博客页面。如果做完 T3 还看不到这些，说明前面偏了，停下来排查。

---

## 五、给执行模型的注意事项

1. **不要推倒后端重做。** 24 个包测试全绿，那是资产。所有任务都是在它之上做加法。
2. **不要改 Go 侧的 props 契约**（除非任务明确要求，如 T6/T8）。主题要适配现有契约，不是反过来。
3. **每次只做一个任务，做完提交。** 不要同时改 admin 和主题。
4. **遇到规格与现有代码冲突时**：现有代码能跑通测试的优先，但要在提交信息里记录冲突点。
5. **代码风格是硬要求**，不是建议。写完自检：
   ```bash
   awk 'length > 120 {print FILENAME":"FNR}' <你改的文件>
   ```
   有输出就重写。
6. **如果发现规格本身有错**，明确指出来，不要沉默地绕过去。前面的规格已经出过错（编造过不存在的 Shiki API），大概率还有。

---

## 六、当前未决问题（需要人决定）

| # | 问题 | 建议 |
|---|---|---|
| Q1 | `themes/default/dist/ssr/entry.js` 是手写的产物，T2 会用构建产物覆盖它 | 先 `git rm --cached` 并加入 `.gitignore`（已在忽略列表），保留一份副本以防回退 |
| Q2 | Admin 是否引入 Tailwind + shadcn/ui | 规格要求引入。但会显著增加 T1 工作量，也可以先纯 CSS 拆分，第二轮再上 |
| Q3 | 第 1.5 阶段（评论/邮件/友链申请/RSS 聚合，`docs/16`）何时开始 | 建议 T15 之后，不要与本表并行 |
