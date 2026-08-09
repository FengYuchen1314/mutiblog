# 09 · 主题系统规格

---

## 1. 主题目录约定

```
themes/default/
├── theme.yaml                  # 元信息与能力声明
├── settings.schema.json        # 配置项 schema，后台据此生成表单
├── package.json
├── vite.config.ts
├── i18n/
│   ├── zh-CN.yaml              # 主题自带文案（可被 data/translations 覆盖）
│   └── en.yaml
├── src/
│   ├── entry.ssr.tsx           # SSR 入口，导出 templates
│   ├── entry.client.ts         # 客户端入口，island 引导
│   ├── templates/
│   │   ├── Home.tsx
│   │   ├── Post.tsx
│   │   ├── Page.tsx
│   │   ├── Category.tsx
│   │   ├── CategoryList.tsx
│   │   ├── Tag.tsx
│   │   ├── TagList.tsx
│   │   ├── Archive.tsx
│   │   ├── Links.tsx
│   │   ├── Search.tsx
│   │   └── NotFound.tsx
│   ├── components/
│   │   ├── Layout.tsx  Header.tsx  Footer.tsx  Nav.tsx
│   │   ├── PostCard.tsx  PostMeta.tsx  Pagination.tsx
│   │   ├── Breadcrumb.tsx  TOC.tsx  Prose.tsx
│   ├── islands/
│   │   ├── ThemeToggle.tsx  LocaleSwitcher.tsx  Search.tsx
│   │   ├── CopyCode.tsx  Lightbox.tsx  Mermaid.tsx
│   │   ├── Comments.tsx  TocScrollSpy.tsx  BackToTop.tsx
│   ├── styles/
│   │   ├── main.css            # Tailwind 入口 + 自定义
│   │   ├── prose.css           # 正文排版（含 Shiki 双主题、KaTeX 微调）
│   └── lib/
│       ├── ctx.tsx             # RenderContext, useSite(), useT()
│       └── Island.tsx
├── static/                     # 原样复制到 generated/public/ 的文件
└── dist/                       # 构建产物（gitignore）
    ├── ssr/entry.js
    ├── client/*.js
    ├── assets/*.css
    └── manifest.json
```

---

## 2. `theme.yaml`

```yaml
name: default
displayName:
  zh-CN: 默认主题
  en: Default
version: 1.0.0
author: Your Name
homepage: https://example.com
license: MIT
description:
  zh-CN: 简洁的默认主题，支持深色模式与多语言
  en: A clean default theme with dark mode and i18n

# 声明支持的模板（渲染器据此校验；缺失的模板会导致该类型页面渲染失败）
templates:
  - home
  - post
  - page
  - category
  - category_list
  - tag
  - tag_list
  - archive
  - links
  - search
  - not_found

# 声明用到的 island（用于 Go 侧校验与客户端 chunk 预算检查）
islands:
  - ThemeToggle
  - LocaleSwitcher
  - Search
  - CopyCode
  - Lightbox
  - Mermaid
  - Comments
  - TocScrollSpy
  - BackToTop

# 兼容性
engine:
  minVersion: "1.0.0"

# 主题自定义的页面模板（供页面的 front matter template 字段选择）
pageTemplates:
  - name: page
    label: { zh-CN: 默认页面, en: Default }
  - name: fullwidth
    label: { zh-CN: 全宽页面, en: Full width }
```

---

## 3. `settings.schema.json`

后台据此**自动生成配置表单**（需求第 58 条）。这是一个受控的 JSON Schema 子集——**不要实现完整 JSON Schema**，只实现下列类型。

