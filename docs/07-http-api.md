# 07 · HTTP 路由与 REST API 规格

---

## 1. 路由总表

```go
r := chi.NewRouter()

// ── 全局中间件
r.Use(middleware.RealIP(trustedProxies))
r.Use(middleware.RequestID)
r.Use(middleware.Logger)
r.Use(middleware.Recoverer)
r.Use(middleware.Timeout(60 * time.Second))
r.Use(SecurityHeaders)

// ── 公开：语言协商（唯一动态公开端点）
r.Get("/", localeHandler.ServeRoot)

// ── 公开：静态站点（serveStatic=true 时）
r.Mount("/media", mediaFileServer)          // 从 media/ 直服
r.Mount("/assets", assetFileServer)         // 从 generated/public/assets 直服
r.Get("/robots.txt", staticFile)
r.Get("/sitemap.xml", staticFile)
r.Get("/sitemap-*.xml", staticFile)
r.NotFound(staticSiteHandler)               // 兜底：generated/public 下查找

// ── 后台 SPA
r.Route("/admin", func(r chi.Router) {
    r.Get("/*", adminSPAHandler)            // 内嵌 assets；未匹配的路径返回 index.html
})

// ── API
r.Route("/api", func(r chi.Router) {
    r.Use(NoStore)
    r.Use(APIRateLimit)

    // 无需认证
    r.Route("/auth", func(r chi.Router) {
        r.Get("/status", h.AuthStatus)          // 是否已安装、是否已登录
        r.With(LoginRateLimit).Post("/login", h.Login)
        r.Post("/setup", h.FirstTimeSetup)      // 仅在无用户时可用
        r.Post("/logout", h.Logout)
        r.Get("/csrf", h.CSRFToken)
    })

    // 需要认证
    r.Route("/admin", func(r chi.Router) {
        r.Use(RequireAuth)
        r.Use(RequireCSRF)                      // 仅对非 GET/HEAD 生效
        // ... 见 §3 各资源
    })
})
```

### 1.1 中间件规格

| 中间件 | 行为 |
|---|---|
| `RealIP` | 仅当 `RemoteAddr` 属于 `trustedProxies` 时，才从 `CF-Connecting-IP` → `X-Real-IP` → `X-Forwarded-For`（取最右侧非可信 IP）解析真实 IP |
| `SecurityHeaders` | 见 [docs/10 §CSP](10-security-ops.md) |
| `NoStore` | `Cache-Control: no-store, must-revalidate` + `Pragma: no-cache` |
| `RequireAuth` | 校验会话 cookie，注入 `*model.User` 到 context；失败 401 |
| `RequireCSRF` | double-submit：比对 `X-CSRF-Token` 头与 `csrf_token` cookie；失败 403 |
| `RequireRole(roles...)` | RBAC 校验；失败 403 |
| `APIRateLimit` | 按 IP 令牌桶，`security.apiRateLimit`；超限 429 + `Retry-After` |
| `LoginRateLimit` | 按 username+IP，`security.loginRateLimit`；锁定期内直接 429 |
| `Audit(action)` | 成功响应后写 `audit_log` |

---

## 2. 通用约定

### 2.1 响应格式

**成功（单个资源）**：直接返回资源对象。

**成功（列表）**：
```json
{
  "items": [...],
  "total": 128,
  "page": 1,
  "perPage": 20,
  "totalPages": 7
}
```

**成功（无内容）**：`204 No Content`。

**错误**：
```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "slug 已被占用",
    "details": [
      { "field": "slug", "code": "DUPLICATE", "message": "已存在同名 slug" }
    ],
    "requestId": "01J..."
  }
}
```

### 2.2 错误码表

| HTTP | code | 场景 |
|---|---|---|
| 400 | `BAD_REQUEST` | 请求体解析失败 |
| 401 | `UNAUTHENTICATED` | 未登录或会话过期 |
| 403 | `FORBIDDEN` | 角色权限不足 |
| 403 | `CSRF_INVALID` | CSRF token 不匹配 |
| 404 | `NOT_FOUND` | 资源不存在 |
| 409 | `CONFLICT` | 并发编辑冲突（见 §2.4）/ 分类有子项无法删除 |
| 409 | `IN_USE` | 分类/标签被文章引用 |
| 413 | `TOO_LARGE` | 上传超限 |
| 422 | `VALIDATION_FAILED` | 字段校验失败 |
| 423 | `LOCKED` | 首次安装未完成 |
| 429 | `RATE_LIMITED` | 限流 |
| 500 | `INTERNAL` | 未预期错误 |
| 502 | `RENDERER_UNAVAILABLE` | Node 渲染器不可用（仅影响预览接口） |
| 503 | `AI_UNAVAILABLE` | AI provider 不可用 |

