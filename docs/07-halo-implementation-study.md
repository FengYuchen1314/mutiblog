# Halo 实现研究与 MutiBlog 对照

研究基线：Halo 官方仓库 `halo-dev/halo`，提交 `815292f426d8f216d02398a2813dc9a52d881775`。只研究产品语义和架构模式，不复制 Halo 源码，不改变 MutiBlog 已确认的 Markdown/YAML 真相源、单站点单管理员、多语言与半静态边界。

VPS 上的只读参考仓库位于 `/opt/reference/halo`。

默认主题的独立官方源码以 `halo-dev/theme-earth` 标签 `v1.17.1`、提交 `6e378ed2eba4b2fe469442e1352371ccf5ee4cea` 为本轮核对基线；只读副本位于 VPS `/tmp/mutiblog-halo-theme-earth-v1.17.1`。研究入口是主题包的 `settings.yaml` 与消费这些值的模板/脚本，不使用主题截图推断需求。

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

Halo 官方 Earth v1.17.1 的 `settings.yaml` 实际包含 `layout / global / style / post / sidebar / footer / beian / plugin` 八组。与 MutiBlog 默认主题直接相关的源码能力包括：三/双/单列文章列表、首页顶部模块及背景、内容页顶部背景、文字/图片 Logo、返回顶部、默认配色及访客切换、正文风格和标题位置、封面高度、分享入口、可排序侧栏组件与站点资料、两种页脚布局、页脚 Logo/标题/标语/菜单/社交链接及自定义版权。备案和插件集成属于 Halo 生态边界，不为了数量照搬；MutiBlog 的 Earth 必须至少完整覆盖布局、全局、样式、文章、侧栏和页脚，并确保每个暴露字段都被 SSR 或公开脚本真实消费。

Earth 源码中的 `popular-posts` 不是置顶文章的别名，而是按 `stats.visit` 降序并展示访问数；`profile` 同时展示公开文章、公开文章实际引用的分类、已审核评论和访问总数。MutiBlog 因此把 Post/Page 详情访问记录为独立 YAML 聚合计数，并由公开只读统计 API 按请求语言返回热门文章及 profile 聚合。永久文件只保存计数、内容种类/ID 和基于创建时间的不可变内容 identity，不保存原始 IP 或任何地址哈希；地址 HMAC 只存在于严格有界、过期淘汰且满载拒绝的进程内防刷窗口。统计响应按已启用语言做 15 秒有界合并缓存，访问量允许短暂最终一致；评论文件异常只令评论数显示不可用，公开静态正文、其他 profile 数字和上一份静态 release 不受影响。文章/Page 永久删除会同步清理计数，备份恢复会打包、staging 校验并原子替换 `visits` 根。

Earth 的首页、分类、标签和归档都消费 Halo 提供的分页对象；首页与分类页顶部使用分类树筛选，子分类通过下拉树展开，标签页使用带文章数的标签筛选。`categories.html` / `tags.html` 不是 taxonomy 卡片目录，而是默认选择首个分类或标签、展示其前 10 篇并提供“更多文章”入口；归档按年/月分组，同时保留分类、标签和摘要。MutiBlog 将首页、归档和 taxonomy 详情固定为每页 12 篇的安全静态分页，第一页保持 canonical 基础路径、第二页起生成 `/page/N/`，并把完整 `allPosts` 与当前页 `posts` 分开，确保分页不会缩短侧栏、profile 或首页筛选数据。文章详情在构建时为 Markdown h1–h4 生成稳定锚点与安全目录数据，桌面侧栏优先展示目录、小屏只保留目录；并按同一公开排序提供上一篇/下一篇导航。分类/标签/归档/友链及集合页全部复用 Earth 的 Sidebar 布局设置；搜索和 404 保持独立。

MutiBlog 对两项服务端主题能力采用明确的安全收紧，而不是假装与 Halo 等价：社交图标从随包内置且经过设置校验的常用平台集合选择，不在公开页加载 Iconify/CDN；自定义版权按纯文本转义渲染，不执行管理员粘贴的任意 HTML。其余 Earth 设置仍按对应源码语义实现；封面高度接受经过范围校验的 CSS 长度并兼容早期三档值。

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
- 恢复后保留评论签名等非 Provider 本机秘密，清空全部 AI Provider API Key，重建 SQLite 投影与静态站，并使旧会话失效；
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

Halo 评论协调器会维护 finalizer、回复清理、未读回复数、同一主题的总评论/已审核计数及 `observedVersion`。MutiBlog 已确认的边界只要求评论动态加载、YAML 真相源和故障局部降级；Earth profile 现在只消费全站公开内容对应的已审核评论聚合数。回复层级、管理员回复、通知与更细的单主题计数语义仍属于 `PROJECT_SPEC.md` 的评论待讨论项，不能因研究 Halo 就自行启用。

参考实现：

- `api/.../core/extension/attachment/Attachment.java`
- `application/.../core/reconciler/CommentReconciler.java`

### 1.7 文章设置来自源码字段，而不是视觉猜测

本轮在 VPS 的只读参考仓库中逐行核对了 `ui/console-src/modules/contents/posts/components/PostSettingModal.vue` 与 `PostEditor.vue`。Halo 的文章设置源码明确分为：

- 常规：标题、slug、分类、标签、自动生成摘要/手工摘要、封面；
- 高级：作者、允许评论、置顶、公开/私有可见性、发布时间、自定义主题模板；
- 扩展注解：由扩展点按资源类型注入动态表单；
- 发布按钮会根据发布时间区分立即发布与定时发布，已发布内容提供取消发布。

MutiBlog 是单站点单管理员，不实现作者选择；也没有通用插件注解机制，不伪造扩展表单。其余字段应映射到文件真相源、发布快照和静态构建，且继续遵守本项目已确认的固定永久 ID 规则。这里的字段清单只来自 Halo 源码，未使用 Halo 页面截图或视觉复刻作为需求来源。

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
- [x] 文章、页面与分类自定义模板（manifest 声明、后台选择、SSR 路由、活动主题校验与跨主题回退）
- [x] Earth `popular-posts` 按真实访问量降序、profile 四项公开统计与 Post/Page 详情侧栏
- [x] 匿名访问聚合计数、严格有界防刷、动态统计故障局部降级及备份恢复/永久删除全链

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
- [x] 恢复保留并核对评论签名等非 Provider 本机秘密，主动清空全部 AI Provider API Key，重建索引和静态站并轮询健康门禁；失败同步回滚数据与公开 release，成功后清空会话并要求管理员重新填写 Key

### 文件投影与协调

- [ ] 文件资源统一 `spec/status/conditions` 语义
- [x] 外部 Markdown/YAML 变更检测与 SQLite 重建
- [x] 发布、翻译、主题安装与备份任务启动恢复；静态启动构建有限退避
- [x] 可重建索引的 ready/version/完整性检查

### 官方实现核对后仍存在的产品差异

- [ ] 菜单资源引用：当前仅保存内部/外部 URL；是否增加持久 `targetRef` 需与永久 YAML 格式一起确认。
- [ ] Halo 式附件分组与标签：文章/页面、站点 Logo、分类、友链及主题图片字段已共用本地媒体选择器；附件分组和标签仍待统一设计。
- [ ] 评论回复、管理员标识、通知和更细的审核语义：仍属于明确待讨论范围。
- [ ] 文件资源统一 `spec/status/conditions`：仍是架构待确认项，不以数据库资源模型替代文件真相源。

以上清单是 Halo 行为对齐基线；当它和 `PROJECT_SPEC.md` 冲突时，以项目说明书和已确认对话为准。
