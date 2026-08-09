// Markdown 管线：remark/rehype + Shiki + KaTeX + GFM + mermaid→island。
import { unified } from 'unified'
import remarkParse from 'remark-parse'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'
import remarkRehype from 'remark-rehype'
import rehypeRaw from 'rehype-raw'
import rehypeSlug from 'rehype-slug'
import rehypeAutolinkHeadings from 'rehype-autolink-headings'
import rehypeKatex from 'rehype-katex'
import rehypeExternalLinks from 'rehype-external-links'
import rehypeStringify from 'rehype-stringify'
import rehypeShiki from '@shikijs/rehype'
import {
  transformerNotationHighlight,
  transformerNotationDiff,
  transformerNotationFocus,
  transformerMetaHighlight,
} from '@shikijs/transformers'

import go from 'shiki/langs/go.mjs'
import bash from 'shiki/langs/bash.mjs'
import python from 'shiki/langs/python.mjs'
import javascript from 'shiki/langs/javascript.mjs'
import typescript from 'shiki/langs/typescript.mjs'
import jsx from 'shiki/langs/jsx.mjs'
import tsx from 'shiki/langs/tsx.mjs'
import json from 'shiki/langs/json.mjs'
import yaml from 'shiki/langs/yaml.mjs'
import markdownLang from 'shiki/langs/markdown.mjs'
import sql from 'shiki/langs/sql.mjs'
import css from 'shiki/langs/css.mjs'
import html from 'shiki/langs/html.mjs'
import rust from 'shiki/langs/rust.mjs'
import diff from 'shiki/langs/diff.mjs'
import dockerfile from 'shiki/langs/dockerfile.mjs'
import ini from 'shiki/langs/ini.mjs'
import java from 'shiki/langs/java.mjs'
import c from 'shiki/langs/c.mjs'
import cpp from 'shiki/langs/cpp.mjs'
import vue from 'shiki/langs/vue.mjs'
import graphql from 'shiki/langs/graphql.mjs'
import ruby from 'shiki/langs/ruby.mjs'
import php from 'shiki/langs/php.mjs'
import toml from 'shiki/langs/toml.mjs'
import xml from 'shiki/langs/xml.mjs'

const shikiLangs = [].concat(
  go,
  bash,
  python,
  javascript,
  typescript,
  jsx,
  tsx,
  json,
  yaml,
  markdownLang,
  sql,
  css,
  html,
  rust,
  diff,
  dockerfile,
  ini,
  java,
  c,
  cpp,
  vue,
  graphql,
  ruby,
  php,
  toml,
  xml,
)

const noop = () => {}

/** HTML 转义，用于属性与文本插值。 */
export function esc(value = '') {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
}

function transformerAddLangLabel() {
  return {
    name: 'add-lang-label',
    pre(node) {
      node.properties['data-lang'] = this.options.lang || 'text'
    },
  }
}

function rehypeShikiLineNumbers(lineNumbers) {
  if (!lineNumbers) return noop
  return (tree) => {
    const walk = (node) => {
      if (!node || typeof node !== 'object') return
      if (
        node.type === 'element' &&
        node.tagName === 'pre' &&
        Array.isArray(node.properties?.className) &&
        node.properties.className.includes('shiki')
      ) {
        node.properties.className.push('with-line-numbers')
      }
      for (const child of node.children || []) walk(child)
    }
    walk(tree)
  }
}

function remarkMermaidToIsland() {
  return (tree) => {
    const walk = (node) => {
      if (node.type === 'code' && node.lang === 'mermaid') {
        const code = String(node.value || '')
        node.type = 'html'
        node.value =
          '<div class="mermaid-island" data-island="Mermaid" data-props="' +
          esc(JSON.stringify({ code })) +
          '"><pre class="mermaid-source">' +
          esc(code) +
          '</pre></div>'
        delete node.children
        return
      }
      for (const child of node.children || []) walk(child)
    }
    walk(tree)
  }
}

/**
 * 渲染 Markdown 为 HTML。
 * 返回 { html }；options 支持 katex/mermaid/externalLinksNewTab/headingAnchors/shiki。
 */
export async function renderMarkdown(source = '', options = {}) {
  const warnings = []
  const file = await unified()
    .use(remarkParse)
    .use(remarkGfm)
    .use(options.mermaid === false ? noop : remarkMermaidToIsland)
    .use(options.katex ? remarkMath : noop)
    .use(remarkRehype, { allowDangerousHtml: true })
    .use(rehypeRaw)
    .use(rehypeSlug)
    .use(options.headingAnchors ? rehypeAutolinkHeadings : noop, {
      behavior: 'append',
      properties: { className: ['heading-anchor'], ariaHidden: 'true' },
    })
    .use(options.katex ? rehypeKatex : noop, { throwOnError: false, strict: false })
    .use(rehypeShiki, {
      themes: { light: 'github-light', dark: 'github-dark' },
      defaultColor: false,
      fallbackLanguage: 'text',
      langs: shikiLangs,
      transformers: [
        transformerNotationHighlight(),
        transformerNotationDiff(),
        transformerNotationFocus(),
        transformerMetaHighlight(),
        transformerAddLangLabel(),
      ],
      onError: (e) => warnings.push(String(e)),
    })
    .use(rehypeShikiLineNumbers(Boolean(options.shiki?.lineNumbers)))
    .use(options.externalLinksNewTab ? rehypeExternalLinks : noop, {
      target: '_blank',
      rel: ['noopener', 'noreferrer'],
    })
    .use(rehypeStringify)
    .process(String(source))
  for (const warning of warnings) console.warn('markdown shiki:', warning)
  return { html: String(file) }
}