### 2.3 分页与筛选

所有列表端点统一支持：`?page=1&perPage=20&sort=date_desc&q=关键词`。
`perPage` 最大 100，默认 20。

### 2.4 并发编辑冲突检测

文章/页面的更新请求必须带 `If-Match: <bodyHash>`（或 body 中的 `baseHash` 字段），值为客户端加载时的 `bodyHash`。
服务端比对当前磁盘上的 `bodyHash`，不一致返回 409 + 当前版本内容，前端显示冲突对话框（保留我的 / 使用服务器的 / 手动合并）。

### 2.5 日期与 ID

所有时间 RFC3339 字符串。所有 ID 字符串。语言代码用**规范形式**（`zh-CN`）。

---

## 3. Admin API 详表

### 3.1 认证 `/api/auth`

| 方法 | 路径 | 请求 | 响应 |
|---|---|---|---|
| GET | `/status` | — | `{installed:bool, authenticated:bool, user?:User}` |
| POST | `/setup` | `{username,password,email,displayName,siteTitle,baseURL,defaultLocale}` | `{user}` + Set-Cookie |
| POST | `/login` | `{username,password,remember:bool}` | `{user}` + Set-Cookie |
| POST | `/logout` | — | 204 + 清 Cookie |
| GET | `/csrf` | — | `{token}` + Set-Cookie `csrf_token` |

### 3.2 仪表盘

| 方法 | 路径 | 响应 |
|---|---|---|
| GET | `/api/admin/dashboard` | 见下 |

```json
{
  "counts": {
    "posts": { "total": 128, "published": 110, "draft": 15, "trashed": 3, "scheduled": 2 },
    "pages": { "total": 6 },
    "categories": 12, "tags": 45, "links": 23, "media": 340,
    "locales": 5
  },
  "recentPosts":   [ { "id","title","locale","status","updatedAt","url" } ],
  "recentUpdated": [ ... ],
  "translation": {
    "pending": 3, "translating": 1, "failed": 2, "outdated": 8,
    "tokensThisMonth": 128400,
    "recentTasks": [ ... ]
  },
  "render": { "queued": 0, "failed": 0, "lastRebuildAt": "...", "lastDurationMs": 4210 },
  "system": {
    "version": "1.0.0", "uptime": 86400, "goroutines": 42,
    "memoryMB": 148, "rendererStatus": "healthy",
    "diskFreeGB": 41.2, "indexGeneration": 1284,
    "needsRestart": false, "warnings": ["..." ]
  }
}
```

### 3.3 文章 `/api/admin/posts`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/posts` | 列表。参数：`locale, status, category, tag, author, year, month, transStatus, q, sort, page, perPage` |
| POST | `/posts` | 新建。body: `{locale?, title, slug?, body?, front?}`；locale 缺省用 sourceLocale |
| GET | `/posts/{id}` | 返回全部语言的元数据 + 翻译状态 |
| GET | `/posts/{id}/content?locale=en` | 返回该语言的 `{front, body, bodyHash, hasDraft, draft?}` |
| PUT | `/posts/{id}/content?locale=en` | 保存。body: `{front, body, baseHash}` |
| POST | `/posts/{id}/publish` | body: `{locale?, at?}`；`at` 为未来时间则定时发布 |
| POST | `/posts/{id}/unpublish` | |
| DELETE | `/posts/{id}` | 移入回收站（`status: trashed`） |
| POST | `/posts/{id}/restore` | 从回收站恢复 |
| DELETE | `/posts/{id}/purge` | 永久删除（删除 bundle 目录 + 所有输出） |
| POST | `/posts/{id}/duplicate` | 复制为新草稿 |
| PUT | `/posts/{id}/slug` | body: `{locale, slug, renameDir:bool}` |
| POST | `/posts/{id}/locales/{locale}` | 手工创建某语言版本（空白或复制源） |
| DELETE | `/posts/{id}/locales/{locale}` | 删除某语言版本 |
| POST | `/posts/batch` | body: `{ids:[], action:"publish"|"unpublish"|"trash"|"restore"|"purge"|"addTag"|"removeTag"|"setCategory", payload:{}}` |
| GET | `/posts/{id}/revisions?locale=zh-CN` | 修订列表 |
| GET | `/posts/{id}/revisions/{rev}?locale=zh-CN` | 单个修订内容 |
| POST | `/posts/{id}/revisions/{rev}/restore` | 回滚 |
| PUT | `/posts/{id}/draft?locale=zh-CN` | 自动保存草稿（不改正式文件） |
| DELETE | `/posts/{id}/draft?locale=zh-CN` | 丢弃草稿 |

