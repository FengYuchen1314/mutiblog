# 13 · 首次部署实地走查：30 个会翻车的地方

> 假设场景：一台 **1 核 1GB 的 Debian 12 VPS**，用户是个人博主，有基本 Linux 能力但不是运维专家。
> 从 `git clone` 到「中文文章发布 + 自动翻译成 4 语言 + 上了 HTTPS 域名」的完整路径。
>
> 每个问题标注严重度：🔴 阻断部署 / 数据错误 · 🟠 体验严重受损 · 🟡 可用但别扭
>
> **本文档中的所有「修正」都是对前 12 份规格的正式修订，实现时以本文档为准。**

---

## 阶段一：拉取与启动

```bash
git clone https://github.com/xxx/mutiblog && cd mutiblog
docker compose up -d
```

### 🔴 P1 · 在 1C1G 上本地构建必然失败

我的 `docker-compose.yml` 写的是 `build: { context: ., dockerfile: deploy/Dockerfile }`,意味着用户要**在这台 1GB 的机器上构建镜像**。而 Stage 1 要跑三次 `npm ci` + 三次 `vite build`（admin 含 CodeMirror/shadcn/TanStack、renderer、theme）。

Rollup 打包 React admin 的峰值内存轻松超过 1GB。用户会看到 `docker compose up` 卡十几分钟,然后:

```
FATAL ERROR: Ineffective mark-compacts near heap limit Allocation failed
- JavaScript heap out of memory
```

**我之前写「1C1G 可运行」时,混淆了「运行时 1C1G」和「构建时 1C1G」。** 这是整个部署路径上最致命的问题——用户在第一条命令就失败了。

**修正**：
1. **必须提供预构建镜像**,`docker-compose.yml` 默认用 `image: ghcr.io/<owner>/mutiblog:latest`,`build:` 段注释掉并标注「仅开发者使用,需要 ≥4GB 内存」。
2. CI 用 GitHub Actions 构建 `linux/amd64` + `linux/arm64` 多架构镜像并推送。这是 M12 的**交付物**,不是可选项。
3. README 首屏就写明:「不要在小内存服务器上构建镜像」。

### 🔴 P2 · `context: .` 路径指错

Compose 的 `build.context` 相对**compose 文件所在目录**解析。我把 compose 放在 `deploy/`,写 `context: .` → 指向 `deploy/` 而不是项目根 → `COPY frontend/ frontend/` 直接失败。

**修正**:`context: ..`,或把 `docker-compose.yml` 放在项目根(推荐,符合用户直觉:`cd mutiblog && docker compose up`)。

### 🔴 P3 · 挂载目录的 root 属主问题

```yaml
volumes:
  - ./content:/app/content
```

首次启动时宿主机上 `./content` 不存在 → **Docker 以 root:root 创建** → 容器内 uid 1001 的 `blog` 用户无写权限 → 所有保存操作失败,报 `permission denied`。

这是 Docker 自托管应用最经典的坑,几乎每个项目都踩过。

**修正**:`entrypoint.sh` 必须(以 root 身份)在降权前做:

```sh
#!/bin/sh
set -e
for d in content data media config generated cache; do
  mkdir -p "/app/$d"
  if ! chown -R blog:blog "/app/$d" 2>/dev/null; then
    echo "WARN: 无法修改 /app/$d 的属主(可能是 NFS/CIFS 挂载)"
    if ! su-exec blog test -w "/app/$d"; then
      echo "FATAL: /app/$d 不可写。请在宿主机执行:"
      echo "  sudo chown -R 1001:1001 ./$d"
      exit 1
    fi
  fi
done
exec su-exec blog "$@"
```

关键在于:**chown 失败不直接退出,而是实测可写性**;确实不可写时给出**可直接复制粘贴的宿主机命令**。

### 🔴 P4 · 挂载 `themes/` 会导致启动失败

Dockerfile 把构建好的主题 `COPY` 进 `/app/themes`。如果用户为了「方便改主题」在 compose 里加了 `- ./themes:/app/themes`,而宿主机的 `themes/` 里只有源码没有 `dist/` → 系统找不到任何可用主题 → 启动失败或渲染全挂。

