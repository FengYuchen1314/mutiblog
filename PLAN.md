# 多语言 AI 原生博客系统 — 实现计划总纲

> 本文档是**实现规格**，不是提案。所有技术选型已经锁定，实现者不需要再做架构决策，只需要按规格写代码。
>
> 版本：v1.0 · 日期：2026-08-09 · 目标：可交付给任意编码模型/工程师分批实现

---

## 0. 给实现者（模型）的强制规则

在写任何一行代码之前，先读完这一节。

### 0.1 不可协商的架构约束

| # | 约束 | 违反后果 |
|---|---|---|
| A1 | **Markdown 文件是唯一的正文真相源**。SQLite、内存索引、`generated/` 全部是派生物，删掉后必须能从 `content/` + `data/` + `config/` 100% 重建 | 违反 = 架构失败，返工 |
| A2 | **页面 HTML 只在发布时生成一次**。生成路径之外不允许出现 Markdown 解析、React 渲染、AI 调用。※ 评论正文在**提交时**渲染成 HTML 存好，读取时零解析（[docs/16 §1](docs/16-social-features.md)） | 性能目标失效 |
| A3 | **文章内容的读路径不依赖后端进程存活**。CMS 崩溃后，文章/导航/样式/图片经 Nginx/CDN 完整可用；※ 仅评论区降级为"暂时不可用"，页面本身不受影响 | 违反 = 核心设计目标失效 |
| A4 | **SQLite 只存"可丢弃的运行状态"**：任务队列、翻译任务、渲染队列、登录尝试、审计日志、定时发布。**绝不存文章正文、分类、标签、友链、菜单、配置** | 违反 = 内容被锁进数据库 |
| A5 | **Node 只做两件事**：构建前端、发布时的静态渲染。绝不承担 API、认证、内容管理、AI 调用 | 违反 = 变成 Headless CMS + Next.js，明确禁止 |
| A6 | **所有文件写入必须原子**：`写 tmp → fsync → rename`。没有例外 | 断电/崩溃导致内容损坏 |
| A7 | **单个 Go 二进制 + 单个容器**。不得引入 Redis / PostgreSQL / MySQL / MongoDB / Elasticsearch / RabbitMQ / Kubernetes | 违反部署目标 |
| A8 | **1 Core / 512MB RAM 必须能跑**。任何设计决策以此为内存预算上限 | 违反资源目标 |

### 0.2 锁定的技术选型

```
后端        Go 1.23+ / chi v5 / 单二进制 / CGO_ENABLED=0
状态库      SQLite (modernc.org/sqlite，纯 Go 无 cgo)，仅运行状态
渲染器      Node 20+ 常驻子进程，Unix Socket + HTTP 通信，仅发布时使用
前端        React 19 + TypeScript 5 + Vite 6+
后台路由    TanStack Router + TanStack Query v5
后台 UI     Tailwind CSS + shadcn/ui (Radix UI)
主题        React 19 SSR + Islands 局部 hydration
Markdown    Node 侧 unified/remark/rehype（唯一 HTML 生成者）
            Go 侧 goldmark（仅用于翻译分段与结构校验，不产出 HTML）
代码高亮    Shiki（发布时渲染，双主题 CSS 变量，零运行时 JS）
数学公式    KaTeX（发布时渲染成 HTML，运行时只需 CSS + 字体）
Mermaid     客户端 island 懒加载（服务端渲染需要 DOM，成本过高）
搜索        发布时生成 per-locale JSON 索引 + 前端 MiniSearch
AI          OpenAI 兼容 /v1/chat/completions，供应商不锁定
```

### 0.3 实现顺序（严格串行）

不要跳步。每个里程碑有明确验收标准，未通过不进入下一个。详见 [docs/11-milestones.md](docs/11-milestones.md)。

