# MutiBlog 后台管理界面（ui/console）审查与修改建议

审查范围：`ui/console/src` 下的路由、布局、状态管理、API 层，以及全部 24 个视图组件中已通读的核心部分（路由、Dashboard、Overview、Backup、Settings、任务相关三件套、Posts 列表、Post 编辑器、AI Provider 管理），合计约 2400/3300 行源码，以及完整的 `api/client.ts`。每条问题给出证据（文件:行号）和具体修改建议。

## 1. 路由里有重复/失效的入口，且用一处 hack 掩盖了这个问题

[ui/console/src/router/index.ts:28-29](ui/console/src/router/index.ts:28)：

```ts
{ path: "theme", name: "theme", component: () => import("@/views/ThemesView.vue") },
{ path: "themes", name: "themes", component: () => import("@/views/ThemesView.vue") },
```

同一个组件注册了两条路由。侧边栏（[ConsoleLayout.vue:36](ui/console/src/layouts/ConsoleLayout.vue:36)）只链接到 `/theme`，`/themes` 是一个没有任何入口指向、只能靠直接改 URL 才能访问的“影子路由”。而为了让 `/themes` 被直接访问时侧边栏仍然高亮“主题”这一项，`ConsoleLayout.vue:117` 专门写了一段特判：

```html
:class="{ 'nav-item--active': route.path === item.to || (item.to === '/theme' && route.path === '/themes') }"
```

**建议**：删除 `themes` 这条冗余路由注册（`router/index.ts:29`）以及 `ConsoleLayout.vue:117` 里的特判代码；如果 `/themes` 这个路径需要保留兼容旧链接，改成 `{ path: "themes", redirect: "/theme" }`（就像同文件里 `ai/tasks` 重定向到 `/tasks` 那样处理，[router/index.ts:36](ui/console/src/router/index.ts:36)），而不是重复注册组件再用样式判断打补丁。

## 2. `TranslationTasksView.vue` 是一整个功能完整但完全没有被引用的“死文件”，且它比目前在用的替代视图更完整

`ui/console/src/views/TranslationTasksView.vue`（80 行）实现了一个专门的翻译任务列表页：请求 `api.translationTasks()`，逐条渲染任务、逐个目标语言的状态标签，并且有一份专门的 `legacyErrors` 映射表用来把旧版后端错误字符串翻译成新错误码（[TranslationTasksView.vue:16-27](ui/console/src/views/TranslationTasksView.vue:16)）。

用全仓库搜索可以确认它没有被任何路由、任何组件引用：

```
grep -rn "TranslationTasksView" ui/console/src --include="*.ts" --include="*.vue"
# 无结果
```

现在真正在用的是统一任务中心 [TaskCenterView.vue](ui/console/src/views/TaskCenterView.vue)（路由 `/tasks`），`/ai/tasks` 会重定向到它（[router/index.ts:36](ui/console/src/router/index.ts:36)）。但 `TaskCenterView.vue` 在渲染翻译任务的目标语言时（[TaskCenterView.vue:164-168](ui/console/src/views/TaskCenterView.vue:164)）只显示每个目标的“语言 · 百分比”，**没有**把 `target.error`（每个目标语言各自的失败原因，比如 `provider-key-missing`、`unsafe-output`）显示出来——这正是被废弃的 `TranslationTasksView.vue` 有而现在这个视图没有的能力（对比 [TranslationTasksView.vue:49-54](ui/console/src/views/TranslationTasksView.vue:49) 的 `taskErrors()`）。也就是说，从“翻译任务专页”迁移到“统一任务中心”的过程中，**每个目标语言失败原因的展示能力被丢掉了**，只剩一个笼统的 `task.error`（例如只会显示 `"target-failed"` 这种任务级汇总码，见后端 [internal/translation/service.go:1068-1073](internal/translation/service.go:1068)），管理员无法从界面上看出“到底是哪个语言、因为什么原因翻译失败”。

