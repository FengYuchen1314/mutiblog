# API 与页面清单

## 1. API 约定

- 管理 API 前缀：`/api/v1/admin`；
- 公开动态 API 前缀：`/api/v1/public`；
- JSON 使用 camelCase；错误响应包含稳定 `code`、本地化 `message`、`requestId` 和可选字段错误；
- 分页为 `page`（从 1 开始）、`size`、`total`、`items`；
- 写操作使用 `If-Match` 或 `revision` 防止覆盖外部修改；
- 长任务返回 `202` 与 `jobId`，通过任务端点查询。

## 2. 初始化与认证

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/setup/status` | 是否已初始化 |
| POST | `/api/v1/setup` | 创建站点、源语言、后台语言与唯一管理员 |
| POST | `/api/v1/auth/login` | 登录并建立会话 |
| POST | `/api/v1/auth/logout` | 注销当前会话 |
| GET | `/api/v1/auth/session` | 当前会话与 CSRF 信息 |
| PUT | `/api/v1/admin/security/password` | 修改密码 |

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
GET    /api/v1/admin/posts/:id/locales
PUT    /api/v1/admin/posts/:id/locales/:locale
POST   /api/v1/admin/posts/:id/translations
```

分类、标签、菜单、友链、附件、评论、站点设置、语言、Provider、翻译任务、主题、备份和系统任务分别拥有独立资源端点。

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
| `/console/comments` | 状态筛选、审核、回复和删除 |
| `/console/attachments` | 本地上传、分组、预览、复制 URL 和删除 |
| `/console/links` | 友链分组和友链管理 |
| `/console/menus` | 菜单集合、嵌套菜单项和排序 |
| `/console/theme` | 当前主题、设置 schema、预览 |
| `/console/themes` | 安装、升级、启用、卸载与重载 |
| `/console/ai/providers` | Provider、Base URL、模型、遮罩 Key、连接测试 |
| `/console/ai/tasks` | 翻译任务、进度、重试、人工译文冲突 |
| `/console/locales` | 源语言、目标语言、框架字典完整度 |
| `/console/settings` | 站点、文章、评论、SEO、代码注入与系统设置 |
| `/console/backup` | 创建、下载、恢复、删除备份 |
| `/console/overview` | 版本、目录、存储、运行与安全信息 |
| `/console/tools` | 重建索引、重建站点、清理缓存、诊断 |

## 6. 公开静态页面

每种启用语言都生成：

```text
/<locale>/
/<locale>/posts/<id>/
/<locale>/pages/<id>/
/<locale>/categories/<id>/
/<locale>/tags/<id>/
/<locale>/archives/
/<locale>/links/
/<locale>/search/
/<locale>/404.html
/<locale>/rss.xml
```

根路径 `/` 只做 302 语言协商。生成顶层 `sitemap.xml`、`robots.txt`、静态搜索数据和回退映射。源语言也必须带前缀。

## 7. 编辑器交互契约

- 顶栏：返回、标题、语言、修订、预览、保存、设置、发布；
- 模式：编辑 / 分栏 / 预览；
- 派生语言可展开只读源文，形成源文/译文/预览三栏；
- 粘贴和拖入图片立即显示上传进度，成功后在原光标插入 Markdown；
- 自动保存有“未保存 / 保存中 / 已保存 / 冲突 / 离线”状态；
- 发布弹窗展示所有目标语言状态；首次发布默认选择全部启用目标语言；
- 重新翻译包含人工译文时必须出现第二次确认，并逐语言列出覆盖对象。
