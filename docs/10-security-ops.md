# 10 · 安全、权限与运维规格

---

## 1. 认证

### 1.1 密码

**Argon2id**，参数：

```go
const (
    argonTime    = 3           // 迭代次数
    argonMemory  = 64 * 1024   // 64 MiB
    argonThreads = 2
    argonKeyLen  = 32
    argonSaltLen = 16
)
```

存储格式（PHC 标准串）：
```
$argon2id$v=19$m=65536,t=3,p=2$<base64-salt>$<base64-hash>
```

**必须实现的细节**：
- 校验时用 `subtle.ConstantTimeCompare`。
- 从存储串中**解析参数**再计算，而非用常量——这样将来调参不会使旧密码失效。
- 登录失败时也执行一次 dummy Argon2 计算（防用户名枚举的时序攻击）。
- 密码策略：长度 ≥ 12，不做复杂度强制（NIST 现代建议），但拒绝常见弱口令（内置 top-1000 列表）。

**内存注意**：64MiB × 并发登录数。在 512MB 环境下，登录并发必须限制（`LoginRateLimit` 已保证）。若目标环境更小，可降到 `m=32768`。

### 1.2 会话

**签名 Cookie，不入库**（避免每请求查 SQLite）。

```
Cookie 名: blog_session
值: base64url(payload) + "." + base64url(HMAC-SHA256(payload, sessionSecret))
payload = {"uid":"admin","tv":1,"exp":1786000000,"iat":1785395200}
```

| 属性 | 值 |
|---|---|
| `HttpOnly` | true |
| `Secure` | `config.security.cookieSecure`（生产必须 true） |
| `SameSite` | `Lax` |
| `Path` | `/` |
| `Max-Age` | `security.sessionMaxAge`（默认 7 天；`remember=false` 时为会话 cookie） |

**校验流程**：
1. 解析并验证 HMAC（`hmac.Equal`）。
2. 检查 `exp`。
3. 从索引中取用户，比对 `tv == user.TokenVersion`；不等则拒绝（这是**登出全部设备/改密码后失效**的机制）。
4. 检查 `user.Disabled`。

**滑动续期**：剩余有效期 < 1/2 时，响应中签发新 cookie。

**`sessionSecret`**：
- 生产必填，长度 ≥ 32，缺失则**启动失败**并打印生成命令。
- dev 模式自动生成并存 `cache/dev-session-secret`。
- 轮换 secret 会使所有会话失效（这是预期行为）。

### 1.3 CSRF

**Double-submit cookie**：

- `GET /api/auth/csrf` 签发 `csrf_token` cookie（**非** HttpOnly，前端要读）+ 返回同值。
- 所有非 `GET`/`HEAD`/`OPTIONS` 的 `/api/admin/*` 请求必须带 `X-CSRF-Token` 头。
- 服务端比对头与 cookie（`subtle.ConstantTimeCompare`）。
- token 本身是 `HMAC(sessionID, csrfSecret)`，与会话绑定，不可跨会话使用。
- 额外校验 `Origin`/`Referer` 头必须匹配 `baseURL`（存在时）。

**为什么不用 SameSite=Strict 就够了**：Lax 允许顶层 GET 导航携带 cookie，而部分老旧浏览器对 SameSite 支持不一致。双保险。

### 1.4 首次安装

`data/users/` 为空时：
- 所有 `/api/admin/*` 返回 423 `LOCKED`。
- `/admin/` 前端跳转到 `/admin/setup`。
- `POST /api/auth/setup` 可用，创建首个 admin 后立即失效（再次调用返回 409）。
- Setup 表单同时收集：站点标题、baseURL、默认语言 —— 一次性完成最小可用配置。

---

## 2. 授权（RBAC）

### 2.1 角色

| 角色 | MVP | 权限 |
|---|---|---|
| `admin` | ✅ | 全部 |
| `editor` | ✅ | 内容全部（文章/页面/分类/标签/友链/菜单/媒体/翻译/渲染）；**不能**改设置、用户、备份、主题激活 |
| `author` | 第二阶段 | 只能管理自己的文章与媒体 |
| `translator` | 第二阶段 | 只能编辑派生语言版本与触发翻译 |

### 2.2 权限表

