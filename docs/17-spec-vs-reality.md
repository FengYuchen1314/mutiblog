# 17 · 规格与实现对账（Codex 必读）

> 核实日期：2026-08-10 · 逐行核对 `backend/` `frontend/` `themes/` 实际代码
>
> **这份文档的存在理由**：前 16 份规格写的是**设计意图**，实现已在若干处走了另一条路。
> 直接照规格写代码，会去实现**根本不存在的接口**，或推翻已经跑通并有测试覆盖的代码。

---

## 0. 权威规则（冲突时怎么办）

按以下优先级判定，**高优先级覆盖低优先级**：

| 优先级 | 来源 | 说明 |
|---|---|---|
| 1（最高） | **本文档 §3 的「处置」列** | 明确写了每处偏差该采纳现状还是按规格改造 |
| 2 | `TASKS.md` | 执行计划与验收标准 |
| 3 | **实际代码**（有测试覆盖的） | 24 个 Go 包测试全绿，是可信资产 |
| 4 | `docs/01`~`docs/16` | 设计意图，非现状描述 |

**一句话**：规格描述未来，代码描述现在，本文档描述**从现在走到未来的路**。

---

## 1. 总览：各规格文档的符合度

| 文档 | 符合度 | 说明 |
|---|---|---|
| `docs/02` 内容与配置格式 | 🟢 **高** | `config.yaml` 含 `schemaVersion: 2`、`index.bodyResidentLimitMB`、`search.maxIndexSizeMB` 等后期补充项，说明是照规格实现的 |
| `docs/03` 后端模块 | 🟢 **高** | 包划分一致；`index` 有 lazy body、`state` 有读写分离、`jobs` 有心跳 —— 连修订版细节都落实了 |
| `docs/05` i18n | 🟢 **高** | Registry / 协商 / hreflang 均有实现与测试 |
| `docs/06` AI 翻译 | 🟡 中 | 分段器 / provider / 队列在；**熔断器缺失** |
| `docs/07` HTTP API | 🟢 **高** | 路由覆盖面与规格基本一致 |
| `docs/10` 安全运维 | 🟢 高 | auth / CSRF / 限流 / 备份齐备 |
| `docs/14` CLI 与生命周期 | 🟢 **高** | `verify` `doctor` `migrate` `admin reset-password` 等全部实现 |
| **`docs/04` 渲染管线** | 🔴 **低** | **契约、UnitKind、meta 返回、islands 全部与实现不符** |
| **`docs/09` 主题系统** | 🔴 **低** | 主题无源码；11 个模板实际只有 5 个 kind 可达 |
| **`docs/08` 管理后台** | 🔴 **低** | 无路由、无 Tailwind/shadcn、单文件 |
| `docs/12` 可靠性 | 🟡 中 | 心跳有；熔断器、stall 检测、三层保存缺失 |
| `docs/16` 社区功能 | ⚪ 未开始 | 第 1.5 阶段，尚未动工 |

**结论：后端可信，渲染层与前端需要按 `TASKS.md` 改造。**

---

## 2. 当前真实契约（Codex 的唯一可信参考）

以下是**实际代码中的契约**，不是规格里的。做 T2/T3/T4 时以此为准。

### 2.1 Go → Node 渲染请求

端点：`POST http://unix/render`（Unix Socket）· 另有 `/markdown` `/health` `/reload`

**实际发送的字段**（`backend/internal/render/render.go`）：

