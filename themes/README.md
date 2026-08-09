# 主题开发指南

> 最后核实：2026-08-10 · 与 [`docs/17-spec-vs-reality.md`](../docs/17-spec-vs-reality.md) 保持同步
>
> ⚠️ **本文档描述的是当前真实契约，不是 `docs/09` 的目标状态。** 两者冲突时以本文档为准。

---

## 1. 先读这一段：当前的真实限制

`theme.yaml` 里可以声明 11 个模板，但 **Go 侧目前只会发送 5 种 `kind`**：

| kind | 覆盖的页面 |
|---|---|
| `post` | 文章 |
| `page` | 独立页面 |
| `collection` | **首页 + 分类 + 标签 + 归档 + 友链（全挤在一起）** |
| `search` | 搜索页 |
| `not_found` | 404 |

**`home` 这个 kind 不存在** —— `RenderHome()` 只是转调 `RenderCollection()`。

更麻烦的是：**`collection` 的 payload 里没有任何字段能区分它是首页还是分类页**（只有 `title` 和 `canonical`）。所以现阶段主题**在物理上无法**为这几类页面渲染不同布局。

拆分它是 `TASKS.md` 的 **T8** 任务（前后端联合）。在那之前，`collection` 只能做一套通用列表布局。

---

## 2. 目录结构

```
themes/<name>/
├── theme.yaml               # 必需：元信息与能力声明
├── settings.schema.json     # 可选：后台自动生成配置表单
├── package.json
├── vite.config.ts           # 双构建：ssr + client
├── src/
│   ├── entry.ssr.tsx        # SSR 入口，导出 templates
│   ├── entry.client.ts      # 客户端 island 引导
│   ├── lib/
│   │   ├── Island.tsx
│   │   └── ctx.tsx
│   ├── templates/           # 5 个必需模板
│   ├── components/
│   ├── islands/
│   └── styles/
└── dist/                    # 构建产物（已在 .gitignore）
    ├── ssr/entry.js         # ← 渲染器只认这一个文件
    ├── client/*.js
    ├── assets/*.css
    └── manifest.json
```

**渲染器只加载 `dist/ssr/entry.js`**，按 mtime 缓存，改了会自动重载（dev 模式无需重启）。

---

## 3. 主题契约

渲染器（`frontend/renderer/server.js` 的 `themeMarkup()`）支持两种，**任选其一**：

### 契约 A：`templates` 映射（推荐）

```tsx
// src/entry.ssr.tsx
import Post from './templates/Post'
import Page from './templates/Page'
import Collection from './templates/Collection'
import Search from './templates/Search'
import NotFound from './templates/NotFound'

export const templates = {
  post: Post,
  page: Page,
  collection: Collection,
  search: Search,
  not_found: NotFound,
}
```

每个模板是 **React 组件**，接收 `{...props, html}`，渲染器用 `renderToStaticMarkup` 转成字符串。

### 契约 B：`render` 函数（旧版 default 主题用的）

```js
export function render(props) {
  return '<main>...</main>'   // 返回 HTML 字符串
}
```

**新主题一律用契约 A。** 契约 B 保留只为向后兼容。

### 找不到模板时

渲染器会回落到自己的内置兜底页。**不会报错**，但页面会很难看——所以 5 个模板必须齐全。

---

## 4. props 契约（真实字段）

### 所有 kind 共有

```ts
{
  kind: 'post' | 'page' | 'collection' | 'search' | 'not_found'
  title: string
  locale: string                  // 'zh-CN'
  theme: Record<string, unknown>  // settings.schema.json 的值
  themeName: string
  themeDir: string
  canonical: string               // 绝对 URL
  alternates: { hrefLang: string; href: string }[]
  html: string                    // ← 渲染器注入的正文 HTML（Markdown 已转好）
}
```

### `post` / `page` 额外字段

```ts
{
  description: string
  body: string          // 原始 Markdown（一般用不到，用 html）
  author: string
  publishedAt: string   // RFC3339
  modifiedAt: string
  markdown: { katex: boolean; externalLinksNewTab: boolean; headingAnchors: boolean }
}
```

### `collection` 额外字段

```ts
{
  items: { title: string; url: string; description: string }[]
  pagination?: {
    total: number
    links: { page: number; url: string; current: boolean }[]
  }
}
```

### `search` / `not_found` 额外字段

```ts
{
  searchIndexURL?: string   // search
  message?: string          // not_found
  homeLabel?: string        // not_found
  prefix?: string           // not_found，当前 locale 的 URL 前缀
}
```

### ⚠️ 规格里有但**实际不存在**的字段

`docs/04 §3.3` / `§3.5` 描述的这些**都不要用**，用了就是 `undefined`：

```
unit  site  seo  props  theme.manifest  markdown.mediaDimensions
meta.toc  meta.plainText  meta.wordCount  prev  next  related
categories  tags  breadcrumb  pinned
```

补齐它们是 T6 / T8 的任务。

---

## 5. `<head>` 由谁控制

**目前主题控制不了 `<head>`。** 文档外壳、`<title>`、meta、SEO 标签、`<style>` 全部由 `frontend/renderer/server.js` 生成，主题只能返回 `<body>` 里的内容。

这是已知问题（`docs/17` 的 H5），**T4 会把它交还给主题**。在那之前：

- 想加全局 CSS → 只能通过 `settings.schema.json` 的 `customCSS` 字段
- 想改 meta 标签 → 改不了

---

## 6. Islands（交互组件）

