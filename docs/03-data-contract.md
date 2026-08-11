# 永久数据契约

## 1. 通用规则

- UTF-8，无 BOM；YAML 使用两个空格缩进；Markdown 换行保持原文。
- 时间统一为 RFC 3339 UTC，展示时按站点时区转换。
- 公共实体 ID 默认是 32 位小写十六进制 UUID；管理员可把之后新建实体的默认生成策略切换为 13 位毫秒时间戳。自定义 slug 仅在创建时允许，匹配 `^[a-z]+(?:-[a-z]+)*$`；数字形式只可能由系统生成，不能手工冒充。策略变更不改写任何既有 ID 或 URL。
- `id` 创建后不可修改，同一实体所有语言共享一个 ID。
- 语言代码在写入时按 BCP 47 规范化，集合来自配置，不在代码中写死。
- 所有 schema 都有 `schemaVersion`，升级必须可迁移且保留备份。

## 2. 文章和页面

每个实体一个目录，元数据与各语言 Markdown 分开：

```text
content/posts/<id>/
├── meta.yaml
├── zh-CN.md
├── en.md
└── ja.md
```

`meta.yaml` 示例：

```yaml
schemaVersion: 1
kind: Post
id: 550e8400e29b41d4a716446655440000
status: published
sourceLocale: zh-CN
createdAt: 2026-08-10T12:00:00Z
updatedAt: 2026-08-10T12:30:00Z
publishedAt: 2026-08-10T12:30:00Z
categories:
  - engineering
tags:
  - release
cover: /media/2026/08/example.webp
commentPolicy: open
template: post
revision: 7
baseRevision: 1
headRevision: 7
releaseRevision: 6
locales:
  zh-CN:
    state: current
    origin: source
    revision: 4
    sourceRevision: 4
  en:
    state: current
    origin: ai
    revision: 2
    sourceRevision: 4
  ja:
    state: stale
    origin: manual
    revision: 3
    sourceRevision: 3
```

语言 Markdown 使用 YAML front matter 保存访客可见元数据：

```markdown
---
title: 示例文章
summary: 一段摘要
seoTitle: 示例文章
seoDescription: 一段摘要
---

# 正文
```

`origin` 为 `source | ai | manual`。任何人工编辑都会把派生语言改为 `manual`；自动任务不得覆盖 `manual`。即使管理员已明确确认一次覆盖，任务也会记录确认时的目标语言修订号，并在调用 Provider 前与最终写入时各校验一次；翻译期间出现的新人工修改会转为 `needs-review`，不会被旧确认覆盖。源文修订增加后，`sourceRevision` 落后的译文变为 `stale`。

`meta.template` 同样进入 head 与公开 release。它不是自由文本：后台只能选择活动主题 manifest 声明的模板；`post` / `page` 是两类内容各自的默认值。主题切换后不存在的旧模板会在渲染时回退当前主题默认模板，但原 ID 保留在内容中。

### 2.1 head 与公开 release

`content/<kind>/<id>/` 始终是当前编辑 head。`baseRevision` 固定指向实体创建时的基线，`headRevision` 与当前乐观锁 `revision` 一起前进，`releaseRevision` 指向访客可见快照；从旧数据迁移时会显式补齐这三个字段。已发布后继续保存只更新 head，不得改变访客正在读取的版本。每次明确发布会写入不可变快照，再原子替换指针：

```text
releases/posts/<id>/
├── current.yaml
└── snapshots/<revision-id>/
    ├── meta.yaml
    ├── zh-CN.md
    └── en.md
```

静态构建对状态为 `published` 的实体只读取 `current.yaml` 指向的 release；分类、菜单、主题或其他重建动作不能意外带出尚未再次发布的 head。旧版本升级时若尚无 release 指针，首次编辑会先把当时公开 head 固化为 release。AI 任务只把已通过源修订校验的目标语言合并进 release，不会顺带发布任务执行期间产生的其他 head 修改。`releases/` 属于永久数据并进入普通备份。

## 3. 分类、标签、菜单和友链

元数据与语言文案分离。以分类为例：

```yaml
schemaVersion: 1
kind: Category
id: engineering
parentId: null
cover: null
template: category
locales:
  zh-CN:
    name: 工程
    description: 工程相关文章
    seoTitle: 工程
  en:
    name: Engineering
    description: Engineering posts
    seoTitle: Engineering
```

菜单项支持 `internal | external` 两类目标、父子关系、排序、打开方式和所有启用语言的标签。链接分组和链接自身都使用相同的 `locales` 映射。