**修正**:
- `docker-compose.yml` 默认**不挂载** `themes/`,并在注释里写明风险。
- 主题发现逻辑:若 `themes/<name>/dist/manifest.json` 缺失,跳过该主题并记录明确日志(`主题 default 未构建,已跳过。请运行 npm run build`)。
- 若**没有任何可用主题**,启动不退出,而是进入「降级模式」:后台可用,前台返回一个内置的极简 HTML 说明页。**绝不能因为主题问题导致后台也进不去**。

### 🟠 P5 · sessionSecret 缺失导致容器退出循环

`security.sessionSecret` 必填,缺失则启动失败。用户直接 `docker compose up -d` 没配 `.env` → 容器 `Exited(1)` → 用户要 `docker compose logs` 才知道原因。

对自托管产品来说,**让用户手工生成并粘贴一个密钥是不必要的摩擦**,而且用户很可能随手填个 `12345678901234567890123456789012` 反而更不安全。

**修正**:改为自动生成。

```
启动时:
  1. 若环境变量 BLOG_SECURITY_SESSIONSECRET 存在 → 用它(最高优先级)
  2. 否则读 config/.secrets.yaml 的 sessionSecret
  3. 否则生成 48 字节随机值,原子写入 config/.secrets.yaml (0600),日志记录
     「已自动生成会话密钥并保存到 config/.secrets.yaml,请勿删除或提交到 Git」
```

`.gitignore` 加 `config/.secrets.yaml`。`.env.example` 中该项改为可选并说明。

---

## 阶段二：首次访问后台

```
http://203.0.113.10:8080/admin/
```

### 🔴 P6 · Secure Cookie 在 HTTP 下导致登录无限循环

`security.cookieSecure` 默认 `true`。用户还没配 HTTPS,通过 `http://IP:8080` 访问。

**浏览器会静默丢弃 Secure cookie。** 后果:
- 服务端认为登录成功,返回 200 + Set-Cookie
- 浏览器不保存该 cookie
- 前端跳转到仪表盘 → 请求 401 → 踢回登录页
- **用户看到的是「登录后又回到登录页」,没有任何错误信息**,会以为密码错了,反复尝试直到触发登录限流被锁 30 分钟

这个症状的误导性极强,是我认为整个流程里**最容易让用户放弃**的一个点。

**修正**(三重):
1. `cookieSecure` 默认值改为 `auto`:请求为 HTTPS(或 `X-Forwarded-Proto: https`)时置 Secure,否则不置。
2. 登录成功后,前端**立即**调 `GET /api/auth/status` 自检。若返回 `authenticated: false`,显示明确诊断:
   > **登录成功,但浏览器未能保存会话。**
   > 你正在通过 HTTP 访问,而系统配置要求 HTTPS 专用 Cookie。
   > 解决方法:使用 HTTPS 访问,或在 `config/config.yaml` 中设置 `security.cookieSecure: false`。
3. 服务端启动时若 `cookieSecure=true` 且 `baseURL` 是 `http://`,打印醒目警告。

### 🟠 P7 · Argon2 的 64MiB 在 1C1G 上有风险

`m=64MiB` × 登录并发。同时如果正在跑全量重建(Node 渲染器 ~150MB)+ Go 进程,首次登录可能触发 OOM Killer。

我在 docs/10 写了「1C1G 下降到 m=32768」,但那是**手动调优**,用户不会知道。

**修正**:启动时探测可用内存(`/proc/meminfo` 的 `MemTotal`),`< 1.5GB` 时自动使用 `m=32768, t=4`(降内存、补迭代次数以维持强度),并记录日志。参数本就存在 PHC 串里,不影响已有密码的校验。

### 🟡 P8 · baseURL 此时填什么

安装向导要求填 `baseURL`,但用户还没有域名。他会填 `http://203.0.113.10:8080`。这会写进所有页面的 canonical / hreflang / sitemap / RSS。等域名配好了要全部重来(见 P22)。

**修正**:安装向导的 baseURL 字段加一个「我还没有域名,暂时用 IP 访问」的选项,选中后自动填入当前访问地址,并显示提示:「配好域名后请到 设置 → 站点 修改,系统会自动重新生成全站页面」。

---

## 阶段三：配置 AI

设置 → AI,填 Base URL / API Key / Model。

### 🟠 P9 · Base URL 的四种写法有三种是错的

我的规格是 `POST {baseURL}/chat/completions`。用户常见输入:

