# 16 · 社区功能：评论、邮件、友链申请、RSS 聚合、版本更新

> 本文档新增五组功能，其中**评论与友链申请引入了全系统第一个公开写入 API**，这会修订核心原则 A2/A3 的措辞。先读 §1 再往下。

---

## 1. 架构影响：第一个公开写入 API

在此之前，公开访问面是**纯静态文件 + 一个 302 跳转**。这是整个项目的核心卖点([PLAN.md §0](../PLAN.md) A2/A3)。评论和友链申请打破了这一点——但可以打破得很克制。

### 1.1 修订后的原则

| 原则 | 原措辞 | 修订后 |
|---|---|---|
| **A2** | 运行时请求路径上不允许出现 Markdown 解析、React 渲染、AI 调用、数据库查询 | **页面 HTML 的生成**路径上不允许出现这些。评论内容在**提交时**渲染成 HTML 存好，读取时只是 JSON 序列化，仍不解析 Markdown |
| **A3** | 前台读路径不依赖后端进程存活 | **文章内容**的读路径不依赖后端存活。CMS 挂掉时文章、导航、样式、图片全部正常，**仅评论区降级**为"评论暂时无法加载" |

**这是可接受的降级**，但必须是**显式设计**而非事后发现：

```
CMS 正常  → 文章静态 HTML + 评论 island 异步拉 /api/public/comments → 完整体验
CMS 挂掉  → 文章静态 HTML 照常 + 评论区显示"评论服务暂时不可用" → 文章仍 100% 可读
```

### 1.2 三条硬约束

1. **评论绝不触发文章页重渲染**。每条评论都重渲染一次文章页是灾难(缓存全废、队列爆炸)。评论走客户端异步加载。
2. **评论内容在提交时就渲染成 HTML 存好**(经 Node 的 `/markdown` + 严格 sanitize)。读取接口只吐已渲染好的 HTML，运行时零解析。
3. **公开写入 API 隔离在 `/api/public/*`**，与 `/api/admin/*` 完全分离：独立的限流、独立的中间件链、无会话、无 CSRF(改用其他反滥用手段)。

### 1.3 SEO 取舍与"烘焙"

评论走客户端加载 → **搜索引擎抓不到评论内容**。Giscus/Waline 有同样问题，通常可接受。

但我们有静态渲染管线，可以做得更好：

```yaml
comments:
  bakeIntoHTML: true        # 审核通过的评论烘焙进文章 HTML
  bakeDebounce: 10m         # 合并窗口，避免频繁重渲染
```

开启后：评论审核通过 → 该文章页加入渲染队列(10 分钟合并窗口，优先级 80)→ 已审核评论作为 SSR 内容写进 HTML → island 接管后再拉取增量的新评论。

**兼顾 SEO 与实时性，且重渲染频率可控**(10 分钟内多条评论只重渲一次)。默认关闭，评论量大的站点建议开启。

---

## 2. 原生评论系统

### 2.1 存储：文件，不是数据库

遵循 A1/A4——评论是**用户内容**，不是可丢弃的运行状态，因此必须是文件。

```
data/comments/
├── 019fd210-e463-7709-9a23-9252a081279d.yaml   # 一篇文章一个文件
├── .blocklist.yaml                              # 屏蔽的 IP/邮箱/关键词
└── .unsubscribed.yaml                           # 退订邮件通知的邮箱哈希
```