## 4. 评论

每条评论一个 YAML 文件，按内容实体分目录：

```text
comments/Post/<post-id>/<comment-id>.yaml
```

```yaml
schemaVersion: 1
kind: Comment
id: 58b7d6f6f8af47fb90620be7f6a07335
subject:
  kind: Post
  id: 550e8400e29b41d4a716446655440000
parentId: null
status: approved
author:
  name: Visitor
  emailHash: sha256:...
  website: https://example.com
content: 很棒的文章。
locale: zh-CN
createdAt: 2026-08-10T13:00:00Z
ipHash: hmac-sha256:...
userAgentFamily: Chrome
```

读取评论时必须核对 YAML 的 `kind`、`id` 与 `subject` 和所在目录完全一致；路径身份不匹配视为损坏数据，不能让审核或删除操作跨到另一个内容目录。原始 IP、邮箱和 User-Agent 不写入永久评论文件。匿名字段、回复层级、Markdown、反垃圾和通知等详细评论产品规则仍按 `PROJECT_SPEC.md` 的待确认项处理，不由当前数据样例宣告最终产品决定。

同一条路径身份规则适用于全部永久资源：文章/页面目录、分类/标签文件、菜单文件、友链分组/友链文件、媒体元数据和主题 manifest 的 `kind`、`id` 必须与实际路径一致。外部编辑造成不一致时该资源视为损坏并阻止后续写删，服务不得使用文件内部伪造的 ID 计算另一个目标路径。列表读取不得把损坏资源静默隐藏；主题和附件应返回明确错误，菜单层级/目标地址和友链公网 URL 也必须重新校验，避免外部 YAML 绕过后台写入规则后进入静态 HTML。

## 5. 站点与语言

`config/site.yaml` 保存非秘密站点设置、当前主题、主导航菜单、源语言、后台语言、时区、URL 和发布选项。站点标题等公开字段使用 `locales` 映射。`primaryMenu` 为空时使用排序后的第一个菜单；指定后必须引用现存菜单，删除前必须先切换主导航。

站点源语言切换只影响之后新建的永久资源。文章、页面、分类、标签、菜单和友链文件各自保存创建时的 `sourceLocale`；发布快照必须保留该字段，实体缺译文时最终回退自身源语言。只要仍有永久资源引用某语言作为源语言，该语言就必须保持启用。

`config/locales.yaml` 保存：

```yaml
schemaVersion: 1
sourceLocale: zh-CN
enabled:
  - code: zh-CN
    label: 简体中文
  - code: en
    label: English
fallback:
  - en
  - zh-CN
```

公开框架文案保存于 `content/dictionaries/<locale>.yaml`，键集合由当前框架版本统一声明。后台显示每种启用语言的完整度并允许人工维护；空缺项按“当前语言 → 站点源语言 → 内置英文安全底座”解析。字典写入后触发静态构建门禁，不改变文章等内容实体的 URL 回退链。

构建器在请求语言后依次尝试 `en`、`zh-CN`、实体自身保存的 `sourceLocale`，去重后选取第一份存在的内容；若命中其他语言，生成静态 302/跳转规则到真实语言 URL。站点聚合页仍以当前站点源语言为最终文案回退。

## 6. AI Provider

非秘密 Provider 资料保存在 `config/providers.yaml`，API Key 只保存在 `config/secrets.yaml`，通过稳定 Provider ID 关联。

翻译任务保存：输入实体、源修订、目标语言、Provider/模型、进度、重试与错误。正文默认按最多 6000 个 Unicode 字符分段翻译，元数据独立翻译；代码块、行内代码、链接目标、数学公式和内部占位符先保护再逐段处理。每个目标语言的整批分段设 15 分钟总时限。任务应用结果前再次核对源修订和目标译文 origin；不匹配时进入 `needs-review`，不能盲写。

## 7. 修订

每次成功保存永久实体前，把旧版本复制到：

```text
revisions/<kind>/<id>/<revision-id>/
```

修订包含数据副本、时间、动作、内容哈希和可选说明，不包含管理员个人身份（系统只有一个管理员）。恢复修订会创建新的修订，不删除后续历史。

## 8. SQLite 投影

SQLite 当前只保存从文章与页面文件导出的列表和全文搜索投影，每行记录源文件路径、mtime 与内容哈希。评论和任务目前直接读取各自的 YAML；以后可以增加关系、统计、任务或评论分页投影，但仍必须能从永久文件完整重建。删除 SQLite 后重建不得损失任何永久信息。
