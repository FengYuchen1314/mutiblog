# 08 · 管理后台前端规格

> 目标：达到 Halo 级别的管理体验与功能完整度。React 19 + TS + Vite + TanStack Router/Query + Tailwind + shadcn/ui。

---

## 1. 工程结构

```
frontend/admin/
├── package.json
├── vite.config.ts               # base: '/admin/'，构建到 ../../backend/web/dist
├── tailwind.config.ts
├── components.json              # shadcn/ui 配置
├── index.html
└── src/
    ├── main.tsx
    ├── router.tsx               # TanStack Router 路由树
    ├── api/
    │   ├── client.ts            # fetch 封装：CSRF、401 处理、错误规范化
    │   ├── schema.d.ts          # 由 openapi.yaml 生成，不手写
    │   └── queries/             # 每个资源一个文件，导出 queryOptions + mutation hooks
    ├── components/
    │   ├── ui/                  # shadcn/ui 生成的原子组件
    │   ├── layout/              # Shell / Sidebar / Topbar / Breadcrumb
    │   ├── data/                # DataTable / EmptyState / Pagination / BulkBar
    │   ├── form/                # SchemaForm（主题设置用）/ 各类字段组件
    │   ├── editor/              # Markdown 编辑器
    │   ├── media/               # MediaPicker / MediaGrid / Uploader
    │   └── i18n/                # LocaleTabs / TranslationBadge / TranslationMatrix
    ├── features/                # 按业务域组织的页面
    │   ├── dashboard/ posts/ pages/ categories/ tags/ links/ menus/
    │   ├── media/ i18n/ themes/ users/ settings/ backup/ logs/
    ├── hooks/
    │   ├── useSSE.ts            # /api/admin/events
    │   ├── useAutosave.ts
    │   ├── useUnsavedGuard.ts
    │   └── useShortcuts.ts
    ├── lib/                     # utils, date, locale, slugify
    ├── stores/                  # zustand：UI 偏好、编辑器状态
    └── styles/
```

**依赖**：
```
react@19 react-dom@19
@tanstack/react-router @tanstack/react-query
tailwindcss @tailwindcss/typography
radix-ui 系列（经 shadcn/ui）
lucide-react                    图标
zustand                         轻量全局状态
@codemirror/*  codemirror       编辑器（见 §4）
@dnd-kit/core @dnd-kit/sortable 拖拽排序
sonner                          Toast
date-fns                        日期
zod                             表单校验
react-hook-form                 表单
```

---

## 2. 路由表

```
/admin/                                → 重定向到 /admin/dashboard
/admin/setup                           → 首次安装向导（installed=false 时强制）
/admin/login

/admin/dashboard

/admin/posts                           → 文章列表
/admin/posts/new
/admin/posts/$id                       → 编辑器（?locale=en 切换语言）
/admin/posts/$id/revisions
/admin/posts/trash

/admin/pages
/admin/pages/new
/admin/pages/$id

/admin/categories
/admin/tags

/admin/media                           → 媒体库
/admin/links                           → 友链
/admin/menus                           → 菜单
/admin/menus/$id

/admin/i18n                            → 语言设置
/admin/i18n/matrix                     → 翻译矩阵
/admin/i18n/tasks                      → 翻译任务
/admin/i18n/strings                    → UI 文案编辑

/admin/themes
/admin/themes/$name/settings

/admin/users
/admin/users/$id
/admin/profile

/admin/settings/site
/admin/settings/i18n
/admin/settings/render
/admin/settings/markdown
/admin/settings/storage
/admin/settings/ai
/admin/settings/seo
/admin/settings/cache
/admin/settings/comments
/admin/settings/security
/admin/settings/advanced

/admin/backup
/admin/logs                            → 系统日志 + 审计日志（Tab）
/admin/render                          → 渲染状态与手动重建
```

---

## 3. 侧边栏结构（对应需求第 93 条）

