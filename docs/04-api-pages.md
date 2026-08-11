# API 与页面清单

## 1. API 约定

- 管理 API 前缀：`/api/v1/admin`；
- 公开动态 API 前缀：`/api/v1/public`；
- JSON 使用 camelCase；错误响应包含稳定 `code`、本地化 `message`、`requestId` 和可选字段错误；
- 分页为 `page`（从 1 开始）、`size`、`total`、`items`；
- 写操作使用 `If-Match` 或 `revision` 防止覆盖外部修改；
- 长任务返回 `202` 与持久 `task.id`，通过任务端点查询；页面可在原处轮询同一 ID，任务中心也会保留终态。

## 2. 初始化与认证

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/setup/status` | 是否已初始化 |
| POST | `/api/v1/setup` | 创建站点、必须填写的公开 Base URL、源语言、后台语言与唯一管理员；建立正常管理员会话并返回首次静态构建任务 |
| POST | `/api/v1/auth/login` | 登录并建立会话 |
| POST | `/api/v1/auth/logout` | 注销当前会话 |
| GET | `/api/v1/auth/session` | 当前会话与 CSRF 信息 |
| PUT | `/api/v1/admin/security/password` | 修改密码 |
| GET | `/api/v1/admin/security/audit` | 最近安全审计事件（不含秘密和原始客户端地址） |

## 3. 内容资源

文章与页面共享 CRUD、草稿、发布、下线、回收站、永久删除、修订与语言端点。核心形式：

```text
GET    /api/v1/admin/posts
POST   /api/v1/admin/posts
GET    /api/v1/admin/posts/:id
PUT    /api/v1/admin/posts/:id
POST   /api/v1/admin/posts/:id/publish
POST   /api/v1/admin/posts/:id/unpublish
POST   /api/v1/admin/posts/:id/recycle
POST   /api/v1/admin/posts/:id/restore
DELETE /api/v1/admin/posts/:id
GET    /api/v1/admin/posts/:id/revisions
POST   /api/v1/admin/posts/:id/revisions/:revision/restore
PUT    /api/v1/admin/posts/:id/locales/:locale
POST   /api/v1/admin/posts/:id/translate
```

分类、标签、菜单、友链、附件、评论、站点设置、语言、Provider、翻译任务、主题、备份和系统任务分别拥有独立资源端点。

公开框架字典提供完整度读取与按语言保存：

```text
GET /api/v1/admin/dictionaries
PUT /api/v1/admin/dictionaries/:locale
```

附件端点至少包括列表、上传和引用保护删除：

```text
GET    /api/v1/admin/attachments
POST   /api/v1/admin/attachments
DELETE /api/v1/admin/attachments/:id
```

删除会同时移除本地原件和 YAML 元数据；当前内容、修订历史、公开 release 或主题设置仍引用附件 URL 时返回 `409 media_in_use`，不会制造已知断链。

备份创建、本地导入和远程导入返回 `202` 与持久 Backup 任务；`GET /api/v1/admin/backups/tasks` 提供 queued/running/succeeded/failed 状态与可本地化错误代码。恢复仍在当前请求内完成事务门禁并强制重新登录，同时写入同一任务历史。

`POST /api/v1/admin/index/rebuild` 也返回 `202` 和 `IndexRebuild` 任务。控制台以 `X-MutiBlog-Index-Task-ID` 提供浏览器生成的关联 ID；任务会在启动、重建、完成或失败时持久化，服务重启中断的任务会明确标记为失败而不会假装索引已完成。

主题列表返回本地截图 URL、主题 API 兼容状态与结构化条件。`POST /api/v1/admin/themes/:id/reload` 重新读取并校验包声明、设置与兼容性；若为活动主题，还必须通过完整静态构建门禁。`POST /api/v1/admin/themes/:id/preview` 同步执行完整构建门禁并返回 30 分钟随机预览 URL；预览 release 位于 `generated/previews`，不修改活动主题或 `generated/current`。预览 URL 只在隔离的同级 `preview` 子域建立临时 Cookie。

## 4. 动态公开 API

第一阶段只有评论需要公开动态服务：

```text
GET  /api/v1/public/comments?kind=Post&id=<id>&page=1
POST /api/v1/public/comments
```

静态页中的评论组件必须设置请求超时并显示可本地化的“评论服务暂时不可用”；失败不得隐藏或破坏正文。

## 5. 控制台页面

| 路径 | 页面与关键行为 |
| --- | --- |
| `/setup` | 站点信息、源语言、后台语言、管理员初始化 |
| `/login` | 单管理员登录 |
| `/console/dashboard` | 内容统计、发布/索引/翻译任务、快捷访问 |
| `/console/posts` | Halo 式文章列表、筛选、批量动作与回收站 |
| `/console/posts/editor/:id?` | Markdown 编辑、预览、分栏、语言矩阵、发布设置 |
| `/console/pages` | 页面列表与回收站 |
| `/console/pages/editor/:id?` | 页面 Markdown 编辑 |
| `/console/categories` | 层级分类、多语言编辑与排序 |
| `/console/tags` | 标签列表、多语言编辑 |
| `/console/comments` | 状态筛选、审核和删除；回复层级仍按评论产品待确认项处理 |
| `/console/attachments` | 本地上传、关键字与类型筛选、排序、预览、复制 URL 和引用保护删除 |
| `/console/links` | 友链分组和友链管理 |
| `/console/menus` | 菜单集合、嵌套菜单项和排序 |
| `/console/theme` | 当前主题、设置 schema、预览 |
| `/console/themes` | 安装、升级、启用、卸载与重载 |
| `/console/ai/providers` | Provider、Base URL、模型、遮罩 Key、连接测试 |
| `/console/tasks` | 翻译、发布、静态重建、搜索索引重建、定时发布和备份任务的统一进度、阶段与错误诊断 |
| `/console/ai/tasks` | 兼容入口，跳转统一任务中心并筛选翻译任务 |
| `/console/locales` | 源语言、目标语言、框架字典完整度与人工维护 |
| `/console/settings` | 站点、评论、后台语言、管理员安全与系统设置；内容 SEO 在对应内容编辑器维护 |
| `/console/backup` | 创建、本地/公网 HTTPS URL 导入、下载、恢复、删除备份 |
| `/console/overview` | 版本、目录、存储、运行与安全信息 |
| `/console/tools` | 以持久任务重建索引、重建站点和搜索索引检查 |

## 6. 公开静态页面

每种启用语言都生成：

```text
/<locale>/
/<locale>/posts/<id>/
/<locale>/pages/<id>/
/<locale>/categories/
/<locale>/categories/<id>/
/<locale>/categories/<id>/page/<n>/
/<locale>/tags/
/<locale>/tags/<id>/
/<locale>/tags/<id>/page/<n>/
/<locale>/archives/
/<locale>/archives/page/<n>/
/<locale>/links/
/<locale>/search/
/<locale>/404.html
/<locale>/rss.xml
```

首页同样从 canonical `/<locale>/` 开始，第二页起使用 `/<locale>/page/<n>/`。首页、归档、分类详情和标签详情固定每页 12 篇；第一页不生成 `/page/1/`，不生成零长度的后续分页或越界页，没有 taxonomy 时也不生成对应集合根页面。内置 Earth 的 `/categories/`、`/tags/` 按官方源码展示首个分类或标签的前 10 篇文章及“更多文章”入口，而不是另造 taxonomy 卡片目录；完整本地化 taxonomy、说明、封面与公开文章数仍通过主题上下文提供给自定义 collection renderer、搜索索引和 SEO 生成器。归档页在每个静态分页内按站点时区的年/月分组，并保留摘要、分类与标签。

根路径 `/` 只做 302 语言协商。生成顶层 `sitemap.xml`、`robots.txt`、静态搜索数据和回退映射。源语言也必须带前缀。

## 7. 编辑器交互契约

- 顶栏：返回、标题、语言、修订、预览、保存、设置、发布；
- 设置：按 Halo 源码的常规/高级分组组织标题、不可变 slug、分类、标签、摘要、封面、评论、置顶、可见性、发布时间和主题模板；单用户版本不显示 owner；
- 模式：编辑 / 分栏 / 预览；
- 派生语言可展开只读源文，形成源文/译文/预览三栏；
- 粘贴和拖入图片立即显示上传进度，成功后在原光标插入 Markdown；
- 手工保存明确显示“未保存 / 保存中 / 已保存 / 冲突”状态；自动保存与离线恢复仍按 `PROJECT_SPEC.md` 作为待确认产品问题，不在当前实现中预设；
- 首次发布在 Provider 可用时自动为全部启用目标语言排队；没有 Provider 时源语言照常发布；
- 重新翻译包含人工译文时必须出现第二次确认，并逐语言列出覆盖对象。
- 翻译任务按 Markdown 安全边界分段；Provider 超时由后台配置控制，只有断线、408、429、5xx 和无效/空响应会最多退避重试三次，认证或参数错误立即失败。任务在花费额度前和落盘前校验目标修订，确认后出现的新人工编辑不会被覆盖。
