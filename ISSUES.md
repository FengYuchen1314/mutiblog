# MutiBlog 问题纠错清单：自动翻译 / 发布流程与 AI 接入

本清单只指出问题所在及其证据（文件与行号），不给出修复方案。范围：自动翻译与发布流程的前后顺序，以及 AI Provider 接入在未配置 / 未填 API Key 时的报错清晰度。

## 1. “发布优先、翻译殿后”的顺序在两条路径上都成立，导致首次发布的站点构建不包含译文

### 1.1 立即发布路径（Post / Page）

[internal/server/posts.go:211-244](internal/server/posts.go:211) 与 [internal/server/pages.go:148-181](internal/server/pages.go:148) 中，`handlePublishPost` / `handlePublishPage` 的执行顺序是：

1. `s.content.PublishPost/PublishPage`（提交发布）
2. `s.publisher.Build(r.Context())`（静态站点重建，`posts.go:222` / `pages.go:159`）
3. 只有在第 2 步之后，才判断 `firstPublish` 并调用 `s.translator.Start(...)`（`posts.go:225` / `pages.go:162`）

`translator.Start` 内部把实际翻译工作放进后台 goroutine 异步执行（见 §2），HTTP 请求在 `Start` 返回后立即响应客户端。也就是说：**站点构建发生在翻译甚至还没开始之前**，首次发布返回给前端的 `report`/`build.status=succeeded` 对应的静态产物里不包含该文章的任何 AI 译文。

### 1.2 定时发布路径

[internal/scheduled/service.go:496-670](internal/scheduled/service.go:496) 的 `run()` 顺序同样是：

1. `publish-start` 检查点（`service.go:524`）
2. `s.publishContent(...)` 提交发布（`service.go:532`）
3. `scheduled-build` 阶段：调用 `s.rebuilder.Build(...)`（`service.go:599`）
4. 只有构建完成之后，才进入 `scheduled-translation` 阶段并调用 `s.translator.Start(...)`（`service.go:614-654`）

同样地，翻译在构建完成之后才“启动”，而不是构建依赖翻译结果。

### 1.3 “补一次构建”依赖翻译异步成功，而这次补建是否发生完全不透明

真正把译文写进静态站点的第二次构建，藏在 [internal/translation/service.go:691-730](internal/translation/service.go:691) 里：翻译服务在某个目标语言的 AI 译文被 `PromoteAITranslation` 提交为公开版本之后，如果 `hasSuccessfulTarget && s.rebuilder != nil`，会**自行**再调用一次 `s.rebuilder.Build(...)`（`service.go:714`）。

问题：
- 这次“补建”只在“至少一个目标语言翻译成功”时触发；如果翻译整体失败（详见 §3）、被判定为 `not-needed`、`not-configured`，或者只是还在排队中，则不会有第二次构建，且没有任何上层信号提示管理员“网站现在只有源语言版本，还差一次构建”。
- 触发第二次构建与第一次构建（发布时那次）之间没有任何显式的先后依赖或等待关系——两次构建是两个完全独立的调用点（`publisher.go:518` 的 `Build` 分别被 `posts.go:222`/`pages.go:159`/`scheduled/service.go:599` 和 `translation/service.go:714` 各自调用），谁先谁后、是否会重复构建，只取决于翻译任务的运行时机，属于典型的顺序不确定的异步耦合。

## 2. 翻译任务是“启动即返回”的异步任务，调用方拿到的只是“已排队”而不是最终结果

[internal/translation/service.go:262-326](internal/translation/service.go:262) 的 `Start()` 只做参数校验、目标语言解析、默认 Provider 凭据检查、写入任务记录，然后调用 `s.launch(task.ID)`（`service.go:322`），真正的翻译工作在独立 goroutine 里的 `s.run(taskID)`（`service.go:141-154`，`service.go:450` 起）异步执行。

- 立即发布路径中，`posts.go:225-236` / `pages.go:162-173` 拿到的 `translationErr` 只反映 `Start()` 这一步的同步错误（目标语言不合法、Provider 未配置、`ErrNoTargets` 等），并不反映后续翻译任务在 goroutine 中实际跑出来的成功/失败结果。HTTP 响应体里的 `translation.status=queued` 是发布请求返回那一刻的快照，此后翻译成功、失败、超时（`translationTargetTimeout = 15 * time.Minute`，`service.go:35`）都不会再通知到这次发布请求的调用方。
- 定时发布路径中同理：`scheduled/service.go:641-654` 只捕获 `Start()` 的同步错误来设置 `task.TranslationStatus`。

## 3. 定时发布任务的顶层状态/结果字段永远反映不出翻译的真实结果

[internal/scheduled/service.go](internal/scheduled/service.go) 中 `Task` 结构体同时有 `BuildStatus`（`service.go:54`）和 `TranslationStatus`（`service.go:56`），以及一个总的 `Outcome` 字段（`service.go:58`）。

但在 `run()` 的收尾逻辑里（`service.go:660-669`）：

