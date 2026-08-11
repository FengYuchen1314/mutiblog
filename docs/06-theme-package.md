# MutiBlog 主题包协议（schemaVersion 1）

本文定义当前可安装主题的最小兼容契约。主题系统复刻 Halo 的安装、升级、启用和卸载体验，但不运行 Halo Thymeleaf 主题；主题服务端渲染格式属于 MutiBlog。

## 1. ZIP 目录结构

主题可以上传 ZIP，也可以由后台提供公网 HTTPS URL 下载；两种入口进入完全相同的校验、升级和构建回滚事务。远程下载拒绝私网/本机/链路本地/保留地址，仅允许 443 端口，最多跟随 5 次受同一策略约束的重定向，45 秒超时且不超过 20 MiB。`theme.yaml` 必须位于压缩包根目录。一个最小主题如下：

```text
midnight.zip
├── theme.yaml
├── server.mjs
└── assets/
    └── client.js
```

```yaml
schemaVersion: 1
id: midnight
name: Midnight
version: 1.0.0
requires: ">=0.1.0 <1.0.0"
engine: react-ssr
server: server.mjs
assets: assets
screenshot: screenshot.webp
settingsSchema: settings.schema.json
settingsReload: rebuild
postTemplates:
  - id: gallery
    name: Gallery
pageTemplates:
  - id: landing
    name: Landing page
```

- `id` 只能包含小写英文字母和单个连接号，必须在升级中保持不变；`earth` 是内置主题保留 ID，自定义主题不得使用。
- `engine` 在 schema 1 中固定为 `react-ssr`。
- `requires` 可省略；它以空格连接对 MutiBlog 主题 API 版本的比较条件，例如 `>=0.1.0 <1.0.0`。版本必须是无前导零的 `x.y.z`，支持 `>`、`>=`、`<`、`<=`、`=`；省略运算符表示精确匹配。当前主题 API 版本是 `0.1.0`。
- `server` 必须是主题包内的 `.mjs` 常规文件。
- `assets` 可省略；存在时必须指向主题包内的目录。
- `screenshot` 可省略；存在时必须指向主题包内的 `.png`、`.jpg`、`.jpeg` 或 `.webp` 常规文件。截图仅通过需要管理员会话的同源接口展示，控制台不会抓取 manifest 中的远程图片。
- `settingsSchema` 可省略；存在时指向主题包内的对象型 JSON Schema。`settingsReload` 当前只允许 `rebuild`：保存活动主题设置后必须通过完整静态构建并原子切换 release；省略时也按 `rebuild` 处理，以兼容早期主题包。MutiBlog 不提供会绕过构建门禁的服务端热重载。
- 设置 schema 的对象和字段可使用 `x-i18n: { en: "...", zh-CN: "..." }` 提供多语言标题；枚举字段可使用 `x-enum-i18n: { en: { value: "..." }, zh-CN: { value: "..." } }` 提供选项文案。控制台按当前后台语言选择，缺失时依次回退普通 `title` 和稳定键名。
- 所有路径均为相对 POSIX 路径，不允许绝对路径、反斜杠、`..` 或符号链接。
- `postTemplates`、`pageTemplates` 是可选的自定义内容模板清单；ID 只能由小写英文字母和单个连字符组成，不能重复，也不能占用默认的 `post` / `page`。

安装器限制压缩包为 20 MiB、500 个条目和 100 MiB 解压后数据，并拒绝路径穿越与符号链接。上传相同 `id` 会被视为升级。

## 2. 服务端模块契约

`server.mjs` 是已经编译好的、自包含的 ESM 模块。它必须导出 CSS 字符串和七个核心渲染函数，可以使用命名导出，也可以放在默认导出对象中；可选导出 `renderNotFound` 以提供主题化错误页：

```js
export const css = "body { margin: 0 }";
export function renderIndex(context) {}
export function renderPost(context) {}
export function renderPage(context) {}
export function renderTaxonomy(context, title, description) {}
export function renderLinks(context, groups) {}
export function renderArchive(context) {}
export function renderSearch(context) {}
export function renderNotFound(context) {} // optional
export const postTemplates = {
  gallery(context) {},
};
export const pageTemplates = {
  landing(context) {},
};
```

