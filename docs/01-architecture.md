# 01 · 架构与目录结构

## 1. 进程模型

生产环境共 **2 个进程 + 1 个可选反向代理**：

```
┌─────────────────────────────────────────────────────────────┐
│ 容器 blog                                                    │
│                                                             │
│  ┌────────────────────┐        Unix Socket (HTTP/1.1)      │
│  │  blog-server (Go)  │◀──────────────────────────────────┐ │
│  │  PID 1             │  /run/blog-render.sock            │ │
│  │  :8080             │                                   │ │
│  │                    │  spawn + 健康检查 + 自动重启        │ │
│  └────────┬───────────┘                                   │ │
│           │ fork/exec                                     │ │
│           ▼                                               │ │
│  ┌────────────────────┐                                   │ │
│  │ render-worker(Node)│───────────────────────────────────┘ │
│  │ 常驻，空闲 ~60MB    │                                     │
│  └────────────────────┘                                     │
└─────────────────────────────────────────────────────────────┘
                    ▲
                    │ :8080  (/admin/*, /api/*)
┌───────────────────┴─────────────────────────────────────────┐
│ Nginx (可选，生产推荐)                                        │
│  /            → proxy_pass Go (302 语言跳转)                  │
│  /zh-cn/ /en/…→ root generated/public  (直接读盘，不过 Go)     │
│  /media/      → root media                                  │
│  /assets/     → root generated/public/assets                │
│  /admin/ /api/→ proxy_pass Go                               │
└─────────────────────────────────────────────────────────────┘
```

### 1.1 关键点

- **Go 进程是父进程**，负责 spawn / 监控 / 重启 Node 渲染器。
- **渲染器只在发布时被调用**。空闲时不占 CPU。若渲染器不可用，后台仍可正常编辑保存，只是发布任务进入 `render_queue` 等待，前台已有页面不受影响。
- **Nginx 是可选的**。不带 Nginx 时 Go 自己用 `http.FileServer` 提供 `generated/public/`，功能等价，性能略低。Docker 镜像默认**不含** Nginx，`docker-compose.yml` 里提供注释掉的 nginx service。

### 1.2 渲染器生命周期

| 事件 | 行为 |
|---|---|
| Go 启动 | 若 `render.workerEnabled=true`，spawn Node，等待 `GET /health` 返回 200（超时 30s） |
| 渲染请求 | Go 通过 Unix Socket 发 `POST /render`，同步等待 |
| Node 崩溃 | Go 捕获子进程退出，指数退避重启（1s,2s,4s…最大 60s），当前渲染任务标记 `failed` 并重新入队 |
| Node 无响应 | 单次渲染超时（默认 30s）→ 杀死并重启 |
| Go 收到 SIGTERM | 先停止接收新请求 → 等待进行中的渲染任务（最多 15s）→ SIGTERM 给 Node → 退出 |
| 主题热更新（dev） | Go 监听 `themes/*/dist/` 变化 → 通知 Node 清空模块缓存并重载 SSR bundle |

---

## 2. 仓库目录结构（开发态）