| 用户输入 | 结果 |
|---|---|
| `https://api.openai.com/v1` | ✅ 正确 |
| `https://api.openai.com` | ❌ 404 |
| `https://api.openai.com/v1/` | ❌ 双斜杠,部分网关 404 |
| `https://api.openai.com/v1/chat/completions` | ❌ 路径重复 |

错误信息是裸的 404,用户很难自己诊断。

**修正**:
1. 保存时规范化:去掉尾斜杠;若以 `/chat/completions` 结尾则剥掉。
2. 若不以 `/v1` 或 `/v\d+` 结尾,**提示但不强制**(有些自建代理确实不带):「Base URL 通常以 /v1 结尾,确定吗?」
3. 「测试连接」的错误必须带诊断结论,不能只回显 HTTP 状态:

| 现象 | 提示 |
|---|---|
| 404 | 「端点未找到。请检查 Base URL 是否需要以 `/v1` 结尾」 |
| 401/403 | 「API Key 无效或无权访问该模型」 |
| 模型不存在 | 「模型 `xxx` 不可用。该端点支持的模型:…(若能列出)」 |
| 连接超时 | 「无法连接到 `api.xxx.com`。请检查服务器网络或代理设置」 |
| TLS 错误 | 「TLS 握手失败,可能是中间人代理或证书问题」 |

### 🟠 P10 · 「测试连接」用了 120 秒超时

`Test()` 复用了 `ai.timeout`(默认 120s)。服务器在国内访问 OpenAI 被墙时,用户点「测试连接」要**盯着转圈两分钟**才知道失败。

**修正**:`Test()` 用独立的 15s 超时,并且分段报告:DNS 解析 → TCP 连接 → TLS → 首字节 → 完成,失败时告诉用户卡在哪一步。

### 🟡 P11 · API Key 掩码的哨兵值

我写的「保存时若值等于掩码则不覆盖」有理论漏洞(真 key 恰好等于掩码串)。

**修正**:前端对未修改的密钥字段**直接不回传**(`undefined`),而不是回传掩码。服务端 `undefined` = 保持原值,`""` = 显式清空。

### 🔴 P12 · translateTargets 与启用语言不取交集

`ai.translateTargets` 默认 `[zh-TW, en, ja, de]`,但用户在 `i18n.locales` 里可能只启用了 `zh-CN` 和 `en`。

后果:系统会为 `ja`/`de` 创建翻译任务 → **消耗 token** → 生成 `index.ja.md`、`index.de.md` → 但站点根本不输出这些语言 → **纯粹的浪费 + 一堆孤儿文件**。

**修正**:
- 配置校验时 `translateTargets = translateTargets ∩ enabledLocales - {sourceLocale}`,并对被过滤掉的项记录警告。
- 后台 UI 里该字段的可选项**只列已启用的语言**。
- 用户禁用某个语言时,提示「该语言有 N 个翻译任务待处理,将被取消」。

---

## 阶段四：写第一篇文章

标题:「我的服务器搭建记录」

### 🔴 P13 · 中文 slug 规则自相矛盾

我在 docs/02 §1.3 里同时写了两条互相冲突的规则:
- 「CJK 字符:保留原样」
- 「若 title 全为 CJK,则用 article id 前 8 位作为 slug」

**必须二选一。** 而且后者的实际效果很糟:绝大多数中文用户不会主动改 slug,结果全站 URL 变成 `/zh-cn/posts/019fd210/` 这样的随机串,可读性和 SEO 都差。

**修正**:采用 `content.slugStrategy` 配置,默认 `preserve`。

| 策略 | 行为 |
|---|---|
| `preserve`(默认) | 保留 CJK,URL 中 percent-encode。`/zh-cn/posts/我的服务器搭建记录/` 是合法且现代浏览器地址栏可读的,中文 SEO 无问题 |
| `ai` | 已配置 AI 时,调用模型生成英文 slug(单次请求,便宜) |
| `id` | 用 id 前 8 位 |

编辑器的 slug 输入框旁,配了 AI 时显示一个「✨ 生成英文链接」小按钮,一键切换。

### 🔴 P14 · Bundle 目录名不能等于中文 slug

我写的「目录名 = 创建时的 slug」在 P13 修正后会产生**中文目录名**:`content/posts/2026/我的服务器搭建记录/`。