```jsonc
{
  "kind": "post",            // 见 §2.3，只有 5 种
  "title": "...",
  "description": "...",
  "body": "原始 Markdown",     // ← 注意：字段名是 body，不是 props.article.markdown
  "locale": "zh-CN",
  "author": "admin",
  "publishedAt": "2026-08-09T12:00:00Z",
  "modifiedAt": "...",
  "theme": { /* 主题设置值 */ },
  "themeName": "default",
  "themeDir": "/app/themes/default",
  "markdown": { "katex": true, "externalLinksNewTab": true, "headingAnchors": true },
  "canonical": "https://example.com/zh-cn/posts/x/",
  "alternates": [ { "hrefLang": "zh-CN", "href": "..." } ],

  // 仅 collection：
  "items": [ { "title", "url", "description" } ],
  "pagination": { "total": 3, "links": [ { "page", "url", "current" } ] },
  // 仅 search：
  "searchIndexURL": "/zh-cn/search-index.json",
  // 仅 not_found：
  "message": "...", "homeLabel": "...", "prefix": "zh-cn"
}
```

⚠️ **规格 `docs/04 §3.3` 描述的 `unit` / `site` / `seo` / `theme.manifest` / `markdown.mediaDimensions` / `props` 这些字段，实际都不存在。**

### 2.2 Node → Go 渲染响应

```jsonc
{ "html": "<!doctype html>..." }        // 成功
{ "error": "..." }                      // 失败，HTTP 400
```

⚠️ **规格 `docs/04 §3.4` 描述的 `meta.plainText` / `meta.toc` / `meta.wordCount` / `warnings` / `durationMs` 全部不存在。** Go 侧只解析 `{html}`。

补齐它是 **T6** 的任务。

### 2.3 kind 取值（**只有 5 种**）

| kind | 产生位置 | 覆盖页面 |
|---|---|---|
| `post` | `Render()` ← `string(article.Type)` | 文章 |
| `page` | `Render()` ← `string(article.Type)` | 独立页面 |
| `collection` | `renderCollection()` | **首页 + 分类 + 标签 + 归档 + 友链（全挤在一起）** |
| `search` | `RenderSearch()` | 搜索页 |
| `not_found` | `RenderNotFound()` | 404 |

⚠️ **`home` 这个 kind 不存在** —— `RenderHome()` 只是转调 `RenderCollection()`。

⚠️ 规格 `docs/04 §1.1` 列的 16 种 `UnitKind`、`docs/09` 与 `theme.yaml` 声明的 11 个模板，**当前有 6 个永远拿不到调用**。

拆分它是 **T8** 的任务（前后端联合）。

### 2.4 主题契约

`frontend/renderer/server.js` 的 `themeMarkup()` 支持两种，**任选其一**：

```js
// 契约 A（推荐，T2 采用）
export const templates = { post, page, collection, search, not_found }
// 每个是 React 组件，接收 {...props, html}

// 契约 B（当前 dist 用的）
export function render(props) { return '<html string>' }
```

> ⚠️ 实现修正（T2）：渲染器原先是 `renderToStaticMarkup(template(props))` 直接调用组件函数，
> 组件内使用 hooks 会触发 "Invalid hook call"。已改为 `createElement(template, props)` 正规渲染。
> 主题模板因此可以使用 React（SSR bundle 将 react 打进产物，避免与渲染器产生双 React 实例）。

主题的 `dist/ssr/entry.js` 按 **mtime 缓存**，改了会自动重载。

**主题只能控制 `<body>` 内容**。`<head>`、`<style>`、SEO 标签全部由 `server.js` 生成 —— 这是 B4 问题，T4 负责修正。

---

## 3. 逐项对账与处置

### 3.1 渲染层（🔴 主要偏差区）

