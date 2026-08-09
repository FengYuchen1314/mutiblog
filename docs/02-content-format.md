# 02 · 内容与配置文件格式规格

> 本文档定义磁盘上所有真相源文件的**精确格式**。实现者必须严格按此实现解析与序列化。
> 所有 YAML 使用 `gopkg.in/yaml.v3`；所有时间使用 RFC3339 带时区。

---

## 1. Article Bundle（文章 / 页面）

### 1.1 目录布局

```
content/posts/<year>/<dir-name>/
├── metadata.yaml          # 必需
├── index.zh-cn.md         # 至少一个语言文件
├── index.en.md
├── index.ja.md
└── assets/                # 可选：随文附件（相对引用）
    └── diagram.png
```

页面同理，位于 `content/pages/<dir-name>/`（**不带年份层级**）。

**规则**：

| 规则 | 说明 |
|---|---|
| 目录名 | **与 slug 解耦**（见 §1.3.1）。取 slug 的 ASCII 安全形式；为空或过短时用 `<YYYYMMDD>-<id前6位>`。**一旦创建永不自动变更**，改 slug 也不动目录 |
| 语言文件名 | `index.<locale-url-form>.md`，locale 一律小写并用 `-` 连接，如 `zh-cn`、`zh-tw`、`en`、`ja`、`pt-br` |
| 权威 ID | `metadata.yaml` 的 `id` 字段；Front Matter 中的 `id` 必须与之一致，不一致时以 `metadata.yaml` 为准并在日志告警 |
| 年份目录 | 取源语言 Front Matter `date` 的年份；`date` 变更跨年时**不移动目录** |
| 目录唯一性 | 同一父目录下目录名唯一；创建时若冲突，追加 `-2`、`-3` |

### 1.2 `metadata.yaml`

承载**跨语言共享**的状态。这是翻译系统的核心状态文件。

```yaml
# content/posts/2026/my-server/metadata.yaml
id: 019fd210-e463-7709-9a23-9252a081279d
type: post                       # post | page
sourceLocale: zh-CN
sourceRevision: 19               # 源语言正文的版本号，单调递增
createdAt: 2026-08-09T12:00:00+08:00

translations:
  zh-CN:
    status: original             # 源语言恒为 original
    revision: 19
  en:
    status: completed
    translatedFromRevision: 19   # 基于源的第几版翻译的
    revision: 4                  # 该语言文件自身的版本号
    manualEdited: false
    provider: openai-compatible
    model: gpt-x
    updatedAt: 2026-08-09T12:03:11+08:00
    tokensUsed: 3412
  ja:
    status: outdated             # 因为 17 < 19
    translatedFromRevision: 17
    revision: 3
    manualEdited: false
    updatedAt: 2026-08-01T09:00:00+08:00
  de:
    status: manual               # 人工编辑过，永不自动覆盖
    translatedFromRevision: 18
    revision: 6
    manualEdited: true
    manualEditedAt: 2026-08-05T20:11:00+08:00
    updatedAt: 2026-08-05T20:11:00+08:00
  zh-TW:
    status: failed
    translatedFromRevision: 19
    error: "provider timeout after 3 retries"
    failedAt: 2026-08-09T12:05:00+08:00
    attempts: 3
```

**字段表**：

| 字段 | 类型 | 必需 | 说明 |
|---|---|---|---|
| `id` | string (UUIDv7) | ✅ | 永不变更 |
| `type` | enum | ✅ | `post` \| `page` |
| `sourceLocale` | BCP47 | ✅ | 规范形式（`zh-CN` 非 `zh-cn`） |
| `sourceRevision` | int | ✅ | 源语言正文每次「保存并发布」+1；仅保存草稿不 +1 |
| `createdAt` | RFC3339 | ✅ | |
| `translations` | map[locale]TranslationState | ✅ | 必须包含 sourceLocale 自身 |

**TranslationState**：

