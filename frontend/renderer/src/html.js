// 文档外壳：head/meta/SEO/结构化数据/FOUC 脚本与整体拼装。
import { esc } from './markdown.js'

const foucScript =
  '<script>try{var t=localStorage.getItem("theme");if(t==="dark"||(!t&&matchMedia(' +
  '"(prefers-color-scheme: dark)").matches))document.documentElement.classList.add("dark")}' +
  'catch(e){}</script>'

function seoHead(props) {
  let out = ''
  if (props.canonical) out += '<link rel="canonical" href="' + esc(props.canonical) + '">'
  for (const alternate of props.alternates || []) {
    if (alternate?.hrefLang && alternate?.href)
      out +=
        '<link rel="alternate" hreflang="' +
        esc(alternate.hrefLang) +
        '" href="' +
        esc(alternate.href) +
        '">'
  }
  return out
}

function structuredData(props) {
  if (!props.canonical || !['post', 'page'].includes(props.kind)) return ''
  const data = {
    '@context': 'https://schema.org',
    '@type': props.kind === 'post' ? 'BlogPosting' : 'WebPage',
    headline: String(props.title || ''),
    description: String(props.description || ''),
    url: String(props.canonical),
  }
  if (props.author) data.author = { '@type': 'Person', name: String(props.author) }
  if (props.publishedAt) data.datePublished = String(props.publishedAt)
  if (props.modifiedAt) data.dateModified = String(props.modifiedAt)
  return (
    '<script type="application/ld+json">' +
    JSON.stringify(data).replaceAll('<', '\\u003c') +
    '</script>'
  )
}

/**
 * 组装完整 HTML 文档。
 * style 由主题负责（body 内联或 manifest 样式链接）；这里只保留元数据与 FOUC。
 */
export function buildHtml({ props, content, themeUrls, islandScripts }) {
  const locale = props.locale || 'en'
  const title = props.title || 'Mutiblog'
  const description = props.description
    ? `<meta name="description" content="${esc(props.description)}">`
    : ''
  return (
    '<!doctype html><html lang="' +
    esc(locale) +
    '"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">' +
    description +
    seoHead(props) +
    structuredData(props) +
    '<title>' +
    esc(title) +
    '</title>' +
    themeUrls.styles +
    islandScripts +
    foucScript +
    '</head><body>' +
    content +
    '</body></html>'
  )
}