```go
var permissions = map[string][]string{  // action → allowed roles
    "post.read":      {"admin","editor","author","translator"},
    "post.write":     {"admin","editor"},
    "post.publish":   {"admin","editor"},
    "post.delete":    {"admin","editor"},
    "post.translate": {"admin","editor","translator"},
    "taxonomy.write": {"admin","editor"},
    "media.write":    {"admin","editor","author"},
    "theme.settings": {"admin"},
    "theme.activate": {"admin"},
    "settings.write": {"admin"},
    "user.manage":    {"admin"},
    "backup.manage":  {"admin"},
    "render.rebuild": {"admin","editor"},
    "log.read":       {"admin"},
}
```

中间件 `RequirePerm("post.publish")` 挂在对应路由上。**前端也要据角色隐藏入口**，但服务端校验是唯一权威。

---

## 3. 输入安全

### 3.1 XSS

| 表面 | 处理 |
|---|---|
| **文章正文** | 作者是可信的（管理员），允许原始 HTML（需求要求支持 HTML）。**但**必须在配置中提供 `markdown.sanitize: true` 开关（默认 `false`，多作者站点应开启），开启时用 `rehype-sanitize` + 自定义白名单 |
| **主题 customCSS** | 输出到 `<style>` 前移除 `</style>`、`<script`、`javascript:`、`expression(` |
| **SVG 上传** | 强制清洗：移除 `<script>`、`<foreignObject>`、所有 `on*` 属性、`href`/`xlink:href` 中的 `javascript:`/`data:text/html` |
| **所有模板输出** | React 默认转义；只有 `<Prose html>` 与 `dangerouslySetInnerHTML` 处允许 HTML，且来源受控 |
| **Island props** | `JSON.stringify` 后需 HTML 属性转义（`&`、`<`、`>`、`"`、`'`） |
| **Admin 显示用户内容** | React 默认转义，不使用 `dangerouslySetInnerHTML`（预览面板除外，其内容来自自己的渲染器） |

### 3.2 路径穿越

**所有**接受路径参数的端点（媒体、备份、预览、静态服务）必须过 `fsutil.SafeJoin`：

```go
func SafeJoin(base, rel string) (string, error) {
    if filepath.IsAbs(rel) { return "", ErrUnsafePath }
    cleaned := filepath.Clean("/" + filepath.ToSlash(rel))   // 强制变成 /xxx 形式，消除 ..
    abs := filepath.Join(base, cleaned)
    // 再次确认在 base 之下（处理符号链接）
    realBase, err := filepath.EvalSymlinks(base)
    if err != nil { return "", err }
    realAbs, err := filepath.EvalSymlinks(abs)
    if err != nil {
        // 目标还不存在（新建场景）：检查其父目录
        realAbs, err = filepath.EvalSymlinks(filepath.Dir(abs))
        if err != nil { return "", err }
        realAbs = filepath.Join(realAbs, filepath.Base(abs))
    }
    if realAbs != realBase && !strings.HasPrefix(realAbs, realBase+string(os.PathSeparator)) {
        return "", ErrUnsafePath
    }
    return abs, nil
}
```

**测试用例**：`../../etc/passwd`、`..%2F..%2Fetc`、`/etc/passwd`、`a/../../b`、指向外部的符号链接、Windows `..\\`、URL 编码变体、空字节 `%00`。

### 3.3 SSRF

