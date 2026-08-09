# 06 · AI 翻译引擎规格

> 全系统最容易做错的模块。核心原则：**让代码保证 Markdown 结构 100% 正确，模型只负责翻译自然语言字符串。**
>
> 绝不把整篇 Markdown 丢给模型说"请翻译并保持格式"——那必然会丢失/改写代码块、链接、公式。

---

## 1. 总体流程

```
源语言 Markdown
      │
      ▼
┌──────────────────┐
│ 1. 分离 FrontMatter│  → 只翻译 title / description / seo.title / seo.description
└────────┬─────────┘
         ▼
┌──────────────────┐
│ 2. goldmark 解析  │  → AST
└────────┬─────────┘
         ▼
┌──────────────────┐
│ 3. 段抽取器       │  → []Segment  （只含可翻译的自然语言）
│    受保护节点整体  │     受保护内容留在原地，不进入 Segment
│    跳过           │
└────────┬─────────┘
         ▼
┌──────────────────┐
│ 4. 按预算分批     │  → []Batch（每批若干 Segment）
└────────┬─────────┘
         ▼
┌──────────────────┐
│ 5. 调用 AI        │  → 输入 JSON 数组，要求输出等长 JSON 数组
│    校验 + 重试     │
└────────┬─────────┘
         ▼
┌──────────────────┐
│ 6. 回填           │  → 按 Segment 的字节区间替换原文
└────────┬─────────┘
         ▼
┌──────────────────┐
│ 7. 结构校验       │  → 重新解析译文 AST，比对结构指纹
└────────┬─────────┘
         ▼
┌──────────────────┐
│ 8. 写 index.en.md │  → 原子写 + 更新 metadata.yaml + 触发渲染
└──────────────────┘
```

---

## 2. Segment 抽取器（核心）

### 2.1 数据结构

```go
package ai

type Segment struct {
    Index    int      // 在批次中的序号
    Kind     SegKind  // heading | paragraph | listItem | tableCell | blockquote |
                      // imageAlt | linkTitle | frontMatterTitle | frontMatterDesc
    Text     string   // 待翻译的文本，可能含占位符 ⟦P1⟧
    Start    int      // 在原始 Markdown 中的字节起始偏移
    End      int      // 字节结束偏移
    Placeholders []Placeholder   // 该段内被替换掉的受保护片段
    Context  string   // 可选：给模型的上下文提示（如所属标题）
}

type Placeholder struct {
    Token   string   // "⟦P1⟧"
    Content string   // 原始内容，如 "`docker compose up`"
}
```

### 2.2 受保护节点（整块跳过，绝不进入 Segment）

以下 goldmark AST 节点类型**整体保留原文**，不抽取：

| 节点 | 说明 |
|---|---|
| `FencedCodeBlock` | ` ```...``` `，含 mermaid |
| `CodeBlock` | 缩进代码块 |
| `HTMLBlock` | 原始 HTML 块 |
| `ThematicBreak` | `---` |
| 数学块 `$$...$$` | 需自定义识别（goldmark 无内置 math，用正则预扫描保护） |
| Front Matter 中除 title/description 外的一切 | |
| 链接的 URL 部分 | 只翻译链接文本 |
| 图片的 URL 部分 | 只翻译 alt |
| 脚注定义标识符 `[^1]` | 只翻译脚注内容文本 |

### 2.3 行内受保护片段（替换为占位符）

段落文本中的以下 inline 节点，替换为 `⟦P<n>⟧` 占位符：

| Inline 节点 | 处理 |
|---|---|
| `CodeSpan` （`` `code` ``） | 整体占位 |
| 行内数学 `$...$` | 整体占位 |
| `AutoLink` | 整体占位 |
| `RawHTML` （如 `<br>`、`<sup>`） | 整体占位 |
| Link 的 `(url)` 部分 | 占位；文本部分留在 Segment 中 |
| Image | 整体占位，但 alt 单独作为一个 Segment |
| 纯 URL / IP / 域名 / 文件路径 | 正则识别后占位（见 §2.4） |
| 脚注引用 `[^1]` | 占位 |
| HTML 实体 | 占位 |

**占位符格式**：`⟦P1⟧`（U+27E6 / U+27E7 数学白括号）。选这对字符的理由：
- 几乎不会出现在自然语言文本中
- 模型不会把它当作 Markdown 语法
- 单个 token，不易被拆分或改写