```
mutiblog/
├── PLAN.md
├── docs/                              # 本套规格文档
│
├── backend/                           # Go 模块，module name: github.com/<owner>/mutiblog
│   ├── go.mod
│   ├── cmd/
│   │   └── blog/
│   │       └── main.go                # 唯一入口
│   ├── internal/
│   │   ├── app/                       # 依赖装配、生命周期
│   │   ├── config/                    # config.yaml 加载/校验/热重载
│   │   ├── model/                     # 纯数据类型（无依赖，被所有包引用）
│   │   ├── fsutil/                    # 原子写、fsync、安全路径拼接
│   │   ├── content/                   # Markdown/Front Matter/Bundle 读写
│   │   ├── taxonomy/                  # 分类/标签/友链/菜单 YAML 读写
│   │   ├── index/                     # 内存索引
│   │   ├── watcher/                   # fsnotify + debounce + 自写抑制
│   │   ├── render/                    # 渲染编排、依赖计算、worker client
│   │   ├── i18n/                      # locale 规范化与协商
│   │   ├── ai/                        # provider、分段器、翻译器
│   │   ├── media/                     # Storage 接口 + local 驱动 + 图片处理
│   │   ├── theme/                     # 主题发现、settings schema、配置合并
│   │   ├── search/                    # 搜索索引生成
│   │   ├── feed/                      # RSS / Sitemap / robots.txt
│   │   ├── auth/                      # 用户、密码、session、RBAC
│   │   ├── jobs/                      # SQLite 队列、worker pool、调度器
│   │   ├── state/                     # SQLite 连接、迁移、DAO
│   │   ├── events/                    # 事件总线 / Hook 点
│   │   ├── backup/                    # 备份、恢复、导入导出
│   │   ├── api/                       # chi handler（按资源分文件）
│   │   └── httpserver/                # 路由装配、中间件、静态服务
│   ├── web/
│   │   └── embed.go                   # //go:embed admin dist
│   └── testdata/
│
├── frontend/
│   ├── admin/                         # React SPA (后台)
│   │   ├── package.json
│   │   ├── vite.config.ts
│   │   ├── index.html
│   │   └── src/
│   ├── renderer/                      # Node 渲染 worker
│   │   ├── package.json
│   │   ├── src/
│   │   │   ├── server.ts              # Unix socket HTTP server
│   │   │   ├── markdown.ts            # unified 管线
│   │   │   ├── render.ts              # React SSR + island 两遍渲染
│   │   │   └── themeLoader.ts
│   │   └── tsconfig.json
│   └── shared/                        # admin/renderer/theme 共用的 TS 类型
│       └── src/types.ts               # 与 Go model 一一对应，手工同步
│
├── themes/
│   └── default/
│       ├── theme.yaml
│       ├── settings.schema.json
│       ├── package.json
│       ├── vite.config.ts             # 产出 dist/ssr 与 dist/client
│       ├── src/
│       │   ├── entry.ssr.tsx          # 导出 templates 映射
│       │   ├── entry.client.ts        # island 引导
│       │   ├── templates/             # index/post/page/category/tag/archive/links/search/404
│       │   ├── components/
│       │   ├── islands/
│       │   └── styles/
│       └── dist/                      # 构建产物（gitignore，镜像内预构建）
│
├── content/                           # ★ 内容真相源
│   ├── posts/
│   │   └── 2026/
│   │       └── my-server/
│   │           ├── metadata.yaml
│   │           ├── index.zh-cn.md
│   │           ├── index.en.md
│   │           └── index.ja.md
│   ├── pages/
│   │   └── about/
│   │       ├── metadata.yaml
│   │       ├── index.zh-cn.md
│   │       └── index.en.md
│   ├── .drafts/                       # 自动保存草稿，不进 Git（.gitignore）
│   │   └── <article-id>.<locale>.md
│   └── .revisions/                    # 历史版本
│       └── <article-id>/
│           ├── 0001.zh-cn.md
│           └── 0002.zh-cn.md
│
├── data/                              # ★ 结构化真相源
│   ├── categories/
│   │   ├── technology.yaml
│   │   └── linux.yaml
│   ├── tags/
│   │   └── docker.yaml
│   ├── links/
│   │   ├── _groups.yaml               # 友链分组定义与排序
│   │   └── example-blog.yaml
│   ├── menus/
│   │   ├── header.yaml
│   │   └── footer.yaml
│   ├── users/
│   │   └── admin.yaml                 # 权限 0600
│   ├── translations/                  # UI 文案（非文章）多语言字典
│   │   ├── zh-CN.yaml
│   │   └── en.yaml
│   ├── themes/
│   │   └── default.settings.yaml      # 主题设置值（schema 在 themes/ 下）
│   └── state.db                       # ★ SQLite，可删除
│
├── media/                             # ★ 媒体真相源
│   ├── 2026/08/
│   │   ├── cover.png
│   │   └── cover.thumb.png
│   └── .meta/                         # 媒体元数据 sidecar（json）
│
├── config/
│   └── config.yaml                    # ★ 站点配置真相源
│
├── generated/                         # 派生物，可整体删除重建
│   ├── public -> releases/20260809-120000/   # symlink
│   └── releases/
│       └── 20260809-120000/
│           ├── zh-cn/
│           ├── en/
│           ├── assets/
│           ├── sitemap.xml
│           └── robots.txt
│
├── cache/                             # 渲染中间产物、图片缓存
│
├── deploy/
│   ├── Dockerfile
│   ├── docker-compose.yml
│   ├── nginx.conf.example
│   └── .env.example
│
└── .gitignore
```

### 2.1 `.gitignore` 规格

```gitignore
# 派生物
/generated/
/cache/
/backend/bin/
/themes/*/dist/
node_modules/

# 运行状态
/data/state.db
/data/state.db-wal
/data/state.db-shm

# 草稿（可选：团队可选择纳入版本管理）
/content/.drafts/

# 密钥
/config/config.local.yaml
/deploy/.env
```