```
◆ 仪表盘

内容
├── 文章            (badge: 草稿数)
├── 页面
├── 分类
└── 标签

外观
├── 主题
├── 菜单
└── 友链

媒体
└── 附件

国际化
├── 语言
├── 翻译矩阵        (badge: 过期+缺失数)
└── 翻译任务        (badge: 进行中数)

系统
├── 渲染            (badge: 失败数)
├── 用户
├── 设置
├── 备份
└── 日志
```

侧边栏可折叠（图标模式），状态存 localStorage。移动端为抽屉。

**顶栏**：站点标题（点击在新标签页打开前台）· 全局搜索（⌘K）· 渲染/翻译状态指示灯 · 语言切换（后台 UI 语言）· 主题切换 · 用户菜单。

---

## 4. Markdown 编辑器

这是后台体验的核心，必须做到位。

### 4.1 技术选型

**CodeMirror 6**（不是 Monaco，不是富文本编辑器）。理由：
- 保存结果**天然是标准 Markdown**（需求第 6/19 条的硬要求），无需序列化转换
- 体积远小于 Monaco（~150KB gzip vs ~1MB）
- 移动端可用
- 扩展性足够实现所有下述功能

**明确拒绝**：ProseMirror / TipTap / Lexical 这类富文本方案。它们的内部模型是 JSON，转回 Markdown 必然有损（表格对齐、HTML 块、脚注、自定义语法都会出问题），违反需求第 6 条。

### 4.2 布局

```
┌──────────────────────────────────────────────────────────────────┐
│ ← 返回   [中文 原文][English ✓][日本語 ⚠][+ 添加语言]    [预览][发布]│
├──────────────────────────────────────────────────────────────────┤
│ 标题输入框（大字号，无边框）                                        │
│ 固定链接: /zh-cn/posts/[my-server ✎]                              │
├────────────────────────────────────────┬─────────────────────────┤
│                                        │  ▸ 发布                  │
│  工具栏: B I H 🔗 🖼 </> ❝ ≡ ☑ 表格 ⌗   │    状态: 草稿             │
│  ─────────────────────────────────     │    可见性: 公开           │
│                                        │    发布时间: [立即 ▾]      │
│  CodeMirror 编辑区                      │    ─────────────────     │
│  （或 分栏预览 / 纯预览）                 │  ▸ 分类  [多选树]         │
│                                        │  ▸ 标签  [标签输入]        │
│                                        │  ▸ 封面  [选择图片]        │
│                                        │  ▸ 摘要  [textarea]       │
│                                        │  ▸ SEO   [折叠面板]        │
│                                        │  ▸ 高级  [置顶/TOC/评论]   │
├────────────────────────────────────────┴─────────────────────────┤
│ 1,234 字 · 约 5 分钟 · 已保存于 14:32 · 行 42:8                    │
└──────────────────────────────────────────────────────────────────┘
```

### 4.3 功能清单（需求第 19 条逐条落实）

| 功能 | 实现 |
|---|---|
| **实时预览** | 三种模式：仅编辑 / 分栏 / 仅预览。分栏时**同步滚动**（按行号-元素映射，非百分比） |
| **代码高亮** | 编辑区：CodeMirror 的 markdown 语言包 + 嵌套语言高亮。预览区：走 `/api/admin/preview/markdown`，得到与前台完全一致的 Shiki 输出 |
| **拖拽图片** | drop 区域覆盖整个编辑器，拖入自动上传到 `media/<year>/<month>/` 并插入 `![](url)`；上传中显示占位 `![上传中...]()`，完成后替换 |
| **粘贴图片** | 监听 `paste` 事件的 `clipboardData.files`，同上。粘贴外链图片 URL 时提供「本地化」按钮 |
| **Markdown 快捷键** | `⌘B` 粗体 / `⌘I` 斜体 / `⌘K` 链接 / `⌘⇧K` 代码块 / `⌘⇧1..6` 标题 / `⌘⇧L` 列表 / `⌘⇧Q` 引用 / `Tab` 列表缩进 / `⌘⏎` 保存并发布 / `⌘S` 保存 |
| **全屏模式** | `F11` 或按钮，隐藏侧边栏与顶栏 |
| **自动保存** | 停止输入 2s 后 `PUT /posts/{id}/draft`（防抖）；离开页面前 `beforeunload` 拦截未保存变更 |
| **字数统计** | 中英文分别统计（CJK 按字符，拉丁按单词），实时更新，含预计阅读时间 |

