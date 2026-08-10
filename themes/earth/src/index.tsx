import React from "react";
import type { ThemeContext, ThemePost } from "./types.js";

const e = (value: string) => encodeURIComponent(value);

function Shell({ context, children }: { context: ThemeContext; children: React.ReactNode }) {
  const { site, currentPath, strings } = context;
  return (
    <html lang={site.locale}>
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <meta name="description" content={site.description ?? site.subtitle ?? site.title} />
        <title>{site.title}</title>
        <link rel="stylesheet" href="/assets/earth.css" />
      </head>
      <body>
        <header className="site-header">
          <div className="header-inner">
            <a className="site-brand" href={`/${site.locale}/`}><span className="brand-symbol">M</span><strong>{site.title}</strong></a>
            <nav className="main-nav">
              <a href={`/${site.locale}/`}>{strings.home}</a>
              <a href={`/${site.locale}/archives/`}>{strings.archives}</a>
              <a href={`/${site.locale}/links/`}>{strings.links}</a>
            </nav>
            <div className="header-actions">
              <a className="icon-action" href={`/${site.locale}/search/`} aria-label={strings.search}>⌕</a>
              <details className="locale-picker">
                <summary>{site.locale}</summary>
                <div>{site.locales.map((locale) => <a key={locale.code} href={`/${locale.code}${currentPath}`}>{locale.label}</a>)}</div>
              </details>
              <button className="icon-action" type="button" aria-label={strings.colorScheme} data-color-scheme>◐</button>
            </div>
          </div>
        </header>
        {children}
        <footer className="site-footer"><div><strong>{site.title}</strong><span>{site.description}</span></div><p>Powered by MutiBlog</p></footer>
        <script dangerouslySetInnerHTML={{ __html: colorSchemeScript }} />
      </body>
    </html>
  );
}

function PostCard({ locale, post }: { locale: string; post: ThemePost }) {
  return (
    <article className="post-card">
      {post.cover ? <a className="post-cover" href={`/${locale}/posts/${e(post.id)}/`}><img src={post.cover} alt="" /></a> : null}
      <div className="post-card-body">
        <div className="post-taxonomy">{post.categories?.map((category) => <a key={category.id} href={`/${locale}/categories/${e(category.id)}/`}>{category.name}</a>)}</div>
        <h2><a href={`/${locale}/posts/${e(post.id)}/`}>{post.title}</a></h2>
        {post.summary ? <p>{post.summary}</p> : null}
        <div className="post-meta"><time>{post.publishedAt ? new Date(post.publishedAt).toLocaleDateString(locale) : ""}</time><span>→</span></div>
      </div>
    </article>
  );
}

export function renderIndex(context: ThemeContext) {
  return (
    <Shell context={context}>
      <section className="hero"><div><span className="hero-kicker">MutiBlog</span><h1>{context.site.title}</h1><p>{context.site.subtitle ?? context.site.description}</p></div></section>
      <main className="earth-layout">
        <section className="post-grid">{context.posts.map((post) => <PostCard key={post.id} locale={context.site.locale} post={post} />)}</section>
        <aside className="sidebar-card"><h3>{context.strings.about}</h3><p>{context.site.description}</p><hr /><h3>{context.strings.languages}</h3>{context.site.locales.map((locale) => <a key={locale.code} href={`/${locale.code}/`}>{locale.label}</a>)}</aside>
      </main>
    </Shell>
  );
}

export function renderPost(context: ThemeContext) {
  const post = context.post!;
  return (
    <Shell context={context}>
      <main className="article-shell">
        <article className="article-card">
          <header><div className="post-taxonomy">{post.categories?.map((category) => <span key={category.id}>{category.name}</span>)}</div><h1>{post.title}</h1><p>{post.summary}</p><time>{post.publishedAt ? new Date(post.publishedAt).toLocaleDateString(context.site.locale) : ""}</time></header>
          {post.cover ? <img className="article-cover" src={post.cover} alt="" /> : null}
          <div className="markdown-body" dangerouslySetInnerHTML={{ __html: post.html }} />
          <section className="comments" data-comments data-subject-kind="Post" data-subject-id={post.id}><h2>{context.strings.comments}</h2><div className="comments-loading">{context.strings.loadingComments}</div></section>
        </article>
      </main>
      <script dangerouslySetInnerHTML={{ __html: commentsScript(context.strings.commentsUnavailable) }} />
    </Shell>
  );
}

const colorSchemeScript = `(()=>{const b=document.querySelector('[data-color-scheme]');const set=v=>{document.documentElement.dataset.theme=v;localStorage.setItem('earth-theme',v)};set(localStorage.getItem('earth-theme')||((matchMedia('(prefers-color-scheme: dark)').matches)?'dark':'light'));b?.addEventListener('click',()=>set(document.documentElement.dataset.theme==='dark'?'light':'dark'))})()`;
const commentsScript = (unavailable: string) => `(()=>{const root=document.querySelector('[data-comments]');if(!root)return;const p=new URLSearchParams({kind:root.dataset.subjectKind,id:root.dataset.subjectId,page:'1'});const c=new AbortController();const t=setTimeout(()=>c.abort(),5000);fetch('/api/v1/public/comments?'+p,{signal:c.signal}).then(r=>{if(!r.ok)throw Error();return r.json()}).then(d=>{root.querySelector('.comments-loading').textContent=d.items?.length?d.items.map(x=>x.content).join(' · '):'—'}).catch(()=>{root.querySelector('.comments-loading').textContent=${JSON.stringify(unavailable)}}).finally(()=>clearTimeout(t))})()`;