**建议**：
- 删除 `TranslationTasksView.vue`（连同它未被使用的 i18n key，如果 `tasksPage.*` 没有被其他视图复用的话，一并清理），避免死代码继续存在、误导后来的维护者以为它还在生效；
- 把它独有的能力（每目标语言错误展示、`legacyErrors` 兼容映射）迁移进 `TaskCenterView.vue` 现有的目标语言渲染块（`TaskCenterView.vue:164-168`），让统一任务中心真正达到被废弃页面的信息完整度，而不是功能倒退后的替代品。

## 3. AI Key 未配置的告警逻辑，在两个组件里各自复制了一份，并且都漏掉了 `not-configured` 状态——这是“报错不明显”问题在前端的直接体现

[ui/console/src/components/TaskProgress.vue:26-36](ui/console/src/components/TaskProgress.vue:26)：

```ts
const warnings = computed(() => {
  if (!task.value) return [];
  const values: string[] = [];
  if (["failed", "unavailable"].includes(task.value.buildStatus ?? "")) {
    values.push(String(t("taskProgressWarnings.buildFailed")));
  }
  if (["failed", "needs-review"].includes(task.value.translationStatus ?? "")) {
    values.push(String(t("taskProgressWarnings.translationFailed")));
  }
  return values;
});
```

[ui/console/src/views/TaskCenterView.vue:96-105](ui/console/src/views/TaskCenterView.vue:96) 里的 `taskWarnings()` 是几乎一模一样的另一份拷贝。两处都只把 `translationStatus` 为 `"failed"` 或 `"needs-review"` 当作需要告警的情况，唯独漏掉了 `"not-configured"`——而根据后端 [internal/scheduled/service.go:648-649](internal/scheduled/service.go:648)，“定时发布时没有配置 AI Provider / 没填 API Key”恰好会把 `translationStatus` 设成 `"not-configured"`。

实际后果可以完整追踪到具体页面：一次定时发布触发后，`scheduled.Task` 最终会以 `status: "succeeded"` 收尾（无论翻译有没有真正发生，参见后端问题清单 [ISSUES.md](ISSUES.md) 第 3 节），管理员在 [PostEditorView.vue](ui/console/src/views/PostEditorView.vue) 里通过 `<TaskProgress :task-id="publicationTaskId" />`（[PostEditorView.vue:471](ui/console/src/views/PostEditorView.vue:471)）看到的进度条会稳定停在绿色的“已完成”，`warnings` 计算属性因为上面这个判断漏掉 `not-configured` 而**不会显示任何警告**。同样地，`TaskCenterView.vue` 的任务列表里这一条记录也不会带任何 `task-center-warning`。管理员从界面上完全无法得知“这次首发因为没配置 AI Key，实际上没有产出任何翻译”。

**建议**：
- 把 `"not-configured"` 加入两处判断的状态列表（改成 `["failed", "needs-review", "not-configured"]`），并为 `taskProgressWarnings` 增加一条区分“未配置”和“失败”的文案（复用当前 `not-configured` 已有的 i18n key，例如 `editorPage.publishedNoAi` 已经在用类似文案，参见 [PostEditorView.vue:369](ui/console/src/views/PostEditorView.vue:369)）；
- 更根本的问题是这段 `warnings`/`taskWarnings` 逻辑在两个组件里逐字复制，导致同一个疏漏出现了两次。建议把它提成一个共享的组合式函数（例如 `src/composables/useTaskWarnings.ts`），`TaskProgress.vue` 和 `TaskCenterView.vue` 都从这一处导入，以后修复或扩展只需要改一个地方。
- 同理，`progressLabel` / `taskProgressTranslated` / `translated` / `taskErrorLabel` 这四个函数在 [TaskProgress.vue:38-70](ui/console/src/components/TaskProgress.vue:38) 和 [TaskCenterView.vue:53-93](ui/console/src/views/TaskCenterView.vue:53) 里几乎逐行相同，建议一并抽到共享的 i18n 辅助模块里（可以放进现有的 `src/i18n/useCodeLabel.ts` 旁边），消除这类“同一处 bug 需要改两次”的隐患。