**绝不用** `{{1}}`、`[[1]]`、`__1__`、`<1>` —— 这些会与 Markdown/模板语法冲突，或被模型"修正"。

### 2.4 需要占位保护的正则

```go
var protectPatterns = []*regexp.Regexp{
    regexp.MustCompile(`https?://[^\s<>()\[\]{}"'，。；：！？]+`),          // URL
    regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`),           // IPv4[:port]
    regexp.MustCompile(`\b[0-9a-fA-F:]{2,}:[0-9a-fA-F:]+\b`),             // IPv6（粗略）
    regexp.MustCompile(`(?:^|\s)(/[\w.\-]+){2,}/?`),                       // Unix 路径
    regexp.MustCompile(`\b[A-Za-z]:\\[\w\\.\-]+`),                         // Windows 路径
    regexp.MustCompile(`\b[\w.\-]+\.(?:com|org|net|io|dev|cn|jp|de|me|sh|app|xyz)\b`), // 域名
    regexp.MustCompile(`\b[\w.+\-]+@[\w\-]+\.[\w.\-]+\b`),                 // Email
    regexp.MustCompile(`\{\{[^}]+\}\}`),                                   // 模板变量
    regexp.MustCompile(`\$\{[^}]+\}`),                                     // shell 变量
    regexp.MustCompile(`:[a-z0-9_+\-]+:`),                                 // :emoji:
}
```

**匹配顺序很重要**：先长后短，先具体后通用。匹配后立即替换为占位符，避免重复匹配。

### 2.5 抽取算法

```go
func Extract(md string, opts ExtractOpts) ([]Segment, error) {
    // 0. 预扫描：把 $$...$$ 与 $...$ 的区间记录为受保护区间
    mathRanges := scanMath(md)

    // 1. goldmark 解析（只用 parser，不用 renderer）
    src := []byte(md)
    doc := goldmark.DefaultParser().Parse(text.NewReader(src))

    var segs []Segment
    ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
        if !entering { return ast.WalkContinue, nil }
        switch n.Kind() {
        case ast.KindFencedCodeBlock, ast.KindCodeBlock, ast.KindHTMLBlock, ast.KindThematicBreak:
            return ast.WalkSkipChildren, nil       // 整块跳过

        case ast.KindHeading:
            segs = append(segs, buildSegment(src, n, SegHeading, mathRanges))
            return ast.WalkSkipChildren, nil

        case ast.KindParagraph, ast.KindTextBlock:
            // TextBlock 出现在 list item / table cell 内
            segs = append(segs, buildSegment(src, n, kindFor(n), mathRanges))
            return ast.WalkSkipChildren, nil
        }
        return ast.WalkContinue, nil
    })

    // 2. 图片 alt 单独抽取（它们在被跳过的 inline 中）
    segs = append(segs, extractImageAlts(src, doc)...)

    // 3. 按 Start 排序，校验区间不重叠
    sort.Slice(segs, func(i,j int) bool { return segs[i].Start < segs[j].Start })
    if err := validateNoOverlap(segs); err != nil { return nil, err }
    return segs, nil
}
```

`buildSegment` 遍历该块的 inline 子节点，把受保护的替换为占位符，产出 `Text` + `Placeholders`，并记录该块在源中的字节区间 `[Start, End)`。

**关键**：`Start/End` 必须是**原始 Markdown 的字节偏移**，回填时直接做字符串切片替换，从后往前替换以免偏移失效。

### 2.6 段落级保护的例外

**表格**：goldmark 的表格单元格是 `TableCell` → 内部有 inline。抽取每个单元格为独立 Segment。**表格分隔行 `|---|---|` 绝不进入 Segment**。

**任务列表**：`- [ ] 待办事项` 的 `[ ]` / `[x]` 标记不进入 Segment，只翻译后面的文本。

**引用块**：`> 引文` 的 `>` 前缀不进入 Segment（goldmark 的 Blockquote 内是 Paragraph，Start 偏移已在 `>` 之后，天然正确）。

**列表项前缀**：`- ` / `1. ` 同理，天然不在 TextBlock 区间内。

---

## 3. 批次组装与提示词

### 3.1 分批

```go
func Batch(segs []Segment, budgetChars int) [][]Segment
```

按 `config.ai.segmentBudget`（默认 2500 字符）累加分批。**单个 Segment 超过预算时单独成批**（不拆分段落——拆分会破坏语义连贯性）。