```yaml
# data/comments/<article-id>.yaml
articleId: 019fd210-e463-7709-9a23-9252a081279d
count: 12
lastCommentAt: 2026-08-09T14:22:00+08:00
comments:
  - id: 01J7XKQ8M2N4P6R8T0V2W4Y6Z8
    parent: null                      # 父评论 id，实现楼中楼
    locale: zh-CN                     # 评论发生在哪个语言版本的页面
    author:
      name: 张三
      email: zhangsan@example.com     # ★ 明文存储，见 §2.2 隐私
      emailHash: "d41d8cd98f00b204"   # md5，用于 Gravatar
      website: https://zhangsan.dev
      ip: "203.0.113.0"               # 末段掩码，脱敏
      ua: "Mozilla/5.0 ..."
      isAdmin: false
    content: |
      写得很好！请问 `docker compose` 那段…
    contentHTML: "<p>写得很好！请问 <code>docker compose</code> 那段…</p>"
    status: approved                  # pending | approved | spam | trashed
    notify: true                      # 是否接收回复邮件
    createdAt: 2026-08-09T14:22:00+08:00
    updatedAt: null
    spamScore: 0.02
    spamReasons: []
```

**设计要点**：

| 决策 | 理由 |
|---|---|
| 一篇文章一个文件 | 单文件通常 < 100KB(几百条评论)。整体读写，`AtomicWrite` 保证一致性 |
| 保留 `content`(Markdown) 与 `contentHTML` 两份 | 前者是真相源可编辑，后者供运行时零解析读取 |
| 评论不分语言存储 | 同一篇文章的评论在所有语言版本下共享(默认)。可配 `comments.perLocale: true` 改为按语言隔离 |
| `count`/`lastCommentAt` 冗余在文件头 | 后台列表、仪表盘、文章列表统计无需读全文件 |
| SQLite 只做索引 | `comment_index` 表(article_id, comment_id, status, created_at)供后台跨文章查询与排序。**可删除，从文件重建** |

高并发下的写入：同一文章的评论写入过 `fsutil.KeyedMutex`(key = articleID)。评论提交本身是低频操作，无性能顾虑。

### 2.2 隐私与合规

评论者邮箱是**必须明文存储**的(否则无法发回复通知)。对应义务：

- `data/comments/` 权限 **0700**，文件 0600
- **邮箱绝不出现在任何 API 响应中**(公开接口只返回 `emailHash` 供 Gravatar)
- 备份包含评论 → `backup create --exclude-secrets` 时**同时脱敏评论邮箱**
- 通知邮件底部必须有**退订链接**(签名 token)
- 评论表单需有"我同意本站存储我的邮箱用于回复通知"的说明(可配置文案)
- 提供 `blog-server comments purge-email <email>` 删除某人的全部评论(GDPR 删除权)

### 2.3 评论的 Markdown 子集

**绝不允许完整 Markdown**——图片、HTML、标题都是滥用向量。

| 允许 | 禁止 |
|---|---|
| `**粗体**` `*斜体*` `~~删除~~` | 图片 `![]()` |
| `` `行内代码` `` 和代码块 | 原始 HTML |
| `[链接](url)`(自动加 `rel="nofollow ugc noopener"`) | 标题 `#` |
| `> 引用` | 表格 |
| 无序/有序列表 | 脚注、数学公式、Mermaid |

渲染路径：Go 收到评论 → 调 Node `/markdown` 并传 `preset: "comment"` → Node 用受限的 unified 管线 + `rehype-sanitize` 严格白名单 → 返回 HTML → Go 存盘。

额外硬限制：正文 ≤ 3000 字符、链接 ≤ 3 个、嵌套深度 ≤ 3 层。

### 2.4 反垃圾（分层，全部内置无外部依赖）

自托管评论的**唯一真正难题**。七层防护，从廉价到昂贵：

| # | 层 | 做法 | 拦截率 |
|---|---|---|---|
| 1 | **蜜罐字段** | 表单含 CSS 隐藏的 `website_url` 字段，填了即判定机器人 | 高，零成本 |
| 2 | **时间陷阱** | 表单渲染时间戳(签名)，提交间隔 < 3 秒或 > 24 小时 → 拒绝 | 高 |
| 3 | **限流** | 同 IP 5 条/小时、同邮箱 10 条/小时、全站 100 条/小时 | 中 |
| 4 | **内容规则** | 链接数、全大写比例、关键词黑名单(`.blocklist.yaml`)、重复内容检测 | 中 |
| 5 | **首评审核** | 该邮箱首次评论默认 `pending`，通过一次后自动放行(可配置) | 高 |
| 6 | **屏蔽名单** | IP 段 / 邮箱 / 域名 / 正则，后台一键"标记为垃圾并屏蔽" | — |
| 7 | **可选外部** | Akismet API 或 Cloudflare Turnstile。**默认关闭**，配置项预留 | 高 |