风险:
- Git 默认 `core.quotepath=true`,中文文件名显示为 `\346\210\221...`,diff 难读
- Windows 上开发/克隆时的编码问题
- 某些 NAS / 备份工具对非 ASCII 路径处理不佳
- zip 备份的文件名编码(zip 的 UTF-8 flag 支持不一致)

**修正**:**目录名与 slug 解耦**。

```
目录名生成规则:
  1. slug 的 ASCII 安全形式(移除非 [a-z0-9-] 字符后)
  2. 若结果为空或过短(<2 字符) → 用 `<YYYYMMDD>-<id前6位>`
     例:content/posts/2026/20260809-019fd2/
  3. 目录名一旦创建永不自动变更(slug 改了也不动)
```

slug 只存在 Front Matter 里。这也顺便解决了「改 slug 要不要重命名目录」的纠结——**答案是不重命名**。

### 🟡 P15 · 粘贴截图的文件名

剪贴板图片没有文件名,浏览器给 `image.png` 或空。多次粘贴变成 `image.png`、`image-2.png`、`image-3.png`,毫无信息量。

**修正**:粘贴的图片命名为 `paste-<YYYYMMDD-HHMMSS>.png`。拖拽上传的保留原文件名。

### 🟠 P16 · Mermaid 的 500KB 没有加载策略

我只写了「mermaid 走 island 懒加载」,没指定 hydrate 策略。mermaid 的 ESM bundle **约 500KB gzip**,在没有 CDN 的自托管场景下,`hydrate="load"` 会让含图表的页面首屏多下 500KB。

**修正**:
- Mermaid island 强制 `hydrate="visible"`(滚动到视口才加载),`rootMargin: 200px`。
- SSR 占位显示原始代码块(已在规格里),无 JS 时也能看到内容。
- 主题设置里给一个 `mermaid.enabled` 开关,不用的人可以完全关掉。

---

## 阶段五：发布

### 🟠 P17 · 首次发布时 Shiki 冷启动卡 3~5 秒

第一次发布时,Node 渲染器要:spawn → 加载主题 SSR bundle → `createHighlighter` 加载 15 种语言的语法文件 + 2 个主题。在 1C1G 上这可能要 3~5 秒。

用户看到的是进度条在 0/12 卡住几秒。虽然没超过 stall 阈值(60s),但**第一印象很差**。

**修正**:Go 在渲染器 `/health` 通过后,立即发一个 **warmup 请求**(渲染一个包含各语言代码块的空文档,不写盘)。这样用户的第一次真实发布就是热的。warmup 失败不阻塞启动,只记日志。

### 🟡 P18 · 1C1G 上常驻 Node 渲染器的 60MB

不发布的时候,Node 常驻 60MB 是纯浪费。但按需启动会让每次发布都慢 3~5 秒。

**修正**:新增 `render.workerIdleTimeout`(默认 `0` = 常驻;1C1G 推荐 `30m`)。空闲超时后 Node 退出,下次渲染请求时按需重启(带 warmup)。文档的「小内存部署」章节推荐开启。

### 🔴 P19 · 浏览器缓存导致「发布了但没生效」的错觉

发布成功,用户点「查看」→ 打开前台。但 HTML 的 `Cache-Control: public, max-age=300`。

用户改了文章再发布,**5 分钟内刷新看到的还是旧内容**。他会以为发布失败,反复重新发布,甚至去查日志。

这个问题在开发和小规模自测时出现频率极高,而且极其误导。

**修正**:
1. 后台的所有「查看」链接附加 `?__v=<该单元的 renderedAt 时间戳>`,既破浏览器缓存又不污染 CDN 的常规缓存键。
2. Nginx 示例配置里对 HTML 加 `ETag` 并允许 `must-revalidate` 的变体作为注释选项。
3. README 的「常见问题」第一条就写这个。

### 🟡 P20 · 发布后 `[查看]` 按钮的出现时机

已在 [docs/12 §6.2](12-reliability-ux.md) 定义(必须等该文章的 HTML 确实写盘后才出现),此处仅确认它在走查中是对的——用户点发布 → 约 1~2 秒后按钮才亮起,点开一定是新内容。

---

## 阶段六：AI 翻译

### 🔴 P21 · hreflang 双向对称导致 O(N²) 重渲染

规格要求 hreflang 必须双向对称,所以新增一个语言版本要重渲染其他所有语言的页面。

