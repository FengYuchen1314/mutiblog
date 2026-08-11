# MutiBlog 全项目整改意见

本文档汇总对整个仓库的审查结果，供后续（例如交给 Codex）逐项修复。已经产出的两份更早的文档仍然有效，本文档不重复其内容，只做索引：

- [ISSUES.md](ISSUES.md) — 自动翻译/发布流程的顺序问题、AI Provider 未配置/未填 Key 时报错不清晰、定时发布持锁阻塞后台（诊断，不含修复方案）
- [CONSOLE_REVIEW.md](CONSOLE_REVIEW.md) — 后台核心页面（路由、Dashboard/Overview、任务中心、Posts 列表、Post 编辑器、AI Provider 页）的问题与修改建议

本文档新增覆盖：`internal/content`、`internal/domain`、`internal/platform/fsrepo`、`internal/auth`、`internal/backup`、`internal/media`、`internal/projection`、`internal/taxonomy`、`internal/menus`、`internal/links`、`internal/comments`、`internal/upvotes`、`internal/visits`、`internal/dictionary`、`internal/themes`、`internal/audit`、`apps/renderer`、`themes/`、部署与 CI 配置，以及后台剩余视图（ThemesView、LocalesView、AttachmentsView、TaxonomiesView、CommentsView、LinksView、MenusView、ToolsView、SetupView、LoginView、PagesView、MarkdownEditor、MediaPickerField、ThemeSettingsField）。

每条问题给出：证据位置、具体失败场景、修复方向。标记 `[已验证]` 的是本人亲自读代码复核过的；其余是子代理审查产出，评审风格与本人已核实的部分一致，但未逐条复核，Codex 实施前建议先用 Read 工具确认行号仍然准确（代码可能已变化）。

---

## A. 内容与存储核心（internal/content / domain / fsrepo / auth / backup）

### A1. `PromoteAITranslation` 用一套独立的版本号系统，破坏了发布版本与历史修订记录的对应关系 `[已验证]`

[internal/content/releases.go:273-313](internal/content/releases.go:273)

`PromoteAITranslation` 在 `released`（通过 `readRelease` 读出的发布快照克隆）上调用 `advanceHead(&released.Meta)`（第 300 行），而不是在真正的头部 `PostMeta` 上。系统里所有别的地方（`UpdateLocale`、`ApplyAITranslation`、`PublishPost`、`ChangeStatus`、`RestoreRevision`、`UpdatePostSettings`）在调用 `advanceHead` 递增 `Revision`/`HeadRevision` 的同时，都会调用 `s.snapshot()` 把这个新版本号写进 `revisions/posts/<id>/` 留痕；唯独 `PromoteAITranslation` 这条路径不会。

**失败场景**：文章在 revision 6 发布，之后继续编辑头部到 revision 9；针对 `sourceRevision=5` 触发的翻译此时完成，调用 `PromoteAITranslation`。它给 `released.Meta` 自行分配一个新的 `Revision=7`，但真实头部的 `Revision`/`HeadRevision` 仍是 9。`ListRevisions`（[internal/content/lifecycle.go:66](internal/content/lifecycle.go:66)）用 `meta.Revision == current.Meta.ReleaseRevision` 判断"这是不是发布版本"——历史记录里恰好存在的、完全无关的 revision 7 快照会被错误标记为"发布版本"，而真正合成出来的发布内容（旧语言 + 新提升的译文）只存在于 `releases/` 里，在修订历史 UI 里完全找不到对应条目。

**修复方向**：给"发布合成版本"一套独立于 `advanceHead`/`Revision` 的计数或指针；或者在 `PromoteAITranslation` 里也调用一次匹配的 `snapshot`，让版本号继续有意义。

### A2. `GetPost`/`GetPage`/`ListPosts` 读取时不加锁，可能读到 meta 和 content 不一致的"撕裂"结果 `[子代理产出，未复核]`

[internal/content/service.go:97-126](internal/content/service.go:97)（`GetPost` 未持有 `s.mu`）对照 [internal/content/service.go:173-225](internal/content/service.go:173)（`UpdateLocale` 持有 `s.mu`，且先写 `fr.md` 再写 `meta.yaml`）。