**额外必备**：
- **智能列表续行**：回车时自动延续 `- ` / `1. ` / `> ` 前缀；空列表项回车则退出列表
- **表格辅助**：插入表格模板；Tab 在单元格间跳转；自动对齐分隔行
- **链接粘贴**：选中文本后粘贴 URL → 自动变成 `[选中文本](URL)`
- **图片/媒体选择器**：工具栏按钮打开媒体库弹窗，支持上传与选择
- **`/` 斜杠命令**：输入 `/` 弹出插入菜单（标题、代码块、表格、图片、分隔线、Mermaid、公式）
- **草稿恢复**：进入编辑器时若存在比正式文件新的草稿，顶部提示条「有未发布的草稿（14:32）· [恢复] [丢弃]」

### 4.4 自动保存的正确语义（重要）

```
草稿保存（自动，每 2s 防抖）
  → PUT /posts/{id}/draft
  → 写 content/.drafts/<id>.<locale>.md
  → 不动正式文件，不触发渲染，不 bump revision

手动保存（⌘S / 「保存」按钮）
  → PUT /posts/{id}/content
  → 写正式文件 index.<locale>.md
  → status 保持不变（草稿仍是草稿）
  → 若 status=published，触发重渲染 + bump sourceRevision

发布（「发布」按钮）
  → PUT content + POST publish
  → status → published，bump sourceRevision，快照 revision
  → 触发渲染 + AI 翻译
```

这三层区分必须清晰，否则会出现"自动保存把草稿发布出去"的严重 bug。

### 4.5 多语言编辑

语言标签栏（`LocaleTabs` 组件）：

- 源语言标签带「原文」徽章。
- 派生语言标签带状态徽章：✓ completed / ⚠ outdated / ✎ manual / ⏳ translating / ✕ failed / ＋ missing。
- 切换语言时若当前有未保存变更，提示确认。
- 编辑派生语言时：
  - 顶部黄色/蓝色状态条，含操作按钮（重新翻译 / 标记人工维护）
  - 权威字段（分类/标签/发布时间/作者/封面/置顶）控件**禁用 + 锁图标 + tooltip**「由源语言（简体中文）控制」
  - 保存时自动 `manualEdited=true`，保存前弹确认「保存后此译文将不再被 AI 自动更新，确定吗？」（可勾选不再提示）
- 「＋ 添加语言」下拉：选择语言后可选「AI 翻译」或「创建空白」或「复制源文」。

---

## 5. 关键页面规格

### 5.1 文章列表

**列（需求第 18 条）**：勾选框 · 标题（含置顶/草稿/定时标记）· 状态 · 分类 · 标签 · 语言矩阵 · 发布时间 · 更新时间 · 翻译状态 · 操作。

**语言矩阵列**：一行小徽章 `中 ✓ | EN ✓ | 日 ⚠ | 独 ＋`，hover 显示详情，点击直达该语言编辑页。这是本系统区别于 Halo 的核心 UI。

**筛选栏**：状态 Tab（全部/已发布/草稿/定时/回收站）· 分类下拉 · 标签下拉 · 语言下拉 · 翻译状态下拉 · 作者 · 时间范围 · 搜索框。筛选条件同步到 URL query（可分享、可后退）。

**批量操作**：选中后底部浮出操作条 —— 发布 / 取消发布 / 移入回收站 / 恢复 / 永久删除 / 添加标签 / 移除标签 / 设置分类 / 批量翻译。

