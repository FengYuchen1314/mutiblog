# 第二轮任务表

> 审阅日期：2026-08-10 · 基于第一轮（T0~T15）完成后的实际代码逐条核实
> 前置：`TASKS.md` 已全部完成并验证通过 · 必读：`docs/17-spec-vs-reality.md`

---

## 一、第一轮验收结论

**已验证通过，无虚报。**

| 项 | 实测 |
|---|---|
| 构建 + 测试 | 21 个包全绿，0 失败 |
| 提交纪律 | 16 个提交，一任务一提交，工作区干净 |
| 行长 ≤120 | 源码 **0 违规**（超长行全在 `backend/web/admin/assets/` 构建产物） |
| 主题源码 | 31 个源文件，vite 双构建 + manifest |
| Islands | 文章页 18 个 `data-island`，仅 3 个 `<script>` |
| 渲染器 | 拆成 5 模块共 531 行（原先单行 5040 字符） |
| `verify` | 退出码 0 |
| 魔鬼夹具 | Shiki / KaTeX / Mermaid / 表格 / 任务列表 / 脚注 **全部通过**，meta 六字段齐全 |

**四个阻断性问题 B1~B4 已全部解决。** 后端保持未动，是正确的。

---

## 二、本轮要解决的问题

按严重度排序。**R1 是产品级缺陷，优先于其他所有项。**

---

### 🔴 R1 · 多语言编辑闭环（**本轮最高优先级**）

**问题**：这是本产品的核心差异点，但当前**编辑器只能编辑源语言**。

核实证据：
```
frontend/admin/src/features/posts/PostEditorPage.tsx:25   api('/api/admin/posts/' + id + '?locale=zh-CN')
frontend/admin/src/features/posts/PostEditorPage.tsx:43   locale: 'zh-CN',
frontend/admin/src/features/posts/PostEditorPage.tsx:116  api('.../revisions?locale=zh-CN')
```
`zh-CN` 是**硬编码**的。后端 `?locale=` 参数其实已经支持（`server.go` 多处读取），是前端没接上。

同时后端缺两个端点（`docs/07 §3.3` 规定但未实现）：
```
POST   /api/admin/posts/{id}/locales/{locale}     手工创建某语言版本
DELETE /api/admin/posts/{id}/locales/{locale}     删除某语言版本
```

#### R1a · 后端补齐语言版本端点
`预估 0.5 人日`

> ✅ 已完成（2026-08-10）：POST/DELETE /posts|pages/{id}/locales/{locale}；
> blank/copy/translate 三种来源（translate 走 AI.Enqueue，AI 未配置返回 422）；
> 源语言删除 409；删除清理 index.<locale>.md、metadata 条目、generated 产物、
> .meta 与非启用语言的整目录、过期 sitemap（feed.Generate 补剪枝）；触发兄弟页
> hreflang 重渲染。临时实例实测创建/删除/verify 收敛，退出码 0。

- `POST /posts/{id}/locales/{locale}`：body `{ source: "blank" | "copy" | "translate" }`
  - `blank` 建空白版本，`copy` 复制源语言正文，`translate` 投递 AI 翻译任务
  - 必须调用 `content.Store` 现有的权威字段镜像逻辑（`mirrorFromSource`）
- `DELETE /posts/{id}/locales/{locale}`：删除该语言 `.md` + `metadata.yaml` 中对应条目 + `generated/` 下该语言产物
  - **禁止删除 `sourceLocale`**，返回 409
- 两个端点都要触发重渲染（新增/删除语言会改变所有兄弟页面的 hreflang）
- 页面（pages）同样支持

**验收**
```bash
cd backend && go test ./internal/httpserver/... ./internal/content/...
# 手工：为一篇文章新建 en 版本 → content/posts/.../index.en.md 存在
#       metadata.yaml 的 translations.en 出现 → 删除后文件与产物都清理干净
./blog-server verify   # 退出码 0
```