**失败场景**：协程 A 执行 `UpdateLocale(id,"fr",...)`，刚写完 `fr.md`、还没写 `meta.yaml`；协程 B 同时调用 `GetPost(id)`，读到旧的 `meta.yaml`（revision N）但读到 A 刚写的新 `fr.md`——返回的 `domain.Post` 里 `Meta.Revision==N` 但 `Content["fr"]` 却是 N+1 的草稿，调用方（站点构建、预览）可能渲染出元数据和正文对不上的内容。

**修复方向**：所有读路径也持有 `s.mu`（或改成读写锁的 RLock），或者改为按文档粒度加锁、在锁内一次性快照 meta+content。

### A3. `PublishPost`/`PublishPage` 回滚失败时错误被 `_ =` 丢弃 `[已验证]`

[internal/content/service.go:313-319](internal/content/service.go:313)，[internal/content/pages.go](internal/content/pages.go) 中有几乎相同的一份：

```go
if err := s.repository.WriteYAML(postPath(id, "meta.yaml"), post.Meta, false); err != nil { ... }
if err := s.writeRelease("Post", post); err != nil {
    _ = s.repository.WriteYAML(postPath(id, "meta.yaml"), previousMeta, false)
    return domain.Post{}, err
}
```

`meta.yaml` 被先写成"已发布"状态，如果随后 `writeRelease` 失败，代码尝试把 `meta.yaml` 回滚到 `previousMeta`，但这次回滚写入本身的错误被 `_ =` 丢弃。如果两次写入都失败（例如文件系统同一时刻变为只读），调用方只能看到最初 `writeRelease` 的错误，完全不知道 `meta.yaml` 现在停留在一个"已发布但没有对应发布产物"的不一致状态。

**修复方向**：用 `errors.Join` 或类似方式把回滚错误也传递出去，而不是静默丢弃，至少要让日志/调用方知道记录处于不一致状态。

### A4. Posts / Pages 之间约 250 行核心逻辑几乎逐字复制，且已经开始出现细节漂移 `[子代理产出，未复核]`

[internal/content/service.go:128-322](internal/content/service.go:128) 对照 [internal/content/pages.go:68-291](internal/content/pages.go:68)：`CreatePost`/`CreatePage`、`UpdateLocale`/`UpdatePageLocale`、`ApplyAITranslation`/`ApplyAIPageTranslation`、`PublishPost`/`PublishPage`、`snapshot`/`snapshotPage` 基本是把 `Post` 换成 `Page`的复制粘贴（包括 A3 的那个 bug，两边都有）。已经出现细节不一致：`UpdateLocale` 显式写 `Revision: 0`（service.go:196），`UpdatePageLocale` 的对应位置省略了这个显式赋值（pages.go:136），目前靠零值兜底功能相同，但预示着以后修 A1/A3 这类 bug 时很容易只改一份、漏掉另一份。

**修复方向**：参考 `lifecycle.go` 里 `ChangeStatus`/`RestoreRevision`/`DeleteRecycled` 已经用 `lifecyclePaths`/`getLifecycleContent` 做到的按 kind 参数化，把 Posts/Pages 共用逻辑收敛成一套。

### A5. `RestoreRevision` 恢复到历史版本后，不会清理当前头部多出来的语言文件 `[子代理产出，未复核]`

[internal/content/lifecycle.go:282-293](internal/content/lifecycle.go:282)（`writeLifecycleContent`，由 `RestoreRevision` 第 119 行调用）只会写入 `restored.Content` 里存在的语言，不会删除当前头部存在、但目标历史版本没有的语言文件。

**失败场景**：文章当前有 `en`/`fr`/`de` 三个语言，管理员恢复到一个早于 `de` 译文诞生的历史版本。恢复后 `meta.Locales` 不再列出 `de`（所以 `GetPost` 不会尝试读它），但 `content/posts/<id>/de.md` 会一直留在磁盘上，成为应用看不到、也永远不会被清理的孤儿文件（除非整篇被彻底删除）。

**修复方向**：计算出 `restored.Content` 后，与当前头部的语言集合做差集，对多出来的语言调用 `RemoveFile`。

---

## B. 次要领域服务（media / projection / taxonomy / menus / links / comments / upvotes / visits / dictionary / themes / audit）

### B1. 分类/标签删除的"是否被引用"检查，靠新建一个不共享锁的 `content.Service` 实例，和真正的内容编辑之间没有互斥 `[子代理产出，未复核]`