```yaml
comments:
  enabled: true
  provider: native              # native | giscus | waline | twikoo | remark42 | none
  requireApproval: first-time   # always | first-time | never
  requireEmail: true
  requireName: true
  allowWebsite: true
  perLocale: false
  maxLength: 3000
  maxLinks: 3
  maxDepth: 3
  bakeIntoHTML: false
  bakeDebounce: 10m
  closeAfterDays: 0             # 0 = 永不关闭；>0 则文章发布 N 天后自动关闭评论
  antiSpam:
    honeypot: true
    minSubmitSeconds: 3
    rateLimitPerIPHour: 5
    rateLimitPerEmailHour: 10
    keywordBlocklist: []
    akismetKey: ""              # 空 = 不启用
    turnstileSiteKey: ""
    turnstileSecret: ""
```

### 2.5 公开 API `/api/public/*`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/public/comments?article=<id>&locale=&after=&limit=` | 只返回 `approved`，按 `createdAt` 升序，支持增量拉取 |
| POST | `/api/public/comments` | 提交评论 |
| GET | `/api/public/comments/token?article=<id>` | 获取表单签名 token(含时间戳，用于时间陷阱) |
| GET | `/api/public/unsubscribe?t=<signed>` | 退订回复通知 |
| DELETE | `/api/public/comments/<id>?t=<signed>` | 评论者自助删除(通知邮件里的链接) |

**响应中的评论对象**(注意没有 email/ip/ua)：
```json
{
  "id": "01J7XK...", "parent": null,
  "author": { "name": "张三", "emailHash": "d41d8c...", "website": "https://…", "isAdmin": false },
  "contentHTML": "<p>…</p>",
  "createdAt": "2026-08-09T14:22:00+08:00"
}
```

`POST` 的响应：
- 直接通过 → `201` + 评论对象
- 进入审核 → `202` + `{"status":"pending","message":"评论已提交，待审核后显示"}`
- 判定垃圾 → **返回 202 假装成功**(不告诉机器人被识破)，实际存为 `spam`

中间件链：`RealIP → PublicRateLimit → NoStore`。**无会话、无 CSRF**(公开接口本就无凭据可窃)，改用 Origin 校验 + 表单 token。

### 2.6 后台评论管理 `/admin/comments`

参考 Halo/WordPress：

- Tab：待审核(带角标) / 已通过 / 垃圾 / 回收站
- 列表：头像、昵称、邮箱、内容摘要、所属文章、IP/地区、时间、垃圾评分
- 操作：通过 / 标记垃圾 / 删除 / 编辑 / **回复**(以管理员身份，`isAdmin: true`)
- 批量：全部通过、全部标垃圾、清空垃圾箱
- 一键"标记垃圾并屏蔽此 IP/邮箱"
- 设置：黑名单关键词、屏蔽名单编辑
- 文章列表增加"评论数"列，可跳转筛选

### 2.7 前台 Comments island

```
hydrate="visible"   # 滚动到评论区才加载，不拖累首屏
```

功能：列表(楼中楼，默认展开 2 层)、Markdown 简易工具栏、实时预览、提交、回复、Gravatar(可配置为纯 CSS 生成的字母头像，避免第三方请求)、加载失败降级提示、`sessionStorage` 记住昵称/邮箱/网站。

**无 JS 时**：SSR 阶段若 `bakeIntoHTML=true` 则显示已审核评论(只读)；否则显示"评论需要 JavaScript"。

---

## 3. 邮件通知

### 3.1 配置