超长段落（> 8000 字符）：记录警告，仍单独发送；若模型返回被截断，标记该文章翻译失败并提示用户拆分段落。

### 3.2 请求格式

使用 OpenAI 兼容的 `POST {baseURL}/chat/completions`：

```json
{
  "model": "gpt-x",
  "temperature": 0.2,
  "messages": [
    { "role": "system", "content": "<系统提示词，见 §3.3>" },
    { "role": "user", "content": "<用户消息，见 §3.4>" }
  ],
  "response_format": { "type": "json_object" }
}
```

**`response_format` 的兼容性处理**：不是所有 OpenAI 兼容端点都支持。实现时：
1. 首次调用带 `response_format`；
2. 若返回 400 且错误信息含 `response_format`/`unsupported`，则记住该 provider 不支持，后续不再发送，改为纯提示词约束 + 输出解析容错（剥离 ```json 围栏）。
3. 该标记存在内存中，配置变更时重置。

### 3.3 系统提示词（内置默认，可被 `ai.systemPromptOverride` 覆盖）

```
You are a professional technical translator for a software engineering blog.

Translate each string in the input JSON array from {SOURCE_LANG} to {TARGET_LANG}.

CRITICAL RULES — violating any of these makes the output unusable:

1. Output ONLY a JSON object of the form {"segments": ["...", "..."]}.
   The array MUST have exactly the same number of elements as the input array,
   in the same order. Never merge, split, add, or drop elements.

2. Placeholder tokens that look like ⟦P1⟧, ⟦P2⟧ are opaque protected content
   (code, URLs, IPs, file paths, math, HTML). You MUST:
   - Reproduce every placeholder EXACTLY as it appears, character for character.
   - Keep the same set of placeholders in each string (none added, none removed).
   - You MAY move a placeholder within the string if the target language's
     word order requires it.
   - NEVER translate, explain, renumber, or reformat a placeholder.

3. Preserve inline Markdown syntax that appears in the text:
   **bold**, *italic*, ~~strikethrough~~, [link text](⟦Pn⟧), footnote markers.
   Translate the human-readable words inside them; keep the syntax characters.

4. Do NOT translate: product names, brand names, programming language names,
   library names, command names, configuration keys, environment variable names,
   error message identifiers, or acronyms that are conventionally left in English
   in {TARGET_LANG} technical writing.

5. Keep the register and tone of the original: technical, concise, factual.
   Do not add explanations, notes, apologies, or extra sentences.
   Do not summarize. Translate faithfully and completely.

6. If a string is a heading, keep it short and heading-like.
   If a string is already in {TARGET_LANG} or is language-neutral
   (e.g. a version number), return it unchanged.

7. For Chinese targets: use the conventions of {TARGET_LANG} specifically
   (zh-CN = Simplified Chinese, Mainland terminology;
    zh-TW = Traditional Chinese, Taiwan terminology).
   Do not merely convert characters — use region-appropriate vocabulary.