翻译是**依次完成**的:
```
en 完成  → 重渲染 zh-CN                    (1 次)
ja 完成  → 重渲染 zh-CN, en                (2 次)
de 完成  → 重渲染 zh-CN, en, ja            (3 次)
zh-TW 完成 → 重渲染 zh-CN, en, ja, de      (4 次)
                                     合计 10 次 + 5 次自身 = 15 次
```

单篇文章 5 语言 = 15 次渲染(而不是 5 次)。**批量翻译 100 篇 = 1500 次渲染而不是 500 次**,在 1C1G 上是几十分钟的差距。

**修正**:引入 **hreflang 重渲染的延迟合并窗口**。

```
翻译完成 → 立即入队「该语言自身的页面」(高优先级,用户要马上看到)
        → 「兄弟语言的 hreflang 更新」入队时使用 30s 合并窗口 + dedupe
```

30 秒内完成的多个语言翻译,其兄弟页面重渲染会被合并成一次。上例从 15 次降到 5 + 4 = 9 次,批量场景下收益更大(所有翻译跑完后统一刷一遍)。

实现:`jobs` 表已有 `dedupe_key` + `run_after`,把 `run_after` 设为 `now+30s` 即可,新任务到来时更新为最新的 `now+30s`(滑动窗口)。

### 🟠 P22 · 译文 slug 沿用中文

P13 修正后源 slug 可能是中文。`ai.translateSlug` 默认 `false` → 英文版 URL 变成 `/en/posts/我的服务器搭建记录/`。对英文读者和英文 SEO 都很怪。

**修正**:`ai.translateSlug` 改为智能默认 `auto`:
- 源 slug 全为 ASCII → 沿用(保持 URL 一致性)
- 源 slug 含非 ASCII → 让 AI 一并生成目标语言的 slug(在翻译 title 的那一批里附带,不额外增加请求)

### 🟠 P23 · 批量翻译前没有强制成本确认

一篇 3000 字文章翻译成 4 种语言约消耗 15~20k tokens。批量翻译 100 篇 = **150~200 万 tokens**。按主流模型定价是几美元到几十美元不等。

我的规格里有 `POST /translations/estimate` 接口,但**没规定批量操作前必须调用它**。用户很可能一次全选 200 篇点下去,然后收到账单才知道。

**修正**:批量翻译**必须**先展示确认对话框:

```
┌──────────────────────────────────────────────────────┐
│ 确认批量翻译                                            │
│                                                      │
│  47 篇文章 × 3 种语言 = 141 个任务                      │
│  预计消耗:约 1,240,000 tokens                         │
│  预计耗时:约 2 小时 15 分钟                            │
│                                                      │
│  ⚠️ 这会产生实际的 API 费用,请确认你的用量额度            │
│                                                      │
│  [ ] 我已了解费用                                      │
│              [取消]  [开始翻译(已禁用)]                 │
└──────────────────────────────────────────────────────┘
```

超过 20 个任务时强制显示;勾选框必须勾上才能提交。另外设置页提供可选的**月度 token 预算上限**,超出时暂停队列并告警。

### 🟡 P24 · zh-CN → zh-TW 是否值得走完整 AI

简繁转换用 AI 成本高、慢,而模型很容易只做字符转换不做用语转换(虽然我在提示词里强调了)。

**保持现状,但文档要说清楚**:zh-TW 走完整 AI 翻译是有意为之(为了「軟體/軟件」这类用语差异),用户若只需字符转换可以把 zh-TW 从 `translateTargets` 移除,手工用 OpenCC 处理。第二阶段可考虑内置 OpenCC 映射表作为 `zh-TW` 的快速通道。

---

## 阶段七：上域名与 HTTPS

### 🔴 P25 · Nginx bind mount symlink → 全量重建后服务旧内容

**这是最隐蔽也最严重的一个 bug。**

我的 compose 示例:
```yaml
volumes:
  - ./generated/public:/var/www/public:ro
```

但 `generated/public` 是一个**指向 `generated/releases/<ts>/` 的 symlink**([docs/04 §5.2](04-render-pipeline.md))。

Docker bind mount 在容器启动时解析宿主机路径 → **实际挂载的是当时 symlink 指向的那个 release 目录的 inode**。之后 Go 做全量重建、切换 symlink 到新 release,**nginx 容器里的挂载点纹丝不动,仍然服务旧 release**。