```
M0  项目骨架 + 配置 + 原子文件层
M1  内容模型 + 解析/序列化 + 内存索引 + 文件监听
M2  认证 + Admin API 骨架 + Admin SPA 壳
M3  文章/页面 CRUD + Markdown 编辑器 + 草稿/修订
M4  分类 / 标签 / 友链 / 菜单 / 媒体库
M5  Node 渲染器 + 默认主题 + 单页渲染打通
M6  渲染编排：增量依赖计算 + 队列 + 原子发布
M7  列表/分类/标签/归档/首页/分页/RSS/Sitemap/搜索索引
M8  i18n：多语言文章 + Locale 路由 + 协商 + hreflang
M9  AI 翻译：分段器 + Provider + 队列 + 状态机 + 后台 UI
M10 主题系统：settings.schema.json → 后台表单 + 主题切换
M11 设置 / 用户 / 备份恢复 / 日志 / 仪表盘
M12 Docker / Nginx / 缓存策略 / 性能验收
```

---

## 1. 文档地图

| 文档 | 内容 | 主要读者 |
|---|---|---|
| [docs/01-architecture.md](docs/01-architecture.md) | 进程模型、目录结构、数据流、启动/关闭序列 | 全部 |
| [docs/02-content-format.md](docs/02-content-format.md) | 文件格式规格：Front Matter、metadata.yaml、各 YAML schema、config.yaml、SQLite 表 | 后端 |
| [docs/03-backend-modules.md](docs/03-backend-modules.md) | Go 包划分、核心类型定义、内存索引、文件监听、原子写、事件总线 | 后端 |
| [docs/04-render-pipeline.md](docs/04-render-pipeline.md) | 渲染器协议、Markdown 管线、增量依赖图、原子发布、CDN 刷新 | 后端 + Node |
| [docs/05-i18n.md](docs/05-i18n.md) | Locale 规范化、URL 方案、语言协商算法、hreflang、SEO | 后端 + 主题 |
| [docs/06-ai-translation.md](docs/06-ai-translation.md) | 分段协议、保护规则、Provider 抽象、队列、状态机、修订追踪 | 后端 |
| [docs/07-http-api.md](docs/07-http-api.md) | 完整 REST API 表、请求/响应体、错误码、中间件链 | 后端 + 前端 |
| [docs/08-frontend-admin.md](docs/08-frontend-admin.md) | Admin SPA 结构、路由表、页面清单、编辑器规格 | 前端 |
| [docs/09-theme-system.md](docs/09-theme-system.md) | 主题目录约定、模板契约、Islands、settings.schema.json、构建产物 | 前端 + 主题 |
| [docs/10-security-ops.md](docs/10-security-ops.md) | 认证、CSRF、XSS、限流、RBAC、备份恢复、审计 | 后端 |
| [docs/11-milestones.md](docs/11-milestones.md) | 12 个里程碑的任务分解与验收标准 | 全部 |
| [docs/12-reliability-ux.md](docs/12-reliability-ux.md) | **失败模式、卡死检测、进度语义、降级矩阵、错误文案、混沌测试** | 全部 |
| [docs/13-first-run-walkthrough.md](docs/13-first-run-walkthrough.md) | **首次部署实地走查：30 个会翻车的地方及其修正（对前 12 份文档的正式修订）** | 全部 |
| [docs/14-cli-and-lifecycle.md](docs/14-cli-and-lifecycle.md) | **CLI（含密码重置）、doctor、verify、版本升级与内容迁移、灾难恢复手册** | 后端 |
| [docs/15-implementation-handoff.md](docs/15-implementation-handoff.md) | **如何把这套规格分批交付给实现模型：投喂清单、提示词模板、一致性保障、高危区域** | 调度者 |
| [docs/16-social-features.md](docs/16-social-features.md) | **原生评论、邮件通知、友链自助申请、友链 RSS 聚合、版本更新（第 1.5 阶段，+21 人日）** | 全部 |

> ⚠️ **`docs/13` 优先级高于前 12 份文档**。它是按真实部署路径逐步走查后写的修订集，凡与前面文档冲突处，**以 docs/13 为准**。其中 P1/P2/P3/P6/P13/P25/P26 是会导致部署失败或数据错误的硬缺陷。

> **实现者注意**：`docs/12` 不是"锦上添花"的文档。它定义的超时矩阵、看门狗、熔断器、三层保存是**功能的一部分**，必须与对应里程碑同步实现，不得推迟到最后。每个里程碑的验收都包含 docs/12 中相关的混沌测试项。