export const earthCSS = `:root{--accent:#4ccba0;--ink:#182037;--muted:#6e7688;--canvas:#f4f6f8;--card:#fff;--border:#e7e9ee}html[data-theme=dark]{--ink:#e8ecf4;--muted:#a7afc1;--canvas:#0f172a;--card:#172036;--border:#26324a}*{box-sizing:border-box}body{margin:0;background:var(--canvas);color:var(--ink);font-family:Inter,ui-sans-serif,system-ui,-apple-system,sans-serif;line-height:1.65}a{color:inherit;text-decoration:none}.site-header{position:sticky;top:0;z-index:20;height:4rem;border-bottom:1px solid var(--border);background:color-mix(in srgb,var(--card) 90%,transparent);backdrop-filter:blur(12px)}.header-inner{display:flex;max-width:75rem;height:100%;margin:auto;align-items:center;gap:2rem;padding:0 1.2rem}.site-brand{display:flex;align-items:center;gap:.6rem}.brand-symbol{display:grid;width:2rem;height:2rem;place-items:center;border-radius:.55rem;background:#0e1731;color:var(--accent);font-weight:800}.main-nav{display:flex;gap:1.4rem;font-size:.9rem}.main-nav a:hover{color:var(--accent)}.header-actions{display:flex;margin-left:auto;align-items:center;gap:.6rem}.icon-action,.locale-picker summary{border:0;background:transparent;color:var(--ink);cursor:pointer;font-size:.82rem}.locale-picker{position:relative}.locale-picker div{position:absolute;right:0;display:flex;min-width:8rem;flex-direction:column;border:1px solid var(--border);border-radius:.6rem;background:var(--card);box-shadow:0 12px 30px #0002;padding:.45rem}.locale-picker div a{padding:.35rem .5rem}.hero{display:grid;min-height:22rem;place-items:center;background:radial-gradient(circle at 25% 25%,#4ccba044,transparent 35%),linear-gradient(145deg,#0e1731,#202d52);padding:3rem 1.2rem;text-align:center;color:#fff}.hero-kicker{color:var(--accent);font-weight:750;letter-spacing:.16em;text-transform:uppercase}.hero h1{margin:.4rem 0;font-size:clamp(2.4rem,6vw,4.5rem);line-height:1.05}.hero p{max-width:40rem;margin:.8rem auto;color:#d8deeb}.earth-layout{display:grid;max-width:75rem;margin:2rem auto;grid-template-columns:minmax(0,1fr) 18rem;gap:1.5rem;padding:0 1.2rem}.post-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:1.2rem}.post-card,.sidebar-card,.article-card{overflow:hidden;border:1px solid var(--border);border-radius:1rem;background:var(--card);box-shadow:0 5px 18px #0e173108}.post-card{transition:.2s ease}.post-card:hover{transform:translateY(-3px);box-shadow:0 12px 30px #0e173117}.post-cover{display:block;aspect-ratio:16/9;overflow:hidden}.post-cover img{width:100%;height:100%;object-fit:cover;transition:.3s ease}.post-card:hover img{transform:scale(1.03)}.post-card-body{padding:1.2rem}.post-taxonomy{display:flex;gap:.5rem;color:var(--accent);font-size:.72rem;font-weight:650}.post-card h2{margin:.5rem 0;font-size:1.3rem;line-height:1.35}.post-card p{display:-webkit-box;overflow:hidden;margin:.5rem 0;color:var(--muted);font-size:.9rem;-webkit-box-orient:vertical;-webkit-line-clamp:3}.post-meta{display:flex;justify-content:space-between;margin-top:1rem;color:var(--muted);font-size:.75rem}.sidebar-card{align-self:start;padding:1.2rem}.sidebar-card h3{margin:.3rem 0}.sidebar-card p{color:var(--muted);font-size:.85rem}.sidebar-card>a{display:block;padding:.25rem 0;color:var(--muted)}.article-shell{max-width:62rem;margin:2rem auto;padding:0 1.2rem}.article-card{padding:clamp(1.2rem,5vw,3.5rem)}.article-card>header{text-align:center}.article-card h1{margin:.7rem 0;font-size:clamp(2rem,5vw,3.2rem);line-height:1.15}.article-card header p,.article-card time{color:var(--muted)}.article-cover{width:100%;margin:2rem 0;border-radius:.8rem}.markdown-body{font-family:ui-serif,Georgia,serif;font-size:1.03rem}.markdown-body img{max-width:100%;border-radius:.6rem}.markdown-body pre{overflow:auto;border-radius:.6rem;background:#0e1731;color:#e9eef8;padding:1rem}.markdown-body blockquote{margin-left:0;border-left:4px solid var(--accent);padding-left:1rem;color:var(--muted)}.comments{margin-top:3rem;border-top:1px solid var(--border);padding-top:1.5rem}.comments-loading{color:var(--muted)}.site-footer{display:flex;max-width:75rem;margin:3rem auto 0;justify-content:space-between;border-top:1px solid var(--border);padding:2rem 1.2rem;color:var(--muted);font-size:.8rem}.site-footer div{display:flex;flex-direction:column}@media(max-width:850px){.main-nav{display:none}.earth-layout{grid-template-columns:1fr}.sidebar-card{display:none}.post-grid{grid-template-columns:1fr}}`;

export type { ThemeContext, ThemePost, ThemeSite } from "./types.js";