#### R1b · 编辑器语言标签栏
`[R1a]` · `预估 1.5 人日` · 规格 `docs/08 §4.5`

> ✅ 已完成（2026-08-10）：GET /posts/{id}/locales 语言矩阵；LanguageTabs 组件
> （原文/✓/⚠过期(版本差)/✎人工/⏳/✕/＋徽章、添加语言三选一）；locale 从 URL
> search 读取并切换；派生语言分类/标签禁用+🔒 tooltip、只读日期/作者；过期黄条+
> 更新翻译（force）；保存派生语言确认框（可勾选不再提示）。实测：保存 en →
> translations.en.manualEdited: true；中文发布后 en 过期（2→3）；deepseek 日语
> 翻译 5 秒完成 → ja completed + index.ja.md 生成。判据 1/2（浏览器点击）代码
> 已实现并通过构建，待浏览器人工确认。

```
┌──────────────────────────────────────────────────────────────┐
│ [中文 原文] [English ✓] [日本語 ⚠过期] [Deutsch ✎人工] [+ 添加] │
└──────────────────────────────────────────────────────────────┘
```

必须实现：
1. **移除所有 `zh-CN` 硬编码**，locale 从 URL query 读（`/admin/posts/$id?locale=en`），刷新保持
2. 标签徽章：`原文` / `✓ completed` / `⚠ outdated` / `✎ manual` / `⏳ translating` / `✕ failed` / `＋ missing`
3. **编辑派生语言时，权威字段控件禁用 + 锁图标 + tooltip「由源语言（简体中文）控制」**
   - 权威字段清单见 `docs/02 §1.3`：`date` `status` `categories` `tags` `author` `pinned` `cover` `comments`
   - 每语言独立的：`title` `description` `slug` `updated` `toc` `seo.*`
4. 过期时顶部黄条：`源文章已更新到版本 21，此译文基于版本 17 · [更新翻译]`
5. 保存派生语言前弹确认：「保存后此译文将不再被 AI 自动更新」（可勾选不再提示）
6. 「＋ 添加语言」下拉 → 调 R1a 的端点，三选一（AI 翻译 / 复制源文 / 创建空白）

**验收**
- 切到 en 标签 → URL 变为 `?locale=en` → 刷新仍在 en
- 分类/发布时间等控件禁用且有锁图标
- 保存 en 后 `metadata.yaml` 中 `translations.en.manualEdited: true`
- 源文章发布后，en 标签变为 `⚠过期` 并显示版本差

---

### 🟠 R2 · AI 熔断器
`预估 0.5 人日` · 规格 `docs/12 §4`

> ✅ 已完成（2026-08-10）：internal/ai/breaker.go（closed/open/half-open 状态机，
> 连续 5 次失败触发、60s 起翻倍上限 15min、半开放 1 个试探、成功即关闭重置）；
> provider 每次请求后 Report，重试循环内复查熔断；ErrCircuitOpen 时任务 Requeue
> 留 pending 且不耗 attempts（jobs.Queue.Requeue）；GET /translations/breaker +
> POST /translations/reset-breaker；矩阵页橙色横幅（倒计时 + 立即重试）。
> `go test ./internal/ai/... -run Breaker -v` 5 项全过；mock 500 下第 6 个请求不发。

**现状**：`provider.go` 有重试（`maxRetries=3`）+ 指数退避 + `Retry-After` 解析，**但没有熔断器**。
后果：provider 持续故障时，每个翻译任务都会各自重试 3 次 —— 烧 token、刷日志、拖慢队列。

**要实现**
```go
// internal/ai/breaker.go
type Breaker struct {
    failures int
    state    State        // closed | open | half-open
    openedAt time.Time
}
```

| 参数 | 值 |
|---|---|
| 触发阈值 | 连续 5 次失败 |
| 打开时长 | 60s，每次连续打开翻倍，上限 15min |
| 半开 | 放 1 个请求试探 |
| 打开期间 | 任务留在 `pending`，**不消耗 `attempts`** |
| 手动重置 | `POST /api/admin/translations/reset-breaker` |