---

## 2. 系统一句话描述

> 一个 Go 单二进制 CMS，把 Markdown 文件当数据库，在**发布那一刻**用 React 把内容预渲染成静态 HTML 落盘，并用 AI 把源语言文章自动翻译成其他语言的 Markdown 再各自渲染成 HTML；访客访问的永远是纯静态文件，不经过 CMS。

**Write Dynamic, Read Static.**

---

## 3. 顶层数据流

```
                        ┌───────────────────────┐
   管理员 ──────────────▶│  React Admin SPA      │
                        │  /admin/  (embed 进二进制)│
                        └───────────┬───────────┘
                                    │ REST /api/admin/*
                                    ▼
        ┌───────────────────────────────────────────────────┐
        │            Go 单二进制  (blog-server)              │
        │                                                   │
        │  httpserver │ api │ auth │ content │ index │ watcher│
        │  render(编排) │ ai │ media │ theme │ jobs │ events   │
        └───┬───────────────┬──────────────┬────────────┬────┘
            │               │              │            │
   ┌────────▼─────┐  ┌──────▼─────┐  ┌─────▼────┐  ┌────▼─────────┐
   │  content/    │  │   data/    │  │  media/  │  │ data/state.db│
   │  Markdown    │  │  YAML      │  │  Files   │  │ 仅运行状态    │
   │ (真相源)      │  │ (真相源)    │  │(真相源)   │  │ (可删除)      │
   └────────┬─────┘  └──────┬─────┘  └─────┬────┘  └──────────────┘
            └───────────────┼──────────────┘
                            │ Publish 事件
                            ▼
              ┌──────────────────────────────┐
              │  Node Render Worker (常驻)     │
              │  remark/rehype + Shiki +KaTeX │
              │  React 19 SSR + Islands       │
              └──────────────┬───────────────┘
                             │ HTML 字符串
                             ▼
                 Go 原子写入 generated/public/
                             │
                             ▼
                    Nginx / Cloudflare
                             │
                             ▼
                          访客
```

访客路径**完全不经过**上图中间那个 Go 方框。

---

## 4. 里程碑总表（含预估）

预估以"一个熟练实现者 / 编码模型的有效工作量"计，单位为人日。

| M | 名称 | 交付物 | 预估 | 依赖 |
|---|---|---|---|---|
| M0 | 骨架与文件层 | 可启动的二进制、config 加载、原子写工具、日志 | 2 | — |
| M1 | 内容模型与索引 | 解析/写回 Markdown+YAML、内存索引、fsnotify 热重载 | 4 | M0 |
| M2 | 认证与 API 壳 | 登录、Session、CSRF、限流、Admin SPA 空壳 | 3 | M0 |
| M3 | 文章与页面 | 文章/页面 CRUD、编辑器、草稿、修订、回收站 | 6 | M1,M2 |
| M4 | 分类法与媒体 | 分类树、标签、友链、菜单、媒体库、Storage 接口 | 5 | M1,M2 |
| M5 | 渲染器打通 | Node worker、默认主题、单篇文章渲染出 HTML | 5 | M1 |
| M6 | 渲染编排 | 依赖计算、渲染队列、原子发布、全量重建 | 4 | M5 |
| M7 | 站点级页面 | 首页/列表/分类/标签/归档/分页/RSS/Sitemap/搜索 | 5 | M6 |
| M8 | 多语言 | 多语言 bundle、Locale 路由、协商、hreflang | 4 | M7 |
| M9 | AI 翻译 | 分段器、Provider、队列、状态机、翻译后台 | 6 | M8 |
| M10 | 主题系统 | theme.yaml、settings schema、后台自动表单、切换 | 3 | M7 |
| M11 | 系统功能 | 设置、用户、备份/恢复、日志、仪表盘、导入导出 | 4 | M4 |
| M12 | 部署与验收 | Dockerfile、compose、Caddy/nginx、缓存策略、走查验收 | 3 | 全部 |
| M13 | CLI 与生命周期 | doctor、verify、密码重置、内容迁移、灾难恢复文档 | 3 | 全部 |
| | | **合计** | **57** | |

