# GitHub Actions 与 VPS 发布

## 发布链路

1. `codex/**` 分支和拉取请求执行完整 CI：Go、TypeScript、单元测试、前端与容器构建。
2. 合并到 `main` 或推送 `v*` 标签后，发布工作流再次执行完整测试和构建门禁，并先构建一个不发布的 amd64 候选镜像。
3. 候选镜像以未初始化数据目录启动并通过 `/health/ready` 冒烟检查后，工作流才构建、发布 `linux/amd64` 与 `linux/arm64` 镜像。发布摘要同时生成 SBOM、最大级别构建来源和 GitHub provenance attestation。
4. VPS 的 systemd timer 每两分钟检查一次 `ghcr.io/fengyuchen1314/mutiblog:latest`。没有新镜像时不重启；首次运行会在应用健康后自动拉起 Caddy，再执行公网门禁。
5. 有新镜像时，更新器保留当前镜像作为本机回滚点，只重建应用容器，继续使用既有数据卷和 Caddy。
6. 新容器必须同时通过 Docker healthcheck、主站公网 `/health/ready` 和预览域 TLS 隔离门禁；否则自动恢复上一镜像。Markdown/YAML 数据卷不会被替换。已初始化站点会在 HTTP 服务启动后后台限次重建，期间继续提供上一版成功 release，不会因小型 VPS 渲染较慢而阻塞容器启动。

Caddy 以只读方式挂载同一数据卷。`/media/*` 始终由 Caddy 直接从 `media/originals` 提供，因此粘贴进正文的本地图片不依赖 CMS 进程。其他请求正常情况下进入应用，以保留语言回退的 HTTP 302 等动态路由逻辑；应用连接失败时，Caddy 从 `generated/current` 提供最后一次成功发布的 HTML。此时正文、主题资源和本地图片继续可读，评论与控制台明确不可用。

备份恢复开始前会先创建一份当前永久数据的安全备份；恢复取得全局独占写门禁，等待在途翻译结束，并让后台写入与公开评论提交排队。目标归档在 staging 中验证并通过静态构建门禁后才提交。目录交换由数据根目录中的持久事务标记保护：未提交时进程中断会在下次启动保守回滚，已持久标记提交后只清理残留目录；回滚只触碰确实存在旧副本的根目录。失败自动换回原目录；成功后保留评论签名等非 Provider 本机秘密，清空全部 AI Provider API Key 与所有旧会话，并要求管理员重新登录、在后台重新填写 Key。

## 首次安装

GHCR 的 `mutiblog` 容器包必须设为 Public，VPS 才能在不保存 GitHub 凭据的情况下拉取。

将 `deploy/` 中以下文件复制到 VPS：

```text
/opt/mutiblog/deployment/Caddyfile
/opt/mutiblog/deployment/compose.production.yaml
/opt/mutiblog/deployment/update.sh
```

安装定时器：

```bash
install -m 0644 deploy/mutiblog-update.service /etc/systemd/system/
install -m 0644 deploy/mutiblog-update.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now mutiblog-update.timer
```

可选配置文件 `/etc/mutiblog/deploy.env`：

```bash
MUTIBLOG_PUBLIC_URL=https://mutiblog.nl.chrono-well.top
MUTIBLOG_PREVIEW_URL=https://preview.nl.chrono-well.top
MUTIBLOG_IMAGE=ghcr.io/fengyuchen1314/mutiblog:latest
```

生产 Caddy 同时为主站 `mutiblog.nl.chrono-well.top` 和隔离主题预览站 `preview.nl.chrono-well.top` 申请普通 HTTP 验证证书；两者都已由 `*.nl.chrono-well.top` 的 DNS 解析覆盖。预览站不提供后台或登录入口，只承载 30 分钟主题预览与共享的公开媒体文件。更新器会校验并热重载当前 Caddyfile；主站就绪且预览域通过 TLS 访问 `/console/` 精确返回 404 后，候选发布才会被接受。

手动触发一次安全更新：

```bash
systemctl start mutiblog-update.service
journalctl -u mutiblog-update.service -n 100 --no-pager
```

更新脚本使用 `flock` 防止并发部署。拉取失败不会影响当前容器；启动或公网健康检查失败时会以 `--pull never` 切回本机保留的上一镜像，避免回滚标签被错误地当作远程仓库拉取。

## 唯一管理员密码恢复

忘记管理员密码时使用离线命令恢复。必须先停止应用容器，避免另一个进程同时写配置；Caddy 在此期间继续提供最后一次成功发布的静态站。新密码从标准输入读取，不放入命令参数或镜像环境：

```bash
cd /opt/mutiblog/deployment
docker compose -f compose.production.yaml stop app
read -rs MUTIBLOG_NEW_PASSWORD
printf '%s\n' "$MUTIBLOG_NEW_PASSWORD" | docker run --rm -i -v mutiblog-data:/var/lib/mutiblog ghcr.io/fengyuchen1314/mutiblog:latest reset-password --data-dir /var/lib/mutiblog
unset MUTIBLOG_NEW_PASSWORD
docker compose -f compose.production.yaml start app
```

也可使用 `--password-file` 读取仅属主可读的常规文件；权限包含任何 group/other 位时命令会拒绝。重新启动会自然清空全部内存会话，管理员必须使用新密码登录。