**关键**：熔断打开时 `attempts` 不增加 —— 外部服务不可用不是任务的错。这与 `docs/12 §3.1` 的心跳回收同理。

后台「翻译任务」页显示橙色横幅 + 倒计时 + 「立即重试」按钮。

**验收**
```bash
cd backend && go test ./internal/ai/... -run Breaker -v
# 用返回 500 的 mock provider：5 次后熔断打开，第 6 个任务不发起请求
```

---

### 🟠 R3 · 编辑器冲突对话框
`预估 0.5 人日` · 规格 `docs/12 §8.5`

**现状**：`baseHash` 已经在发送（`PostEditorPage.tsx:44`、`PagesPage.tsx:81`），但**前端没有处理 409 响应**。乐观并发做了一半。

**要实现**：收到 409 时弹三栏对话框

```
┌────────────────────────────────────────────────────────┐
│ 内容冲突                                                 │
│ 这篇文章在你编辑期间被修改过（14:35）                      │
│  你的版本      │  服务器版本    │  差异                   │
│ [保留我的] [使用服务器的] [下载我的副本]                    │
└────────────────────────────────────────────────────────┘
```

**「下载我的副本」是必需的** —— 永远给用户一条保住劳动成果的退路。

顺带补齐 `docs/12 §8.3` 的同步状态指示器（已同步 / 保存中 / 未同步 / 离线），离线时照常可写不弹阻塞框。

**验收**：两个标签页同时编辑同一文章并保存，后者出现冲突对话框，三个按钮都可用。

---

### 🟡 R4 · 搜索索引补分类与标签
`预估 0.25 人日`

**现状**：索引字段只有 `{i,t,d,u,p,dt}`，缺 `c`(categories) 和 `g`(tags)（`docs/04 §7.3` 规定要有）。
后果：搜不到「所有 Docker 相关文章」这类查询。

- Go 侧生成索引时补 `c`/`g` 字段（存显示名，不是 id）
- Search island 的 MiniSearch 配置里把这两个字段加入 `fields`

**验收**
```bash
./blog-server rebuild
python3 -c "import json;d=json.load(open('generated/public/zh-cn/search-index.json'));print(sorted((d[0] if isinstance(d,list) else d['docs'][0]).keys()))"
# 应含 c 和 g
```

---

### 🟡 R5 · 渲染器与翻译的可观测性
`预估 0.75 人日` · 规格 `docs/12 §1、§3.7`

当前缺三样，都是「用户不知道系统在干什么」的问题：

| 子项 | 要求 |
|---|---|
| **SSE 进度流** | `GET /api/admin/events` 推送渲染/翻译进度。后台顶栏指示器 + 相关页面实时更新。断线降级为 5s 轮询并提示 |
| **stall 检测** | 任务超过阈值无进展标 `stalled`（渲染 60s / 翻译 180s），进度条变黄 + 出现「取消」按钮。**不自动杀** |
| **真进度语义** | 翻译在分段完成前显示「正在分析文章结构…」不确定态，之后才切真进度条。ETA 用 EMA 平滑，禁止跳变 |

**验收**：触发一次批量翻译，后台能看到实时批次进度；断开 SSE 后出现降级提示。

---

### 🔴 R7 · HTTP 缓存与安全头缺失（**实跑服务发现，文件级检查抓不到**）
`预估 0.5 人日` · 规格 `docs/07 §4.1` + `docs/10 §4`

> ✅ 已完成（2026-08-10）：staticHandler 按路径设置 Cache-Control（HTML/资产/media/xml/robots，
> 值取自 config.cache.*）；手动 ETag + If-None-Match → 304；siteCSP 覆盖前台并按
> comments.provider 追加 origin；/api /admin 补 no-store。`--port 8099` 实测全部通过。

**发现方式**：启动 `./blog-server --port 8099` 后逐路径 `curl -D-` 检查响应头。
所有路由都返回 200、语言协商正常、资源可加载 —— **但响应头缺了四样**。