```

### 3.4 用户消息

```json
{
  "sourceLanguage": "zh-CN",
  "targetLanguage": "en",
  "articleTitle": "我的服务器搭建记录",
  "segments": [
    "本文记录我在 ⟦P1⟧ 上搭建服务器的过程。",
    "安装 Docker",
    "执行 ⟦P2⟧ 即可启动全部服务。"
  ]
}
```

`articleTitle` 作为全局上下文帮助模型判断领域术语。

### 3.5 响应校验（每一批都必须校验）

```go
func validateBatch(in []Segment, out []string) error {
    if len(out) != len(in) {
        return fmt.Errorf("segment count mismatch: sent %d, got %d", len(in), len(out))
    }
    for i, s := range in {
        got := out[i]
        // 1. 占位符集合必须完全一致（数量与内容，顺序可变）
        want := placeholderSet(s.Text)
        have := placeholderSet(got)
        if !maps.Equal(want, have) {
            return fmt.Errorf("segment %d: placeholder mismatch, want %v got %v", i, want, have)
        }
        // 2. 非空原文不得翻译成空
        if strings.TrimSpace(s.Text) != "" && strings.TrimSpace(got) == "" {
            return fmt.Errorf("segment %d: empty translation", i)
        }
        // 3. 长度合理性：译文不应超过原文 5 倍或短于 1/8（防止模型答非所问/截断）
        if r := float64(len([]rune(got))) / float64(len([]rune(s.Text))); r > 5 || r < 0.125 {
            return fmt.Errorf("segment %d: suspicious length ratio %.2f", i, r)
        }
        // 4. 不得包含明显的模型自述
        if containsRefusal(got) {
            return fmt.Errorf("segment %d: model refusal detected", i)
        }
    }
    return nil
}
```

**校验失败的重试策略**：
1. 第 1 次失败 → 原样重试（温度降到 0）。
2. 第 2 次失败 → 把批次**对半拆分**，分别重试（大概率是某一段有问题）。
3. 拆到单段仍失败 → 该段**保留原文**（不翻译），记录警告，继续处理其余段。
4. 若失败段数 > 总段数的 20% → 整篇标记 `failed`。

这个策略保证"部分失败不会毁掉整篇翻译"。

---

## 4. 回填与结构校验

### 4.1 回填

```go
func Apply(md string, segs []Segment, translated []string) (string, error) {
    var b strings.Builder
    last := 0
    for i, s := range segs {
        b.WriteString(md[last:s.Start])
        text := restorePlaceholders(translated[i], s.Placeholders)
        b.WriteString(text)
        last = s.End
    }
    b.WriteString(md[last:])
    return b.String(), nil
}
```

（因为 segs 已按 Start 升序且无重叠，正序拼接即可，比倒序替换更清晰。）

`restorePlaceholders` 把 `⟦P1⟧` 换回原始内容。**若译文中缺少某个占位符**（校验应已拦截，这是兜底）：把缺失内容追加到段末，并记录警告。

### 4.2 结构指纹校验

回填后重新解析译文，比对结构指纹：

```go
type Fingerprint struct {
    Headings     []int   // 各级标题的深度序列，如 [1,2,2,3,2]
    CodeBlocks   []string // 各代码块的语言标识 + 内容 hash
    Links        int
    Images       int
    ListItems    int
    TableRows    int
    Footnotes    int
    MathBlocks   int
}

func Fingerprint(md string) Fingerprint
```

源与译文的指纹必须**完全一致**。不一致则：
- 代码块内容不一致 → **严重**，标记 failed（说明模型改了代码）
- 标题深度序列不一致 → **严重**，标记 failed
- 链接/图片数量不一致 → 警告，保留译文但在后台标记「需要人工检查」
- 其余差异 → 警告

### 4.3 Front Matter 处理

译文的 Front Matter：
1. 从源 Front Matter 深拷贝。
2. 替换 `title`、`description`、`seo.title`、`seo.description` 为译文。
3. 设置 `locale` 为目标语言。
4. `slug`：默认沿用源 slug（保持 URL 一致性）。配置 `ai.translateSlug: false` 为默认；为 true 时额外请求模型生成一个 URL 友好的目标语言 slug。
5. 权威字段（date/status/categories/tags/author/pinned/cover）原样镜像。
6. `updated` 设为当前时间。

---

## 5. 翻译任务与状态机

### 5.1 状态转换

```
                  ┌──────────┐
                  │ original │  （源语言，终态）
                  └──────────┘

  (无记录)
      │ 触发翻译
      ▼
  ┌─────────┐  worker 领取  ┌─────────────┐  成功  ┌───────────┐
  │ pending │─────────────▶│ translating │───────▶│ completed │
  └─────────┘               └─────────────┘         └───────────┘
      ▲                            │                     │
      │ 重新触发                    │ 失败(重试耗尽)        │ 源 revision 增加
      │                            ▼                     ▼
      │                       ┌────────┐           ┌──────────┐
      └───────────────────────│ failed │           │ outdated │
                              └────────┘           └──────────┘
                                                        │ 重新触发
                                                        └──────▶ pending

  ┌────────┐   人工编辑译文    任何状态
  │ manual │◀────────────────────────
  └────────┘   （终态：自动翻译永不覆盖，源更新只记录 SourceDrift）
