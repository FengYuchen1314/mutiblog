# Mutiblog

一个 Go 单二进制的自托管博客系统。把 **Markdown 文件当数据库**，在**发布那一刻**把内容预渲染成静态 HTML 落盘，并用 AI 把源语言文章自动翻译成其他语言的 Markdown 再各自渲染。访客访问的永远是纯静态文件，不经过 CMS。

> **Write Dynamic, Read Static.**
> 后台像 Halo，内容像 Hugo，前台性能像静态站，多语言由 AI 原生完成。

---

## 快速开始

在服务器上用预构建镜像启动（首次启动会自动生成会话密钥到 `config/.secrets.yaml`）：

```sh
docker compose up -d
```

打开 `http://localhost:8080/admin/` 创建第一个管理员。首次启动会从内置模板生成 `config/config.yaml`。**发布前记得把 `server.baseURL` 改成正式域名**——改完系统会自动全量重建。

> ⚠️ **不要在 1GB 内存的服务器上构建镜像**（前端打包峰值超过 1GB，必然 OOM）。
> 本地开发（≥4GB 内存）用：
> ```sh
> docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build
> ```

### HTTPS

推荐 Caddy（自动申请续期证书）：

```sh
BLOG_DOMAIN=blog.example.com docker compose -f docker-compose.yml -f deploy/compose.caddy.yml up -d
```

或用内置的 Nginx 静态边缘：

```sh
docker compose -f docker-compose.yml -f deploy/compose.nginx.yml up -d
```

两种方案都挂载 `generated/` **父目录**，让代理跟随每次原子切换的 `public` release，并且**在 CMS 进程停止时已发布页面依然可访问**。

---

## 当前状态

| 模块 | 状态 | 说明 |
|---|---|---|
| **Go 后端** | 🟢 可用 | 24 个包，构建通过，测试全绿 |
| 内容存储 / 索引 / 文件监听 | 🟢 可用 | 含 lazy body 模式、原子写、修订 |
| 认证 / CSRF / 限流 / RBAC | 🟢 可用 | Argon2id + 签名 Cookie |
| REST API | 🟢 可用 | 文章 / 分类法 / 媒体 / 主题 / 设置 / 备份 / 翻译 / 系统 |
| CLI | 🟢 可用 | `doctor` `verify` `rebuild` `migrate` `admin *` `backup *` `import` `export` |
| i18n（协商 / hreflang） | 🟢 可用 | Cookie → Accept-Language → IP → 默认 |
| AI 翻译 | 🟡 部分 | 分段器 / provider / 队列已有；**缺熔断器** |
| Markdown 渲染 | 🟡 部分 | Shiki + KaTeX + GFM 已有；**缺 sanitize、图片尺寸、TOC 元数据** |
| **主题系统** | 🔴 待建 | **只有 13 行的 dist 占位，无源码** |
| **Islands（交互组件）** | 🔴 待建 | **无客户端运行时，Mermaid 图表不显示** |
| **管理后台 UI** | 🔴 部分 | 有登录 / 文章 / 编辑器 / 媒体 / 设置；**缺分类、标签、菜单、友链、多语言矩阵** |
| 搜索 | 🔴 待建 | 当前是子串匹配，非真索引 |
| 评论 / 邮件 / 友链申请 / RSS 聚合 | ⚪ 未开始 | 第 1.5 阶段，见 `docs/16` |

**详细对账见 [`docs/17-spec-vs-reality.md`](docs/17-spec-vs-reality.md)。**

---

## 文档导航

| 你想做什么 | 读哪份 |
|---|---|
| **接手开发** | [`docs/17`](docs/17-spec-vs-reality.md) 真实契约与偏差 → [`TASKS.md`](TASKS.md) 任务表 |
| 了解整体架构 | [`PLAN.md`](PLAN.md) → [`docs/01`](docs/01-architecture.md) |
| 开发主题 | [`themes/README.md`](themes/README.md) |
| 理解文件格式 | [`docs/02`](docs/02-content-format.md) |
| 排查故障 | [`docs/14 §6`](docs/14-cli-and-lifecycle.md) 灾难恢复手册 |
| 部署踩坑 | [`docs/13`](docs/13-first-run-walkthrough.md) 30 个会翻车的地方 |

