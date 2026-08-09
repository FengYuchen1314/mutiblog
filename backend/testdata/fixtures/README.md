# 测试夹具

## `devil-test.zh-cn.md` — 魔鬼测试文章

一篇故意包含全部难处理元素的文章，同时服务两套验收。

### 用途一：渲染验收（`docs/04 §10` R1/R2）

```bash
# 发布后检查渲染产物
./blog-server rebuild
open generated/public/zh-cn/posts/devil-test/
```

| 检查项 | 期望 |
|---|---|
| 代码块 | 有 Shiki 的 `<span style="--shiki-light:...">`，语言标签正确 |
| 公式 | KaTeX 渲染成 `<span class="katex">`，**不是**原始 `$...$` |
| Mermaid | 输出 `data-island="Mermaid"` 容器，内含 `<pre class="mermaid-source">` |
| 表格对齐 | `text-align: right` / `center` 正确应用 |
| 脚注 | 正文有 `<sup><a href="#user-content-fn-1">`，底部有脚注列表 |
| 任务列表 | `<input type="checkbox" checked disabled>` |
| 原始 HTML | `<details>` / `<sup>` / `<br>` 原样保留 |
| 标题锚点 | 每个标题带 `id` 与 `.heading-anchor` |
| 外链 | `target="_blank" rel="noopener noreferrer"` |
| 分隔线 | 末尾的 `---` 渲染成 `<hr>`，**没有**被当成 Front Matter |

**禁用 JavaScript 再看一遍**：排版、代码高亮、公式、表格必须全部正常；Mermaid 显示源码即可（这是预期的渐进降级）。

### 用途二：AI 翻译验收（`docs/06 §9` T1~T22）

文中的 `## T1` ~ `## T22` 小节与 `docs/06 §9` 的验收表一一对应。

```bash
# 翻译成英文后逐项比对
diff <(sed -n '/^## T1/,/^## T2/p' content/posts/.../index.zh-cn.md) \
     <(sed -n '/^## T1/,/^## T2/p' content/posts/.../index.en.md)
```

**核心判据：以下内容翻译后必须逐字节不变。**

| 小节 | 必须不变的内容 |
|---|---|
| T1 | 整个代码块，**包括里面的中文注释** |
| T2 | 所有 `` `行内代码` `` |
| T3 | 所有 URL（含带中文查询参数的） |
| T4 | 所有 IP 地址（IPv4 / IPv6 / 带端口） |
| T5 | 所有文件路径（Unix / Windows） |
| T6 | 所有 `$...$` 与 `$$...$$` 公式 |
| T7 | 整个 mermaid 代码块 |
| T8 | `<details>` `<summary>` `<sup>` `<br>` 标签本身（内部文字**应该**翻译） |
| T9 | 所有链接的 URL 部分与引用式链接定义（链接**文字**应翻译） |
| T10 | 所有图片路径（alt **应该**翻译） |
| T11 | 表格的分隔行与对齐标记；行列数不变 |
| T12 | `[x]` / `[ ]` 标记 |
| T13 | 脚注标识符 `[^1]` `[^note]` |
| T14 | 列表的层级与缩进结构 |
| T16 | `**` `*` `~~` 语法字符 |
| T17 | 标题深度序列 `# ## ### ## ####` |
| T18 | HTML 实体与转义字符 |
| T20 | `{{ .Title }}` `${VARIABLE}` `$(pwd)` |
| T21 | 邮箱与域名 |

**特殊判据**：

- **T19 繁简差异**：翻译成 `zh-TW` 时必须使用台湾用语——「软件」→「軟體」、「网络」→「網路」、「打印」→「列印」。**只做字符转换算不合格**。
- **T22 长段落**：验证分段器把它单独成批，且段内的行内占位符全部正确还原。

### 失败时怎么排查

| 现象 | 多半是 |
|---|---|
| 代码块内容被改写 | 分段器把 `FencedCodeBlock` 当成了可翻译节点 |
| 占位符残留（译文里出现 `⟦P3⟧`） | 回填时 `restorePlaceholders` 没匹配上 |
| 占位符数量不一致 | 模型改写了占位符，校验应拦截而未拦截 |
| 表格行列数变化 | 表格分隔行被送进了翻译 |
| 标题层级错乱 | 结构指纹校验未生效 |

对应的校验逻辑见 `docs/06 §3.5`（响应校验）与 `§4.2`（结构指纹）。