```

### 5.2 触发时机

| 触发源 | 行为 |
|---|---|
| 源语言发布且 `autoTranslateOnPublish=true` | 为 `translateTargets` 中每个语言（跳过 `manual`）投递任务 |
| 后台点击「翻译此文章」 | 单语言或全部语言，可选「强制覆盖 manual」 |
| 后台「翻译矩阵」批量操作 | 批量投递 |
| 源语言更新（未发布） | **不触发**，只在发布时触发 |
| 目标语言状态为 `translating` | 不重复投递（dedupe key = `translate:<articleID>:<locale>`） |

### 5.3 任务执行

```go
func (t *Translator) Run(ctx context.Context, task Task) error {
    // 1. 检查前置条件
    a := t.ix.MustArticle(task.ArticleID)
    if a.Trans[task.Target].ManualEdited && !task.Force {
        return ErrManualProtected
    }

    // 2. 置为 translating，写 metadata.yaml（让后台立即看到状态）
    t.setStatus(a, task.Target, model.TSTranslating)

    // 3. 抽取 → 分批 → 逐批翻译（带限流）
    segs, err := Extract(a.Versions[a.Source].Body, opts)
    batches := Batch(segs, cfg.SegmentBudget)
    out := make([]string, len(segs))
    for bi, b := range batches {
        res, err := t.provider.Translate(ctx, b, task.Source, task.Target, a.Title())
        if err != nil { return t.fail(a, task, err) }
        copy(out[b[0].Index:], res)
        t.progress(task, bi+1, len(batches))
    }

    // 4. 翻译 Front Matter 的 title/description（单独一批）
    // 5. 回填 + 指纹校验
    body := Apply(a.Versions[a.Source].Body, segs, out)
    if err := CompareFingerprint(srcBody, body); err != nil { ... }

    // 6. 原子写 index.<target>.md + 更新 metadata.yaml
    //    translatedFromRevision = a.SourceRev
    //    status = completed, manualEdited = false
    // 7. 更新索引 + 触发渲染（该语言的文章页 + 列表页 + 其他语言页的 hreflang）
    return nil
}
```

**重要**：写译文文件时，`content.Store.SaveVersion` 必须传 `MarkManualEdit: false`，否则会误标为人工编辑。

### 5.4 人工编辑保护

后台编辑派生语言文章并保存时：

```go
opts := content.SaveOpts{ MarkManualEdit: true, MirrorAuthoritative: true }
```

这会把 `metadata.yaml` 中该语言的 `manualEdited: true`、`status: manual`、`manualEditedAt: now`。

之后源语言更新，`recomputeStatus` 只设置 `SourceDrift`，**不改 status**。后台 UI 显示：`manual · 源已更新 2 个版本`，并提供「重新 AI 翻译（将覆盖人工修改）」按钮，点击时二次确认。

---

## 6. Provider 抽象

```go
package ai

type Provider interface {
    Translate(ctx context.Context, segs []Segment, src, dst model.Locale, title string) ([]string, error)
    Test(ctx context.Context) error       // 后台"测试连接"按钮
    Name() string
}

type OpenAICompatible struct {
    baseURL, apiKey, model string
    temperature float32
    maxTokens   int
    client      *http.Client
    limiter     *rate.Limiter          // 自实现令牌桶，RPM
    noJSONMode  atomic.Bool            // §3.2 的降级标记
}
```

**HTTP 细节**：
- 请求头：`Authorization: Bearer <key>`、`Content-Type: application/json`。
- 超时：`config.ai.timeout`（默认 120s）。
- 重试：429 与 5xx 重试（尊重 `Retry-After` 头），指数退避；4xx（除 429）不重试。
- 响应解析：`choices[0].message.content` → 剥离可能的 ```json 围栏 → `json.Unmarshal` 到 `{segments: []string}`。
- 记录 `usage.prompt_tokens` / `completion_tokens` 到 `translation_tasks`。
- **不使用 streaming**（无必要，且增加解析复杂度）。

**API Key 安全**：
- 存 `config.yaml` 时支持 `${BLOG_AI_API_KEY}` 环境变量插值。
- 后台设置页显示为 `sk-...abcd`（只显示前 3 后 4），保存时若值未变（前端传回掩码）则不覆盖。
- 绝不写入日志、审计日志、错误信息。

### 6.1 「测试连接」

后台点击测试 → 发送一个固定的极短翻译请求（如翻译 `["Hello"]` 到目标语言），返回：延迟、模型名、消耗 token、译文内容。这能一次性验证 baseURL / key / model / JSON 模式支持情况。

---

## 7. 队列与并发