```go
if buildFailure != "" {
    task.Outcome = buildFailure
    s.finishBestEffort(&task, "succeeded", buildFailure)
    return
}
s.finishBestEffort(&task, "succeeded", "")
```

`Outcome` 只会被 `buildFailure`（静态构建失败/不可用）赋值，`TranslationStatus` 无论是 `"queued"`、`"failed"`、`"not-configured"` 中的哪一种，都**不会**影响 `Outcome` 或顶层 `task.Status`——顶层任务始终以 `"succeeded"` 收尾。管理员如果只看任务列表的整体状态，看不出“这次首发其实没有配置 AI Provider，翻译根本没跑”和“翻译已经在后台排队且正常完成”是同一个 `"succeeded"`。

进一步地，`TranslationStatus` 被写为 `"queued"`（`service.go:637`/`644`）之后，**没有任何代码路径会再更新它**。`Reconcile()`（`service.go:203-324`）只处理 `task.Status` 仍为 `"queued"`/`"running"` 的记录（`service.go:223`），而定时发布任务在设置完 `TranslationStatus` 后，本身的顶层 `Status` 已经在同一次 `run()` 里被 `finishBestEffort` 置为 `"succeeded"`（终态），因此永远不会再被 `Reconcile()` 捞回来做二次核对。也就是说：一旦某次定时发布把 `TranslationStatus` 记成 `"queued"`，这个字段会永久停留在 `"queued"`，不管对应的翻译任务（`TranslationTaskID` 指向的 `translation.Task`）后来是成功、失败还是被判定 `needs-review`。

（`internal/scheduled/service.go:618-655` 中唯一会“回读”翻译任务当前状态的分支，是 `task.TranslationTaskID != ""` 且 `task.TranslationStatus == ""` 同时成立时——但这只发生在进程崩溃后 `Reconcile()` 重新跑到同一个尚未终结的定时任务时，属于恢复路径，不是常规完成后的对账机制。）

## 4. 未填 / 无效 API Key 时，报错在不同层被合并或丢弃成模糊信息

### 4.1 Client 层把“没填 Key”和“Provider 配置本身不合法”合并成同一个错误

[internal/ai/client.go:76-80](internal/ai/client.go:76)：

```go
func (c Client) Chat(ctx context.Context, provider domain.AIProviderConfig, apiKey string, messages []ChatMessage, maxTokens int) (string, error) {
	endpoint, err := chatEndpoint(provider)
	if err != nil || strings.TrimSpace(apiKey) == "" {
		return "", ErrInvalidProvider
	}
```

`chatEndpoint` 返回的 URL/Scheme/Host/Model 校验错误，和“API Key 是空字符串”这两种完全不同性质的问题，被合并成同一个 `ErrInvalidProvider`（`"invalid AI provider configuration"`）。调用方拿到这个错误后无法区分“Provider 本身配置错误”还是“单纯没填 Key”。目前之所以在主流程里不容易观察到，是因为 [internal/ai/service.go:268-296](internal/ai/service.go:268) 的 `credentials()` 会在调用 `Chat` 之前先单独检查 `secrets.Providers[id] == ""` 并返回更明确的 `ErrKeyMissing`（`service.go:290`）——但这只是上层调用顺序恰好把两种情况分开了，`Client.Chat` 本身的错误分类仍然是模糊、合并的。

### 4.2 Provider 返回非 2xx 时，真实错误原因被主动丢弃

[internal/ai/client.go:113-116](internal/ai/client.go:113)：

```go
if response.StatusCode < 200 || response.StatusCode >= 300 {
    _, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
    retryable := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
    return "", &providerRequestError{status: response.StatusCode, retryable: retryable}
}
```

响应体被读入后直接丢弃（`io.Discard`），错误里只保留 HTTP 状态码。`providerRequestError.Error()`（`client.go:29-34`）最终只会输出类似 `"AI provider request failed: HTTP 401"` 这样的信息。当 API Key 错误、Key 被撤销、或者 Key 格式不被目标 Provider 接受时，Provider 一般会在响应体里给出具体原因（例如 “Incorrect API key provided” 之类的文本），但这条代码路径从不解析、不透出这部分内容——管理员在控制台看到的错误信息里，`HTTP 401` 无法区分“Key 错误”“Key 过期”“Key 没有权限访问该模型”等不同情况。

### 4.3 定时发布：Key 缺失被降级为一个不显眼的字段值，不是可见的失败

如 §3 所述，[internal/scheduled/service.go:648-649](internal/scheduled/service.go:648)：

```go
case errors.Is(translationErr, ai.ErrProviderNotFound), errors.Is(translationErr, ai.ErrKeyMissing), errors.Is(translationErr, ai.ErrInvalidProvider):
    task.TranslationStatus = "not-configured"
```