[internal/taxonomy/service.go:229](internal/taxonomy/service.go:229) 的 `Delete` 调用 `content.NewService(s.repository).ListPosts()`——`content.Service` 用自己的 `sync.Mutex`（[internal/content/service.go:45](internal/content/service.go:45)）保护状态，但这只保护单个实例内部，这里临时新建的实例和服务器长期持有、被 post/page 编辑接口实际使用的那个实例完全不共享锁。

**失败场景**：管理员 A 调用 `DELETE /taxonomies/categories/news`，同时管理员 B 正在保存一篇把 `"news"` 加进 `Meta.Categories` 的文章。`Delete` 的引用扫描先于 B 的写入落盘完成，没发现引用，于是删除了 `content/taxonomies/categories/news.yaml`；B 的保存随后完成，引用了一个已经不存在的分类 ID——公开站点的分类页渲染/链接会出现悬空引用，且没有任何报错提示给任何一方。

**修复方向**：让这个检查在服务器共享的 `mutationGate`（或内容锁）保护下进行，或者直接复用服务器已经持有的那个 `content.Service` 实例，而不是每次都新建一个。

### B2. 媒体删除的"是否被引用"扫描完全没有和内容写入互斥，接口也没有取用共享互斥锁 `[子代理产出，未复核]`

[internal/media/service.go:155-197](internal/media/service.go:155)（`Delete`）与 `:385-420`（`referenced`），以及 [internal/server/media.go:58](internal/server/media.go:58)（`handleDeleteMedia` 从不获取 `mutationGate`）。

**失败场景**：管理员 A 在文章里插入 `![img](/media/2026/08/<id>.jpg)` 的同时，管理员 B 删除同一份媒体资源。`referenced()` 在 A 的编辑落盘前运行，没发现引用，`Delete` 把原图和元数据移进 `.trash` 并移除；A 的编辑随后提交，引用了一个已经消失的文件——发布出去的文章里出现一张裂图。

**修复方向**：媒体的创建/删除也纳入内容写入使用的同一把 `mutationGate`（排他）保护，让引用扫描和任何内容写入互斥。

### B3. `internal/themes` 里存在一个从未被生产代码调用、且比唯一实际使用路径更不安全的"便捷封装" `[子代理产出，未复核]`

[internal/themes/service.go:324-336](internal/themes/service.go:324) 的 `Service.Install()` 只做 `BeginInstall` → `Commit()`，完全跳过了真正在用的路径（[internal/server/themes.go:86-122](internal/server/themes.go:86)，`BeginInstall` → 若主题被激活则先跑一次静态构建校验、失败就回滚 → `Commit`）里的构建校验和回滚步骤。目前只有测试代码调用 `Install()`；但如果日后有人图省事直接调用它（CLI 工具、新的管理动作、"顺手简化"的重构），会在完全不校验站点能否正常渲染的情况下替换掉主题包，且此时 `BeginInstall` 已经把新包 rename 进了 `themes/installed/<id>`,没有回滚路径。

**修复方向**：删掉这个死代码封装；如果确实需要一个更简单的入口，让它复用 `installThemeReader` 同一套构建校验+回滚逻辑，避免同一件事有两套不同安全等级的实现。

### B4. 媒体元数据写入失败后的清理错误被丢弃，可能永久残留孤儿文件 `[子代理产出，未复核]`

[internal/media/service.go:100-108](internal/media/service.go:100)：

```go
if err := s.repository.WriteYAML(...); err != nil {
    _ = s.repository.RemoveFile(originalPath)
    return domain.MediaAsset{}, err
}
```

如果元数据写入失败（瞬时 I/O 错误、磁盘压力），紧接着的清理删除也失败（同一故障下很可能同时发生，比如文件系统被重新挂载为只读），原始图片文件会永久留在 `media/originals/<year>/<month>/<id>.ext` 下，没有任何元数据指向它。`List()` 只枚举 `media/metadata/*.yaml`，这类文件永远不可见；`Recover()`（第 57-61 行）只处理两阶段删除协议里的 `.trash`，不会扫描"有原图无元数据"的情况，长期下来悄悄泄漏磁盘空间。

**修复方向**：单独记录/上报这次清理失败（不要用 `_ =` 丢弃），并考虑让 `Recover()` 增加扫描孤儿原图文件的能力。

### B5. 评论/点赞的限流计数在写入失败时不会退还，导致合法重试被误判为超限；`visits` 包里同样的场景已经正确处理 `[子代理产出，未复核]`