| # | 规格说 | 实际是 | 处置 | 任务 |
|---|---|---|---|---|
| R1 | `docs/04 §1.1` 定义 16 种 `UnitKind` | 只有 5 种 kind | ✅ 已完成（拆出 home/category/tag/archive/links，collection 保留为兜底） | T8 |
| R2 | `docs/04 §3.3` 请求含 `unit/site/seo/props` 嵌套结构 | 扁平字段，无 `site`/`seo` 对象 | **采纳现状**。嵌套结构收益不大，改造会波及所有渲染路径 | — |
| R3 | `docs/04 §3.4` 响应含 `meta{plainText,toc,wordCount}` | 只有 `{html}` | ✅ 已完成（/render 返回 html+meta+warnings；plainText 写入 release/.meta） | T6 |
| R4 | `docs/04 §4.4` React 两遍渲染 + Island | 只有 `renderToStaticMarkup`，无 island 运行时 | ✅ 已完成（SSR data-island 标记 + 客户端按策略引导；模板为纯函数故无需两遍渲染） | T3 |
| R5 | `docs/04 §4.2` Shiki + transformers | ✅ 已实现（含双主题 CSS 变量、语言标签、行号 CSS） | **保持** | — |
| R6 | `docs/04 §4.2` 勘误：`transformerCopyButton` 不存在 | 实现方未踩坑，用了 CSS + 计划中的 island | **保持** | — |
| R7 | `docs/04 §7.3` 搜索用 MiniSearch + 索引 | renderer 内联 `.includes()` 子串匹配 | ✅ 已完成（单字母字段索引 + plainText 正文 + MiniSearch island + 体积护栏） | T5 |
| R8 | `docs/04 §5.2` release + symlink 原子切换 | ✅ 已实现 | **保持** | — |
| R9 | `docs/04 §4.2` 图片 `width`/`height` 注入 | 未实现 | ✅ 已完成（media 尺寸 sidecar + renderer rehype 插件注入 width/height/loading/decoding） | T7 |
| R10 | `docs/04 §2` `AffectedUnits` 增量依赖计算 | ⚠️ 需单独核实覆盖度 | 做 T8 时一并检查，改完跑 `verify` | T8 |

### 3.2 主题系统（🔴）

| # | 规格说 | 实际是 | 处置 | 任务 |
|---|---|---|---|---|
| H1 | `docs/09 §1` React + TS 源码，Vite 双构建 | 只有 13 行手写 `dist/ssr/entry.js`，无 `src/` | ✅ 已完成（src/ + vite 双构建 + manifest，SSR bundle 自包含 react） | T2 |
| H2 | `docs/09` 11 个模板 | 5 个 kind 可达，`theme.yaml` 的声明是空头支票 | ✅ 已完成（10 个模板：home/post/page/category/tag/archive/links/collection/search/not_found） | T2 → T8 |
| H3 | `docs/09 §4.4` Islands + hydrate 策略 | 完全缺失（已发布页面 0 个 `<script>`） | ✅ 已完成（8 个 island + load/idle/visible 策略 + FOUC 防护；引导包 1.5KB gzip） | T3 |
| H4 | `docs/09 §3` settings.schema.json | ✅ 已存在且后台能渲染表单 | **保持** | — |
| H5 | 主题可控制 `<head>` 与样式 | CSS/head 全在 `server.js` 里 | ✅ 已完成（server.js 拆分为 src/ 五模块；样式移交主题，渲染器只保留元数据与 FOUC） | T4 |

### 3.3 管理后台（🔴）

| # | 规格说 | 实际是 | 处置 | 任务 |
|---|---|---|---|---|
| T0 | 代码规范基线（行长 ≤120、Prettier/ESLint/gofmt、Makefile fmt/lint） | 超长行全部拆分，工具链就绪 | ✅ 已完成 | T0 |
| A1 | `docs/08 §2` TanStack Router 路由表 | 已装未用，`useState` 切页，刷新丢状态 | ✅ 已完成（路由树 + URL 持久化） | T1 |
| A2 | `docs/08 §1` Tailwind + shadcn/ui | 纯手写 CSS | **待定**，见 `TASKS.md` Q2 | T1 |
| A3 | `docs/08 §1` 按 feature 分目录 | 16 个组件挤在 63 行单文件 | ✅ 已完成（features/components/api 拆分） | T1 |
| A4 | `docs/08 §4` CodeMirror 编辑器 | ✅ 已实现（含自动保存、localStorage、修订、预览） | **保持** | — |
| A5 | `docs/08 §5` 分类/标签/菜单/友链/多语言矩阵页 | 缺失 | ✅ 已完成：分类/标签（T9）、菜单/友链（T10）、页面（T11）、翻译矩阵+批量成本确认（T12）、回收站/日志/文章列表筛选（T13）。T12 编辑器语言标签栏与权威字段锁待后端多版本接口，已记录 | T9~T13 |

