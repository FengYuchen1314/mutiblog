# Halo 实现研究与 MutiBlog 对照

研究基线：Halo 官方仓库 `halo-dev/halo`，提交 `815292f426d8f216d02398a2813dc9a52d881775`。只研究产品语义和架构模式，不复制 Halo 源码，不改变 MutiBlog 已确认的 Markdown/YAML 真相源、单站点单管理员、多语言与半静态边界。

VPS 上的只读参考仓库位于 `/opt/reference/halo`。

## 1. 最值得复用的四套机制

### 1.1 资源的 `metadata / spec / status`

Halo 用 Extension 表达文章、主题、设置、备份等资源：

- `metadata` 保存稳定名称、版本、标签、注解、创建/删除时间；
- `spec` 保存用户期望状态；
- `status` 保存系统观察到的阶段、条件和诊断；
- API 写入期望状态，Reconciler 异步完成副作用并更新状态；
- finalizer 保证资源清理完成前不会从存储中消失；
- label selector 与 field selector 进入索引，列表筛选和分页不依赖前端全量过滤。

参考实现：

- `application/.../extension/ReactiveExtensionClientImpl.java`
- `application/.../extension/controller/DefaultControllerManager.java`
- `application/.../core/reconciler/ThemeReconciler.java`
- `docs/index/README.md`

MutiBlog 不引入 Halo Extension Store，也不把 SQLite 变成真相源；但任务、主题、备份、翻译和发布都采用同样的“期望状态 + 阶段 + 条件 + 可重试协调”语义。SQLite 只做可删除投影，所有资源仍可从文件重建。

### 1.2 文章的 base / head / release 三指针快照

Halo 的文章内容不是简单覆盖：

- `baseSnapshot` 是第一份完整内容；
- `headSnapshot` 是当前编辑头；
- `releaseSnapshot` 是公开版本；
- 当 `head == release` 时继续编辑会先创建新快照，公开内容不会被草稿覆盖；
- 发布把 `release` 指向 `head`，取消发布只改变发布期望，不销毁快照；
- 恢复历史不是把历史记录搬回去，而是基于历史内容创建新 head，因此历史仍是追加式；
- 乐观版本冲突会创建新的 head，并对更新做有限退避重试；
- 回收站先设置 `deleted`，最终删除与快照清理由协调器完成。

参考实现：

- `application/.../content/AbstractContentService.java`
- `application/.../content/impl/PostServiceImpl.java`
- `application/.../content/impl/SnapshotServiceImpl.java`
- `ui/console-src/modules/contents/posts/PostSnapshots.vue`

MutiBlog 应把这个模型映射为文件：每个内容实体保存稳定 `baseRevision / headRevision / releaseRevision`，修订为不可变 YAML/Markdown 副本。发布静态站只读取 release；恢复修订创建新修订；回收站不立即破坏文件。

### 1.3 主题包、声明、设置值和运行状态分离

Halo 主题系统不是一个“上传 ZIP”按钮，而是多个对象协作：

- 主题目录保存代码和资源；
- Theme 资源保存声明与期望状态；
- Setting 保存动态表单 schema；
- ConfigMap 保存设置值，升级时明确不覆盖原 `configMapName`，避免用户配置丢失；
- Reconciler 加载白名单内的主题资源、建立默认配置、检查系统版本和页面布局，更新 `READY/FAILED` 状态、截图和 UI bundle 地址；
- 删除使用 finalizer，依次清理缓存、设置、注解设置和主题文件；
- 控制台把“当前主题详情/设置”和“主题列表管理”分层，列表管理再分已安装、本地上传、远程下载和未安装；
- 主题详情提供升级、导入/导出配置、重载声明、清缓存、重置配置；
- 列表项提供选择、预览、启用、卸载与失败诊断；
- 动态设置按 group 分路由，带未保存离开保护和吸底保存按钮。

参考实现：

- `application/.../theme/service/ThemeServiceImpl.java`
- `application/.../core/reconciler/ThemeReconciler.java`
- `application/.../theme/endpoint/ThemeEndpoint.java`
- `ui/console-src/modules/interface/themes/ThemeDetail.vue`
- `ui/console-src/modules/interface/themes/ThemeSetting.vue`
- `ui/console-src/modules/interface/themes/components/ThemeListModal.vue`

