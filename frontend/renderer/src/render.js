// 页面渲染编排：Markdown → 主题模板 → 文档外壳。
import { esc, renderMarkdown } from './markdown.js'
import { renderTheme, themeAssetUrls } from './theme.js'
import { buildHtml } from './html.js'

function fallbackContent(props, body) {
  const title = esc(props.title || 'Mutiblog')
  const items = (props.items || [])
    .map((item) => '<li><a href="' + esc(item.url) + '">' + esc(item.title) + '</a></li>')
    .join('')
  const inner = items ? '<ul>' + items + '</ul>' : '<article>' + body + '</article>'
  return (
    '<main style="max-width:760px;margin:2rem auto;padding:0 1rem;' +
    'font-family:system-ui,sans-serif;line-height:1.7">' +
    '<h1>' +
    title +
    '</h1>' +
    inner +
    '</main>'
  )
}

/**
 * 渲染一个页面单元：Markdown 转换 → 主题模板（不可用时用内置兜底）→ 文档组装。
 */
export async function render(props) {
  const markdownResult = await renderMarkdown(props.body, props.markdown || {})
  const body = markdownResult.html
  let content = await renderTheme(props, body)
  if (!content) content = fallbackContent(props, body)
  const themeUrls = await themeAssetUrls(props.themeDir)
  const islandScripts = content.includes('data-island') ? themeUrls.scripts : ''
  const islands = [...new Set([...content.matchAll(/data-island="([^"]+)"/g)].map((m) => m[1]))]
  const meta = { ...markdownResult.meta, islands }
  return {
    html: buildHtml({ props, content, themeUrls, islandScripts }),
    meta,
    warnings: markdownResult.warnings,
  }
}
