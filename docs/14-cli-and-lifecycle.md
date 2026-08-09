# 14 · CLI、升级迁移与灾难恢复

> 补齐前 13 份文档遗漏的**产品生命周期**能力：命令行工具、版本升级、内容迁移、正确性校验、故障自救。
>
> 这些不是"锦上添花"——**忘记密码无法重置**、**升级后数据格式不兼容**、**增量渲染悄悄写错却无人发现**，任何一条都足以让自托管用户弃用。

---

## 1. CLI 总表

同一个二进制，子命令模式。**不带子命令时等价于 `serve`**（保持 `docker run blog-server` 的行为不变）。

```
blog-server [serve]                          启动服务
blog-server version [--json]                 版本、构建信息、schema 版本
blog-server doctor [--fix]                   环境诊断
blog-server verify [--fix] [--sample N]      增量渲染正确性校验 ★
blog-server rebuild [--locale ...]           全量重建（可在服务未运行时执行）
blog-server migrate [--dry-run] [--to N]     内容/数据格式迁移 ★

blog-server admin create                     创建管理员（交互式）
blog-server admin list
blog-server admin reset-password <user>      重置密码 ★★
blog-server admin unlock <user>              解除登录锁定
blog-server admin set-role <user> <role>
blog-server admin hash-password              仅输出 Argon2 哈希（应急手工编辑用）

blog-server backup create [--no-media] [--tag T]
blog-server backup list
blog-server backup restore <file> [--yes]
blog-server import <path.md|directory|archive.zip> [--dry-run] [--locale L] [--status draft|published]
blog-server export <path> [--scope content,data,media,config]

blog-server config validate                  校验配置并列出所有生效值
blog-server config get <path>                如 config get i18n.defaultLocale
blog-server config set <path> <value>        写回 config.yaml（保留注释）

blog-server index rebuild                    仅重建内存索引（诊断用）
blog-server index errors                     列出解析失败的文件
```

### 1.1 设计约束

| 约束 | 说明 |
|---|---|
| **必须能在服务未运行时工作** | 所有命令直接操作文件与 SQLite，不依赖 HTTP。用户的服务起不来时，CLI 正是救命工具 |
| **服务运行中执行也必须安全** | 写文件的命令（`admin reset-password`、`config set`、`import`）通过 `fsutil.AtomicWrite`；运行中的实例经 fsnotify 感知并热重载。写 SQLite 的走同一套 busy_timeout |
| **`--root` 全局 flag** | 默认为 CWD，Docker 内为 `/app` |
| **退出码语义** | `0` 成功 · `1` 一般错误 · `2` 用法错误 · `3` 校验失败（`doctor`/`verify`/`config validate` 发现问题） |
| **输出** | 人类可读为默认；`--json` 供脚本消费。**绝不在任何输出中打印密码、API Key、session secret** |

`import` is offline-safe and writes each accepted item through the canonical
bundle store. It currently imports posts, supports YAML Front Matter, Hugo TOML
Front Matter, and Front-Matter-less Markdown, auto-creates missing category/tag
names, and rebuilds the content index after a successful write. Use `rebuild`
afterwards when imported content was published and a static release needs to be
regenerated.

### 1.2 Docker 下的用法（README 必须写明）

```bash
docker compose exec blog /app/blog-server admin reset-password admin
docker compose exec blog /app/blog-server doctor
docker compose run --rm blog /app/blog-server verify        # 服务未运行时
```

---

## 2. `admin reset-password` — 最关键的一条

**用户忘记密码时唯一的出路。** 没有它，用户被永久锁在自己的博客外面（他无法手工生成 Argon2 哈希）。

```
$ blog-server admin reset-password admin

用户: admin (站长, admin@example.com, 角色 admin)
新密码: ********            ← 隐藏输入，不回显
确认密码: ********

✅ 密码已更新
   · tokenVersion 已从 3 递增到 4，所有现有会话已失效
   · 若服务正在运行，变更将在 1 秒内生效（无需重启）
```

**实现要点**：

```go
1. 加载 data/users/<user>.yaml（不存在则列出所有用户并退出码 1）
2. 交互式读密码（golang.org/x/term.ReadPassword，不回显）
   · 非 TTY 环境（CI/脚本）支持 --stdin 从标准输入读第一行
   · 支持 --random 生成 24 位随机密码并打印一次
3. 校验密码策略（≥12 位、非弱口令表）
4. Argon2id 哈希（参数按当前机器内存自适应，见 docs/13 P7）
5. tokenVersion += 1                          ← 必须，否则旧 session 仍然有效
6. fsutil.AtomicWrite 写回，权限 0600
7. 写 audit_log（actor="cli", action="admin.reset_password"）
```

