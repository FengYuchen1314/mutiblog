# 永久数据契约

## 1. 通用规则

- UTF-8，无 BOM；YAML 使用两个空格缩进；Markdown 换行保持原文。
- 时间统一为 RFC 3339 UTC，展示时按站点时区转换。
- 公共实体 ID 默认是 32 位小写十六进制 UUID；自定义 slug 仅在创建时允许，匹配 `^[a-z]+(?:-[a-z]+)*$`。
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

`origin` 为 `source | ai | manual`。任何人工编辑都会把派生语言改为 `manual`；自动任务不得覆盖 `manual`。源文修订增加后，`sourceRevision` 落后的译文变为 `stale`。

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

原始 IP、邮箱和 User-Agent 不写入永久评论文件。详细评论产品规则仍按 `PROJECT_SPEC.md` 的待确认项处理；第一版默认单层回复、匿名姓名必填、邮箱可选、内容纯文本、管理员可配置预审。

## 5. 站点与语言

`config/site.yaml` 保存非秘密站点设置、当前主题、源语言、后台语言、时区、URL 和发布选项。站点标题等公开字段使用 `locales` 映射。

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

构建器在请求语言后依次尝试 `en`、`zh-CN`、`sourceLocale`，去重后选取第一份存在的内容；若命中其他语言，生成静态 302/跳转规则到真实语言 URL。

## 6. AI Provider

非秘密 Provider 资料保存在 `config/providers.yaml`，API Key 只保存在 `config/secrets.yaml`，通过稳定 Provider ID 关联。

翻译任务保存：输入实体、源修订、目标语言、Provider/模型、分段哈希、进度、重试与错误。任务应用结果前再次核对源修订和目标译文 origin；不匹配时进入 `needs-review`，不能盲写。

## 7. 修订

每次成功保存永久实体前，把旧版本复制到：

```text
revisions/<kind>/<id>/<revision-id>/
```

修订包含数据副本、时间、动作、内容哈希和可选说明，不包含管理员个人身份（系统只有一个管理员）。恢复修订会创建新的修订，不删除后续历史。

## 8. SQLite 投影

SQLite 只包含从上述文件导出的：实体列表、全文搜索、统计、关系、任务查询缓存和评论分页索引。表中每行记录源文件路径、mtime 与内容哈希。`mutiblog reindex` 删除并重建数据库必须不损失任何永久信息。