[internal/comments/service.go:104-106](internal/comments/service.go:104) 与 [internal/upvotes/service.go:126-128](internal/upvotes/service.go:126) 都是在真正的持久化写入之前先记录一次限流时间戳；如果随后 `WriteYAML` 失败，这次尝试已经悄悄消耗了访客在当前窗口内 5 次（评论）或 30 次（点赞）配额里的一次，即使什么都没真正保存下来。对照 [internal/visits/service.go:125-129](internal/visits/service.go:125) 的 `Record`，它在写入失败时会显式 `delete(s.recent, rateKey)` 把配额还回去——同一个模式在评论/点赞里没有做。

**修复方向**：`Create`/`Add` 在写入失败的分支里，用和 `visits.Record` 一样的方式把刚记录的限流条目撤销。

---

## C. 渲染器（apps/renderer）与部署/CI

### C1. 分类/标签分页在跨语言回退时，可能生成指向不存在页面的重定向（或漏掉本该有的重定向）`[子代理产出，未复核，逻辑较复杂建议优先复核]`

[apps/renderer/src/build.ts:91-101](apps/renderer/src/build.ts:91)：当某个分类/标签在当前 `locale` 下没有译文、需要回退到另一个语言时，重定向桩页所用的 `taxonomyPosts`/`totalPages` 是用**当前 locale 自己**选出的文章集合算出来的，而不是用"实际要跳转到的目标语言"（`selection.locale`）的文章集合算出来的。

**失败场景**：`zh-CN` 下"news"分类只有 10 篇文章（1 页），但 `fr` 本地选出的符合条件文章有 13 篇（其中 3 篇只有 `fr` 译文、没有 `zh-CN` 译文），于是回退分支会生成 `/fr/categories/news/page/2/ → /zh-CN/categories/news/page/2/` 这样的重定向，但 `/zh-CN/categories/news/page/2/` 从来没有被渲染出来（`zh-CN` 自己的循环只到第 1 页）——重定向目标 404。反过来页数算少了则会漏掉本该生成的重定向。

**修复方向**：回退分支的 `taxonomyPosts`/`totalPages` 应该用 `selection.locale`（跳转目标语言）选出的文章集合来计算，而不是当前 `locale` 的。

### C2. `themes/earth/theme.yaml` 是一份自己都通不过校验的孤儿清单文件，会误导第三方主题作者 `[子代理产出，未复核]`

[themes/earth/theme.yaml](themes/earth/theme.yaml) 声明 `id: earth`、`server: dist/index.js`；但 [internal/themes/service.go:850-866](internal/themes/service.go:850) 的 `validManifest()` 明确拒绝 `id=="earth"`，且要求 `Server` 必须是 `.mjs` 后缀。这个文件在运行时从不会被读取（内置 earth 主题在 `service.go:198-204` 里硬编码为 `Server: "built-in"`），Dockerfile 也只拷贝 `themes/earth/dist`（[Dockerfile:39](Dockerfile:39)），不拷贝这个 yaml。它纯粹是仓库里的死内容，但如果第三方主题作者把它当模板复制，会因为 `id` 冲突和 `.js` 后缀直接被 `BeginInstall` 拒绝（service.go:370-375）。

**修复方向**：删掉这个文件，或者把它改成一份真正合法、能通过校验的示例清单。

### C3. Earth 主题设置 Schema 在两处手工维护，没有同步校验 `[子代理产出，未复核]`

[themes/earth/settings.schema.json](themes/earth/settings.schema.json) 和 [internal/themes/earth_settings.go:3-130](internal/themes/earth_settings.go:3) 里的 `earthSettingsSchema` 字符串字面量目前内容一致，但后者才是 `Service.Settings("earth")`（service.go:674-677）实际提供给控制台的版本。前者在运行时不会被任何代码读取。没有测试或构建步骤检查两者是否一致——以后改一个忘了改另一个，后台设置界面会悄悄和实际生效的 Schema 脱节，不会有任何编译或测试失败提示。

**修复方向**：用 `go:embed` 直接读取 JSON 文件生成 Go 端的 Schema，而不是手工维护两份拷贝。

### C4. 渲染器构建产物把测试文件和未打包的调试产物一起塞进了生产镜像 `[子代理产出，未复核]`