每个函数返回可由 React `renderToStaticMarkup` 渲染的值。主题构建产物应把 React 等运行依赖打进 `server.mjs`，不能假设主题安装目录旁存在 `node_modules`。

渲染上下文包含：

- 当前语言的站点标题、说明和已启用语言；
- 当前路径、文章或页面以及当前列表中的文章；
- 数据驱动的框架字典 `strings`；
- 数据驱动的导航树；
- 当前主题设置 `settings`。

主题 CSS 总是发布到 `/assets/theme.css`。主题自己的公开资源发布到 `/assets/themes/<theme-id>/`，不会覆盖系统资源。

文章和页面默认分别调用 `renderPost`、`renderPage`。主题在 manifest 声明自定义模板后，必须在同名 `postTemplates` / `pageTemplates` 映射中提供函数；缺少任何已声明函数都会令构建门禁失败。编辑器只允许选择活动主题声明的模板。内容切换到另一个主题后，如果原模板 ID 不存在，则明确回退新主题的默认渲染器；存储的模板 ID 不被破坏，切回原主题时仍可恢复原布局。

仓库中的 `examples/themes/minimal` 是一个不依赖安装目录 `node_modules` 的完整最小样例，可直接压缩该目录的内容后从后台安装。

## 3. 失败和信任边界

主题由唯一管理员主动安装，安装前会执行严格的归档和 manifest 校验。渲染进程同时启用 Node Permission Model：只允许读取本次无秘密构建快照、主题入口/资源和 staging 输出，只允许写入本次 staging 输出；默认禁止网络、子进程、Worker、原生扩展、WASI 和运行时检查器。主题不会获得 `secrets/`、内容真相源或其他发布目录的文件权限。每次渲染还有两分钟硬超时和 256 MiB Node 堆上限，超限只会令 staging 构建失败。

权限模型是纵深防御，不替代来源审查：主题模块仍属于服务端代码，只应安装可信来源的包。MutiBlog 不会在主题安装阶段执行模块；只有构建门禁中的受限渲染进程会加载它。

主题切换先写入候选配置并执行完整静态构建。模块加载、导出校验或任何页面渲染失败时：

1. 新的 staging 构建被丢弃；
2. 当前公开 release 保持不变；
3. 站点配置恢复为原主题。

活动主题升级使用同样的构建门禁；失败时旧主题目录与旧公开 release 一并保留。主题包替换由 `themes/installed/.transaction-*.yaml` 持久事务标记保护：构建门禁提交前中断会在下次启动移除未提交的新装包或恢复升级前旧包；提交标记已经落盘但清理未完成时只清理旧包与标记，不回退已验证的新包。无标记的旧版本 `.previous-*` 残留按未提交处理并保守恢复。活动主题不能直接卸载，内置 Earth 主题不能卸载。

控制台将当前活动主题详情和其余已安装主题分区管理，并显示主题包声明的本地截图；没有截图时显示名称占位。显式“重载配置”会重新读取并校验 manifest、设置 schema、当前设置与兼容条件，活动主题还必须重新通过完整静态构建。任一兼容主题都可先执行完整构建，生成不切换 `generated/current` 的 30 分钟隔离预览 release。随机能力链接只在独立的 `preview` 同级子域设置 HttpOnly Cookie，预览目录最多保留 10 份；预览域不复用后台会话，评论提交被拒绝。主题设置可导出为带 `schemaVersion`、`themeId` 和 `values` 的 JSON，也可导入同主题 JSON 后再明确保存；服务端仍以主题 schema 校验，并对活动主题执行构建门禁。普通卸载非活动主题只移除主题包并保留 `themes/settings/<id>.yaml`，以后重装同 ID 时继续使用；管理员也可明确选择“卸载并删除设置”，一次删除主题包和对应设置文件。“恢复默认”只删除设置文件并继续保留主题包。

## 4. 版本兼容

主题必须声明 `schemaVersion`。未来协议升级通过新的 schema 版本和迁移器处理，不能静默猜测或运行不兼容主题。不兼容主题可以安装以便检查和等待系统升级，但状态为 `incompatible` 且不能启用；若活动主题升级包不兼容，安装事务回滚到旧包。后台返回结构化 `Compatible` 条件及原因。