| 字段 | 类型 | 说明 |
|---|---|---|
| `status` | enum | `original`/`pending`/`translating`/`completed`/`failed`/`outdated`/`manual` |
| `translatedFromRevision` | int | 派生语言必填；用于 outdated 判定 |
| `revision` | int | 该语言文件自身修改次数 |
| `manualEdited` | bool | 一旦为 true，自动翻译永不覆盖该语言 |
| `manualEditedAt` | RFC3339 | |
| `provider` / `model` | string | 审计用 |
| `updatedAt` | RFC3339 | |
| `tokensUsed` | int | 成本统计 |
| `error` / `failedAt` / `attempts` | | 失败信息 |

**Outdated 判定规则**（每次源语言发布后对所有派生语言执行）：

```go
func recomputeStatus(meta *Metadata) {
    for loc, st := range meta.Translations {
        if loc == meta.SourceLocale { st.Status = StatusOriginal; continue }
        if st.Status == StatusPending || st.Status == StatusTranslating { continue }
        if st.ManualEdited {
            // 人工译文：源更新只标 outdated，绝不改成 pending
            if st.TranslatedFromRevision < meta.SourceRevision {
                st.Status = StatusManual   // 保持 manual，但 UI 用 revision 差显示"源已更新"
                st.SourceDrift = meta.SourceRevision - st.TranslatedFromRevision
            }
            continue
        }
        if st.Status == StatusCompleted && st.TranslatedFromRevision < meta.SourceRevision {
            st.Status = StatusOutdated
        }
    }
}
```

### 1.3 Front Matter

每个 `index.<locale>.md` 都以 YAML Front Matter 开头，`---` 包裹。

```markdown
---
id: "019fd210-e463-7709-9a23-9252a081279d"
title: "我的服务器搭建记录"
slug: "my-server"
description: "记录我的服务器搭建过程"
date: 2026-08-09T12:00:00+08:00
updated: 2026-08-09T13:00:00+08:00
status: published
categories:
  - linux
  - server
tags:
  - Debian
  - Docker
cover: "/media/2026/08/my-server-cover.webp"
author: admin
sourceLocale: zh-CN
locale: zh-CN
pinned: false
draft: false
toc: true
comments: true
seo:
  title: "服务器搭建完整记录 | My Blog"
  description: "从零开始的 Debian + Docker 服务器搭建"
  keywords: ["debian", "docker"]
  noindex: false
  ogImage: "/media/2026/08/og.png"
---

# 正文从这里开始
```

#### 权威字段 vs 镜像字段（**极其重要**）

| 分类 | 字段 | 规则 |
|---|---|---|
| **权威**（只以 sourceLocale 文件为准） | `id`, `date`, `status`, `categories`, `tags`, `author`, `pinned`, `cover`, `sourceLocale`, `draft`, `comments` | 派生语言文件中这些字段是**只读镜像**。保存派生语言时，Go 强制用源文件的值覆写。后台编辑派生语言时这些控件禁用 |
| **每语言独立** | `title`, `description`, `slug`, `updated`, `toc`, `seo.*` | 各语言可不同。`slug` 允许各语言不同（如 `/en/posts/my-server/` vs `/ja/posts/watashi-no-server/`），默认沿用源 slug |
| **系统字段** | `locale` | 由文件名推导并写入，用于自检 |

**字段表**：

| 字段 | 类型 | 必需 | 默认 | 说明 |
|---|---|---|---|---|
| `id` | string | ✅ | | UUIDv7，与 metadata.yaml 一致 |
| `title` | string | ✅ | | |
| `slug` | string | ✅ | 从 title 生成 | URL 片段，`[a-z0-9-]`，见 §1.4 |
| `description` | string | | `""` | 摘要，用于列表页与 meta description |
| `date` | RFC3339 | ✅ | 创建时间 | 发布时间；未来时间 + status=published → 定时发布 |
| `updated` | RFC3339 | | | |
| `status` | enum | ✅ | `draft` | `draft`/`published`/`unpublished`/`trashed` |
| `categories` | []string | | `[]` | 分类 **id**（非 name），必须存在于 `data/categories/` |
| `tags` | []string | | `[]` | 标签 **id** |
| `cover` | string | | | 站内绝对路径或外链 URL |
| `author` | string | ✅ | `admin` | 用户 id |
| `sourceLocale` | BCP47 | ✅ | | |
| `locale` | BCP47 | ✅ | | 本文件语言 |
| `pinned` | bool | | `false` | 置顶 |
| `toc` | bool | | 主题默认 | 是否显示目录 |
| `comments` | bool | | `true` | |
| `seo` | object | | | 见上例，全部可选 |

