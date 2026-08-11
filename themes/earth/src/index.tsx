import React from "react";
import type { ThemeContext, ThemeLinkGroup, ThemeMenuItem, ThemePost } from "./types.js";

const e = (value: string) => encodeURIComponent(value);

function Shell({ context, children }: { context: ThemeContext; children: React.ReactNode }) {
  const { site, currentPath, strings } = context;
	const title = context.pageTitle ? `${context.pageTitle} – ${site.title}` : site.title;
	const description = context.pageDescription ?? site.description ?? site.subtitle ?? site.title;
	const accentColor = themeSetting(context, "style.accentColor", "#4ccba0");
	const defaultColorScheme = themeSetting(context, "style.defaultColorScheme", "system");
	const showPoweredBy = themeSetting(context, "footer.showPoweredBy", true);
	const copyright = themeSetting(context, "footer.copyright", "");
  return (
	<html lang={site.locale} style={{ "--accent": accentColor } as React.CSSProperties}>
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <meta name="description" content={description} />
        <title>{title}</title>
        {context.canonicalUrl ? <link rel="canonical" href={context.canonicalUrl} /> : null}
        {context.alternates?.map((alternate) => <link key={alternate.locale} rel="alternate" hrefLang={alternate.locale} href={alternate.href} />)}
        {context.alternates?.length ? <link rel="alternate" hrefLang="x-default" href={(context.alternates.find(({ locale }) => locale === site.sourceLocale) ?? context.alternates[0]).href} /> : null}
        <meta property="og:type" content={context.post ? "article" : "website"} />
        <meta property="og:title" content={title} />
        <meta property="og:description" content={description} />
        {context.canonicalUrl ? <meta property="og:url" content={context.canonicalUrl} /> : null}
        {context.post?.cover ? <meta property="og:image" content={absoluteUrl(site.baseUrl, context.post.cover)} /> : null}
        <link rel="stylesheet" href="/assets/theme.css" />
      </head>
      <body>
        <header className="site-header">
          <div className="header-inner">
            <a className="site-brand" href={`/${site.locale}/`}><span className="brand-symbol">M</span><strong>{site.title}</strong></a>
            <nav className="main-nav">
              {context.navigation?.length ? context.navigation.map((item) => <NavigationItem key={item.id} item={item} />) : <><a href={`/${site.locale}/`}>{strings.home}</a><a href={`/${site.locale}/archives/`}>{strings.archives}</a><a href={`/${site.locale}/links/`}>{strings.links}</a></>}
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
		<footer className="site-footer"><div><strong>{site.title}</strong><span>{copyright || site.description}</span></div>{showPoweredBy ? <p>{strings.poweredBy}</p> : null}</footer>
		<script dangerouslySetInnerHTML={{ __html: colorSchemeScript(defaultColorScheme) }} />
      </body>
    </html>
  );
}

function NavigationItem({ item }: { item: ThemeMenuItem }) {
  const link = <a href={item.href} target={item.openInNew ? "_blank" : undefined} rel={item.openInNew ? "noopener noreferrer" : undefined}>{item.label}</a>;
  return item.children?.length ? <details className="menu-children"><summary>{item.label}</summary><div>{item.children.map((child) => <NavigationItem key={child.id} item={child} />)}</div></details> : link;
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
	const showHero = themeSetting(context, "layout.showHero", true);
	const showSidebar = themeSetting(context, "layout.showSidebar", true);
	const postListLayout = themeSetting<string>(context, "layout.postListLayout", "grid-2");
  return (
    <Shell context={context}>
	  {showHero ? <section className="hero"><div><span className="hero-kicker">MutiBlog</span><h1>{context.site.title}</h1><p>{context.site.subtitle ?? context.site.description}</p></div></section> : null}
      <main className="earth-layout">
		<section className="post-grid" style={postListLayout === "single" ? { gridTemplateColumns: "1fr" } : undefined}>{context.posts.map((post) => <PostCard key={post.id} locale={context.site.locale} post={post} />)}</section>
		{showSidebar ? <aside className="sidebar-card"><h3>{context.strings.about}</h3><p>{context.site.description}</p><hr /><h3>{context.strings.languages}</h3>{context.site.locales.map((locale) => <a key={locale.code} href={`/${locale.code}/`}>{locale.label}</a>)}</aside> : null}
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
          <CommentSection context={context} kind="Post" id={post.id} open={post.commentPolicy !== "closed"} />
        </article>
      </main>
      <script dangerouslySetInnerHTML={{ __html: commentsScript(context.strings.commentsUnavailable) }} />
    </Shell>
  );
}