**注意**：`content/`、`data/`（除 `state.db`）、`media/`、`config/` **必须**保持在版本控制中——这是需求第 69 条 "Git Friendly" 的直接体现。`data/users/*.yaml` 含 Argon2id 哈希（非明文），可以入 Git，但文档中要提醒用户私有仓库。

---

## 3. 运行态目录（生产）

单二进制部署时的最小工作目录：

```
/app/
├── blog-server          # 单二进制，内嵌 admin dist
├── config/config.yaml
├── content/
├── data/
├── media/
├── themes/default/dist/ # 主题构建产物随镜像
├── generated/
└── renderer/            # Node 渲染器（bundle 后的 js + node_modules 最小集）
```

所有路径都从 `config.yaml` 的 `paths` 段读取，默认相对于 `--root` 参数（默认为进程 CWD）。

---

## 4. 启动序列

```
1.  解析 CLI flags (--root, --config, --port, --dev)
2.  加载 config/config.yaml → 校验 → 应用环境变量覆盖（BLOG_ 前缀）
3.  确保目录存在（content/ data/ media/ generated/ cache/）
4.  打开 SQLite（WAL 模式）→ 执行迁移
5.  加载 users / categories / tags / links / menus / theme settings
6.  扫描 content/posts + content/pages → 构建内存索引
      · 并发扫描（GOMAXPROCS 个 worker）
      · 单个文件解析失败 → 记录到 index.Errors，不中断启动
7.  加载主题：读 themes/<active>/theme.yaml + settings.schema.json + dist/manifest.json
8.  spawn Node 渲染器 → 等待 /health
9.  启动 fsnotify watcher（content/ data/ config/ themes/*/dist/）
10. 启动 job workers（render / translate / schedule ticker）
11. 若 generated/public 不存在或 --rebuild → 投递全量重建任务
12. 监听 HTTP :8080
13. 打印启动摘要（文章数/语言数/主题/渲染器状态/监听地址）
```

**启动性能要求**：1000 篇文章 × 4 语言（4000 个 md 文件）冷启动索引构建 < 3s。

### 4.1 关闭序列

```
SIGTERM/SIGINT
  → HTTP server graceful shutdown（15s 超时）
  → 停止接收新 job，等待进行中的 job（15s）
  → flush 未落盘的索引变更（正常情况下没有，因为都是即时落盘）
  → 关闭 Node 渲染器（SIGTERM，5s 后 SIGKILL）
  → 关闭 SQLite（checkpoint WAL）
  → exit 0
```

---

## 5. 数据流：一次完整发布

这是全系统最核心的时序，实现者必须严格遵循。

```
管理员点击「发布」
  │
  ├─1. POST /api/admin/posts/{id}/publish
  │
  ├─2. content 包：
  │     · 读取当前 bundle
  │     · status: draft → published，写 date（若首次发布）、updated
  │     · sourceRevision += 1
  │     · 原子写 index.zh-cn.md
  │     · 快照到 content/.revisions/<id>/000N.zh-cn.md
  │     · 删除对应 .drafts/ 文件
  │
  ├─3. index 包：更新内存索引（不等 fsnotify，直接同步更新）
  │
  ├─4. events 包：发出 ArticlePublished{ID, Locale, IsFirstPublish}
  │
  ├─5. render 包（同步部分）：
  │     · AffectedUnits(ArticlePublished) → []RenderUnit
  │     · 源语言文章页优先级 = High，其余 = Normal
  │     · 入 render_queue（SQLite）
  │
  ├─6. API 立即返回 202 { queued: 12 }         ← 管理员无需等待
  │
  ├─7. render worker pool 消费队列：
  │     · 组装 PageProps（从内存索引读，不碰磁盘）
  │     · POST unix:///render → Node
  │     · Node: markdown→HTML → React SSR → islands → 完整 HTML
  │     · Go: 原子写 generated/public/zh-cn/posts/<slug>/index.html
  │     · 广播 SSE 进度给后台
  │
  ├─8. 全部渲染完成 → events: SiteRendered
  │     · 生成/更新 sitemap.xml、rss.xml、search-index.json
  │     · 调用 CachePurger（MVP 为 Noop）
  │
  └─9. 若 i18n.autoTranslateOnPublish=true：
        · 为每个目标 locale 投递 translation_job
        · 翻译完成 → 写 index.en.md → 触发该 locale 的渲染链（回到步骤 3）
```

### 5.1 时延预算

| 阶段 | 目标 |
|---|---|
| API 返回（步骤 1→6） | < 200ms |
| 源语言文章页可访问 | < 2s |
| 受影响的列表页全部更新 | < 5s（典型 12 个单元） |
| AI 翻译单语言完成 | 10s ~ 90s（取决于文章长度与模型） |