```yaml
mail:
  enabled: false
  driver: smtp                  # smtp | none（第二阶段：resend | ses | mailgun）
  smtp:
    host: smtp.example.com
    port: 587
    username: noreply@example.com
    password: "${BLOG_SMTP_PASSWORD}"
    encryption: starttls        # none | ssl | starttls
    from: "My Blog <noreply@example.com>"
    replyTo: ""
    timeout: 30s
  events:
    commentReceived: true       # → 管理员
    commentReplied: true        # → 被回复的评论者
    commentApproved: false      # → 评论者（评论通过审核）
    linkApplication: true       # → 管理员
    translationFailed: true     # → 管理员
    renderFailed: true
    backupFailed: true
    diskLow: true
    updateAvailable: false
  digest:
    enabled: true
    interval: 1h                # 同类通知在窗口内合并成一封，防轰炸
    maxPerDay: 20               # 硬上限，超过则只发一封汇总
```

### 3.2 实现

- **SMTP 客户端**：Go 标准库 `net/smtp` 功能太弱(不支持 STARTTLS 的完整协商、无连接复用)。用 `github.com/wneessen/go-mail`(纯 Go、无 cgo、维护活跃)。
- **发送走队列**：`jobs` 表 `kind=mail`，复用现有的重试、退避、熔断([docs/12 §4](12-reliability-ux.md))。SMTP 挂掉不影响任何主流程。
- **模板**：`data/mail-templates/<name>.<locale>.{html,txt}`，用户可覆盖。内置 `comment-received`、`comment-replied`、`link-application`、`system-alert`、`digest`。同时发 HTML 与纯文本两个 part。
- **模板变量**：与 [docs/02 §8.3](02-content-format.md) 同一套占位符机制，不引入模板引擎。
- **退订**：所有发给非管理员的邮件底部含签名退订链接。退订记录写 `data/comments/.unsubscribed.yaml`(存邮箱 SHA256，不存明文)。
- **测试**：设置页"发送测试邮件"按钮，独立 15s 超时，失败时分段报告(DNS / 连接 / TLS / 认证 / 投递)。

### 3.3 摘要模式（防轰炸）

一篇热门文章 10 分钟来 50 条评论，管理员不该收 50 封邮件。

```
新事件到达 → 若同类事件在 digest.interval 内已有待发摘要 → 追加进去
           → 否则创建摘要任务，run_after = now + interval
到期 → 发一封"过去 1 小时有 12 条新评论"的汇总邮件（含列表与直达链接）
```

`commentReplied`(发给评论者)**不走摘要**——那是一对一的即时通知，必须立即发。

---

## 4. 友链自助申请

数据模型已支持(`Link.status: pending`)，只需补交互层。

### 4.1 流程

```
访客在友链页填表 → POST /api/public/link-applications
  → 反垃圾（复用 §2.4 的 1/2/3/4/6 层）
  → 可选：自动校验对方站点是否已反向链接到本站（见 §4.2）
  → 写 data/links/<generated-id>.yaml，status: pending
  → 邮件通知管理员
  → 返回 202「申请已提交，审核通过后会出现在友链页」

管理员在 /admin/links 的「待审核」Tab
  → 通过 → status: active → 触发友链页重渲染
  → 拒绝 → status: rejected（保留记录防止重复提交骚扰）
```

### 4.2 反向链接自动校验（很实用）

友链的社区规范是"先加我，我再加你"。系统可以自动查：

```go
// 抓取申请者的 URL（复用 media 的 SSRF 防护 + 10s 超时 + 2MB 上限）
// 在 HTML 中查找指向本站 baseURL 的 <a href>
func checkBacklink(ctx context.Context, siteURL, myBaseURL string) (bool, error)
```

结果作为**参考信息**显示在审核界面(`✅ 已找到指向本站的链接` / `⚠️ 未找到`)，**不自动拒绝**——对方可能放在子页面或还没加。