Halo 当前升级实现删除旧目录前仍有 `TODO Create backup before deleting`。MutiBlog 不照搬这一点：主题升级必须保留旧包，只有新包通过完整 React SSR 静态构建后才提交；失败时同时保留旧主题目录、旧设置和旧公开 release。

### 1.4 备份恢复是异步状态机，不是长请求

Halo 创建 Backup 资源后由 `BackupReconciler` 推进 `PENDING → RUNNING → SUCCEEDED/FAILED`，记录开始/完成时间、文件名、大小和失败原因，并用 finalizer 清理归档。备份包含 Extension 数据流和工作目录，排除数据库、索引、日志、已有备份、缩略图、开发目录等可重建内容。

恢复流程在临时目录解包，先恢复 Extension Store 事务，再覆盖工作目录；控制台在恢复完成后触发重启并轮询健康状态。恢复页面支持本地上传、远程 URL 和服务器已有备份，并在执行前强警告。

参考实现：

- `application/.../migration/impl/MigrationServiceImpl.java`
- `application/.../migration/BackupReconciler.java`
- `ui/console-src/modules/system/backup/tabs/Restore.vue`
- `docs/backup-and-restore.md`

MutiBlog 的边界更严格：

- 使用白名单打包，永远排除 API Key、会话秘密、SQLite、任务状态和 generated；
- 主题包、主题设置、媒体原件、评论、内容和修订必须包含；
- 恢复前自动创建安全备份；
- 先在 staging 解包并验证路径、manifest、哈希和数据引用，再原子替换永久目录；
- 恢复后重新生成秘密、SQLite 投影与静态站，并使旧会话失效；
- 任何阶段失败都保留恢复前数据和公开 release。

### 1.5 菜单保存目标引用，渲染状态由协调器解析

Halo 的菜单项不只保存一个最终 URL：

- 自定义链接直接写入 `spec.displayName / spec.href`；
- 文章、页面、分类和标签使用有类型的 `spec.targetRef`；
- `MenuItemReconciler` 把目标资源解析成 `status.displayName / status.href`；
- 目标更新或删除时，事件会请求相关菜单项重新协调；
- 新层级模型使用 `spec.menuName / spec.parent`，控制台单独提交父级移动位置，避免编辑普通字段时意外重排整棵树；
- 控制台创建时允许选择自定义链接或具体资源，编辑引用型项目时固定引用种类，只调整目标、父级、打开方式等安全字段。

参考实现：

- `api/.../core/extension/MenuItem.java`
- `application/.../core/reconciler/MenuItemReconciler.java`
- `ui/console-src/modules/interface/menus/components/MenuItemEditingModal.vue`

MutiBlog 当前已经有层级、排序、打开方式、多语言标签、循环与悬空父级校验，但仍只把内部地址保存成原始 URL。若引入持久 `targetRef` 会改变永久 YAML 协议，需要在后续数据格式确认时一并决定；在确认前不能偷偷迁移现有菜单文件。

### 1.6 附件和评论同样区分期望字段与观察字段

Halo 附件的 `spec` 保存展示名、分组、存储策略、所有者、媒体类型、大小和标签，`status` 保存最终公开地址与缩略图。MutiBlog 固定为本地存储、单管理员，因此不需要复制存储策略和所有者模型，但 Halo 式附件分组、标签和统一选择器仍是尚未补齐的产品体验。

Halo 评论协调器会维护 finalizer、回复清理、未读回复数、同一主题的总评论/已审核计数及 `observedVersion`。MutiBlog 已确认的边界只要求评论动态加载、YAML 真相源和故障局部降级；回复层级、管理员回复、通知与计数展示仍属于 `PROJECT_SPEC.md` 的评论待讨论项，不能因研究 Halo 就自行启用。

参考实现：

- `api/.../core/extension/attachment/Attachment.java`
- `application/.../core/reconciler/CommentReconciler.java`

## 2. 后台交互必须对齐的细节

Halo 控制台把路由、菜单、权限、搜索元数据作为模块声明的一部分。虽然 MutiBlog 没有多人权限，仍应保留以下产品行为：