`"not-configured"` 只是写入 `TranslationStatus` 这个次要字段，顶层任务仍以 `"succeeded"` 收尾（`service.go:669`），`Outcome` 字段保持空值。相比之下，静态构建失败会被写进 `Outcome`（`service.go:661`）——两类同样属于“首次发布未完全达成预期”的问题，在数据结构和可见性上待遇不一致。

### 4.4 立即发布路径：AI 未配置时的信息只存在于一次性响应体里

[internal/server/posts.go:231-232](internal/server/posts.go:231) / [internal/server/pages.go:168-169](internal/server/pages.go:168) 把 Key 缺失、Provider 不存在、Provider 配置无效这三种不同错误统一映射成同一个 `translation.status = "not-configured"`，随立即发布的 HTTP 响应一次性返回给前端。此后若管理员没有注意这次响应内容（例如批量发布、脚本调用、或前端未展示该字段），系统里不存在其他地方会再次提醒“这篇文章的自动翻译因为没配置 AI 而没有执行”。

## 5. 定时发布任务持有全站互斥锁的时间跨越“发布 + 静态构建 + 启动翻译”整个过程，期间管理后台几乎所有接口（包括任务进度查询本身）都会被阻塞

[internal/server/server.go:106-107](internal/server/server.go:106) 声明了一个进程级的 `mutationGate sync.RWMutex`。定时发布服务的 `acquire` 回调（[server.go:214-221](internal/server/server.go:214)）在被调用时执行 `server.mutationGate.Lock()`——这是**排他写锁**，不是共享读锁。

这个回调在 [internal/scheduled/service.go:496-500](internal/scheduled/service.go:496) 的 `run()` 一开始就被调用，并用 `defer release()` 一直持有到 `run()` 整个函数返回为止：

```go
func (s *Service) run(ctx context.Context, id string) {
    if s.acquire != nil {
        release := s.acquire()
        defer release()
    }
    ...
```

而 `run()` 函数体内依次完成的工作包括：提交发布（`publishContent`，`service.go:532`）、静态站点重建（`s.rebuilder.Build(...)`，`service.go:599`，渲染器超时上限为 `rendererTimeout = 2 * time.Minute`，见 [internal/publisher/service.go:185](internal/publisher/service.go:185)）、以及启动翻译（`s.translator.Start(...)`，`service.go:641`）——这整段时间里，`mutationGate` 的排他锁始终没有被释放。

与此同时，几乎所有管理后台接口都挂在 `requireAdmin` 中间件之下，而 `requireAdmin` 对应 [internal/server/auth.go:210-211](internal/server/auth.go:210) 的 `requireAdminWithGate(next, false, true)`，`exclusive=false` 意味着它会在处理每一个请求前先执行 `s.mutationGate.RLock()`（[auth.go:239-241](internal/server/auth.go:239)）。Go 的 `sync.RWMutex` 语义下，只要有协程持有写锁（或在等待写锁），新的读锁获取就会被阻塞。

其中包括管理后台本应实时展示任务进度所依赖的接口本身：

```
GET /api/v1/admin/tasks         -> s.requireAdmin(s.handleListTasks)      (server.go:400)
GET /api/v1/admin/tasks/{id}    -> s.requireAdmin(s.handleGetTask)        (server.go:401)
GET /api/v1/admin/ai/tasks      -> s.requireAdmin(s.handleListTranslationTasks) (server.go:486)
```

也就是说：一次定时发布触发之后，只要它正处在“发布 → 构建 → 启动翻译”这段临界区内（构建阶段最长可达约 2 分钟，实际取决于渲染器和内容规模），管理员打开控制台去查看“这次发布/翻译进行到哪一步了”所调用的任务列表接口本身也会被同一把锁卡住，直到 `run()` 整体返回才能响应——包括登录校验（[auth.go:240-241](internal/server/auth.go:240) 同样走 RLock）在内的其余管理接口同样受影响。这是“自动发布流程”里一个隐藏的全局串行化点：本意是保护内容一致性的互斥锁，把一次异步定时任务的执行时长直接转嫁成了整个管理后台的不可用窗口。

## 6. `translation.Service.Start` 里凭据检查被放在目标语言与人工译文冲突检查之后

[internal/translation/service.go:262-301](internal/translation/service.go:262) 中 `Start()` 的检查顺序是：

1. 读取文章（`getContent`，`service.go:270`）
2. 计算目标语言、识别人工翻译冲突（`s.targets(...)`，`service.go:274`）
3. 处理人工翻译覆盖确认逻辑（`service.go:278-297`）
4. 最后才调用 `s.ai.DefaultCredentials()` 检查是否配置了默认 Provider 与 Key（`service.go:298`）

也就是说，即便 AI 从未配置，一次翻译请求仍会先完整走完目标语言解析和人工译文冲突判断，才在最后一步暴露“其实没有可用的 AI Provider”这一根本性前提缺失。这与 §1.1 描述的“先构建、再发现没配置 AI”的顺序问题同属一类：AI 可用性检查没有被当作前置门禁来处理，而是穿插在流程中间靠后的位置。