`--random` 的用途：无人值守恢复。密码只在 stdout 打印一次，不写日志。

### 2.1 `admin hash-password` — 最后的兜底

极端情况（文件系统只读、CLI 也跑不起来）下，用户可以在**另一台机器**上生成哈希再手工粘进 YAML：

```
$ blog-server admin hash-password
密码: ********
$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$...

把上面这行填入 data/users/<user>.yaml 的 passwordHash 字段，
并把 tokenVersion 加 1。
```

---

## 3. `doctor` — 环境诊断

用户报"起不来"、"存不了"、"发布没反应"时的第一道工序。**每一项检查都必须附带可执行的修复建议**。

```
$ blog-server doctor

配置
  ✅ config/config.yaml 语法正确
  ✅ 必填项完整
  ⚠️  server.baseURL 为 http://203.0.113.10:8080
      → 配置域名后请更新此项，系统会自动重新生成全站页面
  ❌ security.cookieSecure = true 但 baseURL 是 http://
      → 会导致登录后被踢回登录页。改为 auto 或使用 HTTPS

目录与权限
  ✅ content/ 可读写
  ✅ data/ 可读写
  ❌ media/ 不可写 (uid 1001 无权限)
      → 在宿主机执行: sudo chown -R 1001:1001 ./media
  ✅ 磁盘可用 12.4 GB

状态存储
  ✅ data/state.db 可打开，integrity_check 通过
  ✅ schema 版本 3（当前）
  ⚠️  jobs 表有 42 个 pending 任务

渲染器
  ✅ node v22.11.0
  ✅ renderer/server.js 存在
  ✅ 渲染器可启动，warmup 用时 2.1s
  ✅ 主题 default 已构建 (dist/manifest.json)

内容
  ✅ 128 篇文章 / 6 个页面 / 12 个分类 / 45 个标签
  ⚠️  2 个文件解析失败
      → blog-server index errors 查看详情
  ✅ 5 种语言已启用

产物
  ⚠️  当前 release 的 baseURL 与配置不一致
      release: http://203.0.113.10:8080
      config:  https://blog.example.com
      → 启动时会自动全量重建

AI（--check-ai 时才执行）
  ✅ 连接正常，延迟 420ms，模型 gpt-x，支持 JSON 模式

其他
  ✅ 系统时钟偏差 < 1s
  ✅ 端口 8080 可用

── 结果：2 个错误，4 个警告
```

`--fix` 只修复**明确安全**的项：创建缺失目录、生成缺失的 sessionSecret、修正 `cookieSecure`。**绝不自动改内容或删文件**。

退出码：有 ❌ → 3；只有 ⚠️ → 0（但打印摘要）。

---

## 4. `verify` — 增量渲染正确性校验

### 4.1 为什么必须有

系统的增量渲染依赖手写的 `AffectedUnits(Change)`（[docs/04 §2](04-render-pipeline.md)）。这个设计选择是对的（简单、可推理、可单测），但它有一个结构性代价：

> **每新增一个功能，都必须记得更新 `AffectedUnits`。漏掉一处，结果是页面内容悄悄陈旧——不报错、不告警、可能几个月无人发现。**

`verify` 是这个风险的唯一解药。

### 4.2 做法

```
1. 全量重建到 generated/.verify-<ts>/（不切 symlink，不影响线上）
2. 与当前 generated/public/ 递归逐字节比对
3. 报告：仅存在于一侧的文件、内容不同的文件
4. --fix：把差异文件从 .verify 目录原子复制到 public（即修复陈旧内容）
5. 清理临时目录
```

```
$ blog-server verify

正在全量重建到临时目录… 4,512 个单元，用时 3m41s
正在比对…

❌ 发现 3 处差异（说明增量渲染存在遗漏）

  内容不同:
    zh-cn/categories/linux/page/3/index.html
    zh-cn/categories/linux/page/4/index.html
      → 分页边界处理遗漏（见 docs/04 §2.1）

  多余文件（应已删除）:
    en/posts/old-slug/index.html
      → slug 变更时未清理旧输出

exit 3
```

### 4.3 前置要求：**渲染必须是确定性的**

`verify` 能工作的前提是：**相同输入必然产出逐字节相同的 HTML**。这是一条我之前没有写明、但整个校验机制赖以成立的硬约束。

以下东西**禁止出现在 HTML 输出中**：