**页面（page）额外字段**：

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `template` | string | `page` | 主题模板名 |
| `order` | int | `0` | 菜单/列表排序 |
| `showInMenu` | bool | `false` | |

#### 解析规则

1. 文件必须以 `---\n` 开头（允许 UTF-8 BOM，解析时剥离）。
2. 找到第二个独占一行的 `---`，中间为 YAML。
3. 其后（跳过紧随的一个换行）全部为 Markdown 正文。
4. 若无 Front Matter → 解析失败，标记 `broken`（导入场景例外，见 §7）。
5. **序列化时字段顺序固定**（按上表顺序），保证 Git diff 稳定。使用 `yaml.Node` 手工构造以控制顺序与引号风格。
6. 空值字段**不写入**（除 `status`、`title`、`id` 等必需项），保持文件简洁。

#### Slug 规范化

> ⚠️ 本节已按 [docs/13 P13](13-first-run-walkthrough.md) 修订，消除了原先"保留 CJK"与"全 CJK 用 id"的自相矛盾。

由 `content.slugStrategy` 控制，默认 `preserve`：

| 策略 | 行为 | 适用 |
|---|---|---|
| `preserve`（默认） | 保留 CJK 原样，URL 中 percent-encode | 中日韩用户，URL 可读、SEO 无损 |
| `ai` | 已配置 AI 时调用模型生成英文 slug（单次请求） | 想要全英文 URL |
| `id` | 用 id 前 8 位 | 不在意 URL 可读性 |

规范化算法（三种策略共用，仅第 3 步不同）：

```
1. Unicode NFKC 归一化
2. 转小写
3. 按策略处理非 ASCII：
     preserve → 保留 \p{L}\p{N}（含 CJK）
     ai       → 调用 AI 生成，失败则回退 id
     id       → 直接用 id 前 8 位
4. 空格与 _ → -
5. 移除 [^\p{L}\p{N}-]
6. 折叠连续 -，去除首尾 -
7. 若为空 → 用 id 前 8 位
8. 保留字校验（见 docs/05 §URL 保留字），冲突则追加 -2
9. 同 locale 下 slug 唯一性校验，冲突则追加 -2
```

编辑器的 slug 输入框旁，配了 AI 时显示「✨ 生成英文链接」按钮，可随时一键切换。

##### §1.3.1 目录名生成（与 slug 独立）

```
1. 取 slug，移除所有非 [a-z0-9-] 字符
2. 若结果长度 < 2 → 用 `<YYYYMMDD>-<id前6位>`，例：20260809-019fd2
3. 同父目录下重名 → 追加 -2、-3
4. 创建后永不自动变更
```

**理由**：中文目录名会导致 Git `core.quotepath` 转义显示、Windows 编码问题、zip 备份的文件名编码不一致。目录名是实现细节，slug 才是用户可见的 URL——两者解耦后各自取最优解。

### 1.4 正文 Markdown 规格

支持范围（由 Node 侧 unified 管线保证）：

| 特性 | 插件 |
|---|---|
| CommonMark | `remark-parse` |
| GFM（表格/删除线/任务列表/自动链接） | `remark-gfm` |
| 脚注 | `remark-gfm`（含脚注） |
| 数学公式 `$...$` / `$$...$$` | `remark-math` + `rehype-katex` |
| 代码高亮 | `@shikijs/rehype` |
| 原始 HTML | `remark-rehype({allowDangerousHtml:true})` + `rehype-raw` |
| 标题锚点 | `rehype-slug` + `rehype-autolink-headings` |
| Mermaid | 识别 ` ```mermaid ` → 输出 island 容器，不走 Shiki |

**图片相对引用**：正文中 `![](./assets/diagram.png)` 在渲染时重写为 `/media/_bundles/<article-id>/diagram.png`，Go 在发布时把 bundle 的 `assets/` 复制/硬链到 `generated/public/media/_bundles/<id>/`。

---

## 2. 分类 `data/categories/<id>.yaml`

```yaml
id: linux                       # = 文件名，[a-z0-9-]，永不变更
slug: linux                     # URL 片段，可改
parent: technology              # 父分类 id，null 表示顶级
order: 10                       # 同级排序，升序
color: "#3b82f6"                # 可选，主题用
cover: "/media/cat/linux.png"   # 可选
name:
  zh-CN: Linux
  en: Linux
  ja: Linux
