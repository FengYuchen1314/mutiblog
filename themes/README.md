# 主题开发指南

## 目录约定

```
themes/<name>/
├── theme.yaml            # 元信息、templates/islands 声明、pageTemplates
├── settings.schema.json  # 后台自动生成配置表单
├── package.json / vite.config.ts
├── scripts/manifest.mjs  # 归一化 dist/manifest.json
├── src/
│   ├── entry.ssr.tsx     # export const templates = { kind: Component }
│   ├── entry.client.ts   # island 引导（load/idle/visible + IntersectionObserver）
│   ├── templates/        # Home/Post/Page/Category/Tag/Archive/Links/Search/NotFound...
│   ├── components/       # Layout/Header/Footer/PostCard/Pagination/LocaleSwitcher/Prose
│   ├── islands/          # ThemeToggle/LocaleSwitcher/Search/CopyCode/Lightbox/Mermaid/
│   │                     # TocScrollSpy/BackToTop（各自独立 chunk，按需加载）
│   ├── lib/              # ctx.tsx（纯函数式 props 辅助）、Island.tsx
│   └── styles/           # main.css（布局）+ prose.css（正文/Shiki/KaTeX/暗色）
└── dist/                 # 构建产物（gitignore）
```

## 渲染器契约（务必先读 docs/17 §2）

- 渲染器向主题发送**扁平 props**：`kind/title/description/body/locale/author/
  publishedAt/modifiedAt/theme/themeName/themeDir/markdown/canonical/alternates/
  items/pagination/pinned/category/breadcrumb/children/tag/scope/year/month/years/
  groups/searchIndexURL/message/homeLabel/prefix`。
- `html` 字段由渲染器把 `body` 经 unified 管线（Shiki/KaTeX/GFM/mermaid→island）转换后
  注入，模板通过 `<Prose html={props.html} />` 输出。
- **模板是纯函数组件**（默认导出，接收 props 对象），不要使用 React hooks——
  SSR bundle 自包含 react，hooks 会导致与渲染器 React 实例冲突。
- 目前有 10 种可达 kind：`home/post/page/category/tag/archive/links/search/not_found`
  以及兜底 `collection`。
- 渲染器只负责 `<head>`（SEO/FOUC/主题 manifest 脚本）；页面结构、样式、交互全部由主题负责。

## Island 用法

```tsx
import Island from '../lib/Island'

<Island name="Search" hydrate="idle" props={{ indexURL: '/zh-cn/search-index.json' }}>
  {/* 无 JS 时的降级内容 */}
  <input className="search-input" type="search" placeholder="搜索" />
</Island>
```

策略：`load`（立即）、`idle`（空闲时）、`visible`（滚入视口，rootMargin 200px）。
`Mermaid` 必须用 `visible`（mermaid 约 500KB gzip，按需加载）。
island 组件的 props 必须 JSON 可序列化（`data-island-props` 属性）。

## 构建与调试

```sh
cd themes/default
npm install
npm run build        # 产出 dist/ssr/entry.js + dist/client/* + dist/manifest.json
cd ../.. && ./blog-server rebuild && ./blog-server verify
```

`dist/ssr/entry.js` 按 mtime 缓存，开发时改源码 → 重新 `npm run build` → `rebuild` 即可生效。
页面上的 module 脚本由渲染器按 manifest 注入（仅当页面含 `data-island` 时），
client 资源由 Go 在发布时复制到 `generated/public/assets/`。