`POST /media/upload-from-url` 与友链 Logo 抓取会发起出站请求，必须防护：
- 解析域名后检查 IP：拒绝私有网段（10/8、172.16/12、192.168/16、127/8、169.254/16、::1、fc00::/7）与云元数据地址（169.254.169.254）。
- 禁止重定向到上述地址（自定义 `CheckRedirect`）。
- 只允许 `http`/`https`。
- 超时 10s，响应大小上限 20MB。
- Content-Type 必须是 image/*。

### 3.4 上传安全

- MIME 嗅探（`http.DetectContentType`）与扩展名交叉校验，不信任 `Content-Type` 头。
- 文件名规范化，剥离路径分隔符与控制字符。
- 大小限制（`storage.image.maxUploadSize`）在读取时用 `io.LimitReader` 强制，而非只看 `Content-Length`。
- 上传目录**绝不**允许执行（Nginx 配置中对 `/media/` 加 `add_header X-Content-Type-Options nosniff` 并禁止 `.php`/`.cgi` 等）。
- 存储到磁盘的文件权限 0644，目录 0755。

### 3.5 限流

```go
// 令牌桶，内存实现（单实例部署，无需分布式）
type Limiter struct {
    buckets *lru.Cache[string, *bucket]   // key = IP 或 IP+username
}
```

| 端点 | 限制 |
|---|---|
| `POST /api/auth/login` | 5 次 / 15 分钟 / (username+IP)，超限锁定 30 分钟 |
| `/api/admin/*` | 20 rps，burst 50，按 IP |
| `POST /media/upload` | 10 次 / 分钟 |
| `POST /translations/tasks` | 5 次 / 分钟 |
| `/api/admin/preview/*` | 30 次 / 分钟 |

超限返回 429 + `Retry-After`。登录失败记录到 `login_attempts`，后台可查。

---

## 4. HTTP 安全头

```go
func SecurityHeaders(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        h := w.Header()
        h.Set("X-Content-Type-Options", "nosniff")
        h.Set("X-Frame-Options", "SAMEORIGIN")
        h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
        h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), interest-cohort=()")
        if cfg.Server.HTTPS {
            h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
        }
        if strings.HasPrefix(r.URL.Path, "/admin") || strings.HasPrefix(r.URL.Path, "/api") {
            h.Set("Content-Security-Policy", adminCSP)
        } else {
            h.Set("Content-Security-Policy", siteCSP)
        }
        next.ServeHTTP(w, r)
    })
}
```

**Admin CSP**：
```
default-src 'self';
script-src 'self';
style-src 'self' 'unsafe-inline';
img-src 'self' data: blob: https:;
font-src 'self' data:;
connect-src 'self';
frame-ancestors 'self';
base-uri 'self';
form-action 'self'
```

**站点 CSP**（更宽松，因为要允许评论 iframe 与主题自定义）：
```
default-src 'self';
script-src 'self' 'unsafe-inline' <comments-provider-origin>;
style-src 'self' 'unsafe-inline';
img-src * data:;
font-src 'self' data:;
frame-src <comments-provider-origin>;
base-uri 'self'
```

`'unsafe-inline'` 用于主题的 FOUC 防护脚本与 customCSS。**第二阶段**改用 nonce（发布时静态生成 nonce 不可行，需改用 hash-source：把内联脚本的 SHA-256 写进 CSP，这是可行的静态方案，列为第二阶段优化）。

---

## 5. 备份与恢复

### 5.1 备份内容

| 目录 | 含 | 说明 |
|---|---|---|
| `content/` | ✅ | 含 `.revisions/`，不含 `.drafts/` |
| `data/` | ✅ | YAML 全部；`state.db` 用 `VACUUM INTO` 生成一致性快照 |
| `config/` | ✅ | **含 config.yaml，其中可能有 API Key** → 见 §5.3 |
| `media/` | 可选 | 默认含；大站点可关闭 |
| `themes/` | ❌ | 随镜像分发 |
| `generated/`, `cache/` | ❌ | 派生物 |

### 5.2 格式

`backups/backup-<YYYYMMDD-HHMMSS>.zip`，内含：

```
manifest.json          {version, createdAt, appVersion, counts:{posts,pages,...}, checksums:{}}
content/...
data/...
config/...
media/...
```

创建走 job 队列（大站点耗时长），SSE 推进度。流式写入，不整体读进内存。

### 5.3 敏感信息

备份含明文 API Key 与 session secret。必须：
- 后台创建备份时**明确警告**「备份包含 API 密钥等敏感信息，请妥善保管」。
- 提供 `excludeSecrets: true` 选项：导出时把 `ai.apiKey`、`security.sessionSecret`、`cache.cloudflare.apiToken` 替换为 `""`。
- 下载端点需 admin 权限 + 一次性 token（避免 URL 泄露导致下载）。

### 5.4 恢复

```
1. 校验 zip 结构与 manifest（版本兼容性检查）
2. 二次确认（需要 confirm=true 参数）
3. 进入维护模式：停 watcher、停 job pool、API 返回 503
4. 解压到 <root>/.restore-tmp/
5. 校验 checksums
6. 把现有 content/ data/ config/ media/ 重命名为 *.bak.<ts>
7. 移动 .restore-tmp/* 就位
8. 重开 SQLite、重建索引、重新加载配置
9. 触发全量重建
10. 退出维护模式
失败任一步 → 回滚（把 *.bak.<ts> 移回）
成功后保留 *.bak.<ts> 24 小时再由清理任务删除
```

### 5.5 保留策略

`backup.keep`（默认 10）：超出时删除最旧的。定时备份为第二阶段功能（可用外部 cron + API 实现）。

---

## 6. 导入导出

### 6.1 导出

`GET /api/admin/export?scope=content,data,media,config&format=zip` — 与备份同格式，但可选择范围。

**"整个 content/ data/ media/ config/ 就是导出"** 是设计目标（需求第 70 条）——用户直接 `tar czf` 这些目录同样有效，导出功能只是便利封装。

### 6.2 导入

支持三种输入：
1. 本系统的备份 zip → 走恢复流程
2. 一批 `.md` 文件（zip 或多文件上传）→ 走 [docs/02 §11](02-content-format.md) 的容错规则
3. 单个 `.md` 文件

导入是 job，产出**报告**：成功数、跳过数、失败明细（文件名 + 原因）、自动创建的分类/标签列表。导入前提供**预览**（dry-run）。

---

## 7. 日志

### 7.1 应用日志

`log/slog`，字段规范：

```go
slog.Info("render unit completed",
    "component", "render",
    "unit", u.Key,
    "locale", u.Locale,
    "output", u.OutputPath,
    "dur_ms", elapsed.Milliseconds(),
)
```

**绝不记录**：密码、API Key、session token、CSRF token、完整请求体。

### 7.2 审计日志

所有写操作写 `audit_log`：

```go
audit.Log(ctx, "article.publish", "article", string(id), map[string]any{
    "locale": loc, "title": title, "revision": rev,
})
```

必审计的动作：登录成功/失败、登出、文章增删改发布、分类/标签/友链/菜单增删改、媒体上传删除、设置修改（记录变更的 key，不记录值）、用户增删改、密码修改、主题激活、备份创建/恢复、全量重建、翻译任务触发。

保留 `log.auditRetentionDays`（默认 90 天），每日清理任务。

### 7.3 系统日志

`system_log` 表存需要管理员关注的事件：解析失败的文件、渲染失败、翻译失败、渲染器崩溃重启、磁盘空间不足、配置校验警告。后台「日志」页展示，支持按 level/component 筛选。

---

## 8. 部署

### 8.1 Dockerfile

```dockerfile
# ── Stage 1: 构建前端（admin + 主题 + 渲染器）
FROM node:22-alpine AS web
WORKDIR /src
COPY frontend/admin/package*.json frontend/admin/
COPY frontend/renderer/package*.json frontend/renderer/
COPY themes/default/package*.json themes/default/
RUN cd frontend/admin && npm ci \
 && cd ../renderer && npm ci \
 && cd ../../themes/default && npm ci
COPY frontend/ frontend/
COPY themes/ themes/
RUN cd frontend/admin && npm run build \
 && cd ../renderer && npm run build \
 && cd ../../themes/default && npm run build

# ── Stage 2: 构建 Go 二进制（内嵌 admin dist）
FROM golang:1.23-alpine AS go
WORKDIR /src
COPY backend/go.mod backend/go.sum backend/
RUN cd backend && go mod download
COPY backend/ backend/
COPY --from=web /src/backend/web/dist backend/web/dist
RUN cd backend && CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION:-dev}" \
      -o /out/blog-server ./cmd/blog

# ── Stage 3: 运行时（需要 Node 跑渲染器）
FROM node:22-alpine
RUN apk add --no-cache tzdata ca-certificates su-exec \
 && addgroup -g 1001 blog && adduser -D -u 1001 -G blog blog
WORKDIR /app
COPY --from=go   /out/blog-server            /app/blog-server
COPY --from=web  /src/frontend/renderer/dist /app/renderer
COPY --from=web  /src/frontend/renderer/node_modules /app/renderer/node_modules
COPY --from=web  /src/themes                 /app/themes
COPY deploy/entrypoint.sh                    /app/entrypoint.sh
RUN mkdir -p /app/content /app/data /app/media /app/config /app/generated /app/cache \
 && chown -R blog:blog /app
ENV BLOG_SERVER_PORT=8080
EXPOSE 8080
VOLUME ["/app/content","/app/data","/app/media","/app/config","/app/generated"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s \
  CMD wget -qO- http://127.0.0.1:8080/api/admin/system/health || exit 1
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["/app/blog-server"]
```

`entrypoint.sh`：首次启动时若 `config/config.yaml` 不存在，从内置模板生成；确保目录权限；`su-exec blog "$@"`。

**镜像体积目标**：< 250MB（node:22-alpine ≈ 130MB + 渲染器依赖 ≈ 60MB + 二进制 ≈ 20MB + 主题 ≈ 5MB）。

### 8.2 `docker-compose.yml`

> ⚠️ 本节已按 [docs/13](13-first-run-walkthrough.md) P1/P2/P3/P4/P25 修订。文件放在**项目根目录**（不是 `deploy/`），符合 `cd mutiblog && docker compose up` 的用户直觉。

```yaml
services:
  blog:
    # ★ 默认使用预构建镜像。1C1G 服务器无法本地构建（Rollup 打包峰值 >1GB，必然 OOM）
    image: ghcr.io/<owner>/mutiblog:latest
    # 仅开发者使用，需要 ≥4GB 内存：
    # build: { context: ., dockerfile: deploy/Dockerfile }
    restart: unless-stopped
    ports: ["8080:8080"]
    env_file: .env
    volumes:
      - ./content:/app/content
      - ./data:/app/data
      - ./media:/app/media
      - ./config:/app/config
      - ./generated:/app/generated
      # ⚠️ 不要挂载 ./themes —— 宿主机没有构建产物(dist/)会导致主题全部不可用
    healthcheck:
      test: ["CMD","wget","-qO-","http://127.0.0.1:8080/api/admin/system/health"]
      interval: 30s
```

**绝不包含**：postgres、mysql、redis、rabbitmq、elasticsearch（需求第 81/86 条）。

#### 8.2.1 必须提供预构建镜像（M12 交付物）

GitHub Actions 构建 `linux/amd64` + `linux/arm64` 多架构镜像推送到 GHCR。**这不是可选项**——没有它，目标用户（1C1G VPS 的个人博主）在第一条命令就会失败。

#### 8.2.2 反向代理必须挂载 `generated/` 父目录

**这是一个会导致"全量重建后站点内容不更新"的隐蔽陷阱。**

`generated/public` 是指向 `generated/releases/<ts>/` 的 symlink。Docker bind mount 在**容器启动时**解析宿主机路径，直接挂载 `./generated/public` 会把当时 symlink 指向的那个 release 目录的 inode 固定下来。之后 Go 切换 symlink，代理容器**纹丝不动，永远服务旧 release**。

```yaml
# ❌ 错误
- ./generated/public:/var/www/public:ro

# ✅ 正确：挂载父目录，让代理在每次请求时解析 symlink
- ./generated:/var/www/generated:ro
```

对应地，Nginx `root /var/www/generated/public;`，Caddy `root * /srv/generated/public`。
Nginx 的 `open_file_cache` 会缓存 inode，**示例配置中不要开启它**（默认 off，保持默认）。

### 8.2.3 三套部署方案（README 让用户三选一）

| 方案 | 文件 | 适用 | HTTPS |
|---|---|---|---|
| **A. Caddy（推荐）** | `deploy/compose.caddy.yml` + `Caddyfile` | 大多数个人用户 | ✅ 自动申请与续期 Let's Encrypt |
| **B. Nginx + certbot** | `deploy/compose.nginx.yml` + `nginx.conf` | 已有 nginx 经验 | ⚠️ 需手工配置 certbot |
| **C. Go 直服** | 默认 `docker-compose.yml` | 内网 / 测试 / 已有外层代理 | ❌ |

Caddyfile（零配置自动 HTTPS，这是个人自托管场景的最优解）：

```caddyfile
blog.example.com {
    handle /admin/* { reverse_proxy blog:8080 }
    handle /api/*   { reverse_proxy blog:8080 }
    handle = /      { reverse_proxy blog:8080 }        # 语言协商
    handle /media/* { root * /srv; file_server }
    handle {
        root * /srv/generated/public                    # ← 注意是 generated/public
        try_files {path} {path}/index.html
        file_server
    }
    encode zstd gzip
    header /assets/* Cache-Control "public, max-age=31536000, immutable"
}
```

### 8.3 `entrypoint.sh`（权限与密钥自举）

首次启动时宿主机上的挂载目录由 Docker 以 **root:root** 创建，而容器内进程是 uid 1001 —— 不处理会导致所有写操作 `permission denied`。

```sh
#!/bin/sh
set -e

# 1. 目录与权限
for d in content data media config generated cache; do
  mkdir -p "/app/$d"
  if ! chown -R blog:blog "/app/$d" 2>/dev/null; then
    echo "WARN: 无法修改 /app/$d 属主（可能是 NFS/CIFS 挂载）"
    if ! su-exec blog test -w "/app/$d"; then
      echo "FATAL: /app/$d 不可写。请在宿主机执行："
      echo "  sudo chown -R 1001:1001 ./$d"
      exit 1
    fi
  fi
done

# 2. 配置自举
[ -f /app/config/config.yaml ] || cp /app/defaults/config.yaml /app/config/config.yaml

# 3. 会话密钥自动生成（不再要求用户手工配置）
if [ -z "$BLOG_SECURITY_SESSIONSECRET" ] && [ ! -f /app/config/.secrets.yaml ]; then
  echo "sessionSecret: \"$(head -c 48 /dev/urandom | base64 -w0)\"" > /app/config/.secrets.yaml
  chmod 600 /app/config/.secrets.yaml
  chown blog:blog /app/config/.secrets.yaml
  echo "INFO: 已自动生成会话密钥 → config/.secrets.yaml（请勿删除或提交到 Git）"
fi

exec su-exec blog "$@"
```

`chown` 失败时**不直接退出**，而是实测可写性；确实不可写才退出，并给出可直接复制的宿主机命令。

`.gitignore` 需包含 `config/.secrets.yaml`。

### 8.3.1 自适应安全参数

| 参数 | 自适应规则 | 理由 |
|---|---|---|
| `security.cookieSecure` | 默认 **`auto`**：请求为 HTTPS（或 `X-Forwarded-Proto: https`）时置 Secure，否则不置 | `true` + HTTP 访问会导致浏览器静默丢弃 cookie → **登录成功却被踢回登录页的无限循环，且无任何错误提示**。这是最容易让用户放弃的坑（[docs/13 P6](13-first-run-walkthrough.md)） |
| Argon2 `m` | `MemTotal < 1.5GB` 时自动用 `m=32768, t=4`（降内存、补迭代以维持强度） | 64MiB × 并发登录 + Node 渲染器在 1C1G 上可能触发 OOM |

配合前端自检：登录成功后立即调 `GET /api/auth/status`，若返回 `authenticated:false`，显示明确诊断而非笼统失败。

### 8.3 `.env.example`

```bash
# 必填
BLOG_SECURITY_SESSIONSECRET=            # openssl rand -base64 48
BLOG_SERVER_BASEURL=https://example.com

# AI（可选）
BLOG_AI_APIKEY=
BLOG_AI_BASEURL=https://api.openai.com/v1
BLOG_AI_MODEL=gpt-x

# Cloudflare 缓存刷新（第二阶段，可选）
BLOG_CACHE_CLOUDFLARE_APITOKEN=
BLOG_CACHE_CLOUDFLARE_ZONEID=

TZ=Asia/Shanghai
```

### 8.4 `nginx.conf.example`

```nginx
map $http_accept_encoding $br { default ""; "~*br" ".br"; }

server {
    listen 80;
    server_name example.com;
    root /var/www/public;

    # 语言协商：唯一需要回源的公开路径
    location = / {
        proxy_pass http://blog:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header CF-IPCountry $http_cf_ipcountry;
        proxy_set_header Accept-Language $http_accept_language;
        proxy_buffering off;
        add_header Cache-Control "no-store" always;
    }

    # 后台与 API：不缓存
    location ^~ /admin/ { proxy_pass http://blog:8080; include proxy_params; }
    location ^~ /api/   { proxy_pass http://blog:8080; include proxy_params;
                          client_max_body_size 32m; }

    # 媒体
    location ^~ /media/ {
        alias /var/www/media/;
        add_header Cache-Control "public, max-age=86400, s-maxage=2592000";
        add_header X-Content-Type-Options nosniff;
    }

    # 带 hash 的资产：长期缓存
    location ^~ /assets/ {
        add_header Cache-Control "public, max-age=31536000, immutable";
        try_files $uri =404;
    }

    # 站点静态 HTML（完全不经过 Go）
    location / {
        add_header Cache-Control "public, max-age=300, s-maxage=86400, stale-while-revalidate=604800";
        try_files $uri $uri/index.html $uri.html =404;
    }

    gzip on;
    gzip_types text/html text/css application/javascript application/json application/xml image/svg+xml;
    gzip_min_length 512;
    # 若启用了预压缩（M12）：
    # gzip_static on;
    # brotli_static on;

    error_page 404 /404.html;
}
```

**关键**：`location /` 直接读盘，Go 崩溃不影响已发布内容（需求第 80 条）。

### 8.5 Cloudflare 配置建议

| 路径 | 设置 |
|---|---|
| `/zh-cn/*`, `/en/*`, `/ja/*` … | Cache Everything，Edge TTL: Respect origin，Browser TTL: Respect origin |
| `/assets/*`, `/media/*` | Cache Everything，Edge TTL 1 month |
| `/` | **Bypass Cache** |
| `/admin/*`, `/api/*` | **Bypass Cache**，可加 WAF 规则限制来源 IP |

Transform Rule 建议：为 `/` 保留 `CF-IPCountry` 头（默认开启）。

### 8.6 反向代理注意

`config.server.trustedProxies` 必须正确配置，否则：
- `RealIP` 中间件不会信任 `X-Forwarded-For` → 限流按代理 IP 统计 → 全站共用一个桶（严重问题）
- `CF-IPCountry` 被忽略 → IP fallback 失效

Docker compose 场景下代理 IP 是容器网段（如 `172.16.0.0/12`），文档中需明确说明。

---

## 9. 资源与性能目标

| 指标 | 目标 | 验证方式 |
|---|---|---|
| 空载常驻内存（Go + Node） | < 200MB | `docker stats` |
| 空载 CPU | < 1% | 同上 |
| 冷启动（1000 篇 × 4 语言） | < 5s 到可服务 | 启动日志时间戳 |
| 单页渲染耗时 | < 100ms P95 | `render_units.duration_ms` |
| 全量重建（4500 单元） | < 5 分钟 | 后台显示 |
| 静态 HTML TTFB（本地 Nginx） | < 10ms | `curl -w` |
| 静态 HTML TTFB（CDN 命中） | < 100ms | 实测 |
| API P95 延迟 | < 100ms | 日志统计 |
| 1C1G 环境 | 全功能可用 | 限制容器资源实测 |

**1C1G 下的调优**：`ai.concurrency=1`、`render.concurrency=2`、`render.workerCount=1`、Argon2 `m=32768`。这些应写进文档的「小内存部署」章节。

---

## 10. 安全验收测试

| # | 测试 | 期望 |
|---|---|---|
| S1 | `GET /api/admin/posts` 无 cookie | 401 |
| S2 | 带有效 session 但无 CSRF 头的 POST | 403 |
| S3 | 用其他会话的 CSRF token | 403 |
| S4 | 登录连续失败 6 次 | 第 6 次返回 429，锁定 30 分钟 |
| S5 | `GET /api/admin/media/info?path=../../config/config.yaml` | 400 `ErrUnsafePath` |
| S6 | 上传 `evil.svg`（含 `<script>`） | 存储的文件中 script 已被移除 |
| S7 | 上传伪装成 png 的 php 文件 | 拒绝（MIME 嗅探不匹配） |
| S8 | `POST /media/upload-from-url` 指向 `http://169.254.169.254/` | 拒绝 |
| S9 | editor 角色调用 `PUT /settings/site` | 403 |
| S10 | 修改密码后用旧 cookie 访问 | 401（tokenVersion 不匹配） |
| S11 | 检查任意页面响应头 | 含全部安全头，CSP 正确 |
| S12 | 日志中搜索 API Key 明文 | 无匹配 |
| S13 | 备份 zip 中的 config.yaml（excludeSecrets=true） | apiKey 为空串 |
| S14 | 非可信代理伪造 `X-Forwarded-For` | 限流仍按真实 RemoteAddr |
| S15 | 文章正文写入 `<img src=x onerror=alert(1)>`，sanitize=true | 输出中已移除 onerror |