description:
  zh-CN: Linux 相关文章
  en: Articles about Linux
seo:
  noindex: false
```

**规则**：
- `id` 必须等于文件名（不含 `.yaml`）。
- `name` 必须包含 `defaultLocale`；缺失的语言 fallback 到 defaultLocale。
- **环检测**：加载后必须检测 parent 链无环、深度 ≤ 5，违规则该分类降级为顶级并记录错误。
- 删除分类时：若有子分类或被文章引用 → 返回 409，要求先处理（后台提供「迁移到其他分类」）。

## 3. 标签 `data/tags/<id>.yaml`

```yaml
id: docker
slug: docker
color: "#0ea5e9"
name:
  zh-CN: Docker
  en: Docker
description:
  zh-CN: 容器化相关
```

标签无层级、无 parent。删除标签时自动从所有文章 Front Matter 中移除（批量原子写 + 触发重渲染）。

## 4. 友链

### 4.1 分组 `data/links/_groups.yaml`

```yaml
groups:
  - id: friends
    order: 10
    name:
      zh-CN: 朋友
      en: Friends
  - id: recommended
    order: 20
    name:
      zh-CN: 推荐
      en: Recommended
  - id: tools
    order: 30
    name: { zh-CN: 工具, en: Tools }
  - id: projects
    order: 40
    name: { zh-CN: 项目, en: Projects }
  - id: organizations
    order: 50
    name: { zh-CN: 组织, en: Organizations }
```

### 4.2 单条 `data/links/<id>.yaml`

```yaml
id: example-blog
name: Example
url: https://example.com
logo: https://example.com/avatar.png
description:
  zh-CN: 一个技术博客
  en: A tech blog
group: friends
order: 10
status: active                  # active | pending | rejected | broken
createdAt: 2026-08-09T12:00:00+08:00
lastCheckedAt: 2026-08-09T12:00:00+08:00   # 可选，第二阶段做可用性巡检
```

`description` 允许是纯字符串（单语言）或 map（多语言），解析时统一成 map。

## 5. 菜单 `data/menus/<id>.yaml`

```yaml
id: header
name:
  zh-CN: 主菜单
  en: Header Menu
items:
  - id: m1
    type: page                  # url | post | page | category | tag | links | archive | search | custom
    ref: about                  # 目标 id/slug，type=url 时忽略
    label:
      zh-CN: 关于
      en: About
    icon: "lucide:info"         # 可选
    target: _self               # _self | _blank
    order: 10
    children:
      - id: m1-1
        type: url
        url: "https://github.com/xxx"
        label: { zh-CN: GitHub, en: GitHub }
        target: _blank
        order: 10
```

**链接解析**：渲染时按当前 locale 把 `type+ref` 解析成实际 URL。若目标不存在（如分类被删），该菜单项**静默跳过**并记录警告，不导致渲染失败。

支持嵌套深度 ≤ 2（父 + 子）。

## 6. 用户 `data/users/<id>.yaml`

```yaml
id: admin
username: admin
email: admin@example.com
displayName: 站长
avatar: /media/avatar.png
role: admin                     # admin | editor  (第二阶段: author | translator)
passwordHash: "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$hash..."
tokenVersion: 1                 # 改密码时 +1，使旧 session 失效
locale: zh-CN                   # 后台界面语言
createdAt: 2026-08-09T12:00:00+08:00
lastLoginAt: 2026-08-09T18:00:00+08:00
disabled: false
bio:
  zh-CN: 一个爱折腾的人
  en: Someone who likes tinkering
social:
  github: https://github.com/xxx
  twitter: https://x.com/xxx
