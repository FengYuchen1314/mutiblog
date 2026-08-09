import http from 'node:http'
import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { renderToStaticMarkup } from 'react-dom/server'
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
import { transformerNotationHighlight, transformerNotationDiff, transformerNotationFocus, transformerMetaHighlight } from '@shikijs/transformers'

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

const shikiLangs = [].concat(go, bash, python, javascript, typescript, jsx, tsx, json, yaml, markdownLang, sql, css, html, rust, diff, dockerfile, ini, java, c, cpp, vue, graphql, ruby, php, toml, xml)

const socket=process.env.BLOG_RENDER_SOCKET||'/tmp/blog-render.sock'
try{fs.unlinkSync(socket)}catch(e){if(e.code!=='ENOENT')throw e}
function esc(v=''){return String(v).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;').replaceAll("'",'&#39;')}
const noop = () => {}
function transformerAddLangLabel(){
  return {
    name: 'add-lang-label',
    pre(node) { node.properties['data-lang'] = this.options.lang || 'text' },
  }
}
function rehypeShikiLineNumbers(lineNumbers){
  if (!lineNumbers) return noop
  return (tree) => {
    const walk = (node) => {
      if (!node || typeof node !== 'object') return
      if (node.type === 'element' && node.tagName === 'pre' && Array.isArray(node.properties?.className) && node.properties.className.includes('shiki')) {
        node.properties.className.push('with-line-numbers')
      }
      for (const child of node.children || []) walk(child)
    }
    walk(tree)
  }
}
function remarkMermaidToIsland(){
  return (tree) => {
    const walk = (node) => {
      if (node.type === 'code' && node.lang === 'mermaid') {
        const code = String(node.value || '')
        node.type = 'html'
        node.value = '<div class="mermaid-island" data-island="Mermaid" data-props="' + esc(JSON.stringify({ code })) + '"><pre class="mermaid-source">' + esc(code) + '</pre></div>'
        delete node.children
        return
      }
      for (const child of node.children || []) walk(child)
    }
    walk(tree)
  }
}
async function markdown(source='', options={}) {
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
      target: '_blank', rel: ['noopener', 'noreferrer'],
    })
    .use(rehypeStringify)
    .process(String(source))
  for (const warning of warnings) console.warn('markdown shiki:', warning)
  return String(file)
}
function seoHead(props){let out='';if(props.canonical)out+='<link rel="canonical" href="'+esc(props.canonical)+'">';for(const alternate of props.alternates||[]){if(alternate?.hrefLang&&alternate?.href)out+='<link rel="alternate" hreflang="'+esc(alternate.hrefLang)+'" href="'+esc(alternate.href)+'">'}return out}
function structuredData(props){if(!props.canonical||!['post','page'].includes(props.kind))return '';const data={'@context':'https://schema.org','@type':props.kind==='post'?'BlogPosting':'WebPage',headline:String(props.title||''),description:String(props.description||''),url:String(props.canonical)};if(props.author)data.author={'@type':'Person',name:String(props.author)};if(props.publishedAt)data.datePublished=String(props.publishedAt);if(props.modifiedAt)data.dateModified=String(props.modifiedAt);return '<script type="application/ld+json">'+JSON.stringify(data).replaceAll('<','\\u003c')+'</script>'}
const themeCache=new Map()
async function themeMarkup(props,html){const entry=props.themeDir&&path.join(props.themeDir,'dist','ssr','entry.js');if(!entry||!fs.existsSync(entry))return '';try{const stamp=fs.statSync(entry).mtimeMs;let mod=themeCache.get(entry);if(!mod||mod.stamp!==stamp){mod={stamp,value:await import(pathToFileURL(entry).href+'?v='+stamp)};themeCache.set(entry,mod)}const template=mod.value.templates?.[props.kind];if(template)return renderToStaticMarkup(template({...props,html}));if(typeof mod.value.render==='function')return await mod.value.render({...props,html});return ''}catch(err){console.error('theme SSR fallback:',err);return ''}}
async function render(props){const title=props.title||'Mutiblog';const body=await markdown(props.body, props.markdown||{});const locale=props.locale||'en';const description=props.description?`<meta name="description" content="${esc(props.description)}">`:'';const theme=props.theme||{};const accent=/^#[0-9a-f]{3}(?:[0-9a-f]{3})?$/i.test(theme.accentColor||'')?theme.accentColor:'#2563eb';const custom=String(theme.customCSS||'').replace(/<\/?style/gi,'').replace(/<script/gi,'').replace(/javascript\s*:/gi,'').replace(/expression\s*\(/gi,'');const style='<style>:root{--accent:'+esc(accent)+'}body{max-width:760px;margin:3rem auto;padding:0 1rem;font-family:system-ui,sans-serif;line-height:1.7}pre{padding:1rem;background:#0f172a;color:#f8fafc;overflow:auto}code{font-family:ui-monospace,monospace}a{color:var(--accent)}.heading-anchor{margin-left:.4rem;text-decoration:none;opacity:.55}.heading-anchor:hover{opacity:1}.locale-switcher,.pagination{display:flex;gap:.4rem;list-style:none;padding:1rem 0}.locale-switcher a,.pagination a,.pagination span{padding:.2rem .55rem;border:1px solid #cbd5e1;border-radius:.3rem}.pagination [aria-current=page],.locale-switcher [aria-current=page]{font-weight:700;background:#e2e8f0}.search-input{width:100%;padding:.65rem;font:inherit}.search-results{padding:0;list-style:none}.shiki{position:relative;background:#f6f8fa;padding:1rem;overflow:auto}.shiki code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.9rem;line-height:1.6}.shiki span{color:var(--shiki-light)}html.dark .shiki{background:#0f172a}html.dark .shiki span{color:var(--shiki-dark)}pre[data-lang]::after{content:attr(data-lang);position:absolute;top:.55rem;right:.75rem;font-size:.72rem;color:#64748b;text-transform:uppercase;letter-spacing:.05em;pointer-events:none}pre.with-line-numbers code{counter-reset:line}pre.with-line-numbers .line::before{counter-increment:line;content:counter(line);display:inline-block;width:1.7rem;margin-right:1rem;color:#94a3b8;text-align:right;user-select:none}.mermaid-island{margin:1.2rem 0}.mermaid-source{background:#f8fafc;border:1px solid #e2e8f0;color:#334155;overflow:auto;white-space:pre-wrap}'+custom+'</style>';const itemLink=item=>{const url=esc(item.url);const external=/^https?:\/\//i.test(String(item.url));return '<a href="'+url+'"'+(external?' target="_blank" rel="noopener noreferrer"':'')+'>'+esc(item.title)+'</a>'};const pagination=props.pagination?.total>1?'<nav aria-label="Pagination"><ol class="pagination">'+props.pagination.links.map(link=>'<li>'+(link.current?'<span aria-current="page">'+esc(link.page)+'</span>':'<a href="'+esc(link.url)+'">'+esc(link.page)+'</a>')+'</li>').join('')+'</ol></nav>':'';const locales=(props.alternates||[]).filter(item=>item?.hrefLang&&item.hrefLang!=='x-default').map(item=>'<a href="'+esc(item.href)+'" hreflang="'+esc(item.hrefLang)+'"'+(item.hrefLang===locale?' aria-current="page"':'')+'>'+esc(item.hrefLang)+'</a>').join('');const localeSwitcher=locales?'<nav class="locale-switcher" aria-label="Languages">'+locales+'</nav>':'';let content;if(props.kind==='home'||props.kind==='collection'){content='<main>'+localeSwitcher+'<h1>'+esc(title)+'</h1><section>'+((props.items||[]).map(item=>'<article><h2>'+itemLink(item)+'</h2><p>'+esc(item.description||'')+'</p></article>').join(''))+'</section>'+pagination+'</main>'}else if(props.kind==='search'){const indexURL=esc(props.searchIndexURL||'/search-index.json');content='<main>'+localeSwitcher+'<h1>'+esc(title)+'</h1><label for="site-search">'+esc(title)+'</label><input class="search-input" id="site-search" type="search" autocomplete="off"><ul class="search-results" id="search-results"></ul><script>(function(){const input=document.getElementById("site-search"),results=document.getElementById("search-results");let index;input.addEventListener("input",async function(){const q=input.value.trim().toLowerCase();if(!q){results.replaceChildren();return}if(!index)index=await fetch("'+indexURL+'").then(r=>r.ok?r.json():[]);const found=index.filter(x=>(x.title+" "+x.description).toLowerCase().includes(q)).slice(0,20);results.replaceChildren(...found.map(x=>{const li=document.createElement("li"),a=document.createElement("a"),p=document.createElement("p");a.href=x.url;a.textContent=x.title;p.textContent=x.description||"";li.append(a,p);return li}))})})()</script></main>'}else if(props.kind==='not_found'){content='<main><h1>'+esc(title)+'</h1><p>'+esc(props.message||'Page not found.')+'</p><p><a href="/'+esc(props.prefix||String(locale).toLowerCase())+'/">'+esc(props.homeLabel||'Back to home')+'</a></p></main>'}else{content='<article>'+localeSwitcher+'<h1>'+esc(title)+'</h1>'+body+'</article>'};const themed=await themeMarkup(props,content);if(themed)content=themed;return '<!doctype html><html lang="'+esc(locale)+'"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">'+description+seoHead(props)+structuredData(props)+'<title>'+esc(title)+'</title>'+style+'</head><body>'+content+'</body></html>'}
const server=http.createServer((req,res)=>{
  if(req.method==='GET'&&req.url==='/health'){
    res.writeHead(200,{'content-type':'application/json'});res.end('{"ok":true}');return
  }
  if(req.method==='POST'&&req.url==='/reload'){
    res.writeHead(200,{'content-type':'application/json'});res.end('{"ok":true}');return
  }
  if(req.method==='POST'&&(req.url==='/render'||req.url==='/markdown')){
    let data=''
    req.on('data',chunk=>{data+=chunk;if(data.length>6*1024*1024){req.destroy()}})
    req.on('end',async()=>{try{
      const props=JSON.parse(data)
      const source=props.body??props.markdownText??(typeof props.markdown==='string'?props.markdown:'')
      const options=props.markdownOptions||(typeof props.markdown==='object'?props.markdown:{})
      const html=req.url==='/markdown'?await markdown(source,options):await render(props)
      res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify({html}))
    }catch(err){res.writeHead(400,{'content-type':'application/json'});res.end(JSON.stringify({error:String(err)}))}})
    return
  }
  res.writeHead(404);res.end()
})
server.listen(socket,()=>{try{fs.chmodSync(socket,0o600)}catch{}})
for(const signal of ['SIGINT','SIGTERM'])process.on(signal,()=>server.close(()=>process.exit(0)))