[apps/renderer/package.json:8](apps/renderer/package.json:8) 的 `build` 脚本同时跑 `tsc`（连 `src/build.test.ts` 一起编译成 `dist/build.test.js`）和 `esbuild`（产出实际被使用的 `dist/cli.mjs`）；[Dockerfile:38](Dockerfile:38) 把整个 `dist` 目录拷进镜像，尽管运行时只会执行 `cli.mjs`（`MUTIBLOG_RENDERER_CLI`，Dockerfile:35）。

**修复方向**：把 `tsc` 步骤限定为纯类型检查（`--noEmit`，和已有的 `typecheck` 脚本一致）或排除 `*.test.ts` 的产出，和/或 Dockerfile 只拷贝 `dist/cli.mjs`。

### C5. 发布流水线只对 amd64 镜像跑冒烟测试，但会同时发布 arm64 镜像 `[子代理产出，未复核]`

[.github/workflows/release.yml:44-56](.github/workflows/release.yml:44) 的冒烟测试只构建并运行 `linux/amd64`；[:72-83](.github/workflows/release.yml:72) 实际推送到 `ghcr.io` 的镜像是 `linux/amd64,linux/arm64` 两个平台。arm64 产物从未被 `deploy/smoke-test.sh` 验证过，arm64 特有的问题（原生依赖、基础镜像差异）只能等用户拉取后才会暴露。

**修复方向**：用 QEMU 也对 arm64 构建跑一次冒烟测试，或者至少在文档里明确写出这是一个已知的覆盖缺口。

---

## D. 后台管理界面（剩余视图）

以下问题延续 [CONSOLE_REVIEW.md](CONSOLE_REVIEW.md) 已经确认的两类模式——"单个 ref 被多个并发动作互相覆盖"和"任务追踪样板代码在多处复制且已经出现分叉实现"——在其余视图里同样存在，其中第 1 项已亲自核实。

### D1. `ThemesView.vue` 里，激活主题和重新加载主题共享同一个 `themeTaskId`，设置保存和重置共享同一个 `settingsTaskId` `[已验证]`

[ui/console/src/views/ThemesView.vue:24-25,89,108,173,191](ui/console/src/views/ThemesView.vue:24) 确认：`activate()`（第 89 行）和 `reload()`（第 108 行）都写 `themeTaskId`；`saveSettings()`（第 173 行）和 `resetSettings()`（第 191 行）都写 `settingsTaskId`。对应按钮只检查各自的 `busy` 状态字符串，互相之间没有禁用关系。

**失败场景**：用户点击主题 A 的"激活"（`themeTaskId = taskA`，开始构建），在 A 的构建完成前又点击了主题 B（当前已激活）的"重新加载"。`reload()` 把 `themeTaskId.value` 覆盖成 `taskB`，页面上的 `<TaskProgress :task-id="themeTaskId">`（第 271 行）从此只追踪 taskB——taskA 的构建仍在服务端运行、仍可能失败，但界面上已经看不到它了。同样的问题存在于"从文件安装主题"与"从 URL 安装主题"共享 `installTaskId`（第 46-81/262/278 行）：URL 安装按钮的禁用条件没有检查是否正在进行文件安装，反之亦然。

**修复方向**：把任务 ID 按"主题 id + 动作"分别追踪（用 map，或改成 `CONSOLE_REVIEW.md` 建议过的、`TaxonomiesView.vue`/`LinksView.vue` 已经在用的累加数组方案），不要用一个共享 ref。

### D2. `TaxonomiesView.vue`、`LinksView.vue`、`MenusView.vue` 的保存/新建按钮没有 loading/禁用保护，双击可能造成竞态写入 `[子代理产出，未复核]`

`TaxonomiesView.vue` 的 `save()`（第 131 行按钮）、`LinksView.vue` 的 `createGroup`/`createLink`/`saveStructure`/`saveLocale`（第 42-44 行）、`MenusView.vue` 的 `createMenu`/`addItem`/`saveStructure`/`saveLocale`（第 25-27 行）绑定的 `@click` 都没有 `:disabled`/`:loading`。

**失败场景**：双击某个分类的"保存"按钮会并发触发两次 `save()`。每次调用都先执行 `buildTaskIds.value = []`（TaxonomiesView.vue:89）再各自 await 自己的请求——第二次调用的清空动作可能把第一次调用刚 push 进去的任务 ID 冲掉；且两次请求都用同一个尚未更新的 `selected.value.revision`，第二个请求大概率会被后端当成修订冲突拒绝，用户只是想保存一次，却看到一条费解的冲突报错。