| 禁止 | 原因 | 替代 |
|---|---|---|
| 构建时间戳（`<!-- built at ... -->`） | 每次都不同 | 放进 `.release.json`，不进 HTML |
| `Date.now()` / `new Date()` | 同上 | 只用内容自身的日期字段 |
| 随机 ID / nonce | 同上 | 用内容 hash 派生的稳定 ID |
| Go `map` 的迭代顺序 | 不稳定 | 所有 map 遍历前排序 |
| JS `Object.keys` 的插入序依赖 | 易漂移 | 显式排序 |
| 未排序的 Set/数组 | 同上 | 显式排序 |

**新增单测**：同一篇文章连续渲染两次，断言输出完全一致。这个测试应该在 M5 就加上，它能在早期就抓住确定性破坏。

### 4.4 何时运行

| 场景 | 频率 |
|---|---|
| CI（每次 PR） | 用 `--sample 200` 抽样，控制在几分钟内 |
| 生产（定时任务） | 每周一次全量，发现差异告警 |
| 每次修改 `AffectedUnits` 后 | **强制**，作为 PR 的验收条件 |
| 用户报告"页面内容不对" | 第一诊断手段 |

---

## 5. 版本、Schema 与升级

### 5.1 三个独立的版本号

| 版本 | 位置 | 变更时机 |
|---|---|---|
| **应用版本** | 二进制内置（ldflags 注入） | 每次发布 |
| **State schema 版本** | `state.db` 的 `schema_migrations` | SQLite 表结构变更 |
| **Content schema 版本** | `config/config.yaml` 的 `schemaVersion` | `content/`、`data/` 的 YAML 格式变更 |

**Content schema 版本是之前完全缺失的一环。** 没有它，v1.1 想给 `metadata.yaml` 加个字段、改个结构，就只能靠"解析时容错"无限累积技术债，或者直接破坏老用户的数据。

### 5.2 启动时的版本决策

```
读取 config.schemaVersion (缺失视为 1) 与二进制内置的 currentContentSchema

相等          → 正常启动
config < 当前 → 需要迁移：
     · 服务启动时给出明确告警，但不写入内容
     · 运行 `blog-server migrate`（先用 `--dry-run`）执行受备份保护的迁移
config > 当前 → ❌ 拒绝启动
     「数据由更高版本（schema 5）创建，当前二进制仅支持到 schema 3。
       请升级应用，或从备份恢复。」
```

最后一条防的是**降级踩踏**：用户升级到 v1.2 后发现问题，回滚镜像到 v1.1，如果 v1.1 傻乎乎地去读 v1.2 的数据，会静默丢字段或崩溃。宁可拒绝启动。

### 5.3 迁移流程

```
1. `blog-server migrate` ★ 强制自动备份 → backups/backup-<ts>-pre-migration-v<N>.zip
     备份失败 → 中止迁移，拒绝启动（不冒险）
2. 按版本顺序执行迁移器，每个必须幂等
3. 更新 config.yaml 的 schemaVersion（保留注释）
4. 运行 `blog-server rebuild` 触发全量重建（格式变了，产物必须重生成）
5. 日志与后台记录迁移摘要
任一步失败 → 中止并打印恢复命令：
     blog-server backup restore backups/backup-<ts>-pre-migration-v3.zip
```

### 5.4 迁移器定义

```go
// internal/migrate/content.go
type ContentMigration struct {
    Version int
    Name    string
    Up      func(ctx context.Context, m *Migrator) error
}

var contentMigrations = []ContentMigration{
    {
        Version: 2,
        Name:    "metadata: 增加 createdAt 字段",
        Up: func(ctx context.Context, m *Migrator) error {
            return m.EachBundle(func(b *Bundle) error {
                if b.Meta.CreatedAt.IsZero() {
                    b.Meta.CreatedAt = b.EarliestFileModTime()
                    return b.SaveMeta()          // 内部走 AtomicWrite
                }
                return nil
            })
        },
    },
}
```

**硬性要求**：
- **幂等**：重复执行结果相同（迁移中途崩溃后重跑必须安全）
- **支持 `--dry-run`**：打印将要修改的文件清单与差异摘要，不实际写入
- **只增不改**：已发布的迁移器永不修改
- 迁移器内的所有写入走 `AtomicWrite`

### 5.5 主题的兼容性

`theme.yaml` 的 `engine.minVersion` 目前只是声明，没有强制。补上：