```json
{
  "$schema": "https://mutiblog.dev/theme-settings-schema/v1",
  "groups": [
    { "id": "general",  "label": { "zh-CN": "通用",   "en": "General" } },
    { "id": "home",     "label": { "zh-CN": "首页",   "en": "Home" } },
    { "id": "post",     "label": { "zh-CN": "文章",   "en": "Post" } },
    { "id": "footer",   "label": { "zh-CN": "页脚",   "en": "Footer" } },
    { "id": "social",   "label": { "zh-CN": "社交账号","en": "Social" } }
  ],
  "fields": [
    {
      "key": "accentColor",
      "type": "color",
      "group": "general",
      "label": { "zh-CN": "主题色", "en": "Accent color" },
      "help":  { "zh-CN": "用于链接与按钮", "en": "Used for links and buttons" },
      "default": "#3b82f6"
    },
    {
      "key": "avatar",
      "type": "image",
      "group": "home",
      "label": { "zh-CN": "头像", "en": "Avatar" },
      "default": ""
    },
    {
      "key": "bio",
      "type": "i18n-text",
      "group": "home",
      "label": { "zh-CN": "个人简介", "en": "Bio" },
      "default": {}
    },
    {
      "key": "postLayout",
      "type": "select",
      "group": "post",
      "label": { "zh-CN": "文章布局", "en": "Post layout" },
      "options": [
        { "value": "wide",    "label": { "zh-CN": "宽",       "en": "Wide" } },
        { "value": "narrow",  "label": { "zh-CN": "窄",       "en": "Narrow" } },
        { "value": "sidebar", "label": { "zh-CN": "带侧边栏", "en": "With sidebar" } }
      ],
      "default": "narrow"
    },
    {
      "key": "showTOC",
      "type": "boolean",
      "group": "post",
      "label": { "zh-CN": "显示目录", "en": "Show table of contents" },
      "default": true
    },
    {
      "key": "postsPerRow",
      "type": "number",
      "group": "home",
      "label": { "zh-CN": "每行文章数", "en": "Posts per row" },
      "min": 1, "max": 4, "step": 1,
      "default": 2
    },
    {
      "key": "footerText",
      "type": "i18n-textarea",
      "group": "footer",
      "label": { "zh-CN": "页脚文字", "en": "Footer text" },
      "default": {}
    },
    {
      "key": "socialLinks",
      "type": "array",
      "group": "social",
      "label": { "zh-CN": "社交链接", "en": "Social links" },
      "itemLabel": "{platform}",
      "items": [
        { "key": "platform", "type": "select", "label": {"zh-CN":"平台","en":"Platform"},
          "options": [
            {"value":"github","label":{"zh-CN":"GitHub","en":"GitHub"}},
            {"value":"x","label":{"zh-CN":"X","en":"X"}},
            {"value":"mastodon","label":{"zh-CN":"Mastodon","en":"Mastodon"}},
            {"value":"email","label":{"zh-CN":"邮箱","en":"Email"}},
            {"value":"rss","label":{"zh-CN":"RSS","en":"RSS"}}
          ]},
        { "key": "url", "type": "text", "label": {"zh-CN":"链接","en":"URL"} }
      ],
      "default": []
    },
    {
      "key": "customCSS",
      "type": "code",
      "language": "css",
      "group": "general",
      "label": { "zh-CN": "自定义 CSS", "en": "Custom CSS" },
      "default": "",
      "advanced": true
    }
  ]
}
```

### 3.1 支持的字段类型（完整列表）