export function renderPage(context: ThemeContext) {
  const page = context.post!;
  return (
    <Shell context={context}>
      <main className="article-shell">
        <article className="article-card">
          <header><h1>{page.title}</h1>{page.summary ? <p>{page.summary}</p> : null}</header>
          {page.cover ? <img className="article-cover" src={page.cover} alt="" /> : null}
          <div className="markdown-body" dangerouslySetInnerHTML={{ __html: page.html }} />
          <CommentSection context={context} kind="Page" id={page.id} open={page.commentPolicy !== "closed"} />
        </article>
      </main>
      <script dangerouslySetInnerHTML={{ __html: commentsScript(context.strings.commentsUnavailable) }} />
    </Shell>
  );
}

export function renderTaxonomy(context: ThemeContext, title: string, description?: string) {
  context = { ...context, pageTitle: context.pageTitle || title, pageDescription: context.pageDescription ?? description };
  return (
    <Shell context={context}>
      <section className="hero taxonomy-hero"><div><h1>{title}</h1>{description ? <p>{description}</p> : null}</div></section>
      <main className="earth-layout"><section className="post-grid">{context.posts.map((post) => <PostCard key={post.id} locale={context.site.locale} post={post} />)}</section></main>
    </Shell>
  );
}

export function renderLinks(context: ThemeContext, groups: ThemeLinkGroup[]) {
  context = { ...context, pageTitle: context.strings.links };
  return <Shell context={context}><section className="hero taxonomy-hero"><div><h1>{context.strings.links}</h1></div></section><main className="article-shell link-groups">{groups.map((group) => <section key={group.id} className="article-card"><h2>{group.name}</h2>{group.description ? <p>{group.description}</p> : null}<div className="post-grid">{group.links.map((link) => <a key={link.id} className="post-card link-card" href={link.url} rel="noopener noreferrer"><div className="post-card-body">{link.logo ? <img src={link.logo} alt="" /> : null}<strong>{link.name}</strong>{link.description ? <p>{link.description}</p> : null}</div></a>)}</div></section>)}</main></Shell>;
}

export function renderArchive(context: ThemeContext) {
  context = { ...context, pageTitle: context.strings.archives };
  return <Shell context={context}><section className="hero taxonomy-hero"><div><h1>{context.strings.archives}</h1></div></section><main className="article-shell"><section className="article-card archive-list">{context.posts.map((post) => <article key={post.id}><time>{post.publishedAt ? new Date(post.publishedAt).toLocaleDateString(context.site.locale) : ""}</time><h2><a href={`/${context.site.locale}/posts/${e(post.id)}/`}>{post.title}</a></h2></article>)}</section></main></Shell>;
}

export function renderSearch(context: ThemeContext) {
  context = { ...context, pageTitle: context.strings.search };
  return <Shell context={context}><section className="hero taxonomy-hero"><div><h1>{context.strings.search}</h1></div></section><main className="article-shell"><section className="article-card"><input type="search" data-search-input placeholder={context.strings.search} /><div data-search-results /></section></main><script dangerouslySetInnerHTML={{ __html: searchScript }} /></Shell>;
}

export function renderNotFound(context: ThemeContext) {
	const message = context.strings.notFound || "Not found";
  context = { ...context, pageTitle: message };
  return <Shell context={context}><main className="article-shell"><section className="article-card not-found"><strong>404</strong><h1>{message}</h1><a href={`/${context.site.locale}/`}>{context.strings.home || "Home"}</a></section></main></Shell>;
}

function CommentSection({ context, kind, id, open }: { context: ThemeContext; kind: "Post" | "Page"; id: string; open: boolean }) {
  const s = context.strings;
  return <section className="comments" data-comments data-subject-kind={kind} data-subject-id={id} data-locale={context.site.locale} data-unavailable={s.commentsUnavailable} data-empty={s.commentEmpty} data-pending={s.commentPending}>
    <h2>{s.comments}</h2><div className="comments-loading">{s.loadingComments}</div><div className="comment-list" />
    {open ? <form className="comment-form"><input name="name" required maxLength={80} placeholder={s.commentName} /><input name="email" type="email" placeholder={s.commentEmail} /><input name="website" type="url" placeholder={s.commentWebsite} /><textarea name="content" required rows={5} placeholder={s.commentContent} /><button type="submit">{s.commentSubmit}</button><output /></form> : <p className="comments-closed">{s.commentsClosed}</p>}
  </section>;
}