---

## 6. 模块依赖方向

严格单向，禁止循环依赖：

```
                        model  (无依赖)
                          ▲
        ┌────────┬────────┼────────┬─────────┐
      config   fsutil   i18n    events    state
        ▲        ▲        ▲        ▲        ▲
        │     content  taxonomy  media    jobs
        │        ▲        ▲        ▲        ▲
        └────────┴───┬────┴────────┘        │
                   index                     │
                     ▲                       │
        ┌────────────┼───────────┬───────────┤
      render       search      feed         ai
        ▲            ▲           ▲           ▲
        └────────────┴─────┬─────┴───────────┘
                         api  ←  auth, theme, backup
                          ▲
                     httpserver
                          ▲
                         app
                          ▲
                      cmd/blog
```

规则：
- `model` 只含 struct 与常量，不 import 任何内部包。
- `watcher` 只依赖 `fsutil` + `events`，通过事件通知 `index` 重载，不直接 import `index`。
- `api` 通过接口（在 `api` 包内定义）依赖服务，便于测试打桩。

---

## 7. 并发模型

| 组件 | 并发策略 |
|---|---|
| 内存索引 | `sync.RWMutex` 保护整体；读多写少；写操作粒度 = 单个 Article |
| 文件写入 | 每个 bundle 目录一把 `keyed mutex`（`map[string]*sync.Mutex` + 全局锁），防止同一文章并发写 |
| 渲染 worker | 可配置并发数，默认 `min(4, NumCPU)`；同一 RenderUnit 去重（队列唯一索引） |
| 翻译 worker | 默认并发 2，可配；受 AI provider 限流约束，实现令牌桶 |
| Node 渲染器 | 单进程单并发（Node 单线程，React SSR 是 CPU 密集）。Go 侧用带缓冲的信号量串行化到 worker，`render.workerCount` > 1 时 spawn 多个 Node 进程 |
| fsnotify | 单 goroutine 收集 + 300ms debounce 聚合 → 批量投递重载任务 |

### 7.1 自写抑制（关键）

Go 自己写文件也会触发 fsnotify，必须避免"写 → 事件 → 重载 → 又写"的循环。

实现：`watcher` 维护一个 `recentWrites map[string]time.Time`。`fsutil.AtomicWrite` 在 rename **之前**调用 `watcher.Suppress(path, 2*time.Second)`。事件到达时若命中且未过期，则丢弃该事件。

---

## 8. 错误处理与降级

| 故障 | 降级行为 |
|---|---|
| 某个 md 文件解析失败 | 该文章标记 `broken`，不进索引，后台「日志」页可见，其余正常 |
| Node 渲染器不可用 | 发布 API 仍返回 202，渲染任务停留在 `pending`，后台顶部显示告警条 |
| 单个渲染单元失败 | 重试 3 次（退避 2s/8s/30s），仍失败则标记 `failed`，**保留旧 HTML 不覆盖** |
| AI provider 报错/超时 | 翻译任务重试 3 次，失败则 `translations.<locale>.status = failed`，附错误信息 |
| SQLite 损坏 | 启动时检测失败 → 重命名为 `state.db.corrupt.<ts>` → 新建空库 → 投递全量重建 |
| 磁盘写满 | 原子写失败 → 返回 5xx，**绝不产生半个文件**（因为 rename 前就失败了） |
| `generated/` 被删除 | 启动时检测 symlink 失效 → 自动全量重建 |

---

## 9. 可观测性

- **结构化日志**：`log/slog`，JSON 格式（生产）/ 文本（dev）。字段：`ts, level, msg, component, article_id, locale, unit, dur_ms, err`。
- **审计日志**：写 SQLite `audit_log` 表，记录所有写操作（谁/何时/做了什么/目标）。后台「日志」页可查询。
- **指标端点**：`GET /api/admin/system/stats` 返回 goroutine 数、内存、索引大小、队列深度、渲染器状态、最近渲染耗时 P50/P95。
- **SSE 进度流**：`GET /api/admin/events`（需认证）推送渲染/翻译进度，后台实时显示。

---

## 10. 开发模式（`--dev`）

- Admin SPA 由 Vite dev server（:5173）提供，Go 反代 `/admin/*` 到它。
- 主题以 watch 模式构建（`vite build --watch`），产物变化触发渲染器重载 + 全量重建。
- 关闭 HTML 缓存头。
- 日志文本格式 + DEBUG 级别。
- 提供 `make dev` 一键启动（Go air/自实现 watch + 3 个 npm watch）。