**行内快捷操作**：编辑 · 预览 · 复制 · 在新标签打开前台 · 更多（复制链接、复制 ID、查看修订、翻译）。

### 5.2 分类管理

左侧树（`@dnd-kit` 拖拽，支持跨层级拖动改父级与排序），右侧编辑面板。
树节点显示：名称（当前 UI 语言）· 文章数 · slug。
新建/编辑面板：多语言名称与描述（按启用语言分 Tab）· slug · 父分类 · 排序 · 颜色 · 封面。
删除时弹窗：若有子分类或文章引用，要求选择迁移目标。

### 5.3 媒体库

网格/列表视图切换 · 左侧目录树 · 顶部面包屑 · 拖拽上传（整页 drop 区）· 多选（Shift/⌘）· 右侧详情面板（预览、尺寸、大小、URL 复制、alt 多语言编辑、引用它的文章列表）。

上传队列：右下角浮层显示每个文件的进度、成功/失败，支持重试。

**作为选择器使用时**（编辑器插入图片、设置封面）：同一组件以弹窗形式复用，底部「选择」按钮返回选中项。

### 5.4 菜单管理

左侧「可添加项」面板（Tab：页面 / 文章 / 分类 / 标签 / 自定义链接），右侧菜单树（`@dnd-kit` 拖拽，最多 2 层）。
每项可编辑：多语言 label · icon · target · 自定义 URL。
顶部选择编辑哪个菜单（header/footer/自定义），支持新建菜单。

### 5.5 友链管理

按分组的看板视图，组内拖拽排序、跨组拖拽移动。
新建/编辑弹窗：名称 · URL · Logo（可从 URL 抓取或上传）· 多语言描述 · 分组 · 状态。
分组管理弹窗：增删改 + 拖拽排序 + 多语言名称。

### 5.6 翻译矩阵

表格：行 = 文章，列 = 各语言，格 = 状态徽章。
支持：筛选（有过期 / 有缺失 / 有失败 / 全部）· 列排序 · 批量选择行 · 批量操作（翻译缺失 / 更新过期 / 强制重译）。
每格 hover 显示 tooltip：状态、基于源版本、更新时间、模型、token。
点击格子进入该语言编辑页。

### 5.7 主题设置（SchemaForm）

读取 `settings.schema.json`，自动生成表单。支持的字段类型见 [docs/09 §3](09-theme-system.md)。
分组（`group`）渲染为折叠面板或 Tab；实时校验；「恢复默认」按钮；保存后提示「已保存，正在重新生成站点」并显示渲染进度。

### 5.8 设置页

左侧二级导航 + 右侧表单。每个 section 独立保存。
- 需要重启才生效的字段：加橙色标记 + 保存后顶部提示条「部分设置需要重启服务后生效」。
- AI 设置页：Base URL / API Key（密码框 + 掩码）/ Model / Temperature / Timeout / 并发 / RPM / 自动翻译开关 / 目标语言多选 / **「测试连接」按钮**（显示延迟、模型、示例译文、是否支持 JSON 模式）。
- i18n 设置页：语言列表（拖拽排序、启用开关、自定义 prefix、设为默认/源）· 国家映射表编辑器 · 别名表编辑器 · **协商测试器**（输入 Accept-Language + 国家 + Cookie，实时显示协商结果与原因）。

### 5.9 仪表盘

卡片：文章数（已发布/草稿）· 页面数 · 分类数 · 标签数 · 友链数 · 媒体数 · 语言数。
区块：最近文章 · 最近修改 · AI 翻译任务（进行中/失败）· 渲染状态 · 系统状态（版本、内存、磁盘、渲染器健康、告警）。
快捷操作：写文章 · 上传媒体 · 重建站点 · 查看站点。

---

## 6. 通用组件规格

### 6.1 `DataTable`