function themeSetting<T>(context: ThemeContext, path: string, fallback: T): T {
	let value: unknown = context.settings;
	for (const segment of path.split(".")) {
		if (!value || typeof value !== "object") return fallback;
		value = (value as Record<string, unknown>)[segment];
	}
	return (value === undefined ? fallback : value) as T;
}

function absoluteUrl(baseUrl: string | undefined, value: string) {
	if (!baseUrl || /^(?:https?:)?\/\//.test(value)) return value;
	return `${baseUrl}${value.startsWith("/") ? "" : "/"}${value}`;
}

const colorSchemeScript = (configured: string) => `(()=>{const b=document.querySelector('[data-color-scheme]');const set=v=>{document.documentElement.dataset.theme=v;localStorage.setItem('earth-theme',v)};const d=${JSON.stringify(configured)};set(localStorage.getItem('earth-theme')||(d==='system'?((matchMedia('(prefers-color-scheme: dark)').matches)?'dark':'light'):d));b?.addEventListener('click',()=>set(document.documentElement.dataset.theme==='dark'?'light':'dark'))})()`;
const searchScript = `(()=>{const i=document.querySelector('[data-search-input]'),r=document.querySelector('[data-search-results]');if(!i||!r)return;let d=[];fetch('./index.json').then(x=>x.json()).then(x=>d=x).catch(()=>{});i.addEventListener('input',()=>{const q=i.value.trim().toLocaleLowerCase();r.replaceChildren();if(!q)return;d.filter(x=>(x.title+' '+(x.summary||'')).toLocaleLowerCase().includes(q)).slice(0,30).forEach(x=>{const a=document.createElement('a');a.href=x.url;a.textContent=x.title;r.append(a)})})})()`;
const commentsScript = (_unavailable: string) => `(()=>{const r=document.querySelector('[data-comments]');if(!r)return;const l=r.querySelector('.comment-list'),m=r.querySelector('.comments-loading'),f=r.querySelector('form'),o=f?.querySelector('output');const load=()=>{const p=new URLSearchParams({kind:r.dataset.subjectKind,id:r.dataset.subjectId,page:'1'}),c=new AbortController(),t=setTimeout(()=>c.abort(),5000);fetch('/api/v1/public/comments?'+p,{signal:c.signal}).then(x=>{if(!x.ok)throw Error();return x.json()}).then(d=>{l.replaceChildren();m.textContent=d.items?.length?'':r.dataset.empty;(d.items||[]).forEach(x=>{const a=document.createElement('article'),h=document.createElement('strong'),b=document.createElement('p'),z=document.createElement('time');h.textContent=x.author.name;b.textContent=x.content;z.textContent=new Date(x.createdAt).toLocaleString(r.dataset.locale);a.append(h,b,z);l.append(a)})}).catch(()=>{m.textContent=r.dataset.unavailable}).finally(()=>clearTimeout(t))};if(f&&o)f.addEventListener('submit',e=>{e.preventDefault();const d=Object.fromEntries(new FormData(f)),c=new AbortController(),t=setTimeout(()=>c.abort(),5000);o.textContent='…';fetch('/api/v1/public/comments',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({...d,kind:r.dataset.subjectKind,id:r.dataset.subjectId,locale:r.dataset.locale}),signal:c.signal}).then(async x=>{if(!x.ok)throw Error();const j=await x.json();o.textContent=j.status==='approved'?'':r.dataset.pending;f.reset();load()}).catch(()=>{o.textContent=r.dataset.unavailable}).finally(()=>clearTimeout(t))});load()})()`;

export const earthCSS = `:root{--accent:#4ccba0;--ink:#182037;--muted:#6e7688;--canvas:#f4f6f8;--card:#fff;--border:#e7e9ee}html[data-theme=dark]{--ink:#e8ecf4;--muted:#a7afc1;--canvas:#0f172a;--card:#172036;--border:#26324a}*{box-sizing:border-box}body{margin:0;background:var(--canvas);color:var(--ink);font-family:Inter,ui-sans-serif,system-ui,-apple-system,sans-serif;line-height:1.65}a{color:inherit;text-decoration:none}.site-header{position:sticky;top:0;z-index:20;height:4rem;border-bottom:1px solid var(--border);background:color-mix(in srgb,var(--card) 90%,transparent);backdrop-filter:blur(12px)}.header-inner{display:flex;max-width:75rem;height:100%;margin:auto;align-items:center;gap:2rem;padding:0 1.2rem}.site-brand{display:flex;align-items:center;gap:.6rem}.brand-symbol{display:grid;width:2rem;height:2rem;place-items:center;border-radius:.55rem;background:#0e1731;color:var(--accent);font-weight:800}.main-nav{display:flex;gap:1.4rem;font-size:.9rem}.main-nav a:hover{color:var(--accent)}.header-actions{display:flex;margin-left:auto;align-items:center;gap:.6rem}.icon-action,.locale-picker summary{border:0;background:transparent;color:var(--ink);cursor:pointer;font-size:.82rem}.locale-picker{position:relative}.locale-picker div{position:absolute;right:0;display:flex;min-width:8rem;flex-direction:column;border:1px solid var(--border);border-radius:.6rem;background:var(--card);box-shadow:0 12px 30px #0002;padding:.45rem}.locale-picker div a{padding:.35rem .5rem}.hero{display:grid;min-height:22rem;place-items:center;background:radial-gradient(circle at 25% 25%,#4ccba044,transparent 35%),linear-gradient(145deg,#0e1731,#202d52);padding:3rem 1.2rem;text-align:center;color:#fff}.hero-kicker{color:var(--accent);font-weight:750;letter-spacing:.16em;text-transform:uppercase}.hero h1{margin:.4rem 0;font-size:clamp(2.4rem,6vw,4.5rem);line-height:1.05}.hero p{max-width:40rem;margin:.8rem auto;color:#d8deeb}.earth-layout{display:grid;max-width:75rem;margin:2rem auto;grid-template-columns:minmax(0,1fr) 18rem;gap:1.5rem;padding:0 1.2rem}.post-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:1.2rem}.post-card,.sidebar-card,.article-card{overflow:hidden;border:1px solid var(--border);border-radius:1rem;background:var(--card);box-shadow:0 5px 18px #0e173108}.post-card{transition:.2s ease}.post-card:hover{transform:translateY(-3px);box-shadow:0 12px 30px #0e173117}.post-cover{display:block;aspect-ratio:16/9;overflow:hidden}.post-cover img{width:100%;height:100%;object-fit:cover;transition:.3s ease}.post-card:hover img{transform:scale(1.03)}.post-card-body{padding:1.2rem}.post-taxonomy{display:flex;gap:.5rem;color:var(--accent);font-size:.72rem;font-weight:650}.post-card h2{margin:.5rem 0;font-size:1.3rem;line-height:1.35}.post-card p{display:-webkit-box;overflow:hidden;margin:.5rem 0;color:var(--muted);font-size:.9rem;-webkit-box-orient:vertical;-webkit-line-clamp:3}.post-meta{display:flex;justify-content:space-between;margin-top:1rem;color:var(--muted);font-size:.75rem}.sidebar-card{align-self:start;padding:1.2rem}.sidebar-card h3{margin:.3rem 0}.sidebar-card p{color:var(--muted);font-size:.85rem}.sidebar-card>a{display:block;padding:.25rem 0;color:var(--muted)}.article-shell{max-width:62rem;margin:2rem auto;padding:0 1.2rem}.article-card{padding:clamp(1.2rem,5vw,3.5rem)}.article-card>header{text-align:center}.article-card h1{margin:.7rem 0;font-size:clamp(2rem,5vw,3.2rem);line-height:1.15}.article-card header p,.article-card time{color:var(--muted)}.article-cover{width:100%;margin:2rem 0;border-radius:.8rem}.markdown-body{font-family:ui-serif,Georgia,serif;font-size:1.03rem}.markdown-body img{max-width:100%;border-radius:.6rem}.markdown-body pre{overflow:auto;border-radius:.6rem;background:#0e1731;color:#e9eef8;padding:1rem}.markdown-body blockquote{margin-left:0;border-left:4px solid var(--accent);padding-left:1rem;color:var(--muted)}.comments{margin-top:3rem;border-top:1px solid var(--border);padding-top:1.5rem}.comments-loading{color:var(--muted)}.site-footer{display:flex;max-width:75rem;margin:3rem auto 0;justify-content:space-between;border-top:1px solid var(--border);padding:2rem 1.2rem;color:var(--muted);font-size:.8rem}.site-footer div{display:flex;flex-direction:column}@media(max-width:850px){.main-nav{display:none}.earth-layout{grid-template-columns:1fr}.sidebar-card{display:none}.post-grid{grid-template-columns:1fr}}`;

export type { ThemeContext, ThemeLink, ThemeLinkGroup, ThemeMenuItem, ThemePost, ThemeSite } from "./types.js";