- Worker pool 并发 = `config.ai.concurrency`（默认 2）。
- 全局令牌桶限制 `rateLimitRPM`。
- 同一文章的多个目标语言可并行；同一 (文章, 语言) 通过 dedupe key 保证唯一。
- 任务超时（整篇）：`timeout × 批数 × 1.5`，超时则标记 failed。
- 取消：后台可取消 pending/translating 任务（`translating` 通过 context cancel）。

### 7.1 成本护栏

- `config.ai.maxTokensPerArticle`（默认 100000）：预估超过则拒绝并提示。
- 后台显示本月累计 token 用量（从 `translation_tasks` 聚合）。
- 提供「预估」按钮：抽取段落后只计算字符数与预估 token，不实际调用。

---

## 8. 后台 UI

### 8.1 文章编辑页的语言标签栏

```
┌─────────────────────────────────────────────────────────────┐
│  [中文 原文]  [English ✓]  [日本語 ⚠ 过期]  [Deutsch ✎ 人工] │
│  [+ 繁體中文 未翻译]                                          │
└─────────────────────────────────────────────────────────────┘
```

点击语言标签切换编辑该语言。非源语言时：
- 权威字段控件禁用，显示锁图标与提示「由源语言（中文）控制」。
- 顶部条显示状态与操作：`AI 翻译于 2026-08-09 · 基于源版本 19 · [重新翻译] [标记为人工维护]`。
- 过期时显示醒目黄条：`源文章已更新到版本 21，此译文基于版本 17 · [更新翻译]`。

### 8.2 翻译任务页 `/admin/i18n/tasks`

列表：文章标题 / 目标语言 / 状态 / 进度（3/7 批）/ 耗时 / token / 错误 / 操作（重试、取消、查看日志）。
顶部：队列深度、进行中、今日完成、今日 token、失败数。
SSE 实时更新。

### 8.3 翻译矩阵页 `/admin/i18n/matrix`

行 = 文章，列 = 语言，格 = 状态徽章。支持：
- 筛选：全部 / 有过期 / 有缺失 / 有失败
- 批量选择 → 「翻译选中项的缺失语言」/「更新所有过期译文」
- 点击格子 → 跳转到该语言的编辑页

---

## 9. 验收测试

准备一篇「魔鬼测试文章」，包含以下全部元素，翻译后逐项核对：

| # | 元素 | 期望 |
|---|---|---|
| T1 | ` ```go ` 代码块（含中文注释） | **代码与注释完全不变**（注释也在代码块内，不翻译） |
| T2 | 行内 `` `docker compose up -d` `` | 完全不变 |
| T3 | `https://example.com/a?b=c&d=中文` | URL 完全不变 |
| T4 | `192.168.1.1:8080` | 不变 |
| T5 | `/etc/nginx/nginx.conf` | 不变 |
| T6 | `$E = mc^2$` 与 `$$\sum_{i=1}^n$$` | 完全不变 |
| T7 | ` ```mermaid ` 图表 | 完全不变 |
| T8 | `<details><summary>点击</summary>内容</details>` | 标签不变，内部文本被翻译 |
| T9 | `[点击这里](https://x.com)` | 文本翻译，URL 不变，语法完整 |
| T10 | `![封面图](/media/a.png)` | alt 翻译，路径不变 |
| T11 | 表格（3×4） | 行列数不变，分隔行不变，单元格内容翻译 |
| T12 | 任务列表 `- [x] 完成` | `[x]` 不变，文本翻译 |
| T13 | 脚注 `文本[^1]` + `[^1]: 说明` | 标识符不变，说明文本翻译 |
| T14 | 嵌套列表（3 层） | 层级与缩进完全保持 |
| T15 | 引用块内含代码块 | 结构保持 |
| T16 | `**加粗**` `*斜体*` `~~删除~~` | 语法保持，内容翻译 |
| T17 | 标题层级 `# ## ### ##` | 深度序列完全一致 |
| T18 | Front Matter | title/description 翻译，其余字段完全不变 |
| T19 | 翻译成 zh-TW | 使用台湾用语（「軟體」而非「軟件」），非简单字符转换 |
| T20 | 中途 kill provider（模拟超时） | 状态变 failed，重试后成功，无半个文件产生 |
| T21 | 人工编辑 en 后更新源文章并发布 | en 保持 `manual`，显示 SourceDrift，**文件未被覆盖** |
| T22 | 一篇 200 段的长文 | 正确分批、进度正确、结果结构指纹一致 |