- 列表筛选、分页、排序和关键词写入 URL，刷新可恢复；
- 文章列表区分公开性、发布状态、分类、标签和贡献者筛选；MutiBlog 去掉贡献者，加入语言完整度和翻译状态；
- 批量发布、取消发布、移入回收站和批量设置均先二次确认，并限制并发批次；
- 异步发布、删除或调度中的条目短间隔轮询，稳定后停止轮询；
- 设置抽屉支持上一条/下一条，不强迫返回列表；
- 快照页明确标识 base、head、release，禁止删除 base 和当前 release；
- 当前主题页面显示版本、作者、兼容要求、存储位置、运行阶段和诊断；
- 主题管理器使用大弹窗/独立层，而不是把所有安装操作堆在当前主题设置表单内；
- 危险操作必须有具体对象名、影响说明和确定/取消按钮。

## 3. 明确不采用的 Halo 实现

- 不采用 Java、Spring WebFlux、R2DBC 或 Halo Extension Store；
- 不采用 Thymeleaf 主题运行时；
- 不采用插件市场或通用插件机制；
- 不采用多用户、角色和贡献者模型；
- 不把数据库、内存索引或生成 HTML 当真相源；
- 不采用 Halo 当前主题升级的先删目录策略；
- 不让主题切换在未通过完整静态构建时改变公开站点；
- 不让恢复过程直接覆盖永久目录而没有预恢复备份和 staging 验证。

## 4. 对 MutiBlog 的直接实现清单

### 主题

- [x] 本地 ZIP 安装与同 ID 升级
- [x] 归档路径、符号链接、数量和大小校验
- [x] React SSR 导出契约校验
- [x] 活动主题切换/升级失败回滚
- [x] JSON Schema 设置、默认值、保存与重置
- [x] 当前主题详情与主题列表管理分层
- [x] 远程 URL 安装（仅公网 HTTPS，限制重定向、时间和体积）
- [x] 主题包本地截图声明、安装校验与后台展示
- [x] 主题 API 兼容版本与结构化状态条件
- [x] 不切换公开站点的短期隔离预览 release
- [x] 配置 JSON 导入/导出；卸载主题包保留设置，重装同 ID 后继续生效
- [x] 设置重载声明（静态架构固定为完整 rebuild，旧包省略时采用同一安全默认）
- [x] 文章/页面自定义模板（manifest 声明、后台选择、SSR 路由与跨主题回退；分类模板后续随分类主题协议补充）

### 内容与修订

- [x] 为文章和页面落地显式 base/head/release 三指针，并迁移旧 meta
- [x] 发布后编辑只更新 head，不改不可变 release
- [x] 取消发布、回收站、恢复、永久删除状态机
- [x] 修订列表、双栏比较、恢复为新 head 和不可删除保护
- [x] URL 可恢复的筛选/分页/排序与批量发布、下线、回收、恢复和删除

### 备份与恢复

- [x] 白名单备份并排除秘密和可重建数据
- [x] 创建、列出、下载和删除
- [x] 创建与本地/远程导入使用持久 Backup 任务状态机、失败诊断与安全启动重试；同步事务恢复也记录任务结果
- [x] 本地上传与服务器已有备份两种恢复入口（导入归档先校验、列入备份，再由管理员确认恢复）
- [x] 远程公网 HTTPS URL 导入入口（SSRF 防护、重定向/时间/体积限制；校验后再明确恢复）
- [x] 恢复前安全备份、staging 验证、构建门禁、原子替换与失败回滚
- [x] 恢复保留并核对本机秘密，重建索引和静态站，轮询健康门禁，失败同步回滚数据与公开 release，成功后清空会话

### 文件投影与协调

- [ ] 文件资源统一 `spec/status/conditions` 语义
- [x] 外部 Markdown/YAML 变更检测与 SQLite 重建
- [x] 发布、翻译、主题安装与备份任务启动恢复；静态启动构建有限退避
- [x] 可重建索引的 ready/version/完整性检查

### 官方实现核对后仍存在的产品差异

- [ ] 分类自定义主题模板：说明书已经确认主题为文章、页面和分类提供模板，当前协议只覆盖文章和页面。
- [ ] 菜单资源引用：当前仅保存内部/外部 URL；是否增加持久 `targetRef` 需与永久 YAML 格式一起确认。
- [ ] Halo 式附件分组、标签和统一媒体选择器：本地存储与单管理员边界不变，但产品体验尚未完整。
- [ ] 评论回复、管理员标识、通知和更细的审核语义：仍属于明确待讨论范围。
- [ ] 文件资源统一 `spec/status/conditions`：仍是架构待确认项，不以数据库资源模型替代文件真相源。

以上清单是 Halo 行为对齐基线；当它和 `PROJECT_SPEC.md` 冲突时，以项目说明书和已确认对话为准。
