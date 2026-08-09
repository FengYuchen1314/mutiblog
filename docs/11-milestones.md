# 11 · 里程碑、任务分解与验收标准

> 严格串行执行。每个里程碑的**验收标准全部通过**才进入下一个。
> 每个里程碑结束时应能 `git commit` 一个可运行的状态。

### 混沌测试的归属

[docs/12 §14](12-reliability-ux.md) 的 C1~C20 不是 M12 才做的收尾工作，而是分散到各里程碑，**与功能同步实现和验证**：

| 里程碑 | 必须通过的混沌测试 |
|---|---|
| M0 | C7（磁盘满）、C15（state.db 损坏） |
| M1 | C16（文章损坏隔离） |
| M3 | C8、C9、C10（编辑器三层保存、断网、冲突） |
| M5 | C1（渲染器崩溃）、C13（渲染 OOM）、C14（超大代码块） |
| M6 | C2（Go 崩溃回收）、C17（重建中途崩溃）、C19（队列积压） |
| M9 | C3、C4、C5、C6（AI 熔断、挂起、格式错误、取消） |
| M11 | C11、C12（上传边界与中断） |
| M12 | C18（SSE 降级）、**C20（综合压力，总验收）** |

**理由**：可靠性机制事后补是补不上的——三层保存要改编辑器架构，心跳要改队列 schema，熔断要改 provider 调用路径。放到最后做等于返工。

### CLI 命令的归属

同理，[docs/14](14-cli-and-lifecycle.md) 的 CLI 也分散到各里程碑，**不要攒到最后**：

| 里程碑 | 必须交付的命令 | 理由 |
|---|---|---|
| M0 | `version`、`config validate` | 调试地基时就要用 |
| M1 | `index rebuild`、`index errors` | 排查解析问题 |
| M2 | **`admin create`、`admin reset-password`、`admin unlock`、`admin hash-password`** | ★ 密码重置是刚需，做认证的同时就该有。攒到最后意味着中间所有测试环境忘了密码都得删库 |
| M5 | `doctor`（渲染器检查部分） | 渲染器起不来时的第一诊断 |
| M6 | **`rebuild`、`verify`** | ★ verify 必须与 `AffectedUnits` 同批交付，否则无法验证它的正确性 |
| M11 | `backup *`、`import`、`export`、`migrate` | 与对应 API 同批 |
| M12 | `doctor` 完整版 + 灾难恢复手册 | 收口 |

### 确定性渲染

从 **M5 开始**，必须有一条单测：同一篇文章连续渲染两次，断言输出**逐字节相同**。

这是 `verify` 赖以工作的前提（[docs/14 §4.3](14-cli-and-lifecycle.md)）。一旦输出里混入构建时间戳、`Date.now()`、未排序的 map 迭代，`verify` 就永远报差异而失去意义。早测早发现。

---

## M0 · 项目骨架与文件层（2 人日）

### 任务
1. 初始化 `backend/` Go module，目录骨架按 [docs/01 §2](01-architecture.md)。
2. `internal/config`：完整 schema 结构体、YAML 加载、`config.local.yaml` 合并、`BLOG_` 环境变量覆盖、`${VAR}` 插值、校验、默认值填充。
3. `internal/fsutil`：`AtomicWrite`、`AtomicWriteBatch`、`SafeJoin`、`EnsureDir`、`AtomicSymlink`、`HardLinkOrCopy`、`KeyedMutex`、`WriteSuppressor` 接口。
4. `internal/state`：SQLite 打开（WAL）、迁移框架、`0001_init.sql`（全部表）、integrity_check 与损坏恢复。
5. `internal/model`：全部数据类型 + `LocalizedString` 的自定义 YAML 编解码。
6. `cmd/blog/main.go`：flags、slog 初始化、目录创建、优雅关闭骨架。
7. `Makefile`：`build` / `dev` / `test` / `lint` / `api-types`。

### 验收
- [ ] `go build` 通过，`./blog-server --root ./testdata` 启动并打印摘要后正常退出（SIGTERM）
- [ ] `SafeJoin` 的 10 个路径穿越用例全部拦截（[docs/10 §3.2](10-security-ops.md)）
- [ ] `AtomicWrite` 单测：写入过程中 panic 不产生半个文件；tmp 文件被清理
- [ ] 删除 `state.db` 后重启自动重建；手工写入垃圾字节后重启自动重命名并新建
- [ ] 配置：`BLOG_SERVER_PORT=9000` 生效；`${BLOG_AI_API_KEY}` 插值生效；缺 `sessionSecret` 时启动失败并提示