| # | 缺失 | 实测 | 规格要求 | 后果 |
|---|---|---|---|---|
| a | **静态 HTML 无 `Cache-Control`** | 只有 `Content-Type` / `Last-Modified` / `X-Request-Id` | `public, max-age=300, s-maxage=86400, stale-while-revalidate=604800` | **项目的头号卖点（CDN 缓存、TTFB < 100ms）直接失效** —— CDN 不知道能缓存多久 |
| b | **带 hash 的资源无 `Cache-Control`** | `/assets/style-D3-kUxWp.css` 无任何缓存头 | `public, max-age=31536000, immutable` | 文件名已带 hash 却每次重新校验，浪费往返 |
| c | **`/api/*` 无 `Cache-Control: no-store`** | 完全没有该头 | `no-store, must-revalidate` | ⚠️ **安全相关** —— API 响应可能被浏览器或中间代理缓存 |
| d | **前台页面无 CSP** | `/admin/` 与 `/api/` 有 CSP，`/zh-cn/` **没有** | `docs/10 §4` 的 siteCSP | 公开站点缺少 XSS 纵深防御 |

**要做什么**

1. 在 `internal/httpserver` 的静态服务处理器上按路径模式设置 `Cache-Control`，值全部从 `config.cache.*` 读取（配置项**已存在**，见 `config/config.yaml` 的 `cache:` 段 —— 是配了没用上）：

   | 路径 | 值 |
   |---|---|
   | `/` | `no-store` ✅ 已正确 |
   | `/api/*`、`/admin/*` | `no-store, must-revalidate` |
   | `/assets/*`（文件名含 8+ 位 hash） | `public, max-age=31536000, immutable` |
   | `/media/*` | `public, max-age=86400, s-maxage=2592000` |
   | `*.html` / 目录索引 | `public, max-age=300, s-maxage=86400, stale-while-revalidate=604800` |
   | `*.xml`、`search-index.json` | `public, max-age=600, s-maxage=3600` |
   | `robots.txt` | `public, max-age=3600` |

2. 静态 HTML 补 `ETag`（基于 mtime+size）并处理 `If-None-Match` → 304
3. 前台响应补 siteCSP（注意要按 `comments.provider` 自动追加其 origin 到 `script-src`/`frame-src`）

**验收**
```bash
./blog-server --root . --port 8099 &
curl -sD- -o/dev/null localhost:8099/zh-cn/          | grep -i cache-control   # max-age=300
curl -sD- -o/dev/null localhost:8099/assets/*.css    | grep -i cache-control   # immutable
curl -sD- -o/dev/null localhost:8099/api/auth/status | grep -i cache-control   # no-store
curl -sD- -o/dev/null localhost:8099/zh-cn/          | grep -i content-security # 非空
# ETag 304 验证
E=$(curl -sD- -o/dev/null localhost:8099/zh-cn/ | grep -i etag | cut -d' ' -f2 | tr -d '\r')
curl -s -o/dev/null -w '%{http_code}\n' -H "If-None-Match: $E" localhost:8099/zh-cn/   # 应为 304
```

> **备注**：`/` 的语言协商本轮无法完整验证 —— 当前 `config.yaml` 只启用了 1 个 locale（启动日志 `"locales":1`），
> 所以 `Accept-Language: ja` 与 `preferred_locale=en` 都回落到 `/zh-cn/` 是**正确行为**，不是 bug。
> R1 完成后应启用多个 locale，再按 `docs/05 §9` 的 I1~I10 完整验证协商逻辑。

---

### ⚪ R6 · Tailwind + shadcn/ui（**需你先拍板**）
`预估 1.5 人日` · 规格 `docs/08 §1`

这是 `TASKS.md` 遗留的 Q2。当前是纯手写 CSS，功能完整但视觉朴素。