**修复方向**：加一个 `saving` ref 在请求期间禁用按钮；或者让底层的任务追踪逻辑本身对重入安全。

### D3. `PagesView.vue` 的批量操作有和 `PostsView.vue` 完全相同的进度覆盖 bug（不是各自独立的问题，是同一处代码复制粘贴的结果）`[子代理产出，未复核，但与本人已确认的 PostsView 问题为同一模式，可信度高]`

[ui/console/src/views/PagesView.vue](ui/console/src/views/PagesView.vue) 的 `bulkAction()`（第 134-172 行）与 `PostsView.vue` 的对应实现（第 141-179 行）几乎逐行相同：`startBuildTask(true)`（第 150/157 行）在循环每一轮都覆盖同一个 `activeTaskId` ref，模板（第 181/194 行）只渲染一个 `<TaskProgress>`。批量操作 N 个页面时，只有最后一项的构建进度可见；`bulkCurrent`/`bulkTotal` 只给出"3 / 10"这样的数字进度，没有任何逐项成功/失败的详情。

**修复方向**：与 CONSOLE_REVIEW.md 第 4 条一起修，两个文件应该共用同一套修好之后的任务追踪方案，而不是分别修一遍。

### D4. 任务追踪样板代码（`discardTaskIfMissing`/类似逻辑）在至少 7 个文件里重复实现，且已经分裂成两种互不兼容的策略 `[子代理产出，未复核]`

`ThemesView.vue:38-44`、`LocalesView.vue:61-67`、`TaxonomiesView.vue:67-84`、`LinksView.vue:29-30`、`MenusView.vue:14-15`、`PostsView.vue:117-123`、`PagesView.vue:110-116` 都各自实现了一遍"轮询任务状态，404 就清空追踪状态"的逻辑。其中 `ThemesView`/`PostsView`/`PagesView` 用的是"单个可覆盖 ref"策略（正是 D1、D3 两个 bug 的根源），`TaxonomiesView`/`LinksView`/`MenusView` 则各自独立想出了"累加数组"策略，没有这个覆盖问题。这正是 [CONSOLE_REVIEW.md](CONSOLE_REVIEW.md) 第 3 条里指出的"同一处逻辑复制导致同一个漏洞出现两次"模式的又一次重演，且规模更大（7 处而非 2 处）。

**修复方向**：抽成一个共享组合式函数（如 `useBuildTasks()`），统一采用累加数组的策略，所有这些视图改为从这一处导入。

### D5. `ThemeSettingsField.vue` 里数字类型的设置项，清空输入框重新输入时会被强制归零，和用户的编辑动作打架 `[子代理产出，未复核]`

[ui/console/src/components/ThemeSettingsField.vue:41-44](ui/console/src/components/ThemeSettingsField.vue:41)：

```js
if (props.schema.type === "number" || props.schema.type === "integer") {
  const number = Number(input.value);
  emit("update:modelValue", Number.isFinite(number) ? number : 0);
```

`Number("")` 等于 `0`，是有限数，所以清空输入框准备重新输入时会立刻把值归零。因为这个输入框是受控组件、绑定的是 `:value="String(modelValue ?? '')"`（第 140-146 行），Vue 会立刻把 DOM 值写回 `"0"`，用户没法正常清空后输入负数或以小数点开头的数字。

**修复方向**：输入为空字符串时先保留"尚未确定"的本地状态，不要立刻强制转换成 `0`。

---

## 优先级建议

给 Codex 安排顺序时，建议按下面的分组处理，同组内没有严格先后依赖：

1. **数据一致性/正确性（最高优先级，涉及内容丢失或悬空引用）**：A1、A3、A5、B1、B2、C1
2. **并发/异步正确性**：A2、B3、B5，以及 [ISSUES.md](ISSUES.md) 里已经列出的定时发布互斥锁问题
3. **前端任务追踪的系统性重构（一次性解决 D1/D3/D4 以及 CONSOLE_REVIEW.md 里的第 3/4 条，而不是逐个文件打补丁）**
4. **代码重复/死代码清理**：A4、B3、C2、C3、C4，以及 CONSOLE_REVIEW.md 第 2 条
5. **较小的体验/边界问题**：A4 已含、B4、D2、D5、C5，以及 CONSOLE_REVIEW.md 第 5、6、7 条