| type | 后台控件 | 值类型 | 额外属性 |
|---|---|---|---|
| `text` | 单行输入 | string | `placeholder`, `maxLength`, `pattern` |
| `textarea` | 多行输入 | string | `rows`, `maxLength` |
| `i18n-text` | 按语言分 Tab 的单行输入 | `{locale: string}` | |
| `i18n-textarea` | 按语言分 Tab 的多行输入 | `{locale: string}` | |
| `number` | 数字输入/滑块 | number | `min`, `max`, `step` |
| `boolean` | 开关 | boolean | |
| `select` | 下拉 | string | `options[]` |
| `radio` | 单选组 | string | `options[]` |
| `multiselect` | 多选 | string[] | `options[]` |
| `color` | 取色器 | string (#hex) | |
| `image` | 媒体选择器 | string (路径) | `accept` |
| `url` | URL 输入 + 校验 | string | |
| `code` | CodeMirror | string | `language` |
| `array` | 可增删拖拽的子表单 | object[] | `items[]`（嵌套字段定义，**只允许一层嵌套**）, `itemLabel` |
| `group-divider` | 分隔标题 | — | 纯展示 |

**公共属性**：`key`（必需，允许 `a.b` 点号路径）、`group`、`label`（i18n map）、`help`、`default`（必需）、`required`、`advanced`（折叠到「高级」区）、`showIf`（`{key, equals}` 条件显示）。

### 3.2 后端校验规则

```go
func ValidateSettings(schema SettingsSchema, vals map[string]any) []ValidationError
```

- 未在 schema 中声明的 key → **丢弃**（不报错，容忍主题降级）
- 类型不匹配 → 用 default 替代并记录警告
- `number` 越界 → 钳制到 min/max
- `select`/`radio` 值不在 options → 用 default
- `image` 路径不存在 → 保留值但警告（媒体可能后补）
- `code` 类型的 CSS → **不做 CSS 解析**，但渲染时用 `<style>` 输出前做基本清洗（移除 `</style>`、`<script`）
- 缺失的 key → 用 default 补齐

**存储**：`data/themes/<name>.settings.yaml`，只存**与默认值不同**的项（保持文件精简、升级友好）。读取时与 default 合并。

---

## 4. 模板契约

### 4.1 SSR 入口

```tsx
// src/entry.ssr.tsx
import Home from './templates/Home';
import Post from './templates/Post';
// ...

export const templates = {
  home: Home,
  post: Post,
  page: Page,
  category: Category,
  category_list: CategoryList,
  tag: Tag,
  tag_list: TagList,
  archive: Archive,
  links: Links,
  search: Search,
  not_found: NotFound,
} as const;

// 可选：主题可导出这个钩子，在每个页面渲染前修改 props
export function transformProps(kind: string, props: any, ctx: RenderCtx) { return props; }
```

渲染器 `themeLoader.ts` 动态 `import(themeDir + '/dist/ssr/entry.js')`，取 `templates[unit.kind]`。

### 4.2 模板签名

```tsx
export default function Post(props: PostProps) {
  const site = useSite();          // 站点信息、菜单、locale、i18n 文案
  const theme = useThemeSettings(); // settings.schema.json 的值
  const t = useT();                 // t('site.readMore')

  return (
    <Layout>
      <article>
        <h1>{props.article.title}</h1>
        <PostMeta article={props.article} />
        {theme.showTOC && props.article.toc && <TOC items={props.toc} />}
        <Prose html={props.article.html} />
        <PostNav prev={props.prev} next={props.next} />
        {site.comments.provider !== 'none' && (
          <Island name="Comments" props={site.comments} hydrate="visible" />
        )}
      </article>
    </Layout>
  );
}
```

**注意**：`props.article.html` 是渲染器把 `props.article.markdown` 经 unified 管线转换后**注入**的字段（渲染器在调用模板前完成 Markdown 转换，把 `html`/`toc`/`plainText` 挂到 props 上）。主题作者不需要自己处理 Markdown。

`<Prose html>` 内部就是 `<div className="prose" dangerouslySetInnerHTML={{__html: html}} />`。

### 4.3 上下文 API

```tsx
// src/lib/ctx.tsx
export function useSite(): SiteContext;         // title, baseURL, locale, urlPrefix, locales[], menus, logo, copyright, search, comments
export function useThemeSettings<T>(): T;
export function useT(): (key: string, vars?: Record<string,string|number>) => string;
export function useURL(): {
  home(): string;
  post(slug: string): string;
  category(slug: string, page?: number): string;
  tag(slug: string, page?: number): string;
  archive(year?: number, month?: number): string;
  links(): string;
  search(): string;
  page(slug: string): string;
  asset(path: string): string;     // 加 manifest hash
  media(path: string): string;
};
```

`useURL()` 自动带当前 locale 前缀。**主题不得手工拼接 URL**——这是多语言正确性的保证。

### 4.4 Island 组件

```tsx
// src/lib/Island.tsx  （由主题模板提供，渲染器提供 RenderContext）
<Island
  name="Search"                    // 必须在 theme.yaml 的 islands 中声明
  props={{ indexURL: site.search.indexURL, placeholder: t('site.searchPlaceholder') }}
  hydrate="idle"                   // load | idle | visible
>
  {/* SSR 时渲染的初始内容（无 JS 时的降级） */}
</Island>
```

**Island props 必须是 JSON 可序列化的**。渲染器会 `JSON.stringify` 到 `data-island-props`，注意 HTML 属性转义。

**Island 使用纪律**（需求第 14 条）：
- 文章正文、导航、页脚、列表 —— **绝不** island 化
- 只有真正需要交互的部分才 island：搜索框、主题切换、语言切换、评论、灯箱、复制按钮、Mermaid、TOC 滚动高亮、返回顶部
- 一个典型文章页应只有 2~4 个 island，合计客户端 JS < 20KB gzip（不含按需加载的 mermaid）

---

## 5. 构建

### 5.1 `vite.config.ts`（主题）

```ts
export default defineConfig(({ mode }) => ({
  plugins: [react(), tailwindcss()],
  build: mode === 'ssr'
    ? {
        ssr: 'src/entry.ssr.tsx',
        outDir: 'dist/ssr',
        rollupOptions: { output: { format: 'esm', entryFileNames: 'entry.js' } },
        // react/react-dom 由渲染器提供，不打包进去
        // external 在 ssr 模式下 vite 默认外部化 node_modules
      }
    : {
        outDir: 'dist',
        manifest: true,
        rollupOptions: {
          input: { client: 'src/entry.client.ts', style: 'src/styles/main.css' },
          output: {
            entryFileNames: 'client/[name]-[hash].js',
            chunkFileNames: 'client/[name]-[hash].js',
            assetFileNames: 'assets/[name]-[hash][extname]',
          },
        },
      },
}));
```

`package.json`：
```json
{
  "scripts": {
    "build": "vite build --mode client && vite build --mode ssr && node scripts/manifest.mjs",
    "dev": "vite build --mode client --watch & vite build --mode ssr --watch"
  }
}
```

### 5.2 `dist/manifest.json`

由 `scripts/manifest.mjs` 从 Vite 的 manifest 归一化产出，Go 读取它来注入 `<link>` 与 `<script>`：

```json
{
  "client": ["/assets/client-a1b2c3d4.js"],
  "css": ["/assets/style-e5f6a7b8.css"],
  "chunks": {
    "Mermaid": "/assets/Mermaid-1a2b3c.js",
    "Search": "/assets/Search-4d5e6f.js"
  },
  "static": ["favicon.ico", "fonts/xxx.woff2"]
}
```

发布时 Go 把 `dist/client/*`、`dist/assets/*`、`static/*` 复制到 `generated/public/assets/` 与站点根。

---

## 6. 默认主题的具体要求

MVP 必须交付一个**完整可用**的 `default` 主题，达到以下标准：

| 项 | 要求 |
|---|---|
| 响应式 | 375px ~ 2560px 全部正常 |
| 深色模式 | 跟随系统 + 手动切换 + 无 FOUC（内联 init 脚本） |
| 排版 | `@tailwindcss/typography` 为基础，针对中日文调整行高/字距/标点 |
| 代码块 | Shiki 双主题、圆角、语言标签、复制按钮、可横向滚动、行高亮样式 |
| 公式 | KaTeX 样式正确，块级公式可横向滚动 |
| 表格 | 可横向滚动容器，不撑破布局 |
| 图片 | `loading="lazy"`、`decoding="async"`、宽高属性防抖动、点击放大（Lightbox island） |
| TOC | 桌面侧边浮动，移动端折叠；滚动高亮当前章节 |
| 分页 | 页码窗口 + 上下页 + 首末页 |
| 语言切换 | 下拉，显示各语言原生名称，缺失版本的语言标灰并跳首页 |
| 404 | 每语言一份，含搜索框与返回首页 |
| 性能 | Lighthouse 移动端 Performance ≥ 95、Accessibility ≥ 95、SEO = 100 |
| 无 JS 可用 | 禁用 JS 后所有内容、导航、排版、高亮、公式正常 |
| 打印样式 | `@media print` 基础优化 |

**页面清单**：首页（含置顶）、文章、页面、分类页、分类总览、标签页、标签总览、归档（年月分组）、友链（分组展示）、搜索、404。

---

## 7. 主题切换与热更新

```
后台切换主题
  → 校验目标主题（theme.yaml 存在、templates 齐全、dist/ 已构建）
  → 写 config.yaml 的 theme.active
  → 通知渲染器 POST /reload（清模块缓存）
  → 触发全量重建（Release 模式）
  → 新 release 就绪后切 symlink
  → 期间旧站点持续可访问
```

主题设置修改同理（无需 reload，只需重建）。

**dev 模式**：监听 `themes/<active>/dist/` 变化 → 自动 `/reload` + 全量重建。

---

## 8. 主题开发者文档（需交付）

`themes/README.md` 需包含：目录约定、模板契约、props 类型、Island 用法、settings schema 编写、构建命令、调试方法、从 default 主题派生的步骤。

---

## 9. 验收测试

| # | 测试 | 期望 |
|---|---|---|
| H1 | 全新安装 | default 主题渲染出完整站点，无控制台错误 |
| H2 | 禁用 JS 浏览全站 | 所有页面内容、导航、高亮、公式正常 |
| H3 | 修改主题色并保存 | 全站重建，新颜色生效 |
| H4 | 在 schema 中新增一个字段 | 后台表单自动出现该字段，无需改后台代码 |
| H5 | 主题缺少 `archive` 模板 | 激活时报错并拒绝切换，给出明确提示 |
| H6 | Lighthouse 跑文章页 | Performance ≥ 95, A11y ≥ 95, SEO 100 |
| H7 | 文章页 JS 传输量 | < 20KB gzip（不含 mermaid 页面） |
| H8 | 375px 宽度浏览 | 无横向滚动，表格与代码块内部滚动 |