基于 TanStack Table（headless）+ shadcn/ui。统一提供：列显隐、排序、行选择、粘性表头、空态、骨架屏、分页器、URL 状态同步、行操作菜单、批量操作条。所有列表页复用。

### 6.2 `api/client.ts`

```ts
export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api${path}`, {
    ...init,
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      ...(needsCSRF(init?.method) ? { 'X-CSRF-Token': getCSRFToken() } : {}),
      ...init?.headers,
    },
  });
  if (res.status === 401) { redirectToLogin(); throw new UnauthenticatedError(); }
  if (res.status === 204) return undefined as T;
  const data = await res.json();
  if (!res.ok) throw new ApiError(data.error, res.status);
  return data;
}
```

`ApiError` 携带 `code` / `message` / `details`，表单层据 `details[].field` 定位到具体字段显示错误。

### 6.3 SSE 集成

`useSSE()` 建立单一 `EventSource` 连接，按事件类型分发到 zustand store。渲染/翻译进度在顶栏指示灯与相关页面实时更新。断线自动重连（指数退避），重连后刷新相关 query。

### 6.4 乐观更新与失效

所有 mutation 成功后 `queryClient.invalidateQueries` 对应 key。列表内的状态切换（发布/取消发布）用乐观更新。

---

## 7. 后台自身的多语言

后台 UI 文案用简单的 `t(key)` 函数 + `src/locales/{zh-CN,en}.json`。**不引入 i18next**（体积不值）。语言取自用户 `locale` 字段，可在顶栏切换。

MVP 提供 `zh-CN` 与 `en` 两套后台文案。

---

## 8. 可访问性与体验细节

- 所有交互元素可键盘操作；Radix 组件天然满足大部分要求。
- 全局快捷键：`⌘K` 全局搜索 · `g p` 跳转文章 · `g m` 跳转媒体 · `?` 快捷键帮助。
- 危险操作二次确认（永久删除、恢复备份、切换主题、删除语言）。
- 所有异步操作有 loading 态与失败重试。
- 深色模式跟随系统 + 手动切换。
- 移动端可用（列表转卡片、侧边栏转抽屉、编辑器单栏）。

---

## 9. 构建集成

`vite.config.ts`：
```ts
export default defineConfig({
  base: '/admin/',
  build: {
    outDir: '../../backend/web/dist',
    emptyOutDir: true,
    rollupOptions: { output: { manualChunks: { editor: ['@codemirror/state', '@codemirror/view'] } } },
  },
  server: { proxy: { '/api': 'http://localhost:8080' } },
});
```

Go 侧 `backend/web/embed.go`：
```go
package web

import "embed"

//go:embed all:dist
var FS embed.FS
```

**构建顺序**：`npm run build`（admin）→ `go build`。Makefile 与 Dockerfile 必须保证这个顺序。

---

## 10. 验收测试

| # | 测试 | 期望 |
|---|---|---|
| A1 | 全新环境访问 `/admin/` | 进入安装向导，创建管理员后自动登录 |
| A2 | 编辑器输入 Markdown | 预览与前台发布后的 HTML 完全一致（同一渲染管线） |
| A3 | 拖入一张图片 | 自动上传并插入正确路径，前台可见 |
| A4 | 编辑中刷新页面 | 提示存在草稿，可恢复到刷新前的内容 |
| A5 | 自动保存后不点发布 | 前台内容**未改变**（草稿未污染正式文件） |
| A6 | 切换到 en 标签编辑 | 分类/时间等控件禁用；保存后 metadata 中 en 变为 manual |
| A7 | 拖拽调整分类层级 | 保存后前台分类页层级与文章归属正确更新 |
| A8 | 在翻译矩阵批量触发 5 篇文章的翻译 | 任务页实时显示进度，完成后矩阵徽章更新 |
| A9 | 断网后操作 | 显示明确错误提示，恢复后可重试，无数据丢失 |
| A10 | 后台构建产物体积 | 首屏 JS < 300KB gzip（编辑器懒加载） |