## 4. 批量操作用一个共享的 `activeTaskId` 追踪进度，批量发布时只能看到最后一条的进度

[ui/console/src/views/PostsView.vue:141-179](ui/console/src/views/PostsView.vue:141) 的 `bulkAction()` 对每个选中的文章依次调用发布/下线/回收接口，每次循环都会调用 `startBuildTask(true)` 生成新任务 ID 并写回同一个 `activeTaskId` ref（[PostsView.vue:107-115](ui/console/src/views/PostsView.vue:107)，[PostsView.vue:157](ui/console/src/views/PostsView.vue:157)）；批量发布分支里还会在拿到结果后再次覆盖它（[PostsView.vue:163-167](ui/console/src/views/PostsView.vue:163)）：

```ts
else if (action === "publish") {
  const result = await api.publishPost(csrfToken, post.meta.id, post.meta.revision, taskId);
  if (result.build.status === "scheduled") activeTaskId.value = result.build.taskId ?? "";
  else if (result.translation.status === "queued") activeTaskId.value = result.translation.taskId ?? "";
  if(result.build.status==="failed"||result.translation.status==="failed")failed+=1;
}
```

页面上只有一个 `<TaskProgress v-if="activeTaskId" :task-id="activeTaskId" />`（[PostsView.vue:194](ui/console/src/views/PostsView.vue:194)）。批量勾选 5 篇文章一起发布时，前 4 篇的构建/翻译任务 ID 会被逐次覆盖、从未被展示过，界面上唯一能看到进度的，只有循环里最后处理的那一篇。管理员既不知道前几篇是否已经构建成功，也看不到它们各自是否触发了翻译。

**建议**：批量操作不应该复用单一 `activeTaskId` 做进度展示。至少应该：
- 把 `bulkCurrent`/`bulkTotal` 的简单计数进度条（已有，[PostsView.vue:194](ui/console/src/views/PostsView.vue:194)）作为批量场景下唯一可信的进度来源，批量循环内不再更新 `activeTaskId`（即删除 163-167 行里对 `activeTaskId` 的写入）；
- 如果需要展示每篇文章各自的构建/翻译结果，改成收集一个 `Array<{ postId: string; taskId: string }>`，批量结束后跳转到 `/tasks?...`（任务中心已经支持按 `kind`/`status` 过滤，参见 `api.tasks()` 定义 [api/client.ts:429-437](ui/console/src/api/client.ts:429)），而不是试图在同一个进度条组件里塞多条任务的状态。

## 5. AI Provider 的“设为默认”复选框，取消勾选对已是默认的 Provider 不起作用，UI 没有说明这一点

[ui/console/src/views/ProvidersView.vue:131](ui/console/src/views/ProvidersView.vue:131)：

```html
<label class="provider-check"><input v-model="form.default" type="checkbox" />{{ t("providersPage.setDefault") }}</label>
```

这是一个看起来独立可勾选/取消的复选框。但后端 `Upsert` 的语义（[internal/ai/service.go:126-128](internal/ai/service.go:126)）是：

```go
if input.Default || config.DefaultProvider == "" {
    config.DefaultProvider = id
}
```

只有“勾选 = true”才会把某个 Provider **设为**默认；“取消勾选 = false”**不会**把已经是默认的 Provider 从默认位置上移除——`DefaultProvider` 字段只能被别的 Provider 的 `Default=true` 覆盖，没有任何路径可以把它清空。也就是说，如果管理员编辑当前默认 Provider、取消这个复选框、点击保存，界面上复选框会显示为未勾选，但这个 Provider 实际上仍然是默认 Provider——下一次列表刷新（`edit()` 重新 `Object.assign(form, provider)`，[ProvidersView.vue:41-46](ui/console/src/views/ProvidersView.vue:41)）会让复选框又跳回勾选状态，行为上像是“保存没生效”，但用户不知道原因。

