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

const socket=process.env.BLOG_RENDER_SOCKET||'/tmp/blog-render.sock'
try{fs.unlinkSync(socket)}catch(e){if(e.code!=='ENOENT')throw e}
function esc(v=''){return String(v).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;').replaceAll("'",'&#39;')}
const noop = () => {}
async function markdown(source='', options={}) {
  const file = await unified()
    .use(remarkParse)
    .use(remarkGfm)
    .use(options.katex ? remarkMath : noop)
    .use(remarkRehype, { allowDangerousHtml: true })
    .use(rehypeRaw)
    .use(rehypeSlug)
    .use(options.headingAnchors ? rehypeAutolinkHeadings : noop, {
      behavior: 'append',
      properties: { className: ['heading-anchor'], ariaHidden: 'true' },
    })
    .use(options.katex ? rehypeKatex : noop, { throwOnError: false, strict: false })
    .use(options.externalLinksNewTab ? rehypeExternalLinks : noop, {
      target: '_blank', rel: ['noopener', 'noreferrer'],
    })
    .use(rehypeStringify)
    .process(String(source))
  return String(file)
}
function seoHead(props){let out='';if(props.canonical)out+='<link rel="canonical" href="'+esc(props.canonical)+'">';for(const alternate of props.alternates||[]){if(alternate?.hrefLang&&alternate?.href)out+='<link rel="alternate" hreflang="'+esc(alternate.hrefLang)+'" href="'+esc(alternate.href)+'">'}return out}
function structuredData(props){if(!props.canonical||!['post','page'].includes(props.kind))return '';const data={'@context':'https://schema.org','@type':props.kind==='post'?'BlogPosting':'WebPage',headline:String(props.title||''),description:String(props.description||''),url:String(props.canonical)};if(props.author)data.author={'@type':'Person',name:String(props.author)};if(props.publishedAt)data.datePublished=String(props.publishedAt);if(props.modifiedAt)data.dateModified=String(props.modifiedAt);return '<script type="application/ld+json">'+JSON.stringify(data).replaceAll('<','\\u003c')+'</script>'}
const themeCache=new Map()
async function themeMarkup(props,html){const entry=props.themeDir&&path.join(props.themeDir,'dist','ssr','entry.js');if(!entry||!fs.existsSync(entry))return '';try{const stamp=fs.statSync(entry).mtimeMs;let mod=themeCache.get(entry);if(!mod||mod.stamp!==stamp){mod={stamp,value:await import(pathToFileURL(entry).href+'?v='+stamp)};themeCache.set(entry,mod)}const template=mod.value.templates?.[props.kind];if(template)return renderToStaticMarkup(template({...props,html}));if(typeof mod.value.render==='function')return await mod.value.render({...props,html});return ''}catch(err){console.error('theme SSR fallback:',err);return ''}}
async function render(props){const title=props.title||'Mutiblog';const body=await markdown(props.body, props.markdown||{});const locale=props.locale||'en';const description=props.description?`<meta name="description" content="${esc(props.description)}">`:'';const theme=props.theme||{};const accent=/^#[0-9a-f]{3}(?:[0-9a-f]{3})?$/i.test(theme.accentColor||'')?theme.accentColor:'#2563eb';const custom=String(theme.customCSS||'').replace(/<\/?style/gi,'').replace(/<script/gi,'').replace(/javascript\s*:/gi,'').replace(/expression\s*\(/gi,'');const style='<style>:root{--accent:'+esc(accent)+'}body{max-width:760px;margin:3rem auto;padding:0 1rem;font-family:system-ui,sans-serif;line-height:1.7}pre{padding:1rem;background:#0f172a;color:#f8fafc;overflow:auto}code{font-family:ui-monospace,monospace}a{color:var(--accent)}.heading-anchor{margin-left:.4rem;text-decoration:none;opacity:.55}.heading-anchor:hover{opacity:1}.locale-switcher,.pagination{display:flex;gap:.4rem;list-style:none;padding:1rem 0}.locale-switcher a,.pagination a,.pagination span{padding:.2rem .55rem;border:1px solid #cbd5e1;border-radius:.3rem}.pagination [aria-current=page],.locale-switcher [aria-current=page]{font-weight:700;background:#e2e8f0}.search-input{width:100%;padding:.65rem;font:inherit}.search-results{padding:0;list-style:none}'+custom+'</style>';const itemLink=item=>{const url=esc(item.url);const external=/^https?:\/\//i.test(String(item.url));return '<a href="'+url+'"'+(external?' target="_blank" rel="noopener noreferrer"':'')+'>'+esc(item.title)+'</a>'};const pagination=props.pagination?.total>1?'<nav aria-label="Pagination"><ol class="pagination">'+props.pagination.links.map(link=>'<li>'+(link.current?'<span aria-current="page">'+esc(link.page)+'</span>':'<a href="'+esc(link.url)+'">'+esc(link.page)+'</a>')+'</li>').join('')+'</ol></nav>':'';const locales=(props.alternates||[]).filter(item=>item?.hrefLang&&item.hrefLang!=='x-default').map(item=>'<a href="'+esc(item.href)+'" hreflang="'+esc(item.hrefLang)+'"'+(item.hrefLang===locale?' aria-current="page"':'')+'>'+esc(item.hrefLang)+'</a>').join('');const localeSwitcher=locales?'<nav class="locale-switcher" aria-label="Languages">'+locales+'</nav>':'';let content;if(props.kind==='home'||props.kind==='collection'){content='<main>'+localeSwitcher+'<h1>'+esc(title)+'</h1><section>'+((props.items||[]).map(item=>'<article><h2>'+itemLink(item)+'</h2><p>'+esc(item.description||'')+'</p></article>').join(''))+'</section>'+pagination+'</main>'}else if(props.kind==='search'){const indexURL=esc(props.searchIndexURL||'/search-index.json');content='<main>'+localeSwitcher+'<h1>'+esc(title)+'</h1><label for="site-search">'+esc(title)+'</label><input class="search-input" id="site-search" type="search" autocomplete="off"><ul class="search-results" id="search-results"></ul><script>(function(){const input=document.getElementById("site-search"),results=document.getElementById("search-results");let index;input.addEventListener("input",async function(){const q=input.value.trim().toLowerCase();if(!q){results.replaceChildren();return}if(!index)index=await fetch("'+indexURL+'").then(r=>r.ok?r.json():[]);const found=index.filter(x=>(x.title+" "+x.description).toLowerCase().includes(q)).slice(0,20);results.replaceChildren(...found.map(x=>{const li=document.createElement("li"),a=document.createElement("a"),p=document.createElement("p");a.href=x.url;a.textContent=x.title;p.textContent=x.description||"";li.append(a,p);return li}))})})()</script></main>'}else if(props.kind==='not_found'){content='<main><h1>'+esc(title)+'</h1><p>'+esc(props.message||'Page not found.')+'</p><p><a href="/'+esc(props.prefix||String(locale).toLowerCase())+'/">'+esc(props.homeLabel||'Back to home')+'</a></p></main>'}else{content='<article>'+localeSwitcher+'<h1>'+esc(title)+'</h1>'+body+'</article>'};const themed=await themeMarkup(props,content);if(themed)content=themed;return '<!doctype html><html lang="'+esc(locale)+'"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">'+description+seoHead(props)+structuredData(props)+'<title>'+esc(title)+'</title>'+style+'</head><body>'+content+'</body></html>'}
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