**列表项形状**：
```json
{
  "id": "019fd210-...",
  "type": "post",
  "bundleDir": "posts/2026/my-server",
  "sourceLocale": "zh-CN",
  "sourceRevision": 19,
  "title": "我的服务器搭建记录",
  "slug": "my-server",
  "status": "published",
  "date": "2026-08-09T12:00:00+08:00",
  "updated": "2026-08-09T13:00:00+08:00",
  "author": { "id":"admin", "displayName":"站长" },
  "categories": [ {"id":"linux","name":"Linux","slug":"linux"} ],
  "tags": [ {"id":"docker","name":"Docker","slug":"docker"} ],
  "cover": "/media/2026/08/cover.png",
  "pinned": false,
  "url": "/zh-cn/posts/my-server/",
  "locales": {
    "zh-CN": { "status":"original", "title":"我的服务器搭建记录", "exists":true },
    "en":    { "status":"completed","title":"My Server Setup Log", "exists":true,
               "translatedFromRevision":19 },
    "ja":    { "status":"outdated", "title":"…", "exists":true,
               "translatedFromRevision":17, "sourceDrift":2 },
    "de":    { "status":"missing",  "exists":false }
  },
  "hasDraft": false,
  "scheduledAt": null,
  "broken": false
}
```

### 3.4 页面 `/api/admin/pages`

与文章相同的端点集合（除 `revisions` 外全部一致），额外字段 `template`、`order`、`showInMenu`。路由用同一套 handler，通过 `type` 参数区分。

### 3.5 分类 `/api/admin/categories`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/categories` | 返回**树形结构** + 每个分类的文章计数 |
| GET | `/categories/flat` | 平铺列表（供下拉选择） |
| POST | `/categories` | body: `{id?, slug, parent?, order?, color?, cover?, name:{}, description:{}}`；`id` 缺省由 slug 生成 |
| GET | `/categories/{id}` | |
| PUT | `/categories/{id}` | |
| DELETE | `/categories/{id}` | 参数 `?migrateTo=<id>`；有子分类且未指定 migrateTo 时 409 |
| PUT | `/categories/reorder` | body: `[{id, parent, order}]`，拖拽排序后整体提交 |

### 3.6 标签 `/api/admin/tags`

| 方法 | 路径 |
|---|---|
| GET/POST | `/tags` |
| GET/PUT/DELETE | `/tags/{id}` |
| POST | `/tags/merge` — body: `{from:[], into:"id"}` |

### 3.7 友链 `/api/admin/links`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/links` | 按分组返回 |
| POST | `/links` | |
| GET/PUT/DELETE | `/links/{id}` | |
| PUT | `/links/reorder` | body: `[{id, group, order}]` |
| GET/PUT | `/links/groups` | 分组的增删改与排序 |
| POST | `/links/{id}/check` | 可用性检测（HEAD 请求，第二阶段） |

### 3.8 菜单 `/api/admin/menus`

| 方法 | 路径 |
|---|---|
| GET | `/menus` |
| GET/PUT | `/menus/{id}` — 整体替换 items 树（拖拽排序后提交完整结构） |
| POST | `/menus` |
| DELETE | `/menus/{id}` |
| GET | `/menus/targets?type=post&q=xx` — 菜单项目标选择器的搜索接口 |

### 3.9 媒体 `/api/admin/media`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/media?dir=2026/08&q=&type=image&page=1` | 列目录 |
| POST | `/media/upload` | multipart，字段 `file`（可多个）、`dir`。返回上传结果数组（含每个文件的成功/失败） |
| DELETE | `/media` | body: `{paths:[]}` 批量删除 |
| PUT | `/media/rename` | body: `{path, newName}` |
| PUT | `/media/move` | body: `{paths:[], toDir}` |
| POST | `/media/mkdir` | body: `{dir}` |
| GET | `/media/info?path=` | 返回 sidecar 元数据 |
| PUT | `/media/info?path=` | 更新 alt 等元数据 |
| POST | `/media/upload-from-url` | body: `{url, dir}` — 编辑器粘贴外链图片时本地化 |

**上传响应**：
```json
{ "items": [
  { "ok": true, "path": "2026/08/cover.png", "url": "/media/2026/08/cover.png",
    "width": 2560, "height": 1440, "size": 348211,
    "variants": [{"name":"thumb","url":"/media/2026/08/cover.thumb.png"}] },
  { "ok": false, "name": "big.psd", "error": "unsupported type" }
]}
```