**建议**：把这个字段在 UI 上表达成它真实的语义——“单选式”的默认选择，而不是独立开关。例如：默认状态由 Provider 列表里的一个操作（“设为默认”按钮，只在非默认项上出现）驱动，编辑表单里去掉可勾选的 `default` 复选框，或者至少在它是当前默认项时禁用取消操作并给出提示文案（复用 `providersPage.*` 的 i18n 命名空间）。

## 6. Dashboard 和 Overview 是两个内容大幅重叠的“系统状态”页面，且都挂在侧边栏里

[DashboardView.vue](ui/console/src/views/DashboardView.vue) 的“系统状态”卡片（[DashboardView.vue:63-72](ui/console/src/views/DashboardView.vue:63)）展示：服务健康（硬编码为绿色 `success`/“健康”，并不基于任何真实的健康检查结果——只要 `api.systemStatus()` 请求成功就显示为健康，见下方单独说明）、索引状态与文档数、发布器类型、版本号。

[OverviewView.vue](ui/console/src/views/OverviewView.vue) 同样调用 `api.systemStatus()`（[OverviewView.vue:16](ui/console/src/views/OverviewView.vue:16)），展示版本号、发布器类型、数据目录、索引状态、文档数、最近索引时间、最近错误，外加一份安全审计日志。两者数据源相同，Overview 是 Dashboard 那张卡片的超集，只是多展示了数据目录、索引时间/错误、审计日志。二者同时出现在侧边栏（[ConsoleLayout.vue:19](ui/console/src/layouts/ConsoleLayout.vue:19) 的根分组 和 [ConsoleLayout.vue:52](ui/console/src/layouts/ConsoleLayout.vue:52) 的“系统”分组），管理员需要在两个入口之间猜测该去哪里看系统信息。

另外单独指出：`DashboardView.vue:66` 的服务健康行 `<VStatusDot state="success" /> {{ t("dashboard.healthy") }}` 是写死的常量，不读取任何字段——`api.systemStatus()` 只要 HTTP 200 就会让这一行渲染为“健康”，它既不调用后端真正的 `/health/live`、`/health/ready` 探活接口，也不反映索引是否处于 `error` 状态。当索引状态已经是 `error`（下面那一行会显示出来）时，“服务健康”这一行依然是绿色的“健康”，两行信息互相矛盾。

**建议**：
- 二选一：要么把 Overview 的内容合并进 Dashboard（保留一个统一的“系统状态”入口），要么明确分工（比如 Dashboard 只做内容运营入口——文章/页面/评论数与快捷创建，去掉系统状态卡片；系统健康/审计只在 Overview/系统分组下呈现），并从侧边栏去掉重复入口；
- 把 `dashboard.healthy` 这一行改成基于真实信号：至少应该在 `status.index.status === "error"` 时联动显示为异常，而不是恒定为 `success`；如果要展示“服务存活”，应该调用后端的 `/health/live` 而不是复用 `systemStatus` 的请求成功与否作为健康判断依据。

## 7. 项目里没有配置 ESLint/Prettier，导致同一套代码风格在文件间严重不一致，加大了阅读和排查问题的成本

`ui/console/package.json` 的 `scripts` 里只有 `dev`/`build`/`typecheck`/`test`，没有 `lint` 或 `format`；仓库里也没有找到 `.eslintrc*`、`eslint.config.*`、`.prettierrc*`。这直接反映在代码风格上：