> ⚠️ **当前 islands 完全没有客户端运行时**（`docs/17` 的 H3）。渲染器会输出 `data-island="Mermaid"` 容器，但没有任何 JS 去激活它。**T3 负责补上。** 以下是 T3 完成后的用法。

### SSR 侧

```tsx
import { Island } from '../lib/Island'

<Island name="Search" props={{ indexURL }} hydrate="idle">
  {/* 无 JS 时显示的降级内容 */}
  <p>搜索需要 JavaScript</p>
</Island>
```

### hydrate 策略

| 值 | 时机 | 用在 |
|---|---|---|
| `load` | 立即 | ThemeToggle、LocaleSwitcher |
| `idle` | `requestIdleCallback` | Search、CopyCode |
| `visible` | 滚入视口（`rootMargin: 200px`） | Mermaid、Lightbox、TocScrollSpy、BackToTop |

### 纪律

- **Mermaid 必须用 `visible`** —— 它的 chunk 约 500KB gzip，用 `load` 会毁掉首屏
- 正文、导航、页脚、列表 **绝不 island 化**
- 一个典型文章页应只有 2~4 个 island，客户端 JS 合计 < 20KB gzip
- **无 island 的页面必须 0 字节 JS**

### props 必须可序列化

`props` 会被 `JSON.stringify` 到 `data-island-props` 属性，注意 HTML 转义。不能传函数、Symbol、循环引用。

---

## 7. `settings.schema.json`

后台据此**自动生成配置表单**，无需写任何后端代码。

```json
{
  "groups": [ { "id": "general", "label": { "zh-CN": "通用", "en": "General" } } ],
  "fields": [
    {
      "key": "accentColor",
      "type": "color",
      "group": "general",
      "label": { "zh-CN": "主题色", "en": "Accent color" },
      "default": "#2563eb"
    }
  ]
}
```

支持的 `type`：`text` `textarea` `i18n-text` `i18n-textarea` `number` `boolean`
`select` `radio` `multiselect` `color` `image` `url` `code` `array`（仅一层嵌套）

完整字段说明见 [`docs/09 §3.1`](../docs/09-theme-system.md)。

**存储规则**：值写入 `data/themes/<name>.settings.yaml`，**只存与默认值不同的项**。未在 schema 中声明的键会被丢弃（容忍主题降级）。

在模板里读取：

```tsx
const accent = props.theme.accentColor ?? '#2563eb'
```

---

## 8. 构建

```jsonc
// package.json
{
  "scripts": {
    "build": "vite build --mode client && vite build --mode ssr && node scripts/manifest.mjs",
    "dev": "vite build --mode client --watch & vite build --mode ssr --watch"
  }
}
```

`vite.config.ts` 要点：

```ts
// mode=ssr    → dist/ssr/entry.js   format: esm，react/react-dom 外部化
// mode=client → dist/client/*.js + dist/assets/*.css，文件名带 hash
```

构建后跑 `scripts/manifest.mjs` 产出 `dist/manifest.json`：

```json
{
  "client": ["/assets/client-a1b2c3.js"],
  "css": ["/assets/style-d4e5f6.css"],
  "chunks": { "Mermaid": "/assets/Mermaid-1a2b3c.js" }
}
```

⚠️ **Dockerfile 目前直接 `COPY themes`，没有主题构建阶段。** 主题源码化（T2）后必须补上，否则镜像里是空的 `dist/`。

---

## 9. 从 default 派生一个新主题

```bash
cp -r themes/default themes/mytheme
cd themes/mytheme
# 1. 改 theme.yaml 的 name（必须与目录名一致）
# 2. 改 settings.schema.json，加你要的字段
# 3. 改 src/ 下的模板与样式
npm install && npm run build
```

激活：

```bash
./blog-server config set theme.active '"mytheme"'
./blog-server rebuild
```

或在后台「外观 → 主题」里切换（会自动触发全量重建）。

---

## 10. 常见陷阱

| 陷阱 | 说明 |
|---|---|
| **手工编辑 `dist/`** | `dist/` 在 `.gitignore` 里，且下次 `npm run build` 会覆盖。永远改 `src/` |
| **以为有 11 个模板可用** | 只有 5 个 kind 会被调用，见 §1 |
| **给 `collection` 写分类专属布局** | 拿不到 category 对象，见 §1 |
| **依赖 `props.meta.toc`** | 不存在，见 §4 |
| **在模板里手工拼 URL** | locale 前缀会错。T2 会提供 `useURL()` helper |
| **Mermaid 用 `load` 策略** | 首屏多下 500KB |
| **给 island 传函数 props** | `JSON.stringify` 会丢掉 |
| **改了主题不重建** | 主题设置变更需要全量重建才生效 |
| **`theme.yaml` 的 name 与目录名不一致** | 主题发现会失败 |

---

## 11. 验收清单

新主题上线前逐条确认：

- [ ] 5 个模板齐全（`post` `page` `collection` `search` `not_found`）
- [ ] `npm run build` 产出 `dist/ssr/entry.js` 与 `dist/manifest.json`
- [ ] `./blog-server rebuild` 后页面正常
- [ ] **禁用 JavaScript** 后：排版、代码高亮、公式、导航全部正常
- [ ] 375px 宽度无横向滚动，表格与代码块内部滚动
- [ ] 深色模式正常，无 FOUC
- [ ] 无 island 的页面 0 个 `<script>` 标签
- [ ] Lighthouse：Performance ≥ 90、Accessibility ≥ 95、SEO = 100
- [ ] `./blog-server verify` 退出码为 0（渲染确定性）