### 3.4 可靠性（🟡）

| # | 规格说 | 实际是 | 处置 | 任务 |
|---|---|---|---|---|
| C1 | `docs/12 §3.1` job 心跳 + 120s 回收 | ✅ 已实现 | **保持** | — |
| C2 | `docs/12 §4` AI / 渲染器熔断器 | **缺失** | **按规格改造** | 建议并入 T7 |
| C3 | `docs/12 §3.7` stall 检测 | 缺失 | 优先级低，可延后 | — |
| C4 | `docs/12 §8` 编辑器三层保存 | 部分（有 localStorage + 服务端草稿，缺离线状态指示与冲突对话框） | 并入 T1 后续 | T1+ |
| C5 | `docs/14 §4.3` 确定性渲染 | 无测试 | ✅ 已完成（TestDeterministicRender 两次渲染逐字节一致） | T7 |

### 3.5 后端（🟢 无需改造）

| # | 项 | 状态 |
|---|---|---|
| B1 | `docs/03` 包划分与依赖方向 | ✅ 一致 |
| B2 | `docs/03 §4.4` index lazy body 模式 | ✅ 已实现 |
| B3 | `docs/03 §10` SQLite 读写分离 | ✅ 已实现 |
| B4 | `docs/02 §8` config schema | ✅ 一致，含 `schemaVersion: 2` |
| B5 | `docs/14 §1` CLI 全表 | ✅ `verify` `doctor` `migrate` `admin *` `backup *` `import` `export` `config *` `index *` 全部实现 |
| B6 | `docs/05` i18n 协商与 hreflang | ✅ 有实现与测试 |
| B7 | `docs/10` 认证 / CSRF / 限流 / 备份 | ✅ 齐备 |

**不要重写这些。** 24 个包测试全绿是本项目最有价值的资产。

---

## 4. 规格文档中当前属于「未实现」的章节

Codex 阅读这些章节时，请理解为**目标状态而非现状**：

| 文档章节 | 状态 |
|---|---|
| `docs/04 §1.1` 16 种 UnitKind | 目标（T8 拆到 10 种） |
| `docs/04 §3.3` 嵌套请求契约 | **不采纳**，见 R2 |
| `docs/04 §3.4` meta 响应 | 目标（T6） |
| `docs/04 §3.5` PageProps TS 接口 | 目标（T8 时按此定义新 props） |
| `docs/04 §4.4`~`§4.5` Islands | 目标（T3） |
| `docs/04 §7.3` 搜索索引 | 目标（T5） |
| `docs/09 §1`~`§5` 主题源码结构 | 目标（T2） |
| `docs/08 §1`~`§5` 后台结构 | 目标（T1、T9~T13） |
| `docs/12 §4` 熔断器 | 目标（T7） |
| `docs/16` 全部 | 第 1.5 阶段，未开始 |

---

## 5. 维护约定

**每完成一个任务，回来更新本文档对应行的「处置」列为 ✅ 已完成。**

本文档是项目的**真实状态镜像**。它一旦过期，Codex 就会重新开始基于幻觉写代码 —— 那正是它要防止的事。

核实方法（任何人都可以重跑）：

```bash
# kind 取值
grep -oE '"kind": *(string\(article\.Type\)|"[a-z_]+")' backend/internal/render/render.go | sort -u

# renderer 响应字段
grep -A3 "var response struct" backend/internal/render/render.go | head -5

# 客户端 hydration 是否存在
grep -rl "hydrateRoot" --include='*.ts' --include='*.tsx' themes/ frontend/renderer/ 2>/dev/null || echo "无"

# 已发布页面的 script 数量
grep -c '<script' generated/public/zh-cn/index.html

# 后端健康度
cd backend && go build ./... && go test ./...
```