- [BackupView.vue:9-17](ui/console/src/views/BackupView.vue:9) 和 [SettingsView.vue:11-20](ui/console/src/views/SettingsView.vue:11) 把整个 `<script setup>` 里几乎所有变量声明和函数体压缩成一行/几行超长代码（例如 `SettingsView.vue:11` 一行就声明了 8 个 `ref`/变量，中间没有任何空格分隔语句边界之外的可读性处理）；
- 同一目录下的 [TranslationTasksView.vue](ui/console/src/views/TranslationTasksView.vue)、[TaskCenterView.vue](ui/console/src/views/TaskCenterView.vue)、[OverviewView.vue](ui/console/src/views/OverviewView.vue) 则是正常的多行、有缩进的现代 Vue 代码风格。

两种风格混杂在同一个代码库里，说明代码要么来自不同时间/不同方式生成（例如未经格式化的自动生成代码直接提交），要么曾经格式化过但后来的修改又退化成压缩风格，且没有任何自动化手段（CI 里没有 lint 步骤，`package.json` 没有 lint 脚本）能够发现或阻止这种劣化。压缩成一行的文件（`BackupView.vue`、`SettingsView.vue`）明显更难做代码审查、更难定位问题行号、diff 也更难看清改动范围。

**建议**：给 `ui/console` 引入 ESLint（`eslint-plugin-vue` + `@typescript-eslint`）和 Prettier，把 `lint`/`format` 加进 `package.json` 的 `scripts`，并对 `BackupView.vue`、`SettingsView.vue` 这类文件跑一次格式化让它们与其余文件风格一致；如果仓库有 CI（`.github/` 下已有工作流），建议把 lint 检查接入进去，防止这类风格退化再次发生。

## 8. `handlePublicationTaskUpdate` 只在“定时发布”分支里生效，没有处理“定时任务已经复用已有翻译任务”的情况

[PostEditorView.vue:339-343](ui/console/src/views/PostEditorView.vue:339)：

```ts
function handlePublicationTaskUpdate(task: UnifiedTask) {
  if (task.kind === "ScheduledPublish" && task.translationTaskId) {
    translationTaskId.value = task.translationTaskId;
  }
}
```

这个函数只在 `publicationTaskId` 对应的任务是 `ScheduledPublish` 且带有 `translationTaskId` 字段时，才会把翻译任务 ID 接到 `translationTaskId` 上、从而在编辑器里展示第二条翻译进度条。按后端 [internal/scheduled/service.go:618-655](internal/scheduled/service.go:618) 的逻辑，`translationTaskId` 只有在翻译**真正被启动**（`TranslationStatus` 变成 `"queued"`）时才会被写入；如果是 `"not-needed"`（没有需要翻译的目标语言）或 `"not-configured"`（AI 未配置）这两种情况，`translationTaskId` 始终是空字符串，`handlePublicationTaskUpdate` 不会做任何事，界面上也就不会出现第二条进度条——这与第 3 条问题是同一个根因在编辑器页面的另一处体现：只要翻译“根本没有被启动”，管理员在这个页面上看到的信息就只有第一条构建进度条本身不带任何翻译相关提示。

**建议**：与第 3 条一起修：`handlePublicationTaskUpdate` 应该在 `task.translationStatus === "not-configured"` 时也给出明确反馈（例如复用 `editorPage.publishedNoAi` 对应的文案，设置到 `publicationWarning` 或新增一个专门的提示区），而不是只在“已经拿到 `translationTaskId`”这一种情况下才有动作。

## 附：未展开审查的部分

限于篇幅，`ThemesView.vue`（312 行）、`LocalesView.vue`、`AttachmentsView.vue`、`TaxonomiesView.vue`、`CommentsView.vue`、`LinksView.vue`、`MenusView.vue`、`ToolsView.vue`、`SetupView.vue`、`LoginView.vue`、以及 `MarkdownEditor.vue`/`MediaPickerField.vue`/`ThemeSettingsField.vue` 三个组件本次没有逐行通读，只是在追踪上面问题时顺带看过引用关系。如果需要，可以继续对这几个文件做同等深度的审查。