```
激活主题 / 启动时校验：
  appVersion < theme.engine.minVersion
    → 拒绝激活，提示「主题 X 需要 v1.2.0 或更高版本，当前 v1.0.0」
  主题的 templates 声明缺少必需项
    → 拒绝激活，列出缺失的模板名
  主题的 dist/manifest.json 缺失
    → 跳过该主题并记录日志（不是致命错误）

若当前激活的主题在升级后不兼容：
  → 自动回退到内置 default 主题
  → 后台红色横幅说明原因，不静默降级
  → 绝不因主题问题导致后台进不去
```

---

## 6. 灾难恢复手册（README 必须包含）

每一条都给出可直接复制的命令。这一节的价值不在技术含量，而在**用户凌晨两点慌乱时能照着做**。

| 症状 | 处理 |
|---|---|
| **忘记管理员密码** | `docker compose exec blog /app/blog-server admin reset-password admin` |
| **登录后被踢回登录页** | 多半是 `cookieSecure` + HTTP。`blog-server doctor` 会直接指出。改 `security.cookieSecure: auto` |
| **连续输错密码被锁定** | `blog-server admin unlock admin` |
| **容器起不来** | `docker compose logs blog` 看最后 20 行；再 `docker compose run --rm blog /app/blog-server doctor` |
| **后台能进但保存失败** | 权限或磁盘。`doctor` 的"目录与权限"段会指出，附 chown 命令 |
| **站点全白 / 全 404** | `ls -la generated/public`（symlink 是否有效）→ `blog-server rebuild` |
| **发布了但前台没变** | ① 浏览器缓存（强刷或用后台"查看"按钮）② `blog-server verify` 查增量遗漏 ③ 反代挂载了 symlink（[docs/13 P25](13-first-run-walkthrough.md)） |
| **页面内容陈旧且说不清哪里不对** | `blog-server verify --fix` |
| **误删了一篇文章** | 回收站 → 恢复；已永久删除 → `content/.revisions/<id>/` 里有历史版本；再不行从备份恢复 |
| **误删 `generated/`** | 无需处理，重启自动全量重建。这是设计如此 |
| **`state.db` 损坏** | 直接删除，重启自动新建。只丢队列进度与历史日志，内容不受影响 |
| **磁盘满** | 清 `generated/releases/` 的旧 release（保留最新一个）、清 `backups/`、清 `cache/` |
| **升级后出问题要回滚** | 恢复镜像旧版本 + `blog-server backup restore backups/backup-<ts>-pre-migration-v<N>.zip` |
| **AI 翻译一直失败** | `doctor --check-ai` → 后台「翻译任务」页看具体错误 → 检查 Base URL 是否需要 `/v1` |
| **想彻底从零开始** | 停容器 → 删 `content data media generated cache` → 起容器 → 重走安装向导 |

### 6.1 数据带走

需求第 70 条的直接体现，README 用一句话说清：

```bash
tar czf my-blog-backup.tar.gz content/ data/ media/ config/
```

这就是完整的博客。不需要导出功能、不需要系统在线、不需要任何工具。`generated/` 和 `cache/` 是派生物，不用带。

---

## 7. 验收

| # | 测试 | 期望 |
|---|---|---|
| L1 | 全新环境 `blog-server doctor` | 准确列出所有问题，每条附可执行的修复建议 |
| L2 | 忘记密码 → `admin reset-password` | 成功；旧 session 全部失效；服务运行中无需重启即生效 |
| L3 | 非 TTY 环境 `admin reset-password --stdin` | 可用（CI/脚本场景） |
| L4 | 同一文章连续渲染两次 | 输出逐字节相同（确定性） |
| L5 | 人为让 `AffectedUnits` 漏掉分类页 → `verify` | 准确报出差异文件；`--fix` 后再 verify 干净 |
| L6 | 构造 schema v1 数据 + v2 二进制 | 自动备份 → 迁移 → 全量重建 → 数据正确 |
| L7 | `migrate --dry-run` | 打印变更清单，**不修改任何文件** |
| L8 | 迁移中途 `kill -9` → 重启 | 幂等重跑成功，无损坏 |
| L9 | schema v3 数据 + 只支持 v2 的二进制 | **拒绝启动**并提示从备份恢复 |
| L10 | 激活一个 `minVersion` 过高的主题 | 拒绝并给出明确原因；后台仍可正常访问 |
| L11 | 当前主题在升级后不兼容 | 自动回退 default + 红色横幅；后台可用 |
| L12 | 按灾难恢复手册逐条演练 | 每条命令都能直接复制执行并解决问题 |