| 选项 | 代价 | 收益 |
|---|---|---|
| **A. 引入**（规格要求） | 1.5 人日重写全部组件样式 | 与规格一致；后续加页面更快；有现成的无障碍组件 |
| **B. 保持手写 CSS** | 0 | 省时；但每个新页面都要手写样式，长期更慢 |
| **C. 只引入 Tailwind，不上 shadcn** | 0.75 人日 | 折中 |

**我的建议：C**。Tailwind 解决样式复用，shadcn 的价值主要在复杂交互组件（Dialog/Combobox/DatePicker），而 R1b 的语言标签栏和 R3 的冲突对话框正好需要 Dialog —— 如果这两个做完发现手写吃力，再补 shadcn 不迟。

**这一项在你决定前不要动工。**

---

## 三、执行顺序

```
第 1 天    R7 ─────────────► 缓存与安全头（半天，收益最高，先做）
第 1 天    R1a ────────────► 后端语言版本端点
第 2-3 天  R1b ────────────► 编辑器语言标签栏   ★ 本轮关键里程碑
第 4 天    R2 + R3 ────────► 熔断器 + 冲突对话框
第 4 天    R4 ─────────────► 搜索索引字段（半天内可完成）
第 5 天    R5 ─────────────► SSE 与 stall 检测
（待定）   R6 ─────────────► 样式体系，等拍板
```

**R7 排在最前**：只要半天，但它修复的是项目的头号卖点（CDN 缓存）和一处安全相关缺陷，
而且配置项早已存在、只是没接上，改动面小、风险低。

**每个任务完成后的回归**（与第一轮相同）：
```bash
cd backend && go build ./... && go test ./...
cd frontend/admin && pnpm build
cd themes/default && npm run build
./blog-server rebuild && ./blog-server verify
git commit
```

**并回到 `docs/17-spec-vs-reality.md` 更新对应行状态。**

---

## 四、本轮关键里程碑的判据

**R1b 完成后应该能做到**：

1. 打开一篇中文文章 → 点「English」标签 → 编辑器切到英文版本，URL 是 `?locale=en`
2. 英文版本里，分类/发布时间/作者控件是**灰的**，带锁图标
3. 保存英文版本 → `metadata.yaml` 里 `translations.en.manualEdited: true`
4. 回到中文改一下并发布 → 英文标签变成 `⚠ 过期`，显示「基于版本 N，源已到版本 M」
5. 点「＋ 添加语言」→ 选日语 → 选「AI 翻译」→ 任务进队列，完成后标签变 ✓

**做不到这五条，就不算完成。** 这是本产品区别于普通 CMS 的地方，也是整套 AI 多语言架构最终是否成立的验证点。

---

## 五、不在本轮范围

| 项 | 原因 |
|---|---|
| `docs/16` 第 1.5 阶段（评论 / 邮件 / 友链申请 / RSS 聚合 / 版本更新） | 21 人日，独立阶段，等本轮收尾后再开 |
| `docs/12 §8` 编辑器三层保存的完整形态 | R3 已覆盖冲突与状态指示，IndexedDB 层可延后 |
| 主题市场 / 插件运行时 / S3 存储 | 第二阶段路线图 |
| 性能压测（1C1G 实测） | 等 R1~R5 完成后一次性做，见 `docs/13` W1~W11 |

---

## 六、给执行者的提醒

1. **R1 是产品主线，不要因为它工作量大就先做容易的 R4。** 多语言编辑不通，这个项目就只是个普通博客。
2. **后端仍然不要重写。** 21 个包测试全绿，R1a 只是加两个 handler。
3. **代码规范继续执行**：单行 ≤ 120 字符，一个文件一件事。第一轮做得很好，保持。
4. **`docs/17` 的权威顺序不变**：`docs/17 处置列` > `TASKS-2.md` > 有测试的代码 > `docs/01~16`。
5. **发现规格有错就说出来。** 第一轮你在 `docs/17 §2.4` 补的那条 "Invalid hook call → createElement" 修正非常好，继续这样做。