### 3.10 翻译 `/api/admin/translations`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/translations/matrix?type=post&filter=outdated&page=1` | 翻译矩阵数据 |
| GET | `/translations/tasks?status=&page=` | 任务列表 |
| POST | `/translations/tasks` | body: `{articleIds:[], targets:[], force:bool}` 批量投递 |
| POST | `/translations/tasks/{id}/retry` | |
| POST | `/translations/tasks/{id}/cancel` | |
| DELETE | `/translations/tasks?status=done` | 清理历史 |
| POST | `/translations/estimate` | body: `{articleId, targets:[]}` → 返回段数、字符数、预估 token |
| POST | `/translations/test` | 测试 AI 连接，返回 `{ok, latencyMs, model, sample, jsonMode}` |
| PUT | `/translations/{articleId}/{locale}/manual` | 标记/取消「人工维护」 |
| GET | `/translations/usage?month=2026-08` | token 用量统计 |

### 3.11 渲染 `/api/admin/render`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/render/status` | 队列深度、进行中、失败列表、渲染器健康 |
| POST | `/render/rebuild` | 全量重建。body: `{locales?:[]}` |
| POST | `/render/unit` | 重渲染指定单元。body: `{keys:[]}` |
| POST | `/render/retry-failed` | |
| POST | `/render/purge-cache` | 手动触发 CDN 刷新 |
| GET | `/render/releases` | release 列表 |
| POST | `/render/releases/{ts}/activate` | 回滚到某个 release（切 symlink） |

### 3.12 预览 `/api/admin/preview`

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/preview/markdown` | body: `{markdown, locale}` → `{html, toc, wordCount, warnings}` |
| POST | `/preview/page` | body: `{type, id, locale, front, body}` → 完整页面 HTML（在新标签页预览未发布内容） |

`/preview/page` 的产物写到 `cache/preview/<token>.html`，通过 `/api/admin/preview/view/{token}` 返回，5 分钟过期。

### 3.13 主题 `/api/admin/themes`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/themes` | 列表 + 当前激活 |
| GET | `/themes/{name}` | 含 `settings.schema.json` 与当前值 |
| POST | `/themes/{name}/activate` | 切换并触发全量重建 |
| GET/PUT | `/themes/{name}/settings` | 主题配置读写 |
| POST | `/themes/{name}/settings/reset` | 恢复默认 |
| POST | `/themes/reload` | 重新扫描 themes/ 目录 |

### 3.14 设置 `/api/admin/settings`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/settings` | 完整 config（敏感字段掩码） |
| PUT | `/settings/{section}` | section ∈ site,i18n,render,markdown,storage,ai,search,comments,seo,cache,security,log |
| POST | `/settings/validate` | 校验但不保存 |
| GET | `/settings/locales` | 语言配置 |
| PUT | `/settings/locales` | 增删改语言；删除语言时返回受影响文章数，需 `confirm=true` |
| POST | `/settings/locales/test-negotiation` | body: `{acceptLanguage, country, cookie}` → `{locale, reason}` |

**敏感字段掩码**：`ai.apiKey`、`security.sessionSecret`、`cache.cloudflare.apiToken` 返回 `"sk-…abcd"` 形式；PUT 时若值等于掩码则保持原值不变。

### 3.15 用户 `/api/admin/users`

| 方法 | 路径 | 权限 |
|---|---|---|
| GET | `/users` | admin |
| POST | `/users` | admin |
| GET/PUT/DELETE | `/users/{id}` | admin（自己可 PUT 自己） |
| PUT | `/users/{id}/password` | 自己或 admin；body: `{oldPassword?, newPassword}` |
| GET/PUT | `/profile` | 当前用户 |

### 3.16 备份 `/api/admin/backup`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/backup` | 备份列表 |
| POST | `/backup` | 创建。body: `{includeMedia:bool}` → 返回 job id，SSE 推进度 |
| GET | `/backup/{name}/download` | 下载 zip（流式） |
| POST | `/backup/restore` | multipart 上传 zip 或 body `{name}`；需 `confirm=true` |
| DELETE | `/backup/{name}` | |
| GET | `/export?format=zip&scope=content,data,media,config` | 直接导出 |
| POST | `/import` | multipart 上传 zip/md 文件；body 参数 `{createMissingTaxonomy:bool, defaultLocale, defaultStatus}` |
| GET | `/import/{jobId}` | 导入进度与结果报告 |