配置 `links.application.requireBacklink: false`(默认)，开启后未找到反链的申请直接提示申请者先添加。

### 4.3 表单字段

名称、URL、Logo URL(可选)、描述、期望分组、联系邮箱(仅管理员可见，用于通知审核结果)。全部走 §2.4 的反垃圾。

配置：
```yaml
links:
  application:
    enabled: true
    requireEmail: true
    requireBacklink: false
    autoApprove: false          # ⚠️ 强烈不建议开启
    notice:                     # 显示在表单上方的多语言说明（申请要求）
      zh-CN: "请先在贵站添加本站链接，并保证站点可正常访问、无违规内容。"
```

---

## 5. 友链 RSS 聚合（朋友圈）

中文独立博客圈的经典功能：定期抓取友链的 RSS，聚合成一个"朋友圈"页面。

### 5.1 配置

```yaml
friendsFeed:
  enabled: false
  fetchInterval: 6h
  timeout: 15s
  concurrency: 3
  maxItemsPerFeed: 10
  maxTotalItems: 200
  retentionDays: 90
  autoDiscover: true            # 从友链 URL 自动发现 feed 地址
  userAgent: "MutiBlog/1.0 (+{{baseURL}})"
  hideBrokenAfterFailures: 5    # 连续失败 N 次后从页面隐藏（不删除）
```

### 5.2 抓取

```
调度器每 fetchInterval 触发（jobs, kind=feed_fetch, dedupe_key=link-id）
  → 确定 feed 地址：
       Link.feedURL 显式配置 > 自动发现（抓首页找 <link rel="alternate" type="application/rss+xml">）
       > 常见路径探测（/rss.xml, /feed, /atom.xml, /index.xml, /feed.xml）
  → HTTP GET（带 If-Modified-Since / If-None-Match，尊重 304）
  → 解析 RSS 2.0 / Atom 1.0 / JSON Feed
  → 归一化为统一结构，写入 SQLite feed_items 表
  → 更新 Link 的 feedStatus / lastFetchedAt / consecutiveFailures
```

**安全**(复用现有 SSRF 防护)：拒绝私有网段与云元数据地址、限制重定向次数(≤3)、响应大小上限 5MB、只接受 XML/JSON Content-Type、**解析时禁用 XML 外部实体(XXE)**。

**存储在 SQLite**：这是真正可丢弃的缓存数据(随时可重新抓取)，符合 A4。

```sql
CREATE TABLE feed_items (
  id            TEXT PRIMARY KEY,      -- sha256(link_id + item_guid)
  link_id       TEXT NOT NULL,
  title         TEXT NOT NULL,
  url           TEXT NOT NULL,
  summary       TEXT,                  -- 纯文本，截断到 200 字
  published_at  TEXT NOT NULL,
  fetched_at    TEXT NOT NULL
);
CREATE INDEX idx_feed_published ON feed_items(published_at DESC);
```

**内容安全**：只取 title / link / pubDate / summary(**剥离全部 HTML 转纯文本**)。绝不渲染友站的 HTML 内容——那是 XSS 直通车。

### 5.3 页面生成

新增 RenderUnit：`friends_feed:<locale>` → `<prefix>/friends/index.html`。

每个抓取周期结束后重新生成(每语言 1 个页面，成本可忽略)。同时输出 `<prefix>/friends-feed.json` 供主题的 island 做客户端筛选/排序。

主题模板 `FriendsFeed.tsx`：按时间倒序的卡片流，显示友站头像、站名、文章标题、时间、摘要。支持按友链筛选。

### 5.4 后台

`/admin/links` 增加「订阅状态」列：✅ 正常(最后抓取时间) / ⚠️ 未发现 feed / ❌ 连续失败 N 次。
操作：手工指定 feed 地址、立即抓取、暂停订阅、查看抓取日志。
OPML 导入导出(便于从其他阅读器迁移友链)。

---

## 6. 版本更新

### 6.1 Docker 场景：应用无法自我更新