⚠️ **`docs/01`~`docs/16` 描述的是设计意图，不是现状。** 动工前先读 `docs/17`。

---

## 运维命令

```sh
docker compose exec blog /app/blog-server doctor
docker compose exec blog /app/blog-server admin list
docker compose exec blog /app/blog-server rebuild
docker compose run  --rm blog /app/blog-server --root /app verify
docker compose exec blog /app/blog-server backup create
docker compose exec blog /app/blog-server config get site.title
docker compose exec blog /app/blog-server config set site.title '"My Blog"'
```

重置密码（隐藏输入）：

```sh
docker compose exec blog /app/blog-server admin reset-password admin
```

非交互场景：

```sh
printf '%s\n' 'a-new-long-password' | docker compose exec -T blog /app/blog-server admin reset-password admin --stdin
```

迁移与导入导出：

```sh
docker compose run --rm blog /app/blog-server --root /app migrate --dry-run
docker compose run --rm blog /app/blog-server --root /app export /app/backups/site-export.zip --scope content,data,media,config
docker compose run --rm -v "$PWD/import:/import:ro" blog /app/blog-server --root /app import /import --dry-run
```

恢复备份（**先停服务**）：

```sh
docker compose run --rm blog /app/blog-server --root /app backup restore backups/backup-YYYYMMDD-HHMMSS.zip --yes
```

---

## 故障速查

| 症状 | 处理 |
|---|---|
| 忘记密码 | `blog-server admin reset-password admin` |
| 登录后被踢回登录页 | `cookieSecure` + HTTP 所致，`blog-server doctor` 会直接指出 |
| 页面内容陈旧 | `blog-server verify --fix` |
| `generated/` 被删 | 重启即自动全量重建，或 `blog-server rebuild` |
| `state.db` 损坏 | 直接删除，重启自动新建（只丢队列与日志，内容不受影响） |
| 发布了前台没变 | ① 浏览器缓存 ② 反代挂载了 symlink 而非父目录 |

完整手册见 [`docs/14 §6`](docs/14-cli-and-lifecycle.md)。

---

## 数据可携带

内容的真相源就是这四个目录：

```sh
tar czf my-blog-backup.tar.gz content/ data/ media/ config/
```

这就是完整的博客。不需要导出功能、不需要系统在线、不需要任何工具。
`generated/` 和 `cache/` 是派生物，删掉可以从上面四个目录完整重建。

`export` 产出与备份相同的带校验和 ZIP，但支持 `--scope` 选取子集；它拒绝覆盖已存在的归档。

`import` 接受单个 Markdown 文件、目录或 ZIP，识别 YAML 与 Hugo TOML Front Matter。**总是先跑 `--dry-run`**：它会报告将要生成的 slug 与状态而不写入。无 Front Matter 的文件按文件名与修改时间转成草稿；slug 冲突自动加数字后缀；未知的分类/标签显示名会被创建为可复用的分类法条目。

---

## 开发

```sh
cd backend && go build ./... && go test ./...   # 后端
cd frontend/admin && pnpm install && pnpm build # 管理后台
cd themes/default && npm install && npm run build # 主题（T2 源码化后）
```

**代码规范**：单行 ≤ 120 字符、一个文件只做一件事。详见 [`TASKS.md §二`](TASKS.md)——当前代码里有单行 5000+ 字符的历史遗留，正在清理。

**测试夹具**：`backend/testdata/fixtures/devil-test.zh-cn.md` 包含 22 类边界元素，同时用于渲染验收与 AI 翻译验收，判据表见同目录 README。

---

## 技术栈

```
后端    Go + chi · 单二进制 · CGO_ENABLED=0
状态    SQLite（仅任务队列/日志等运行状态，可删除）
渲染    Node 常驻进程 · unified/remark/rehype · Shiki · KaTeX · React SSR
前端    React 19 + TypeScript + Vite
AI      OpenAI 兼容 API，供应商不锁定
```

**不需要**：PostgreSQL、MySQL、MongoDB、Redis、Elasticsearch、Kubernetes。