症状:增量发布的文章能看到(因为增量是就地写入当前 release),但**全量重建后所有变更消失**,重启 nginx 容器才恢复。用户完全无法理解发生了什么。

**修正**:
```yaml
volumes:
  - ./generated:/var/www/generated:ro     # 挂载父目录
```
```nginx
root /var/www/generated/public;           # 让 nginx 每次请求解析 symlink
```

额外注意:
- Nginx 的 `open_file_cache` 会缓存 inode,必须配 `open_file_cache_valid 10s;` 或在本场景直接关掉(`open_file_cache off;`,默认就是 off,示例配置里不要画蛇添足开启它)。
- `disable_symlinks` 保持默认 `off`。
- Go 自带静态服务(`serveStatic=true`)不受影响,因为它每次都走 `filepath.EvalSymlinks`。

### 🔴 P26 · baseURL 变更后不会自动重建

用户从 `http://203.0.113.10:8080` 改成 `https://blog.example.com`。

`server.baseURL` 属于「需重启」类配置。用户重启容器后:启动序列第 11 步**只在 `generated/public` 不存在时才触发重建** → 不会重建 → **全站的 canonical、hreflang、sitemap、RSS、og:url 全是旧的 IP 地址**。

用户不会发现,直到 Google Search Console 报错。

**修正**:引入 **release 指纹**。每次全量重建时在 `generated/releases/<ts>/.release.json` 写入:

```json
{
  "builtAt": "2026-08-09T12:00:00+08:00",
  "baseURL": "https://blog.example.com",
  "theme": "default",
  "themeSettingsHash": "sha256:...",
  "locales": ["zh-CN","en","ja","de","zh-TW"],
  "appVersion": "1.0.0"
}
```

启动时比对当前配置与当前 release 的指纹,**任一项不一致则自动触发全量重建**,并在日志和后台说明原因(「检测到站点地址已变更,正在重新生成全站页面」)。

同样适用于:切换主题、修改主题设置、增删语言、升级应用版本。

### 🟠 P27 · 没有 HTTPS 方案

`nginx.conf.example` 只有 `listen 80`,没有证书配置,compose 里 nginx 还是注释掉的。个人博主要自己搞 certbot,门槛不低。

**修正**:提供三套开箱即用的部署方案,README 让用户三选一:

| 方案 | 适用 | 提供物 |
|---|---|---|
| **A. Caddy(推荐)** | 大多数个人用户 | `deploy/compose.caddy.yml` + `Caddyfile`,**自动申请续期 Let's Encrypt 证书,零配置** |
| **B. Nginx + certbot** | 已有 nginx 经验 | `compose.nginx.yml` + `nginx.conf` + certbot 容器与续期说明 |
| **C. 仅 Go 直服** | 内网/测试/已有外层代理 | 默认 compose,`serveStatic: true` |

Caddy 方案的 Caddyfile 极简:
```
blog.example.com {
    root * /srv/generated/public
    handle /admin/* { reverse_proxy blog:8080 }
    handle /api/*   { reverse_proxy blog:8080 }
    handle /media/* { root * /srv; file_server }
    handle = /      { reverse_proxy blog:8080 }
    handle          { try_files {path} {path}/index.html; file_server }
    encode zstd gzip
}
```
注意同样要挂 `./generated:/srv/generated:ro`(P25 的同类问题)。

### 🟡 P28 · 备份下载被反代超时切断

500MB 的备份 zip 下载,Nginx 默认 `proxy_read_timeout 60s` 会切断。我在 [docs/12 §2](12-reliability-ux.md) 的超时矩阵里豁免了 Go 侧,但**忘了反代侧**。

**修正**:示例配置里为备份路由单独设置:
```nginx
location ^~ /api/admin/backup/ {
    proxy_pass http://blog:8080;
    proxy_read_timeout 3600s;
    proxy_buffering off;          # 流式下载,不缓冲到磁盘
}
```

---

## 阶段八：日常使用

### 🟠 P29 · 外部编辑(vim / git pull)的变更链路未定义

「Git Friendly」是核心卖点,用户一定会直接 `vim content/posts/.../index.zh-cn.md`。

我定义了 fsnotify → 重载索引 → 发业务事件 → 渲染入队,但**没定义 `Change.Before` 从哪来**——增量渲染的依赖计算需要变更前的快照(旧分类、旧 slug)才能正确清理。

**修正**:明确链路。