> M13 的各条目**分散实现**到对应里程碑更合适（`admin reset-password` 应在 M2 就有，`verify` 应在 M6 就有），M13 只是它们的收口与文档化。详见 [docs/14](docs/14-cli-and-lifecycle.md)。

---

## 5. 全局验收标准（Definition of Done）

系统被认为完成，当且仅当以下全部为真：

1. `docker compose up -d` 后，`http://localhost:8080/admin/` 可登录，无需任何外部服务。
2. 后台新建一篇中文文章并发布后，**5 秒内** `generated/public/zh-cn/posts/<slug>/index.html` 存在且内容正确。
3. AI 翻译完成后，`content/posts/<...>/index.en.md` 是**人类可直接编辑的标准 Markdown**，且 `generated/public/en/posts/<slug>/index.html` 已生成。
4. `kill -9` 掉 Go 进程后，Nginx 仍能正常提供所有已发布页面（含 CSS/JS/图片）。
5. `rm -rf generated/ data/state.db` 后重启，触发全量重建，站点 100% 恢复。
6. `git init && git add content data config && git commit` 可完整版本化博客内容。
7. 文章页 HTML 在**禁用 JavaScript** 时排版、代码高亮、公式、图片、导航全部正常。
8. 访问 `/` 按 Cookie → Accept-Language → CF-IPCountry → 默认 的顺序 302 到正确 locale。
9. 单实例常驻内存 < 200MB（含 Node 渲染进程空闲态），空载 CPU ≈ 0%。
10. 静态页面 TTFB（本地 Nginx，无 CDN）< 10ms；命中 CDN 时 < 100ms。

---

## 6. 明确不做的事（第一阶段）

写下来是为了防止实现者自作主张扩大范围：

- ❌ 评论系统的服务端实现（MVP 只预留 island + 设置项，接 Giscus/Waline 等外部服务）
      → **原生评论已在 [docs/16 §2](docs/16-social-features.md) 完整规格化，属第 1.5 阶段 M14**
- ❌ 邮件通知 / 友链自助申请 / 友链 RSS 聚合 / 版本更新
      → 均已规格化，见 [docs/16](docs/16-social-features.md)，属第 1.5 阶段 M15–M18
- ❌ 插件运行时（只做事件总线 + Hook 点，见 [docs/03](docs/03-backend-modules.md)）
- ❌ S3 / R2 存储驱动（只定义 `Storage` 接口 + Local 实现）
- ❌ Cloudflare Purge API（只定义 `CachePurger` 接口 + Noop 实现）
- ❌ 主题市场、在线安装主题
- ❌ Revision 的可视化 diff UI（只做文件落盘 + API 列表/回滚）
- ❌ AI 术语表 / 翻译记忆库
- ❌ 文章密码、Custom CSS/JS、Analytics
- ❌ 多站点、多租户
- ❌ WebP/AVIF 自动转码（见 [docs/03 §媒体](docs/03-backend-modules.md)，MVP 只做同格式缩略图，理由是纯 Go 编码器缺失，不引入 cgo）

第二阶段路线图见 [docs/11-milestones.md](docs/11-milestones.md) 末尾。

---

## 7. 术语表

| 术语 | 定义 |
|---|---|
| **Article** | 一个逻辑文章实体，由一个 UUID 标识，包含 1..N 个 locale 的 Markdown 文件 |
| **Bundle** | 磁盘上承载一个 Article 的目录，含 `index.<locale>.md` 与 `metadata.yaml` |
| **Source Locale** | 该 Article 的原始写作语言，其 Front Matter 为权威元数据 |
| **Derived Locale** | 由 AI 或人工翻译产生的语言版本 |
| **Source Revision** | 源语言正文的单调递增版本号，用于判断译文是否 outdated |
| **Render Unit** | 一个可独立渲染的输出单元，对应磁盘上一个 HTML/XML/JSON 文件 |
| **Island** | 前台页面中需要客户端 hydration 的最小交互组件 |
| **Release** | `generated/releases/<ts>/` 下的一次完整站点快照 |