必须说清楚这个物理事实：**容器内的进程无法替换自己所在的镜像**。所谓"一键更新"在 Docker 下只有三条路：

| 方案 | 评价 |
|---|---|
| 挂载 `/var/run/docker.sock` 让应用自己拉镜像重启 | ❌ **明确拒绝**。等于把宿主机 root 权限交给 Web 应用，一个 RCE 就是完整的容器逃逸。任何要求挂 docker.sock 的博客系统都不该装 |
| 外部工具(Watchtower 等)监听镜像更新 | ✅ 可行，但不是我们的代码。README 提一句即可 |
| **应用内检查 + 通知 + 给出复制即用的命令** | ✅ **本项目采用** |

### 6.2 采用的方案

```
后台 → 系统 → 关于
┌────────────────────────────────────────────────────────┐
│ 当前版本  v1.0.3                                        │
│ 🎉 有新版本 v1.1.0 (2026-08-20)                         │
│                                                        │
│ 更新内容：                                              │
│  · 新增友链 RSS 聚合                                     │
│  · 修复分页增量渲染的边界问题                             │
│  · ⚠️ 包含内容格式迁移 (schema 3 → 4)                    │
│                                                        │
│ 更新方式（复制到服务器执行）：                             │
│ ┌──────────────────────────────────────────────────┐  │
│ │ docker compose pull && docker compose up -d      │  │
│ └──────────────────────────────────────────────────┘  │
│                                    [复制] [查看完整日志] │
│                                                        │
│ ℹ️ 升级会自动备份并迁移数据，无需手工操作                   │
└────────────────────────────────────────────────────────┘
```

检查更新：`GET https://api.github.com/repos/<owner>/<repo>/releases/latest`(可配置为自建端点)，比较 semver，缓存 24h。**失败静默**(不能因为 GitHub 不可达就在后台报错)。

```yaml
update:
  checkEnabled: true
  checkInterval: 24h
  channel: stable               # stable | prerelease
  endpoint: ""                  # 空 = 用 GitHub Releases API
  notifyInAdmin: true
  notifyByEmail: false
```

### 6.3 裸机部署：`blog-server self-update`

非 Docker 部署(直接跑二进制)可以真正自更新：

```
1. 查询最新版本，比较 semver
2. 下载对应平台的二进制 + .sha256 + .sig
3. 校验 SHA256；校验 minisign/cosign 签名（★ 公钥内置在二进制里）
4. 备份当前二进制到 blog-server.v1.0.3.bak
5. 原子替换：写 blog-server.new → chmod +x → rename
6. 提示用户重启服务（或 --restart 自动 exec 新二进制）
7. 新版本启动时自动执行 §6.4 的升级链
失败任一步 → 回滚，原二进制不受影响
```

**签名校验不可跳过**。没有签名的自更新等于给用户装了一个后门——中间人替换下载内容即可完全控制服务器。若发布流程暂无签名能力，则 `self-update` 命令**不实现**，只保留检查与通知。

### 6.4 升级链（与 docs/14 §5 串联）

无论 Docker 还是裸机，新版本首次启动时：

```
1. 比较 content schemaVersion
2. 需要迁移 → 强制自动备份 → 执行迁移 → 更新 schemaVersion
3. 比较 release 指纹（appVersion 变了）→ 触发全量重建
4. 校验当前主题的 engine.minVersion → 不兼容则回退 default + 红色横幅
5. 后台显示「已从 v1.0.3 升级到 v1.1.0」+ 更新日志 + 备份位置
```

**降级保护**：旧二进制读到更高的 schemaVersion 直接拒绝启动([docs/14 §5.2](14-cli-and-lifecycle.md))。

---

## 7. 工作量与排期