---

## M1 · 内容模型、解析与索引（4 人日）

### 任务
1. `internal/content`：Front Matter 分割/解析/**固定顺序序列化**、Bundle 加载、并发 `ScanAll`、`SaveVersion`、`CreateBundle`、metadata.yaml 读写、权威字段镜像、草稿、修订快照。
2. `internal/taxonomy`：categories/tags/links/menus/users 的 YAML 读写、分类树构建与**环检测**。
3. `internal/index`：全部查询与变更 API、排序视图、slug 索引、分类倒排（**含祖先继承**）、标签倒排、归档索引、`Stats`。
4. `internal/watcher`：fsnotify 递归监听、事件过滤、对象归并、300ms debounce、自写抑制、大批量降级为全量重建。
5. `internal/events`：事件总线 + 全部事件类型定义。
6. `internal/i18n`：`Registry`、`Canonical`、`URLPrefix`、`FromPrefix`。

### 验收
- [ ] 解析 `testdata/` 中的 20 篇多语言文章，索引数量、分类归属、归档分组全部正确
- [ ] Round-trip 测试：解析 → 序列化 → 再解析，字节完全一致（含 `Extra` 未知字段）
- [ ] 分类继承：文章属于 `linux`（父 `technology`），`List{Category:"technology"}` 能查到
- [ ] 分类环（a→b→a）被检测并降级，不死循环
- [ ] 外部编辑 `.md` 文件 → 300ms 内索引更新；`AtomicWrite` 写入**不**触发重载循环
- [ ] `git checkout` 切换分支（300+ 文件变更）→ 触发一次全量重建而非 300 次单文件重载
- [ ] 1000 篇 × 4 语言（正文合计 ~30MB）冷启动索引 < 3s，内存 < 150MB
- [ ] **正文合计 200MB 的数据集**：自动降级为 lazy 模式，进程 RSS < 250MB，列表查询性能不受影响（[docs/03 §4.4](03-backend-modules.md)）
- [ ] `resident` 与 `lazy` 两种模式的查询结果完全一致（同一组断言跑两遍）

---

## M2 · 认证与 API 骨架（3 人日）

### 任务
1. `internal/auth`：Argon2id（PHC 串解析）、签名 Cookie 会话、tokenVersion、RBAC 权限表、dummy hash 防时序攻击。
2. 中间件：`RealIP`、`RequireAuth`、`RequireCSRF`、`RequirePerm`、`APIRateLimit`、`LoginRateLimit`、`SecurityHeaders`、`NoStore`、`Audit`。
3. `/api/auth/*` 全部端点 + 首次安装模式。
4. `internal/httpserver`：路由装配、错误响应统一格式、`RequestID`。
5. `frontend/admin` 初始化：Vite + TS + Tailwind + shadcn/ui + TanStack Router/Query + `api/client.ts`。
6. Admin：登录页、安装向导、Shell 布局（侧边栏 + 顶栏）、空的仪表盘。
7. `backend/web/embed.go` + Go 服务 `/admin/*`。
8. `openapi.yaml` 起始 + `make api-types`。

### 验收
- [ ] 全新环境访问 `/admin/` → 安装向导 → 创建管理员 → 自动登录进入仪表盘
- [ ] [docs/10 §10](10-security-ops.md) 的 S1/S2/S3/S4/S9/S10/S11 全部通过
- [ ] `go build` 后单二进制包含 admin 资源，无外部文件依赖即可提供后台
- [ ] 审计日志记录了登录成功/失败

---

## M3 · 文章与页面（6 人日）

### 任务
1. `/api/admin/posts/*` 与 `/api/admin/pages/*` 全部端点（[docs/07 §3.3](07-http-api.md)）。
2. 冲突检测（`baseHash` → 409）。
3. 草稿 / 手动保存 / 发布 三层语义（[docs/08 §4.4](08-frontend-admin.md)）。
4. 修订快照与回滚。
5. 回收站与永久删除。
6. 定时发布（`scheduled_publish` + 30s ticker）。
7. Admin：文章列表（含语言矩阵列、筛选、批量操作、URL 状态同步）、CodeMirror 编辑器（全部 [docs/08 §4.3](08-frontend-admin.md) 功能）、侧边栏面板、修订页、回收站。

### 验收
- [ ] 新建 → 编辑 → 自动保存 → 手动保存 → 发布 全流程正确，三层语义无混淆
- [ ] 自动保存不修改 `index.<locale>.md`，不 bump `sourceRevision`
- [ ] 两个标签页同时编辑同一文章 → 后保存者收到 409 并可选择处理方式
- [ ] 编辑器：拖拽图片、粘贴图片、快捷键、斜杠命令、列表续行、全屏、字数统计 全部可用
- [ ] 定时发布：设置 2 分钟后发布，到点自动 published
- [ ] 修订回滚后内容正确，且回滚本身也产生一个新修订

---

## M4 · 分类法与媒体（5 人日）

### 任务
1. `/api/admin/{categories,tags,links,menus,media}/*` 全部端点。
2. `internal/media`：`Storage` 接口 + `LocalStorage`、上传流程（MIME 嗅探、SVG 清洗、EXIF 剥离、缩略图）、sidecar 元数据、目录操作。
3. SSRF 防护（`upload-from-url`）。
4. 分类删除的迁移逻辑、标签合并。
5. Admin：分类树（拖拽）、标签管理、友链看板（跨组拖拽）、菜单编辑器（拖拽树）、媒体库（网格/列表/上传队列/选择器模式）。

### 验收
- [ ] 拖拽调整分类层级 → YAML 正确更新，索引与继承关系正确
- [ ] 删除被引用的分类 → 409；指定 `migrateTo` 后文章的 Front Matter 被批量更新
- [ ] 删除标签 → 所有引用文章的 `tags` 字段被移除该标签
- [ ] [docs/10 §10](10-security-ops.md) 的 S5/S6/S7/S8 通过
- [ ] 上传 5MB 图片：生成 thumb/medium，sidecar 正确，媒体库可见
- [ ] 媒体库作为选择器嵌入编辑器可用

---

## M5 · 渲染器打通（5 人日）

### 任务
1. `frontend/renderer`：unix socket HTTP server、`/health`、`/markdown`、`/render`、`/reload`。
2. Markdown 管线（[docs/04 §4.2](04-render-pipeline.md)）：remark/rehype 全插件、Shiki 双主题、KaTeX、Mermaid → island、相对资源重写、TOC/plainText/wordCount 提取。
3. React SSR 两遍渲染 + Island 机制 + 文档外壳组装。
4. `themes/default`：目录结构、`theme.yaml`、`settings.schema.json`、Layout/Header/Footer、**Post 模板**、Prose 样式、Shiki/KaTeX 样式、entry.client + island runtime、Vite 双构建 + manifest。
5. `internal/render`：`WorkerClient`（unix socket）、进程管理（spawn/健康检查/崩溃重启/优雅关闭）、单单元渲染 + 原子写。
6. `POST /api/admin/preview/markdown` 打通。

### 验收
- [ ] 发布一篇文章 → `generated/public/zh-cn/posts/<slug>/index.html` 生成，浏览器打开排版正确
- [ ] [docs/04 §10](04-render-pipeline.md) 的 R1、R2、R8、R10 通过
- [ ] 编辑器预览与发布后的 HTML 正文完全一致
- [ ] `kill -9` Node 进程 → Go 自动重启，日志清晰，发布任务不丢失
- [ ] 无 island 的文章页 HTML 中不含任何 `<script src>`

---

## M6 · 渲染编排（4 人日）

### 任务
1. `AffectedUnits(Change)` 完整实现（[docs/04 §2.1](04-render-pipeline.md) 全部规则，含 `Before` 快照）。
2. `jobs` 队列 + render worker pool + dedupe + 优先级 + 退避重试 + 僵死回收。
3. `render_units` 表状态维护。
4. Release 模式全量重建 + symlink 原子切换 + 旧 release 清理 + 完整性校验。
5. 输出删除（文章删除、slug 变更、语言版本删除）。
6. SSE 进度广播 + Admin 渲染状态页。
7. `CachePurger` 接口 + `NoopPurger`。

### 验收
- [ ] [docs/04 §10](04-render-pipeline.md) 的 R3、R4、R5、R6、R7 通过
- [ ] 修改文章分类：旧分类页与新分类页都被重渲染，其余页面未被触碰（检查 mtime）
- [ ] 全量重建期间持续 `curl` 站点：无一次 404 或半截 HTML
- [ ] 连续 5 次快速发布：`render_units` 中该单元只有一次渲染记录
- [ ] `rm -rf generated/` 后重启 → 自动全量重建，站点完整

---

## M7 · 站点级页面（5 人日）

### 任务
1. 主题模板补齐：Home、Category、CategoryList、Tag、TagList、Archive、Links、Search、NotFound、Page。
2. 分页逻辑（Go 侧计算 `Pagination`，含页码窗口与 URL）。
3. `internal/feed`：RSS 2.0、sitemap（含 hreflang `xhtml:link`）、sitemap index、分片、robots.txt。
4. `internal/search`：per-locale 索引生成、plainText 缓存（内存 + `cache/searchtext/`）、体积护栏。
5. 主题 Search island（MiniSearch，首次输入才 fetch 索引）。
6. 聚合型单元的 250ms 合并窗口。
7. 主题：TOC、TocScrollSpy、CopyCode、Lightbox、BackToTop、ThemeToggle island。

### 验收
- [ ] 全部 11 种页面类型生成正确，链接互通无死链
- [ ] 分页：11 篇文章 perPage=10 → `/zh-cn/` 与 `/zh-cn/page/2/` 正确，页码导航正确
- [ ] RSS 通过 W3C Feed Validator；sitemap 通过 XML schema 校验
- [ ] 搜索：输入关键词能搜到文章，索引文件体积在护栏内
- [ ] [docs/09 §9](09-theme-system.md) 的 H1、H2、H6、H7、H8 通过
- [ ] Lighthouse 文章页：Performance ≥ 95、A11y ≥ 95、SEO 100

---

## M8 · 多语言（4 人日）

### 任务
1. 多语言 bundle 的完整 CRUD（增删语言版本、权威字段镜像与同步刷新）。
2. `LocaleHandler`：`/` 协商（Cookie → Accept-Language → Country → Default）、`matchAcceptLanguage` 完整算法、中文特例、trustedProxy 校验。
3. hreflang 生成（含 x-default、自引用、双向对称、新增语言触发其他语言页重渲染）。
4. 结构化数据（BlogPosting / WebSite / CollectionPage / BreadcrumbList）。
5. 主题 LocaleSwitcher island + 各语言 slug 差异处理 + 缺失语言降级到首页。
6. Admin：语言设置页、UI 文案编辑、协商测试器。
7. 每语言 404 页 + Nginx/Go 的本地化 404 服务。

### 验收
- [ ] [docs/05 §9](05-i18n.md) 的 I1~I10 **全部**通过（特别是 §3.3 的 13 个 Accept-Language 用例）
- [ ] 一篇 3 语言文章：3 个页面的 hreflang 互相指向且各自自引用，含 x-default
- [ ] 新增第 4 个语言版本 → 前 3 个页面自动重渲染，hreflang 更新
- [ ] 各语言 slug 不同时，语言切换器链接正确
- [ ] `/` 响应 302 + `no-store`

---

## M9 · AI 翻译（6 人日）

### 任务
1. `internal/ai/segment.go`：goldmark AST 抽取器、受保护节点、占位符替换、正则保护、区间校验。
2. `internal/ai/provider.go`：OpenAI 兼容客户端、JSON 模式降级、限流、重试、token 统计、`Test()`。
3. `internal/ai/translate.go`：分批、提示词、响应校验、失败拆分重试、回填、结构指纹校验。
4. Front Matter 翻译。
5. 状态机、`recomputeStatus`、人工编辑保护、`SourceDrift`。
6. `translation_tasks` 表 + worker pool + 进度上报 + 取消。
7. `/api/admin/translations/*` 全部端点。
8. Admin：编辑器语言标签栏与状态条、翻译矩阵页、翻译任务页、AI 设置页 + 测试连接。
9. 发布 → 自动翻译 → 渲染 的完整链路打通。

### 验收
- [ ] [docs/06 §9](06-ai-translation.md) 的 **T1~T22 全部通过**（用「魔鬼测试文章」）
- [ ] 中文文章发布后，en/ja/de/zh-TW 的 `.md` 文件生成，人可直接打开编辑
- [ ] 译文渲染出的 HTML 与源语言结构一致（标题层级、代码块、表格）
- [ ] 人工编辑 en 后更新源文章 → en 文件**未被覆盖**，状态显示 `manual · 源已更新 N 版`
- [ ] 模拟 provider 超时/返回错误格式 → 重试与拆分逻辑生效，最终状态正确
- [ ] 翻译过程中的进度在后台实时可见

---

## M10 · 主题系统（3 人日）

### 任务
1. `internal/theme`：发现、`theme.yaml` 解析、`settings.schema.json` 解析与校验、设置合并与存储（只存非默认值）、模板完整性校验。
2. 主题切换流程（校验 → 写配置 → reload 渲染器 → 全量重建）。
3. `/api/admin/themes/*`。
4. Admin：`SchemaForm` 组件（支持 [docs/09 §3.1](09-theme-system.md) 全部字段类型，含 `array`、`i18n-*`、`showIf`）、主题列表页、主题设置页。
5. 主题 i18n 文案合并（`data/translations` > `themes/*/i18n` > 内置）。
6. `themes/README.md` 主题开发文档。

### 验收
- [ ] [docs/09 §9](09-theme-system.md) 的 H3、H4、H5 通过
- [ ] 在 `settings.schema.json` 新增一个 `array` 字段 → 后台表单自动出现，可增删拖拽，保存后主题读到
- [ ] 只有与默认值不同的项被写入 `data/themes/default.settings.yaml`
- [ ] 缺失模板的主题激活时被拒绝并给出明确原因

---

## M11 · 系统功能（4 人日）

### 任务
1. `/api/admin/settings/*`：分 section 读写、**保留注释地写回 config.yaml**（`yaml.Node` 就地修改）、敏感字段掩码、热重载 vs 需重启的区分。
2. `/api/admin/users/*` + 用户管理 UI + 修改密码 + tokenVersion。
3. `internal/backup`：创建（流式 zip、`VACUUM INTO`）、列表、下载（一次性 token）、恢复（含回滚）、保留策略、`excludeSecrets`。
4. 导入器：备份 zip / 批量 md / Hugo TOML front matter / 容错规则 / dry-run 预览 / 导入报告。
5. 导出（scope 可选）。
6. 日志页（system_log + audit_log，筛选分页）+ 日志保留清理任务。
7. 仪表盘完整实现。
8. `/api/admin/system/*`。

### 验收
- [ ] 后台修改站点标题 → `config.yaml` 更新且**原有注释保留**
- [ ] 修改 `server.port` → 提示需重启；修改 `site.title` → 立即生效并触发重建
- [ ] 创建备份 → 下载 → 在全新目录恢复 → 站点完全一致（文章数、分类、媒体、配置）
- [ ] 恢复过程中人为制造失败 → 自动回滚，原数据完好
- [ ] 导入 50 个 Hugo 格式 md → 报告准确，分类标签自动创建，内容可正常渲染
- [ ] [docs/10 §10](10-security-ops.md) 的 S12、S13 通过

---

## M12 · 部署与性能验收（3 人日）

### 任务
1. `deploy/Dockerfile`（三阶段）、`entrypoint.sh`、`docker-compose.yml`、`.env.example`、`nginx.conf.example`。
2. 缓存头实现与验证；预压缩（`.gz`/`.br`）生成（可选优化）。
3. 健康检查端点与容器 healthcheck。
4. 1C1G 资源限制下的完整功能测试与调优参数文档。
5. 压测：静态页面 TTFB、并发、内存峰值。
6. `README.md`：快速开始、配置说明、部署指南、小内存部署、常见问题。
7. 版本号注入与 `--version`。

### 验收
- [ ] `docker compose up -d` 后 60s 内可访问后台，无任何外部服务依赖
- [ ] 镜像体积 < 250MB
- [ ] `docker stats` 空载 < 200MB 内存
- [ ] `--cpus=1 --memory=1g` 限制下全流程（发布 + 翻译 + 全量重建）正常
- [ ] `docker kill blog` 后 Nginx 仍完整提供站点（**PLAN.md 全局验收 #4**）
- [ ] 静态 HTML TTFB < 10ms（本地 Nginx，`curl -w "%{time_starttransfer}"`）
- [ ] [PLAN.md §5](../PLAN.md) 全局验收 1~10 **全部通过**

---

## 第 1.5 阶段：社区功能（M14–M18，+21 人日）

完整规格见 [docs/16](16-social-features.md)。这五项**全部依赖一个能正常发布文章的系统**，因此排在 M13 之后。

| M | 名称 | 人日 | 依赖 |
|---|---|---|---|
| M14 | 原生评论系统 | 8 | M4, M5 |
| M15 | 邮件通知 | 4 | M11 |
| M16 | 友链自助申请 | 2 | M14, M15 |
| M17 | 友链 RSS 聚合（朋友圈） | 4 | M4, M7 |
| M18 | 版本更新与升级链 | 3 | M13 |

**两条排期建议**：
1. **M18 的"检查+通知"部分（约 1 人日）建议提前到 M12**——第一批用户装上后立刻就需要知道有新版本，这个价值随时间递增。完整的 `self-update` 可以留后。
2. **M14+M15 应该连着做**——回复通知邮件是评论体验的一半，只做评论不做邮件是残缺的。

## 第二阶段路线图（不在 MVP 范围）

按优先级排列，每项都应能在不重构核心的前提下增量加入。

| 优先级 | 功能 | 接入点 |
|---|---|---|
| P0 | **Cloudflare 缓存刷新** | 实现 `CachePurger` 接口，配置已预留 |
| P0 | **S3 / R2 / MinIO 存储** | 实现 `Storage` 接口，配置已预留 |
| ~~P0~~ | ~~评论管理~~ | **已升级为第 1.5 阶段 M14**（原生评论系统），见 [docs/16 §2](16-social-features.md) |
| P1 | **Revision 可视化 diff** | 数据已在 `.revisions/`；只需前端 diff 组件 |
| P1 | **Webhooks** | 订阅 `events.Bus`，配置 URL + 签名 |
| P1 | **API Token** | 复用 auth 层，增加 token 表与 Bearer 中间件 |
| P1 | **文章密码保护** | 需要运行时判断 → 用边缘中间件或独立的加密 HTML 方案 |
| P2 | **AI 术语表 / 翻译记忆** | 在 `ai.Segment` 前后插入术语替换层 |
| P2 | **AI 摘要 / 标签建议 / SEO 建议** | 复用 `Provider` 接口 |
| P2 | **Custom CSS / JS 注入** | 主题设置已有 `code` 字段类型 |
| P2 | **Analytics（自托管轻量统计）** | 独立 island + Go 端点 + SQLite 表 |
| P2 | **主题市场 / 在线安装** | `theme.Manager` 增加下载与校验 |
| P3 | **插件运行时** | 事件总线与 Hook 点已就绪；需设计 WASM 或子进程沙箱 |
| P3 | **MCP Server** | 暴露内容 API 给 AI 客户端 |
| P3 | **多作者协作 / 工作流** | RBAC 已预留 author/translator 角色 |
| P3 | **WebP / AVIF 转码** | 等待成熟的纯 Go 编码器，或接受可选外部二进制依赖 |
| P3 | **分片搜索索引（Pagefind 风格）** | 大站点（>2000 篇）时替换当前单文件索引 |

---

## 跨里程碑的持续要求

以下不是某个里程碑的任务，而是**每个 PR 都要满足**的：

1. **测试**：核心逻辑（解析、序列化、索引、依赖计算、locale 协商、AI 分段）必须有单元测试。目标覆盖率：`content`/`index`/`i18n`/`ai` 包 ≥ 70%。
1b. **超时自检**：任何新增的超时值都要进 `internal/config/timeouts.go` 的常量组，并被 `validateTimeoutOrdering()` 覆盖（[docs/12 §2](12-reliability-ux.md)）。外层超时小于内层是静默故障的主要来源。
2. **`go vet` + `staticcheck` 零告警**。
3. **无 TODO 遗留**：确实要留的写成 GitHub issue，不留在代码里。
4. **错误处理**：不吞错误；所有 `err` 要么处理要么带上下文包装（`fmt.Errorf("...: %w", err)`）。
5. **日志**：关键路径有结构化日志，但不刷屏（渲染每个单元一条 debug，完成一条 info）。
6. **不引入新的运行时依赖**（数据库、消息队列、缓存服务）。
7. **每个里程碑结束更新 `README.md` 的功能进度表**。