### 3.17 日志与系统 `/api/admin/system`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/system/logs?level=&component=&page=` | 系统日志 |
| GET | `/system/audit?actor=&action=&page=` | 审计日志 |
| GET | `/system/stats` | 运行指标 |
| GET | `/system/index-errors` | 解析失败的文件列表 |
| POST | `/system/reindex` | 重建内存索引 |
| GET | `/system/health` | 健康检查（也用作容器 healthcheck） |
| POST | `/system/restart-renderer` | 重启 Node 渲染器 |

### 3.18 事件流 `/api/admin/events`

`GET /api/admin/events` — Server-Sent Events，需认证。

事件类型：
```
event: render
data: {"done":12,"total":34,"current":"zh-cn/posts/x/","failed":0}

event: translation
data: {"taskId":42,"articleId":"019f…","locale":"en","status":"translating","batch":3,"batches":7}

event: job
data: {"kind":"backup","status":"running","progress":0.6}

event: notice
data: {"level":"warn","message":"渲染器已重启"}
```

心跳：每 25s 发 `: ping`。前端用 `EventSource` + 自动重连。

---

## 4. 静态站点服务（`serveStatic=true`）

```go
func staticSiteHandler(w http.ResponseWriter, r *http.Request) {
    p := path.Clean(r.URL.Path)

    // 1. 路径穿越防护
    abs, err := fsutil.SafeJoin(publicDir, strings.TrimPrefix(p, "/"))
    if err != nil { http.Error(w, "", 400); return }

    // 2. 目录 → index.html；无尾斜杠 → 301 加斜杠
    if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
        if !strings.HasSuffix(r.URL.Path, "/") {
            http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently); return
        }
        abs = filepath.Join(abs, "index.html")
    }

    f, err := os.Open(abs)
    if err != nil { serveLocalized404(w, r); return }
    defer f.Close()

    // 3. 缓存头（见 §4.1）
    setCacheHeaders(w, r.URL.Path)

    // 4. ETag（基于 mtime+size）+ If-None-Match 处理
    // 5. http.ServeContent 处理 Range / Last-Modified
    http.ServeContent(w, r, abs, fi.ModTime(), f)
}
```

### 4.1 缓存头规则

| 路径匹配 | Cache-Control |
|---|---|
| `/` | `no-store` |
| `/api/*`, `/admin/*` | `no-store, must-revalidate` |
| `/assets/*` （文件名含 8+ 位 hash） | `public, max-age=31536000, immutable` |
| `/media/*` | `public, max-age=86400, s-maxage=2592000` |
| `*.html`, 目录索引 | `public, max-age=300, s-maxage=86400, stale-while-revalidate=604800` |
| `*.xml`, `*.json`（sitemap/rss/search-index） | `public, max-age=600, s-maxage=3600` |
| `robots.txt` | `public, max-age=3600` |

全部来自 `config.cache.*`，可配置。

### 4.2 压缩

Go 自服务时对 `text/html`、`application/json`、`application/xml`、`text/css`、`application/javascript` 启用 gzip（自实现中间件或用 `chi/middleware.Compress`）。

**更好的做法**：发布时预生成 `.br` 与 `.gz`（Brotli 用纯 Go 的 `github.com/andybalholm/brotli`），Nginx 用 `gzip_static on; brotli_static on;` 直接返回，零 CPU 开销。这是 M12 的优化项。

---

## 5. 后台 SPA 服务

```go
//go:embed all:dist
var adminFS embed.FS

func adminSPAHandler(w http.ResponseWriter, r *http.Request) {
    p := strings.TrimPrefix(r.URL.Path, "/admin/")
    if p == "" { p = "index.html" }

    data, err := adminFS.ReadFile("dist/" + p)
    if err != nil {
        // SPA fallback：所有未匹配路径返回 index.html
        data, _ = adminFS.ReadFile("dist/index.html")
        p = "index.html"
    }
    if strings.HasPrefix(p, "assets/") {
        w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
    } else {
        w.Header().Set("Cache-Control", "no-store")
    }
    w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(p)))
    w.Write(data)
}
```

Vite 配置 `base: '/admin/'`。

---

## 6. OpenAPI

M2 起维护 `backend/api/openapi.yaml`，用它生成 Admin 前端的 TS 类型（`openapi-typescript`）。**这是保证前后端类型一致的唯一机制**——不要手写两遍类型。

生成命令写进 `Makefile`：
```
make api-types   # openapi.yaml → frontend/admin/src/api/schema.d.ts
```