| 功能 | 人日 | 依赖 | 建议阶段 |
|---|---|---|---|
| 评论系统(存储/API/反垃圾/后台/island) | 8 | M4, M5 | **M14** |
| 邮件通知(SMTP/模板/队列/摘要/退订) | 4 | M11 | **M15**(评论的回复通知依赖它) |
| 友链自助申请 | 2 | M14(复用反垃圾) + M15 | M16 |
| 友链 RSS 聚合 | 4 | M4, M7 | M17 |
| 版本更新(检查/通知/self-update/升级链) | 3 | M13 | M18 |
| | **21** | | |

在原 57 人日基础上 **+37%**，总计 **78 人日**。

### 7.1 排期建议

这五项**不应插入 MVP**——它们全部依赖一个能正常发布文章的系统。建议作为**第 1.5 阶段**，在 M13 之后：

```
M0 ─────────────── M13   核心系统（78 人日中的 57）
                    │
                    ├── M14 评论  ┐
                    ├── M15 邮件  ├─ 强关联，建议连着做
                    ├── M16 友链申请 ┘
                    ├── M17 RSS 聚合  （独立，可提前或延后）
                    └── M18 版本更新  （独立，越早越好——早期用户就要升级）
```

**唯一的例外是 M18(版本更新)**：它的价值随时间递增，且第一批用户装上后就需要升级路径。如果要提前，建议只做 §6.2 的"检查+通知+命令"部分(约 1 人日)，`self-update` 留后。

### 7.2 如果只能做两个

**评论 + 邮件**。它们互相成就(回复通知是评论体验的一半)，且是"博客"作为社区节点的核心。RSS 聚合虽然讨喜，但没有它博客依然完整。

---

## 8. 验收

| # | 测试 | 期望 |
|---|---|---|
| S1 | 提交一条含 `<script>` 与图片语法的评论 | HTML 中无 script，图片语法被剥离，链接带 `rel="nofollow ugc"` |
| S2 | 3 秒内提交表单 / 填了蜜罐字段 | 判定垃圾，返回 202 但存为 spam |
| S3 | 同 IP 1 小时内提交 6 条 | 第 6 条被限流 |
| S4 | 首次评论者提交 | 状态 pending；管理员通过后该邮箱再评论直接放行 |
| S5 | **`kill -9` CMS 后访问文章页** | 文章、导航、样式、图片全部正常；评论区显示"暂时不可用"；**页面本身无任何报错** |
| S6 | 评论审核通过（`bakeIntoHTML=true`） | 10 分钟内该文章页重渲染，HTML 中含评论内容 |
| S7 | 回复某条评论 | 被回复者 60 秒内收到邮件，含直达链接与退订链接 |
| S8 | 点击退订链接后再次被回复 | 不再收到邮件 |
| S9 | 1 小时内 50 条评论 | 管理员收到 **1 封摘要邮件**，不是 50 封 |
| S10 | SMTP 配置错误 | 邮件任务重试后失败并告警；**评论提交本身不受影响** |
| S11 | 友链申请提交 | 生成 pending 的 YAML；管理员收到通知；反链校验结果正确显示 |
| S12 | 友链指向 `http://169.254.169.254/` | 反链校验拒绝该地址 |
| S13 | RSS 聚合抓取含 XXE 攻击的 feed | 外部实体被禁用，无文件读取 |
| S14 | 友站 feed 返回含 `<script>` 的 summary | 输出为纯文本，无 HTML |
| S15 | 友站连续 5 次抓取失败 | 从朋友圈页隐藏，后台标记为异常，**不删除该友链** |
| S16 | 备份后检查 zip 中的评论文件 | `--exclude-secrets` 时评论者邮箱已脱敏 |
| S17 | `comments purge-email <email>` | 该邮箱的全部评论被删除，相关文章页重渲染 |
| S18 | 后台检查更新 | 正确显示新版本与更新日志；GitHub 不可达时静默失败不报错 |
| S19 | 升级到含 schema 迁移的新版本 | 自动备份 → 迁移 → 全量重建 → 后台显示升级摘要 |
| S20 | 用旧版本二进制启动新版本的数据 | **拒绝启动**并提示从备份恢复 |