```
fsnotify 事件
  → index 在替换对象**之前**,先从当前内存索引取出旧对象作为 Before 快照
  → 解析新文件 → 构造 After 快照
  → 发 ArticleUpdated{Before, After}
  → render.AffectedUnits 正常工作
```

并明确外部编辑的语义(与后台编辑不同):

| 行为 | 后台编辑 | 外部编辑(vim/git) |
|---|---|---|
| bump `sourceRevision` | 发布时 ✅ | ❌ 不自动 bump |
| 权威字段镜像到其他语言 | ✅ | ❌ |
| 重算 outdated 状态 | ✅ | ✅ (仅当文件里的 revision 变了) |
| 自动触发 AI 翻译 | 按配置 | ❌ **绝不自动触发**(防止 git pull 一批文章烧掉大量 token) |
| 触发重渲染 | ✅ | ✅ |

最后一条尤其重要:`git pull` 拉入 300 篇文章时**绝不能自动触发 300 × N 个翻译任务**。后台在这种情况下应显示提示:「检测到 300 篇文章从外部导入,有 1200 个语言版本缺失 · [查看翻译矩阵]」,让用户主动决定。

### 🟡 P30 · 时区双源

`.env` 里有 `TZ=Asia/Shanghai`,`config.yaml` 里有 `site.timezone`。两者不一致时,定时发布的判定、归档的年月分组、后台的时间显示会打架。

**修正**:明确 `site.timezone` 是**唯一权威**,用于:定时发布比较、归档年月分组、后台时间显示、RSS 的 pubDate 格式化。容器的 `TZ` 只影响日志时间戳。两者不一致时启动打印一条 info 说明,不报错。

---

## 修订汇总

需要同步修改的规格文件:

| 文档 | 修改点 |
|---|---|
| `docs/02-content-format.md` | P13 slug 策略(消除自相矛盾)、P14 目录名与 slug 解耦、P12 translateTargets 取交集、P30 时区权威 |
| `docs/04-render-pipeline.md` | P21 hreflang 合并窗口、P26 release 指纹与自动重建触发 |
| `docs/06-ai-translation.md` | P22 translateSlug=auto、P23 批量成本确认、P9/P10 provider 诊断与快速测试 |
| `docs/10-security-ops.md` | P1 预构建镜像、P2 compose context、P3 entrypoint 权限、P4 themes 挂载、P5 密钥自动生成、P6 cookieSecure=auto、P7 Argon2 自适应、P25 symlink 挂载、P27 Caddy 方案、P28 备份超时 |
| `docs/12-reliability-ux.md` | P19 缓存破坏、P29 外部编辑链路 |
| `docs/11-milestones.md` | 新增 M12 的部署走查验收项 |

---

## 新增验收项(M12)

在一台**全新的 1C1G VPS** 上,由一个**没读过源码的人**执行,全程只看 README:

| # | 步骤 | 通过标准 |
|---|---|---|
| W1 | `docker compose up -d` | 90 秒内全部就绪,无需任何前置配置 |
| W2 | 通过 `http://IP:8080/admin/` 完成安装 | 一次成功,**不出现登录循环** |
| W3 | 配置 AI 并点「测试连接」 | 15 秒内给出结果;填错 Base URL 时提示能指导用户改对 |
| W4 | 写一篇含代码/图片/公式/mermaid 的中文文章并发布 | 3 秒内可访问,点「查看」看到的一定是新版本 |
| W5 | 触发翻译到 4 种语言 | 有真实进度,完成后 4 个 `.md` 文件人类可读可编辑 |
| W6 | 修改文章重新发布,立刻刷新前台 | **看到新内容**(不被浏览器缓存欺骗) |
| W7 | 配置域名 + Caddy 方案启用 HTTPS | 按 README 操作一次成功,证书自动签发 |
| W8 | 改 baseURL 并重启 | **自动全量重建**,sitemap/canonical 全部更新为新域名 |
| W9 | 用 vim 改一篇文章 | 索引与页面自动更新,**不触发 AI 翻译** |
| W10 | 触发全量重建,期间持续访问站点 | 无中断;重建完成后 Nginx/Caddy **立即**服务新内容(P25 不复现) |
| W11 | 全程观察内存 | 峰值 < 800MB,不触发 OOM Killer |

**W2、W6、W8、W10 是四个最容易翻车的点,必须重点验证。**