```

文件权限 **0600**。启动时若 `data/users/` 为空，进入**首次安装模式**：`/admin/` 显示安装向导，创建首个 admin。

## 7. UI 文案 `data/translations/<locale>.yaml`

主题与后台的界面文案（非文章内容）。

```yaml
site:
  readMore: 阅读全文
  publishedAt: 发布于
  updatedAt: 更新于
  minRead: "{n} 分钟阅读"
  prevPost: 上一篇
  nextPost: 下一篇
  noResults: 没有找到结果
  searchPlaceholder: 搜索文章…
  tocTitle: 目录
  categories: 分类
  tags: 标签
  archive: 归档
  links: 友链
  page: "第 {n} 页"
```

缺失键 fallback 到 `defaultLocale`，再缺失则输出键名本身。主题可在 `themes/<name>/i18n/<locale>.yaml` 提供自己的文案，合并优先级：`data/translations` > `themes/*/i18n` > 内置默认。

---

## 8. 主配置 `config/config.yaml`

完整 schema，所有字段带默认值：

```yaml
schemaVersion: 2                 # content/data format; use `blog migrate` for older sites
server:
  host: "0.0.0.0"
  port: 8080
  baseURL: "https://example.com"     # 用于绝对 URL 生成，必填
  trustedProxies: ["127.0.0.1"]      # 用于 X-Forwarded-For / CF-Connecting-IP
  serveStatic: true                  # false 时不自己服务 generated/，交给 Nginx
  gracefulTimeout: 15s

paths:
  content: ./content
  data: ./data
  media: ./media
  themes: ./themes
  generated: ./generated
  cache: ./cache

site:
  title: My Blog
  description: Personal Technology Blog
  keywords: ["blog", "tech"]
  logo: /media/logo.png
  favicon: /media/favicon.ico
  author: admin
  copyright: "© 2026 My Blog"
  postsPerPage: 10
  excerptLength: 200                 # 自动摘要字符数
  timezone: "Asia/Shanghai"

i18n:
  defaultLocale: zh-CN               # / 无法判定时的兜底
  sourceLocale: zh-CN                # 新建文章的默认写作语言
  locales:                           # 启用的语言，顺序决定语言切换器顺序
    - code: zh-CN
      name: 简体中文
      urlPrefix: zh-cn
      enabled: true
    - code: zh-TW
      name: 繁體中文
      urlPrefix: zh-tw
      enabled: true
    - code: en
      name: English
      urlPrefix: en
      enabled: true
    - code: ja
      name: 日本語
      urlPrefix: ja
      enabled: true
    - code: de
      name: Deutsch
      urlPrefix: de
      enabled: true
  localeAliases:                     # Accept-Language 归一化
    zh: zh-CN
    zh-Hans: zh-CN
    zh-SG: zh-CN
    zh-MY: zh-CN
    zh-Hant: zh-TW
    zh-HK: zh-TW
    zh-MO: zh-TW
  countryLocaleMap:                  # IP fallback
    CN: zh-CN
    TW: zh-TW
    HK: zh-TW
    MO: zh-TW
    JP: ja
    DE: de
    AT: de
    CH: de
  cookieName: preferred_locale
  cookieMaxAge: 31536000
  redirectRoot: true                 # / → 302 /<locale>/
  autoTranslateOnPublish: true
  translateTargets: [zh-TW, en, ja, de]   # 空数组 = 全部启用的非源语言

render:
  workerEnabled: true
  workerCommand: ["node", "./renderer/server.js"]
  workerSocket: /tmp/blog-render.sock
  workerCount: 1                     # >1 时 spawn 多个 Node 进程
  concurrency: 4                     # Go 侧渲染任务并发（受 workerCount 限制）
  timeout: 30s
  output: ./generated/public
  keepReleases: 3                    # 保留几个历史 release
  prettyURLs: true                   # /posts/x/  → index.html
  minifyHTML: true

markdown:
  shiki:
    themes:
      light: github-light
      dark: github-dark
    langs: [go, rust, ts, js, tsx, jsx, python, bash, yaml, json, sql, html, css, diff, dockerfile, nginx]
    lineNumbers: false
    copyButton: true
  katex: true
  mermaid: true
  externalLinksNewTab: true
  headingAnchors: true
  tocMinDepth: 2
  tocMaxDepth: 3

storage:
  driver: local                      # local | s3 | r2  (MVP 仅 local)
  local:
    root: ./media
    publicPrefix: /media
  image:
    maxUploadSize: 20MB
    allowedTypes: [image/jpeg, image/png, image/gif, image/webp, image/avif, image/svg+xml]
    thumbnails:
      - { name: thumb, width: 400 }
      - { name: medium, width: 1200 }
    stripEXIF: true

ai:
  enabled: true
  provider: openai-compatible
  baseURL: "https://api.example.com/v1"
  apiKey: "${BLOG_AI_API_KEY}"       # 支持环境变量插值
  model: "gpt-x"
  temperature: 0.2
  maxTokensPerRequest: 4000
  timeout: 120s
  concurrency: 2
  maxRetries: 3
  rateLimitRPM: 60
  segmentBudget: 2500                # 每批发送给模型的字符预算
  systemPromptOverride: ""           # 空则用内置提示词

theme:
  active: default

search:
  enabled: true
  maxIndexSizeMB: 3                  # 超过则告警并截断正文字段
  bodyCharsPerDoc: 2000

comments:
  enabled: false
  provider: none                     # none | giscus | waline | twikoo | remark42
  # options 的字段由 provider 决定，见 §8.2。后台按 provider 渲染对应表单
  options: {}

seo:
  generateSitemap: true
  generateRSS: true
  rssItemCount: 20
  robotsTxt: |
    User-agent: *
    Allow: /
    Sitemap: {{baseURL}}/sitemap.xml

cache:
  htmlMaxAge: 300
  htmlSMaxAge: 86400
  htmlStaleWhileRevalidate: 604800
  assetMaxAge: 31536000
  mediaMaxAge: 86400
  mediaSMaxAge: 2592000
  purger: none                       # none | cloudflare
  cloudflare:
    zoneID: ""
    apiToken: "${BLOG_CF_TOKEN}"

security:
  sessionSecret: "${BLOG_SESSION_SECRET}"   # 必填，缺失时启动失败（dev 模式自动生成到 cache/）
  sessionMaxAge: 604800
  cookieSecure: true
  loginRateLimit: { attempts: 5, window: 15m, lockout: 30m }
  apiRateLimit: { rps: 20, burst: 50 }

backup:
  dir: ./backups
  keep: 10
  includeMedia: true

log:
  level: info                        # debug | info | warn | error
  format: json                       # json | text
  auditRetentionDays: 90
```

### 8.2 评论 provider 的 options schema

原先只写了 `options: {}`，未定义结构 —— 但各 provider 的参数完全不同，前端无法渲染表单，island 也不知道传什么。

```yaml
# giscus
options:
  repo: "owner/repo"
  repoId: "R_kgDO..."
  category: "Announcements"
  categoryId: "DIC_kwDO..."
  mapping: pathname            # pathname | url | title | og:title
  reactionsEnabled: true
  inputPosition: bottom        # top | bottom
  theme: preferred_color_scheme
  lang: ""                     # 空 = 跟随页面 locale

# waline
options:
  serverURL: "https://waline.example.com"
  emoji: true
  reaction: false
  pageview: true
  locale: {}                   # 覆盖内置文案

# twikoo
options:
  envId: "https://twikoo.example.com"
  region: ""

# remark42
options:
  host: "https://remark.example.com"
  siteId: "blog"
```

**实现要求**：
- 每个 provider 一份内置的字段定义（复用 [docs/09 §3.1](09-theme-system.md) 的 SchemaForm 字段类型），后台按选中的 provider 渲染对应表单并校验必填项。
- 切换 provider 时保留各自的 options（存成 `options.<provider>` 的子 map），避免来回切换丢配置。
- `Comments` island 收到 `{provider, options, locale, pageId}`，按 provider 动态 import 对应的加载器。
- **CSP**：`site` 的 CSP 需按 provider 追加其 origin 到 `script-src` 与 `frame-src`（[docs/10 §4](10-security-ops.md)），这必须是自动的，不能让用户手工改 CSP。

### 8.3 配置中的模板变量

`seo.robotsTxt` 里用了 `{{baseURL}}`，需明确模板语义：**仅支持一组固定的占位符做字符串替换，不引入模板引擎**。

| 占位符 | 值 |
|---|---|
| `{{baseURL}}` | `server.baseURL`（已去尾斜杠） |
| `{{siteTitle}}` | `site.title` |
| `{{year}}` | 当前年份（**仅用于 `site.copyright`**；robots.txt 中禁用，否则破坏渲染确定性，见 [docs/14 §4.3](14-cli-and-lifecycle.md)） |
| `{{locales}}` | 启用语言的 urlPrefix，逗号分隔 |

未知占位符**原样保留**并记录一条 warning，不报错。

### 8.1 配置加载规则

1. 读 `config/config.yaml`。
2. 若存在 `config/config.local.yaml`，深度合并覆盖（不入 Git）。
3. 环境变量覆盖：`BLOG_` 前缀 + 大写下划线路径，如 `BLOG_SERVER_PORT=9000`、`BLOG_AI_APIKEY=sk-x`。
4. 值中的 `${VAR}` 做环境变量插值，未定义时保持原样并告警。
5. 校验：`server.baseURL` 必填且为合法 URL；`i18n.locales` 非空且含 `defaultLocale` 与 `sourceLocale`；`security.sessionSecret` 长度 ≥ 32。
6. **热重载**：`config.yaml` 变化时重新加载。可热更的：`site.*`、`markdown.*`、`cache.*`、`ai.*`、`search.*`、`seo.*`、`theme.active`。需重启的：`server.*`、`paths.*`、`i18n.locales`（结构性变更）—— 后台提示「需要重启」。
7. 后台修改设置时**写回 config.yaml**，保留注释（用 `yaml.Node` 就地修改，不整体重写）。

---

## 9. SQLite `data/state.db`

WAL 模式，`busy_timeout=5000`，`foreign_keys=ON`。驱动 `modernc.org/sqlite`（纯 Go）。

```sql
-- 迁移版本
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);

-- 通用任务队列
CREATE TABLE jobs (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  kind          TEXT NOT NULL,        -- render | translate | rebuild | purge | backup
  dedupe_key    TEXT,                 -- 同 key 的 pending 任务合并
  payload       TEXT NOT NULL,        -- JSON
  priority      INTEGER NOT NULL DEFAULT 100,  -- 越小越优先
  status        TEXT NOT NULL DEFAULT 'pending', -- pending|running|done|failed|cancelled
  attempts      INTEGER NOT NULL DEFAULT 0,
  max_attempts  INTEGER NOT NULL DEFAULT 3,
  last_error    TEXT,
  run_after     TEXT NOT NULL,        -- RFC3339，退避与定时用
  locked_by     TEXT,
  locked_at     TEXT,
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_jobs_dedupe ON jobs(kind, dedupe_key) WHERE status='pending';
CREATE INDEX idx_jobs_claim ON jobs(status, priority, run_after);

-- 翻译任务（jobs 的业务视图，便于后台查询与统计）
CREATE TABLE translation_tasks (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  article_id    TEXT NOT NULL,
  source_locale TEXT NOT NULL,
  target_locale TEXT NOT NULL,
  source_revision INTEGER NOT NULL,
  status        TEXT NOT NULL,        -- pending|translating|completed|failed|cancelled
  job_id        INTEGER REFERENCES jobs(id) ON DELETE SET NULL,
  segments_total INTEGER DEFAULT 0,
  segments_done  INTEGER DEFAULT 0,
  tokens_in     INTEGER DEFAULT 0,
  tokens_out    INTEGER DEFAULT 0,
  provider      TEXT, model TEXT,
  error         TEXT,
  started_at    TEXT, finished_at TEXT,
  created_at    TEXT NOT NULL
);
CREATE INDEX idx_tt_article ON translation_tasks(article_id, target_locale);
CREATE INDEX idx_tt_status ON translation_tasks(status, created_at DESC);

-- 渲染队列（业务视图）
CREATE TABLE render_units (
  unit_key      TEXT PRIMARY KEY,     -- 如 "zh-CN:post:019fd2...", "en:home:2"
  kind          TEXT NOT NULL,        -- post|page|home|category|tag|archive|links|search|rss|sitemap|404
  locale        TEXT NOT NULL,
  output_path   TEXT NOT NULL,
  status        TEXT NOT NULL,        -- pending|rendering|done|failed
  attempts      INTEGER DEFAULT 0,
  last_error    TEXT,
  duration_ms   INTEGER,
  rendered_at   TEXT,
  updated_at    TEXT NOT NULL
);
CREATE INDEX idx_ru_status ON render_units(status, updated_at);

-- 定时发布
CREATE TABLE scheduled_publish (
  article_id    TEXT PRIMARY KEY,
  locale        TEXT NOT NULL,
  publish_at    TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'scheduled', -- scheduled|done|cancelled
  created_at    TEXT NOT NULL
);
CREATE INDEX idx_sp_due ON scheduled_publish(status, publish_at);

-- 登录尝试（限流与锁定）
CREATE TABLE login_attempts (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  identifier    TEXT NOT NULL,        -- username 或 IP
  ip            TEXT NOT NULL,
  success       INTEGER NOT NULL,
  user_agent    TEXT,
  created_at    TEXT NOT NULL
);
CREATE INDEX idx_la_lookup ON login_attempts(identifier, created_at DESC);

-- 审计日志
CREATE TABLE audit_log (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  actor         TEXT NOT NULL,
  action        TEXT NOT NULL,        -- article.publish / category.delete / settings.update ...
  target_type   TEXT, target_id TEXT,
  detail        TEXT,                 -- JSON
  ip            TEXT,
  created_at    TEXT NOT NULL
);
CREATE INDEX idx_audit_time ON audit_log(created_at DESC);
CREATE INDEX idx_audit_target ON audit_log(target_type, target_id);

-- 系统日志（渲染/翻译/解析错误，供后台"日志"页）
CREATE TABLE system_log (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  level         TEXT NOT NULL,
  component     TEXT NOT NULL,
  message       TEXT NOT NULL,
  detail        TEXT,
  created_at    TEXT NOT NULL
);
CREATE INDEX idx_syslog_time ON system_log(created_at DESC);
```

**再次强调**：以上所有表删除后系统必须能正常启动，仅丢失队列进度与历史日志。启动时若 `render_units` 为空且 `generated/public` 不存在 → 自动全量重建。

---

## 10. 媒体元数据 `media/.meta/<path>.json`

上传时生成 sidecar，避免每次读图片头。

```json
{
  "path": "2026/08/cover.png",
  "originalName": "screenshot 2026-08-09.png",
  "size": 348211,
  "mime": "image/png",
  "width": 2560,
  "height": 1440,
  "hash": "sha256:ab12...",
  "uploadedBy": "admin",
  "uploadedAt": "2026-08-09T12:00:00+08:00",
  "alt": { "zh-CN": "封面图", "en": "Cover" },
  "variants": [
    { "name": "thumb",  "path": "2026/08/cover.thumb.png",  "width": 400,  "height": 225 },
    { "name": "medium", "path": "2026/08/cover.medium.png", "width": 1200, "height": 675 }
  ]
}
```

若 sidecar 缺失（用户手工拷入文件），首次列目录时惰性生成。

---

## 11. 导入 Markdown 的容错规则

批量导入（`POST /api/admin/import`）需处理外部 Markdown：

| 情况 | 处理 |
|---|---|
| 无 Front Matter | 用文件名作 title/slug，`date` 取文件 mtime，`status: draft` |
| Hugo 风格 `+++` TOML | 支持解析 TOML front matter |
| `date` 格式非 RFC3339 | 依次尝试 `2006-01-02T15:04:05Z07:00`、`2006-01-02 15:04:05`、`2006-01-02`，失败则用 mtime |
| `categories`/`tags` 是显示名而非 id | 按 name 反查；查不到则**自动创建**分类/标签（可在导入选项中关闭） |
| 无 `id` | 生成新 UUIDv7 |
| 文件名含语言后缀（`post.en.md`） | 识别为同一 bundle 的语言版本 |
| 重复 slug | 追加 `-2` |
| 图片为相对路径 | 若同目录存在该文件，复制到 bundle 的 `assets/` |
