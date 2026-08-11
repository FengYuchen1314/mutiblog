import React from "react";
import type { ThemeContext, ThemeLinkGroup, ThemeMenuItem, ThemePost, ThemeTaxonomySummary } from "./types.js";

const e = (value: string) => encodeURIComponent(value);
type SocialLink = { icon: string; name?: string; url: string; kind: "link" | "image" };
const defaultHeaderImage = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 1600 700'%3E%3Cdefs%3E%3ClinearGradient id='s' x2='1' y2='1'%3E%3Cstop stop-color='%23071126'/%3E%3Cstop offset='1' stop-color='%231f4165'/%3E%3C/linearGradient%3E%3CradialGradient id='g'%3E%3Cstop stop-color='%235ad7bd' stop-opacity='.85'/%3E%3Cstop offset='.52' stop-color='%231d79a2' stop-opacity='.72'/%3E%3Cstop offset='1' stop-color='%23071126' stop-opacity='0'/%3E%3C/radialGradient%3E%3C/defs%3E%3Cpath fill='url(%23s)' d='M0 0h1600v700H0z'/%3E%3Ccircle cx='1270' cy='565' r='520' fill='url(%23g)'/%3E%3Cg fill='%23fff' opacity='.68'%3E%3Ccircle cx='150' cy='110' r='2'/%3E%3Ccircle cx='390' cy='195' r='1.5'/%3E%3Ccircle cx='680' cy='85' r='2'/%3E%3Ccircle cx='980' cy='150' r='1.5'/%3E%3Ccircle cx='1420' cy='92' r='2'/%3E%3C/g%3E%3C/svg%3E";

function Shell({ context, children }: { context: ThemeContext; children: React.ReactNode }) {
  const { site, currentPath, strings } = context;
  const title = context.pageTitle ? `${context.pageTitle} – ${site.title}` : site.title;
  const description = context.pageDescription ?? site.description ?? site.subtitle ?? site.title;
  const accentColor = safeColor(stringSetting(context, "style.accentColor", "#4ccba0"));
  const defaultColorScheme = choiceSetting(context, "style.defaultColorScheme", ["system", "light", "dark"] as const, "system");
  const cardRadius = choiceSetting(context, "style.cardRadius", ["square", "soft", "round"] as const, "soft");
  const cardShadow = choiceSetting(context, "style.cardShadow", ["none", "subtle", "floating"] as const, "subtle");
  const bodyFont = choiceSetting(context, "style.bodyFont", ["system", "humanist", "serif"] as const, "system");
  const showCardBorder = booleanSetting(context, "style.showCardBorder", true);
  const stickyHeader = booleanSetting(context, "global.sticky", true);
  const logoType = choiceSetting(context, "global.logoType", ["text", "symbol", "image"] as const, "symbol");
  const logoImage = safeAssetUrl(stringSetting(context, "global.logoImage", "")) || safeAssetUrl(site.logo ?? "");
  const showScrollButton = booleanSetting(context, "global.showScrollButton", true);
  const showBrandSymbol = booleanSetting(context, "global.showBrandSymbol", true);
  const brandSymbol = shortText(stringSetting(context, "global.brandSymbol", "M"), "M", 3);
  const showSearch = booleanSetting(context, "global.showSearch", true);
  const showLanguageSwitcher = booleanSetting(context, "global.showLanguageSwitcher", true);
  const showColorSchemeToggle = booleanSetting(context, "global.showColorSchemeToggle", true);
  const showFooter = booleanSetting(context, "footer.showFooter", true);
  const footerStyle = choiceSetting(context, "footer.style", ["style-1", "style-2"] as const, "style-1");
  const legacyFooterLayout = choiceSetting(context, "footer.layout", ["split", "centered"] as const, "split");
  const footerLayout = footerStyle === "style-2" ? "centered" : legacyFooterLayout;
  const showSiteDescription = booleanSetting(context, "footer.showSiteDescription", true);
  const showPoweredBy = booleanSetting(context, "footer.showPoweredBy", true);
  const showTopBorder = booleanSetting(context, "footer.showTopBorder", true);
  const footerLogo = safeAssetUrl(stringSetting(context, "footer.logo", "")) || safeAssetUrl(site.logo ?? "");
  const footerTitle = shortText(stringSetting(context, "footer.title", ""), site.title, 120);
  const footerSlogan = footerStyle === "style-2" ? shortText(stringSetting(context, "footer.slogan", ""), "", 300) : "";
  const rightMenu = stringSetting(context, "footer.rightMenu", "").trim();
  const footerMenuIDs = footerStyle === "style-1" ? (rightMenu ? [rightMenu] : []) : stringArraySetting(context, "footer.menuIds", [], 6);
  const footerMenus = footerMenuIDs.flatMap((id) => context.menus?.find((menu) => menu.id === id) ?? []);
  const footerSocialLinks = socialLinksSetting(context, "footer.socialLinks");
  const copyright = stringSetting(context, "footer.copyright", "").trim() || `© ${new Date().getUTCFullYear()} ${site.title}`;
  const styleVariables = {
    "--accent": accentColor,
    "--card-radius": { square: "0", soft: "1rem", round: "1.6rem" }[cardRadius],
    "--card-shadow": {
      none: "none",
      subtle: "0 5px 18px rgb(14 23 49 / 4%)",
      floating: "0 16px 40px rgb(14 23 49 / 14%)",
    }[cardShadow],
  } as React.CSSProperties;

  return (
    <html lang={site.locale} style={styleVariables}>
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <meta name="description" content={description} />
        <title>{title}</title>
        {context.canonicalUrl ? <link rel="canonical" href={context.canonicalUrl} /> : null}
				{context.pagination?.previousPath ? <link rel="prev" href={absoluteUrl(site.baseUrl, `/${site.locale}${context.pagination.previousPath}`)} /> : null}
				{context.pagination?.nextPath ? <link rel="next" href={absoluteUrl(site.baseUrl, `/${site.locale}${context.pagination.nextPath}`)} /> : null}
        {context.alternates?.map((alternate) => <link key={alternate.locale} rel="alternate" hrefLang={alternate.locale} href={alternate.href} />)}
        {context.alternates?.length ? <link rel="alternate" hrefLang="x-default" href={(context.alternates.find(({ locale }) => locale === site.sourceLocale) ?? context.alternates[0]).href} /> : null}
        <meta property="og:type" content={context.post ? "article" : "website"} />
        <meta property="og:title" content={title} />
        <meta property="og:description" content={description} />
        {context.canonicalUrl ? <meta property="og:url" content={context.canonicalUrl} /> : null}
        {context.post?.cover ? <meta property="og:image" content={absoluteUrl(site.baseUrl, context.post.cover)} /> : null}
        <link rel="stylesheet" href="/assets/theme.css" />
      </head>
      <body id="top" className={classes(`body-font-${bodyFont}`, !showCardBorder && "cards-borderless")}>
        <header className={classes("site-header", !stickyHeader && "site-header-static")}>
          <div className="header-inner">
            <a className="site-brand" href={`/${site.locale}/`}>
              {logoType === "image" && logoImage ? <img className="brand-image" src={logoImage} alt={site.title} /> : null}
              {logoType === "symbol" && showBrandSymbol ? <span className="brand-symbol" aria-hidden="true">{brandSymbol}</span> : null}
              {logoType !== "image" || !logoImage ? <strong>{site.title}</strong> : null}
            </a>
            <nav className="main-nav"><NavigationLinks context={context} /></nav>
            <div className="header-actions">
              {showSearch ? <a className="icon-action" href={`/${site.locale}/search/`} aria-label={strings.search}>⌕</a> : null}
              {showLanguageSwitcher && site.locales.length > 1 ? (
                <details className="locale-picker">
                  <summary>{site.locale}</summary>
                  <div>{site.locales.map((locale) => <a key={locale.code} href={`/${locale.code}${currentPath}`}>{locale.label}</a>)}</div>
                </details>
              ) : null}
              {showColorSchemeToggle ? <button className="icon-action" type="button" aria-label={strings.colorScheme} data-color-scheme>◐</button> : null}
              <details className="mobile-nav"><summary className="icon-action" aria-label={strings.menu}>☰</summary><nav><NavigationLinks context={context} /></nav></details>
            </div>
          </div>
        </header>
        {children}
        {showFooter ? (
          <footer className={classes("site-footer", `site-footer-${footerLayout}`, `site-footer-${footerStyle}`, !showTopBorder && "site-footer-borderless")}>
            <div className="footer-identity">
              {footerLogo ? <img className="footer-logo" src={footerLogo} alt="" /> : null}
              <strong>{footerTitle}</strong>
              {footerSlogan ? <span className="footer-slogan">{footerSlogan}</span> : null}
              {showSiteDescription && (site.description || site.subtitle) ? <span>{site.description || site.subtitle}</span> : null}
              {copyright ? <span className="footer-copyright">{copyright}</span> : null}
            </div>
            {footerMenus.length ? <div className="footer-menus">{footerMenus.map((menu) => <nav key={menu.id} aria-label={menu.label}><strong>{menu.label}</strong>{menu.items.map((item) => <NavigationItem key={item.id} item={item} expanded />)}</nav>)}</div> : null}
            <div className="footer-meta">
              {footerSocialLinks.length ? <SocialLinks links={footerSocialLinks} /> : null}
              {showPoweredBy ? <p>{strings.poweredBy}</p> : null}
            </div>
          </footer>
        ) : null}
        {showScrollButton ? <a className="scroll-top" href="#top" aria-label={strings.scrollTop}>↑</a> : null}
        <script dangerouslySetInnerHTML={{ __html: colorSchemeScript(defaultColorScheme) }} />
        <script dangerouslySetInnerHTML={{ __html: earthInteractionScript }} />
      </body>
    </html>
  );
}

function NavigationLinks({ context }: { context: ThemeContext }) {
  return context.navigation?.length
    ? <>{context.navigation.map((item) => <NavigationItem key={item.id} item={item} />)}</>
    : <>
			<a href={`/${context.site.locale}/`}>{context.strings.home}</a>
			{context.taxonomyCollections.categories.length ? <a href={`/${context.site.locale}/categories/`}>{context.strings.categories}</a> : null}
			{context.taxonomyCollections.tags.length ? <a href={`/${context.site.locale}/tags/`}>{context.strings.tags}</a> : null}
			<a href={`/${context.site.locale}/archives/`}>{context.strings.archives}</a>
			<a href={`/${context.site.locale}/links/`}>{context.strings.links}</a>
		</>;
}

function NavigationItem({ item, expanded = false }: { item: ThemeMenuItem; expanded?: boolean }) {
  const link = <a href={item.href} target={item.openInNew ? "_blank" : undefined} rel={item.openInNew ? "noopener noreferrer" : undefined}>{item.label}</a>;
  return item.children?.length
    ? <details className="menu-children" open={expanded || undefined}><summary>{item.label}</summary><div>{link}{item.children.map((child) => <NavigationItem key={child.id} item={child} expanded={expanded} />)}</div></details>
    : link;
}

function SocialLinks({ links }: { links: SocialLink[] }) {
  return <nav className="social-links">{links.map((link, index) => <a key={`${link.url}-${index}`} href={link.url} target="_blank" rel="noopener noreferrer" aria-label={link.name || link.icon} data-image-link={link.kind === "image" ? "true" : undefined}><span aria-hidden="true">{socialIconGlyph(link.icon)}</span>{link.name ? <span>{link.name}</span> : null}</a>)}</nav>;
}

function PostCard({ context, post }: { context: ThemeContext; post: ThemePost }) {
  const showCover = booleanSetting(context, "layout.showPostCovers", true);
  const showSummary = booleanSetting(context, "layout.showPostSummaries", true);
  const showCategories = booleanSetting(context, "layout.showCategories", true);
  const showPublishedAt = booleanSetting(context, "layout.showPublishedAt", true);
  const coverRatio = choiceSetting(context, "layout.coverRatio", ["wide", "landscape", "square"] as const, "landscape");
  const href = `/${context.site.locale}/posts/${e(post.id)}/`;

  return (
    <article className="post-card">
      {showCover && post.cover ? <a className={classes("post-cover", `post-cover-${coverRatio}`)} href={href}><img src={post.cover} alt="" /></a> : null}
      <div className="post-card-body">
        {showCategories && post.categories?.length ? <div className="post-taxonomy">{post.categories.map((category) => <a key={category.id} href={`/${context.site.locale}/categories/${e(category.id)}/`}>{category.name}</a>)}</div> : null}
        <h2><a href={href}>{post.title}</a></h2>
        {showSummary && post.summary ? <p>{post.summary}</p> : null}
        <div className="post-meta">
          {showPublishedAt && post.publishedAt ? <time dateTime={post.publishedAt}>{formatPublishedDate(context, post.publishedAt)}</time> : <span />}
          <span aria-hidden="true">→</span>
        </div>
      </div>
    </article>
  );
}

export function renderIndex(context: ThemeContext) {
  const postListLayout = choiceSetting(context, "layout.postListLayout", ["grid-3", "grid-2", "single"] as const, "grid-3");

  return (
    <Shell context={context}>
			<ListHero context={context} />
			<ListLayout context={context}>
				<CategoryFilter context={context} />
				{context.posts.length
					? <section id="post-list" className={classes("post-grid", `post-grid-${postListLayout}`)}>{context.posts.map((post) => <PostCard key={post.id} context={context} post={post} />)}</section>
					: <p className="empty-list">{context.strings.noPosts}</p>}
				<Pagination context={context} />
			</ListLayout>
    </Shell>
  );
}

function ListHero({ context }: { context: ThemeContext }) {
	const showHero = booleanSetting(context, "layout.showHero", true);
	const headerWidget = choiceSetting(context, "layout.headerWidget", ["none", "latest-post", "latest-post-grid", "site-title"] as const, "site-title");
	return showHero && headerWidget !== "none" ? <HomeHero context={context} widget={headerWidget} /> : null;
}

function ListLayout({ context, children }: { context: ThemeContext; children: React.ReactNode }) {
	const showSidebar = booleanSetting(context, "layout.showSidebar", true);
	const sidebarPosition = choiceSetting(context, "sidebar.position", ["right", "left"] as const, "right");
	return <main className={classes("earth-layout", !showSidebar && "earth-layout-no-sidebar", showSidebar && `sidebar-${sidebarPosition}`)}>
		<section className="list-content">{children}</section>
		{showSidebar ? <Sidebar context={context} /> : null}
	</main>;
}

function CategoryFilter({ context }: { context: ThemeContext }) {
	if (!context.taxonomyCollections.categories.length) return null;
	const activeID = context.taxonomy?.kind === "categories" ? context.taxonomy.id : context.collection?.kind === "categories" ? context.collection.selectedId : undefined;
	const tree = categoryTree(context.taxonomyCollections.categories);
	return <nav className="taxonomy-filter" aria-label={context.strings.categories}>
		<ul><li><a href={`/${context.site.locale}/`} aria-current={context.pagination?.basePath === "/" ? "page" : undefined}>{context.strings.all}</a></li>{tree.map((node) => <CategoryFilterItem key={node.item.id} node={node} activeID={activeID} />)}</ul>
	</nav>;
}

type CategoryTreeNode = { item: ThemeTaxonomySummary; children: CategoryTreeNode[] };

function categoryTree(items: ThemeTaxonomySummary[]): CategoryTreeNode[] {
	const nodes = new Map(items.map((item) => [item.id, { item, children: [] as CategoryTreeNode[] }]));
	const roots: CategoryTreeNode[] = [];
	for (const node of nodes.values()) {
		const parent = node.item.parentId && node.item.parentId !== node.item.id ? nodes.get(node.item.parentId) : undefined;
		if (parent) parent.children.push(node); else roots.push(node);
	}
	return roots.length ? roots : [...nodes.values()].map((node) => ({ item: node.item, children: [] }));
}

function CategoryFilterItem({ node, activeID }: { node: CategoryTreeNode; activeID?: string }) {
	const active = activeID === node.item.id;
	const childActive = node.children.some((child) => categoryBranchContains(child, activeID));
	return <li className={node.children.length ? "taxonomy-filter-branch" : undefined}>
		<div><a href={node.item.href} aria-current={active ? "page" : undefined}>{node.item.name}</a>{node.children.length ? <details open={childActive || undefined}><summary aria-label={node.item.name}>⌄</summary><ul>{node.children.map((child) => <CategoryFilterItem key={child.item.id} node={child} activeID={activeID} />)}</ul></details> : null}</div>
	</li>;
}

function categoryBranchContains(node: CategoryTreeNode, activeID?: string): boolean {
	return node.item.id === activeID || node.children.some((child) => categoryBranchContains(child, activeID));
}

function TagFilter({ context }: { context: ThemeContext }) {
	if (!context.taxonomyCollections.tags.length) return null;
	const activeID = context.taxonomy?.kind === "tags" ? context.taxonomy.id : context.collection?.kind === "tags" ? context.collection.selectedId : undefined;
	return <nav className="taxonomy-filter taxonomy-filter-tags" aria-label={context.strings.tags}>
		{context.taxonomyCollections.tags.map((tag) => <a key={tag.id} href={tag.href} aria-current={activeID === tag.id ? "page" : undefined}>#{tag.name}<sup>{tag.count}</sup></a>)}
	</nav>;
}

function Pagination({ context }: { context: ThemeContext }) {
	const pagination = context.pagination;
	if (!pagination || pagination.totalPages <= 1) return null;
	const href = (path: string) => `/${context.site.locale}${path}`;
	return <nav className="pagination" aria-label={context.strings.pagination}>
		{pagination.previousPath ? <a rel="prev" href={href(pagination.previousPath)}>← <span>{context.strings.previousPage}</span></a> : <span aria-disabled="true">← <span>{context.strings.previousPage}</span></span>}
		<div className="pagination-pages">{pagination.pages.map((item) => item.current
			? <strong key={item.number} aria-current="page" aria-label={`${context.strings.page} ${item.number}`}>{item.number}</strong>
			: <a key={item.number} href={href(item.path)} aria-label={`${context.strings.page} ${item.number}`}>{item.number}</a>)}</div>
		{pagination.nextPath ? <a rel="next" href={href(pagination.nextPath)}><span>{context.strings.nextPage}</span> →</a> : <span aria-disabled="true"><span>{context.strings.nextPage}</span> →</span>}
	</nav>;
}

function HomeHero({ context, widget }: { context: ThemeContext; widget: "latest-post" | "latest-post-grid" | "site-title" }) {
  const heroKicker = shortText(stringSetting(context, "layout.heroKicker", "MutiBlog"), "", 40);
  const heroHeight = choiceSetting(context, "layout.heroHeight", ["compact", "standard", "tall"] as const, "standard");
  const backgroundType = choiceSetting(context, "layout.headerBackgroundType", ["gradient", "image"] as const, "image");
  const background = safeBackground(stringSetting(context, "layout.headerBackground", "linear-gradient(145deg, #0e1731, #202d52)"));
  const backgroundImage = safeAssetUrl(stringSetting(context, "layout.headerBackgroundImage", "")) || defaultHeaderImage;
  const titleColor = safeColor(stringSetting(context, "layout.headerTitleColor", "#ffffff"), "#ffffff");
  const style = {
    color: titleColor,
    background: backgroundType === "image" && backgroundImage ? `linear-gradient(rgb(8 16 36 / 52%), rgb(8 16 36 / 62%)), url(${JSON.stringify(backgroundImage)}) center / cover` : background,
  } as React.CSSProperties;
	const latest = context.allPosts[0];

  return (
    <section className={classes("hero", `hero-${heroHeight}`, `hero-widget-${widget}`)} style={style}>
      {widget === "site-title" || !latest ? <div>{heroKicker ? <span className="hero-kicker">{heroKicker}</span> : null}<h1>{context.site.title}</h1>{context.site.subtitle || context.site.description ? <p>{context.site.subtitle ?? context.site.description}</p> : null}</div> : null}
      {widget === "latest-post" && latest ? <a className="hero-latest" href={`/${context.site.locale}/posts/${e(latest.id)}/`}>{latest.cover ? <img src={latest.cover} alt="" /> : null}<div><span className="hero-kicker">{heroKicker}</span><h1>{latest.title}</h1>{latest.summary ? <p>{latest.summary}</p> : null}</div></a> : null}
      {widget === "latest-post-grid" && latest ? <div className="hero-post-grid">{context.allPosts.slice(0, 5).map((post) => <a key={post.id} href={`/${context.site.locale}/posts/${e(post.id)}/`} style={post.cover ? { backgroundImage: `linear-gradient(rgb(8 16 36 / 40%), rgb(8 16 36 / 70%)), url(${JSON.stringify(post.cover)})` } : undefined}><strong>{post.title}</strong></a>)}</div> : null}
    </section>
  );
}

function Sidebar({ context }: { context: ThemeContext }) {
  const sticky = booleanSetting(context, "sidebar.sticky", false);
  const profileStyle = choiceSetting(context, "sidebar.profileStyle", ["card", "plain"] as const, "card");
  const showAbout = booleanSetting(context, "sidebar.showAbout", true);
  const showLanguages = booleanSetting(context, "sidebar.showLanguages", true);
  const widgets = stringArraySetting(context, "sidebar.widgets", ["popular-posts", "categories", "tags"], 8)
    .filter((widget) => ["profile", "popular-posts", "recent-posts", "categories", "tags", "languages"].includes(widget));
  const profileLogo = safeAssetUrl(stringSetting(context, "sidebar.profileLogo", ""));
  const socialLinks = socialLinksSetting(context, "sidebar.socialLinks");
	const categories = uniqueTaxonomies(context.allPosts.flatMap((post) => post.categories ?? []));
	const tags = uniqueTaxonomies(context.allPosts.flatMap((post) => post.tags ?? []));
	const hasTOC = context.post?.kind === "post" && Boolean(context.post.headings?.length);

  return (
		<aside className={classes("sidebar-card", hasTOC && "sidebar-has-toc", sticky && "sidebar-sticky", `sidebar-profile-${profileStyle}`)} data-public-stats data-stats-locale={context.site.locale} data-statistics-unavailable={context.strings.statisticsUnavailable} data-visits-label={context.strings.visits}>
			{hasTOC ? <TableOfContents context={context} /> : null}
      {widgets.map((widget, index) => {
		if (widget === "profile") return <section key={`${widget}-${index}`} className="sidebar-profile">{profileLogo ? <img src={profileLogo} alt="" /> : null}<h3>{context.site.title}</h3>{showAbout && (context.site.description || context.site.subtitle) ? <p>{context.site.description ?? context.site.subtitle}</p> : null}<dl className="profile-stats"><div><dt>{context.strings.posts}</dt><dd data-profile-stat="posts">{context.allPosts.length}</dd></div><div><dt>{context.strings.categories}</dt><dd data-profile-stat="categories">{categories.length}</dd></div><div><dt>{context.strings.comments}</dt><dd data-profile-stat="comments">–</dd></div><div><dt>{context.strings.visits}</dt><dd data-profile-stat="visits">–</dd></div></dl>{socialLinks.length ? <SocialLinks links={socialLinks} /> : null}</section>;
		if (widget === "popular-posts") return <section key={`${widget}-${index}`} className="sidebar-post-list"><h3>{context.strings.popularPosts}</h3><div data-popular-posts aria-live="polite"><span className="statistics-loading">…</span></div></section>;
		if (widget === "recent-posts") return <section key={`${widget}-${index}`} className="sidebar-post-list"><h3>{context.strings.recentPosts}</h3>{context.allPosts.slice(0, 5).map((post) => <a key={post.id} href={`/${context.site.locale}/posts/${e(post.id)}/`}><span>{post.title}</span></a>)}</section>;
		if (widget === "categories" && categories.length) return <section key={`${widget}-${index}`}><h3>{context.strings.categories}</h3>{categories.map((category) => <a key={category.id} href={`/${context.site.locale}/categories/${e(category.id)}/`}>{category.name}</a>)}</section>;
		if (widget === "tags" && tags.length) return <section key={`${widget}-${index}`}><h3>{context.strings.tags}</h3><div className="sidebar-tags">{tags.map((tag) => <a key={tag.id} href={`/${context.site.locale}/tags/${e(tag.id)}/`}>#{tag.name}</a>)}</div></section>;
        if (widget === "languages" && showLanguages) return <section key={`${widget}-${index}`}><h3>{context.strings.languages}</h3>{context.site.locales.map((locale) => <a key={locale.code} href={`/${locale.code}/`}>{locale.label}</a>)}</section>;
        return null;
      })}
	  <script dangerouslySetInnerHTML={{ __html: statisticsScript }} />
    </aside>
  );
}

function TableOfContents({ context }: { context: ThemeContext }) {
	const headings = context.post?.headings ?? [];
	if (!headings.length) return null;
	return <section className="table-of-contents" aria-labelledby="table-of-contents-title">
		<h3 id="table-of-contents-title">{context.strings.tableOfContents}</h3>
		<nav aria-label={context.strings.tableOfContents}>
			<ol>{headings.map((heading) => <li key={heading.id} className={`toc-level-${heading.level}`}><a href={`#${e(heading.id)}`}>{heading.text}</a></li>)}</ol>
		</nav>
	</section>;
}

export function renderPost(context: ThemeContext) {
  const post = context.post!;
  const showCover = booleanSetting(context, "post.showCover", true);
  const showSummary = booleanSetting(context, "post.showSummary", true);
  const showCategories = booleanSetting(context, "post.showCategories", true);
  const showTags = booleanSetting(context, "post.showTags", true);
  const showPublishedAt = booleanSetting(context, "post.showPublishedAt", true);
  const contentWidth = choiceSetting(context, "post.contentWidth", ["narrow", "standard", "wide"] as const, "standard");
  const contentFont = choiceSetting(context, "post.contentFont", ["serif", "sans"] as const, "serif");
  const contentStyle = choiceSetting(context, "post.contentStyle", ["github", "tailwind", "typography"] as const, "github");
  const titlePosition = choiceSetting(context, "post.titlePosition", ["content", "cover"] as const, "content");
  const coverHeight = coverHeightSetting(context);
  const contentHeader = booleanSetting(context, "layout.contentHeader", true) && showCover && Boolean(post.cover);
  const showUpvote = booleanSetting(context, "post.showUpvoteButton", true);
  const showShare = booleanSetting(context, "post.showShareButton", true);
  const shareItems = stringArraySetting(context, "post.shareItems", ["wechat", "x", "telegram", "facebook", "qq", "qzone", "weibo", "douban", "native"], 10);
  const showComments = booleanSetting(context, "comments.showSection", true);
	const showSidebar = booleanSetting(context, "layout.showSidebar", true);
	const sidebarPosition = choiceSetting(context, "sidebar.position", ["right", "left"] as const, "right");

  return (
    <Shell context={context}>
      {contentHeader ? <ContentCover context={context} post={post} height={coverHeight} showTitle={titlePosition === "cover"} showCategories={showCategories} showSummary={showSummary} showPublishedAt={showPublishedAt} /> : null}
	  <main className={classes("earth-layout", !showSidebar && "earth-layout-no-sidebar", showSidebar && `sidebar-${sidebarPosition}`)}>
		<section className={classes("article-shell", `article-width-${contentWidth}`)}>
		<article className="article-card" data-visit-subject data-visit-kind="Post" data-visit-id={post.id}>
          {titlePosition === "content" || !contentHeader ? <header>
            {showCategories && post.categories?.length ? <div className="post-taxonomy">{post.categories.map((category) => <a key={category.id} href={`/${context.site.locale}/categories/${e(category.id)}/`}>{category.name}</a>)}</div> : null}
            <h1>{post.title}</h1>
            {showSummary && post.summary ? <p>{post.summary}</p> : null}
            {showPublishedAt && post.publishedAt ? <time dateTime={post.publishedAt}>{formatPublishedDate(context, post.publishedAt)}</time> : null}
          </header> : null}
          {showCover && post.cover && !contentHeader ? <img className="article-cover" src={post.cover} alt="" /> : null}
          <div id="content" className={classes("markdown-body", `markdown-font-${contentFont}`, `markdown-style-${contentStyle}`)} dangerouslySetInnerHTML={{ __html: post.html }} />
			{context.cursor ? <PostCursor context={context} /> : null}
          {showTags && post.tags?.length ? <nav className="article-tags">{post.tags.map((tag) => <a key={tag.id} href={`/${context.site.locale}/tags/${e(tag.id)}/`}>#{tag.name}</a>)}</nav> : null}
          {showUpvote ? <UpvoteAction context={context} post={post} /> : null}
          {showShare ? <ShareActions context={context} post={post} items={shareItems} /> : null}
          {showComments ? <CommentSection context={context} kind="Post" id={post.id} open={post.commentPolicy !== "closed"} /> : null}
		</article>
		</section>
		{showSidebar ? <Sidebar context={context} /> : null}
	  </main>
	  {showUpvote ? <script dangerouslySetInnerHTML={{ __html: upvoteScript }} /> : null}
	  {showComments ? <script dangerouslySetInnerHTML={{ __html: commentsScript }} /> : null}
	  <script dangerouslySetInnerHTML={{ __html: visitScript }} />
    </Shell>
  );
}

function PostCursor({ context }: { context: ThemeContext }) {
	const previous = context.cursor?.previous;
	const next = context.cursor?.next;
	if (!previous && !next) return null;
	return <nav className="post-cursor" aria-label={context.strings.postNavigation}>
		{previous ? <a className="post-cursor-previous" rel="prev" href={`/${context.site.locale}/posts/${e(previous.id)}/`} title={previous.title}><span aria-hidden="true">←</span><span><small>{context.strings.previousPost}</small>{previous.title}</span></a> : <span />}
		{next ? <a className="post-cursor-next" rel="next" href={`/${context.site.locale}/posts/${e(next.id)}/`} title={next.title}><span><small>{context.strings.nextPost}</small>{next.title}</span><span aria-hidden="true">→</span></a> : <span />}
	</nav>;
}

export function renderPage(context: ThemeContext) {
  const page = context.post!;
  const showCover = booleanSetting(context, "post.showCover", true);
  const showSummary = booleanSetting(context, "post.showSummary", true);
  const contentWidth = choiceSetting(context, "post.contentWidth", ["narrow", "standard", "wide"] as const, "standard");
  const contentFont = choiceSetting(context, "post.contentFont", ["serif", "sans"] as const, "serif");
  const contentStyle = choiceSetting(context, "post.contentStyle", ["github", "tailwind", "typography"] as const, "github");
  const titlePosition = choiceSetting(context, "post.titlePosition", ["content", "cover"] as const, "content");
  const coverHeight = coverHeightSetting(context);
  const contentHeader = booleanSetting(context, "layout.contentHeader", true) && showCover && Boolean(page.cover);
  const showComments = booleanSetting(context, "comments.showSection", true);
	const showSidebar = booleanSetting(context, "layout.showSidebar", true);
	const sidebarPosition = choiceSetting(context, "sidebar.position", ["right", "left"] as const, "right");

  return (
    <Shell context={context}>
      {contentHeader ? <ContentCover context={context} post={page} height={coverHeight} showTitle={titlePosition === "cover"} showSummary={showSummary} /> : null}
	  <main className={classes("earth-layout", !showSidebar && "earth-layout-no-sidebar", showSidebar && `sidebar-${sidebarPosition}`)}>
		<section className={classes("article-shell", `article-width-${contentWidth}`)}>
		<article className="article-card" data-visit-subject data-visit-kind="Page" data-visit-id={page.id}>
          {titlePosition === "content" || !contentHeader ? <header><h1>{page.title}</h1>{showSummary && page.summary ? <p>{page.summary}</p> : null}</header> : null}
          {showCover && page.cover && !contentHeader ? <img className="article-cover" src={page.cover} alt="" /> : null}
          <div className={classes("markdown-body", `markdown-font-${contentFont}`, `markdown-style-${contentStyle}`)} dangerouslySetInnerHTML={{ __html: page.html }} />
          {showComments ? <CommentSection context={context} kind="Page" id={page.id} open={page.commentPolicy !== "closed"} /> : null}
		</article>
		</section>
		{showSidebar ? <Sidebar context={context} /> : null}
	  </main>
	  {showComments ? <script dangerouslySetInnerHTML={{ __html: commentsScript }} /> : null}
	  <script dangerouslySetInnerHTML={{ __html: visitScript }} />
    </Shell>
  );
}

function ContentCover({ context, post, height, showTitle, showCategories = false, showSummary = false, showPublishedAt = false }: { context: ThemeContext; post: ThemePost; height: string; showTitle: boolean; showCategories?: boolean; showSummary?: boolean; showPublishedAt?: boolean }) {
  if (!post.cover) return null;
  const legacyHeight = ["compact", "standard", "tall"].includes(height) ? height : "";
  return <section className={classes("content-cover", legacyHeight && `content-cover-${legacyHeight}`)} style={{ backgroundImage: `linear-gradient(rgb(8 16 36 / 32%), rgb(8 16 36 / 68%)), url(${JSON.stringify(post.cover)})`, minHeight: legacyHeight ? undefined : height }}>{showTitle ? <header>{showCategories && post.categories?.length ? <div className="post-taxonomy">{post.categories.map((category) => <a key={category.id} href={`/${context.site.locale}/categories/${e(category.id)}/`}>{category.name}</a>)}</div> : null}<h1>{post.title}</h1>{showSummary && post.summary ? <p>{post.summary}</p> : null}{showPublishedAt && post.publishedAt ? <time dateTime={post.publishedAt}>{formatPublishedDate(context, post.publishedAt)}</time> : null}</header> : null}</section>;
}

function ShareActions({ context, post, items }: { context: ThemeContext; post: ThemePost; items: string[] }) {
  const url = context.canonicalUrl ?? `/${context.site.locale}/posts/${e(post.id)}/`;
  const absoluteURL = /^https?:\/\//i.test(url) ? url : "";
  const dialogID = `share-dialog-${post.id}`;
  const available = [...new Set(items)].filter((item) => ["wechat", "native", "x", "telegram", "facebook", "qq", "qzone", "weibo", "douban", "email"].includes(item));
  if (!available.length) return null;
  return <div className="share-entry">
    <button className="share-trigger" type="button" data-share-open={dialogID} aria-haspopup="dialog"><span aria-hidden="true">↗</span>{context.strings.share}</button>
    <dialog id={dialogID} className="share-dialog" data-share-dialog data-share-url={url} data-share-title={post.title} data-share-unavailable={context.strings.shareUnavailable}>
      <header><strong>{context.strings.share}</strong><button type="button" data-share-close aria-label={context.strings.close}>×</button></header>
      <nav className="share-actions" aria-label={context.strings.share}>{available.map((item) => {
        if (item === "native") return <button key={item} type="button" data-share-native data-share-url={url} data-share-title={post.title}>{context.strings.nativeShare}</button>;
        if (item === "wechat") return <button key={item} type="button" data-share-wechat data-share-url={url}>{context.strings.wechatScan}</button>;
        return absoluteURL
          ? <a key={item} href={shareURL(item, absoluteURL, post.title)} target="_blank" rel="noopener noreferrer">{shareLabel(item)}</a>
          : <button key={item} type="button" data-share-external={item} data-share-url={url} data-share-title={post.title}>{shareLabel(item)}</button>;
      })}</nav>
      <div className="share-copy-row"><input readOnly data-share-copy-value value={url} aria-label={context.strings.copyLink} /><button type="button" data-share-copy data-copy-label={context.strings.copyLink} data-copied-label={context.strings.copied}>{context.strings.copyLink}</button></div>
      <section className="share-qr" data-share-qr-panel hidden><strong>{context.strings.wechatScan}</strong><img data-share-qr alt={context.strings.wechatScan} /><output data-share-qr-status aria-live="polite" /></section>
    </dialog>
  </div>;
}

function UpvoteAction({ context, post }: { context: ThemeContext; post: ThemePost }) {
  const label = context.strings.upvote;
  const upvotedLabel = context.strings.upvoted;
  const unavailableLabel = context.strings.upvotesUnavailable;
  return <div className="upvote-action">
    <button type="button" disabled data-upvote data-kind="Post" data-id={post.id} data-state="loading" data-upvoted-label={upvotedLabel} data-unavailable-label={unavailableLabel} aria-pressed="false">
      <span className="upvote-icon" aria-hidden="true">♥</span>
      <span>{label}</span>
      <output aria-label={context.strings.upvoteCount}>–</output>
    </button>
    <span className="upvote-status" aria-live="polite" />
  </div>;
}

export function renderTaxonomy(context: ThemeContext, title: string, description?: string) {
  context = { ...context, pageTitle: context.pageTitle || title, pageDescription: context.pageDescription ?? description };
  const postListLayout = choiceSetting(context, "layout.postListLayout", ["grid-3", "grid-2", "single"] as const, "grid-3");

  return (
    <Shell context={context}>
			<ListHero context={context} />
			<ListLayout context={context}>
				{context.taxonomy?.kind === "tags" ? <TagFilter context={context} /> : <CategoryFilter context={context} />}
				<header className="list-heading taxonomy-heading">
					{context.taxonomy?.cover ? <img src={context.taxonomy.cover} alt="" /> : null}
					<div><h1>{title}</h1>{description ? <p>{description}</p> : null}</div>
				</header>
				{context.posts.length
					? <section id="post-list" className={classes("post-grid", `post-grid-${postListLayout}`)}>{context.posts.map((post) => <PostCard key={post.id} context={context} post={post} />)}</section>
					: <p className="empty-list">{context.strings.noPosts}</p>}
				<Pagination context={context} />
			</ListLayout>
    </Shell>
  );
}

export function renderCollection(context: ThemeContext) {
	const collection = context.collection!;
	const selected = collection.items.find((item) => item.id === collection.selectedId)!;
	const postListLayout = choiceSetting(context, "layout.postListLayout", ["grid-3", "grid-2", "single"] as const, "grid-3");
	return <Shell context={context}>
		<ListHero context={context} />
		<ListLayout context={context}>
			{collection.kind === "categories" ? <CategoryFilter context={context} /> : <TagFilter context={context} />}
			{context.posts.length
				? <section id="post-list" className={classes("post-grid", `post-grid-${postListLayout}`)} data-taxonomy-collection={collection.kind}>{context.posts.map((post) => <PostCard key={post.id} context={context} post={post} />)}</section>
				: <p className="empty-list" data-taxonomy-collection={collection.kind}>{context.strings.noPosts}</p>}
			{selected.count > context.posts.length ? <a className="more-posts" href={selected.href}>{context.strings.morePosts}<span aria-hidden="true">→</span></a> : null}
		</ListLayout>
	</Shell>;
}

export function renderLinks(context: ThemeContext, groups: ThemeLinkGroup[]) {
	context = { ...context, pageTitle: context.pageTitle || context.strings.links };
  return (
    <Shell context={context}>
			<ListHero context={context} />
			<ListLayout context={context}>
				<header className="list-heading"><h1>{context.strings.links}</h1></header>
				<div className="link-groups">
        {groups.map((group) => (
          <section key={group.id} className="article-card">
            <h2>{group.name}</h2>
            {group.description ? <p>{group.description}</p> : null}
            <div className="post-grid post-grid-grid-2">
              {group.links.map((link) => <a key={link.id} className="post-card link-card" href={link.url} rel="noopener noreferrer"><div className="post-card-body">{link.logo ? <img src={link.logo} alt="" /> : null}<strong>{link.name}</strong>{link.description ? <p>{link.description}</p> : null}</div></a>)}
            </div>
          </section>
        ))}
				</div>
			</ListLayout>
    </Shell>
  );
}

export function renderArchive(context: ThemeContext) {
	context = { ...context, pageTitle: context.pageTitle || context.strings.archives };
	const groups = archiveGroups(context);
  return (
    <Shell context={context}>
			<ListHero context={context} />
			<ListLayout context={context}>
				<section className="article-card archive-list"><h1>{context.strings.archives}</h1>{groups.length ? groups.map((group) => <section key={group.label || "archive-undated"} className="archive-group"><h2>{group.label}</h2><div>{group.posts.map((post) => <article key={post.id}>
					<div className="archive-post-heading"><h3><a href={`/${context.site.locale}/posts/${e(post.id)}/`}>{post.title}</a></h3>{post.publishedAt ? <time dateTime={post.publishedAt}>{formatPublishedDate(context, post.publishedAt)}</time> : null}</div>
					{post.categories?.length || post.tags?.length ? <nav className="archive-taxonomy">{post.categories?.map((category) => <a key={`category-${category.id}`} href={`/${context.site.locale}/categories/${e(category.id)}/`}>{category.name}</a>)}{post.tags?.map((tag) => <a key={`tag-${tag.id}`} href={`/${context.site.locale}/tags/${e(tag.id)}/`}>#{tag.name}</a>)}</nav> : null}
					{post.summary ? <p>{post.summary}</p> : null}
				</article>)}</div></section>) : <p className="empty-list">{context.strings.noPosts}</p>}</section>
				<Pagination context={context} />
			</ListLayout>
    </Shell>
  );
}

export function renderSearch(context: ThemeContext) {
  context = { ...context, pageTitle: context.strings.search };
  return <Shell context={context}><section className="hero taxonomy-hero"><div><h1>{context.strings.search}</h1></div></section><main className="article-shell"><section className="article-card search-card"><input type="search" data-search-input placeholder={context.strings.search} /><div data-search-results /></section></main><script dangerouslySetInnerHTML={{ __html: searchScript }} /></Shell>;
}

export function renderNotFound(context: ThemeContext) {
  const message = context.strings.notFound || "Not found";
  context = { ...context, pageTitle: message };
  return <Shell context={context}><main className="article-shell"><section className="article-card not-found"><strong>404</strong><h1>{message}</h1><a href={`/${context.site.locale}/`}>{context.strings.home || "Home"}</a></section></main></Shell>;
}

function CommentSection({ context, kind, id, open }: { context: ThemeContext; kind: "Post" | "Page"; id: string; open: boolean }) {
  const s = context.strings;
  const showAvatars = booleanSetting(context, "comments.showAvatars", true);
  const showEmailField = booleanSetting(context, "comments.showEmailField", true);
  const showWebsiteField = booleanSetting(context, "comments.showWebsiteField", true);
  const showTimestamps = booleanSetting(context, "comments.showTimestamps", true);
  const formLayout = choiceSetting(context, "comments.formLayout", ["stacked", "two-column"] as const, "two-column");

  return (
    <section className="comments" data-comments data-state="loading" data-subject-kind={kind} data-subject-id={id} data-locale={context.site.locale} data-timezone={context.site.timezone || "UTC"} data-unavailable={s.commentsUnavailable} data-empty={s.commentEmpty} data-pending={s.commentPending} data-show-avatars={showAvatars ? "true" : "false"} data-show-timestamps={showTimestamps ? "true" : "false"}>
      <h2>{s.comments}</h2>
      <div className="comments-loading" aria-live="polite">{s.loadingComments}</div>
      <div className="comment-list" />
      <button type="button" className="comments-more" hidden>{s.commentsMore || "More"}</button>
      {open ? (
        <form className={classes("comment-form", `comment-form-${formLayout}`)}>
          <input name="name" required maxLength={80} autoComplete="name" placeholder={s.commentName} />
          {showEmailField ? <input name="email" type="email" autoComplete="email" placeholder={s.commentEmail} /> : null}
          {showWebsiteField ? <input name="website" type="url" autoComplete="url" placeholder={s.commentWebsite} /> : null}
          <textarea name="content" required rows={5} maxLength={context.site.commentMaxLength} placeholder={s.commentContent} />
          <div className="comment-form-actions"><button type="submit">{s.commentSubmit}</button><output aria-live="polite" /></div>
        </form>
      ) : <p className="comments-closed">{s.commentsClosed}</p>}
    </section>
  );
}

function themeSetting(context: ThemeContext, path: string): unknown {
  let value: unknown = context.settings;
  for (const segment of path.split(".")) {
    if (!value || typeof value !== "object") return undefined;
    value = (value as Record<string, unknown>)[segment];
  }
  return value;
}

function formatPublishedDate(context: ThemeContext, value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(context.site.locale, { timeZone: context.site.timezone || "UTC" }).format(date);
}

function archiveGroups(context: ThemeContext): Array<{ label: string; posts: ThemePost[] }> {
	const formatter = new Intl.DateTimeFormat(context.site.locale, { timeZone: context.site.timezone || "UTC", year: "numeric", month: "long" });
	const groups = new Map<string, ThemePost[]>();
	for (const post of context.posts) {
		const date = post.publishedAt ? new Date(post.publishedAt) : undefined;
		const label = date && !Number.isNaN(date.getTime()) ? formatter.format(date) : context.strings.undated;
		const posts = groups.get(label) ?? [];
		posts.push(post);
		groups.set(label, posts);
	}
	return [...groups].map(([label, posts]) => ({ label, posts }));
}

function booleanSetting(context: ThemeContext, path: string, fallback: boolean): boolean {
  const value = themeSetting(context, path);
  return typeof value === "boolean" ? value : fallback;
}

function stringSetting(context: ThemeContext, path: string, fallback: string): string {
  const value = themeSetting(context, path);
  return typeof value === "string" ? value : fallback;
}

function choiceSetting<const T extends readonly string[]>(context: ThemeContext, path: string, choices: T, fallback: T[number]): T[number] {
  const value = themeSetting(context, path);
  return typeof value === "string" && (choices as readonly string[]).includes(value) ? value as T[number] : fallback;
}

function stringArraySetting(context: ThemeContext, path: string, fallback: string[], maximum: number): string[] {
  const value = themeSetting(context, path);
  if (!Array.isArray(value)) return [...fallback];
  return value.filter((item): item is string => typeof item === "string").map((item) => item.trim()).filter(Boolean).slice(0, maximum);
}

function coverHeightSetting(context: ThemeContext): string {
  const value = stringSetting(context, "post.coverHeight", "24rem").trim().toLowerCase();
  if (["compact", "standard", "tall"].includes(value)) return value;
  const match = /^(0|(?:\d+(?:\.\d+)?)(px|rem|em|vh|vw|%))$/.exec(value);
  if (!match) return "24rem";
  const amount = Number.parseFloat(match[1]);
  const maximum = match[2] === "px" ? 2000 : 200;
  return Number.isFinite(amount) && amount >= 0 && amount <= maximum ? value : "24rem";
}

function socialLinksSetting(context: ThemeContext, path: string): SocialLink[] {
  const value = themeSetting(context, path);
  if (!Array.isArray(value)) return [];
  return value.flatMap((item) => {
    if (!item || typeof item !== "object" || Array.isArray(item)) return [];
    const record = item as Record<string, unknown>;
    const name = typeof record.name === "string" ? shortText(record.name, "", 80) : "";
    const url = typeof record.url === "string" ? safeLinkUrl(record.url) : "";
    const kind = record.kind === "image" ? "image" as const : "link" as const;
    const icon = typeof record.icon === "string" ? shortText(record.icon, "link", 40) : "link";
    return url ? [{ icon, name: name || undefined, url, kind }] : [];
  }).slice(0, 20);
}

function safeColor(value: string, fallback = "#4ccba0"): string {
  return /^#(?:[0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})$/i.test(value) ? value : fallback;
}

function safeBackground(value: string): string {
  const candidate = value.trim();
  if (/^#(?:[0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})$/i.test(candidate)) return candidate;
  if (/^(?:linear|radial)-gradient\([#(),.%\w\s-]+\)$/i.test(candidate) && !/[;{}]|url\s*\(/i.test(candidate)) return candidate;
  return "linear-gradient(145deg, #0e1731, #202d52)";
}

function safeAssetUrl(value: string): string {
  const candidate = value.trim();
  if (/^\/(?!\/)[^\s<>"']{1,499}$/.test(candidate)) return candidate;
  try {
    const parsed = new URL(candidate);
    return parsed.protocol === "https:" || parsed.protocol === "http:" ? parsed.toString() : "";
  } catch {
    return "";
  }
}

function safeLinkUrl(value: string): string {
  const candidate = value.trim();
  if (/^\/(?!\/)[^\s<>"']{1,499}$/.test(candidate)) return candidate;
  try {
    const parsed = new URL(candidate);
    return ["https:", "http:", "mailto:"].includes(parsed.protocol) ? parsed.toString() : "";
  } catch {
    return "";
  }
}

function shortText(value: string, fallback: string, maximum: number): string {
  const text = value.trim();
  return text ? [...text].slice(0, maximum).join("") : fallback;
}

function classes(...values: Array<string | false | null | undefined>): string {
  return values.filter(Boolean).join(" ");
}

function absoluteUrl(baseUrl: string | undefined, value: string) {
  if (!baseUrl || /^(?:https?:)?\/\//.test(value)) return value;
  return `${baseUrl}${value.startsWith("/") ? "" : "/"}${value}`;
}

function uniqueTaxonomies(items: Array<{ id: string; name: string }>) {
  return [...new Map(items.map((item) => [item.id, item])).values()];
}

function shareLabel(item: string) {
  return ({ x: "X", telegram: "Telegram", facebook: "Facebook", qq: "QQ", qzone: "Qzone", weibo: "Weibo", douban: "Douban", email: "Email" } as Record<string, string>)[item] ?? item;
}

function shareURL(item: string, url: string, title: string) {
  const encodedURL = encodeURIComponent(url);
  const encodedTitle = encodeURIComponent(title);
  if (item === "x") return `https://x.com/intent/post?url=${encodedURL}&text=${encodedTitle}`;
  if (item === "telegram") return `https://t.me/share/url?url=${encodedURL}&text=${encodedTitle}`;
  if (item === "facebook") return `https://www.facebook.com/sharer/sharer.php?u=${encodedURL}`;
  if (item === "qq") return `https://connect.qq.com/widget/shareqq/index.html?url=${encodedURL}&title=${encodedTitle}`;
  if (item === "qzone") return `https://sns.qzone.qq.com/cgi-bin/qzshare/cgi_qzshare_onekey?url=${encodedURL}&title=${encodedTitle}`;
  if (item === "weibo") return `https://service.weibo.com/share/share.php?url=${encodedURL}&title=${encodedTitle}`;
  if (item === "douban") return `https://www.douban.com/share/service?href=${encodedURL}&name=${encodedTitle}`;
  return `mailto:?subject=${encodedTitle}&body=${encodedURL}`;
}

function socialIconGlyph(icon: string) {
  const normalized = icon.toLowerCase();
  return ({
    link: "↗",
    mail: "✉",
    github: "GH",
    rss: "RSS",
    wechat: "微",
    telegram: "TG",
    qq: "QQ",
    qzone: "QZ",
    weibo: "微博",
    zhihu: "知",
    bilibili: "B",
    instagram: "IG",
    linkedin: "in",
    x: "X",
    facebook: "f",
    youtube: "▶",
    douban: "豆",
  } as Record<string, string>)[normalized] ?? "↗";
}

const colorSchemeScript = (configured: string) => `(()=>{const b=document.querySelector('[data-color-scheme]');const set=v=>{if(v!=='light'&&v!=='dark')return;document.documentElement.dataset.theme=v;try{localStorage.setItem('earth-theme',v)}catch{}};let saved='';try{saved=localStorage.getItem('earth-theme')||''}catch{}const d=${JSON.stringify(configured)},system=()=>matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light';set(saved==='light'||saved==='dark'?saved:(d==='system'?system():d));b?.addEventListener('click',()=>set(document.documentElement.dataset.theme==='dark'?'light':'dark'))})()`;

const earthInteractionScript = `(()=>{
  const absolute=value=>{try{return new URL(value||location.href,location.href).href}catch{return location.href}};
  const copy=async value=>{const text=absolute(value);try{if(navigator.clipboard){await navigator.clipboard.writeText(text);return}const area=document.createElement('textarea');area.value=text;area.style.position='fixed';area.style.opacity='0';document.body.append(area);area.select();document.execCommand('copy');area.remove()}catch{}};
  const shareURL=(item,url,title)=>{const u=encodeURIComponent(absolute(url)),t=encodeURIComponent(title||document.title);if(item==='x')return 'https://x.com/intent/post?url='+u+'&text='+t;if(item==='telegram')return 'https://t.me/share/url?url='+u+'&text='+t;if(item==='facebook')return 'https://www.facebook.com/sharer/sharer.php?u='+u;if(item==='qq')return 'https://connect.qq.com/widget/shareqq/index.html?url='+u+'&title='+t;if(item==='qzone')return 'https://sns.qzone.qq.com/cgi-bin/qzshare/cgi_qzshare_onekey?url='+u+'&title='+t;if(item==='weibo')return 'https://service.weibo.com/share/share.php?url='+u+'&title='+t;if(item==='douban')return 'https://www.douban.com/share/service?href='+u+'&name='+t;return 'mailto:?subject='+t+'&body='+u};
  document.querySelectorAll('[data-share-open]').forEach(button=>button.addEventListener('click',()=>{const dialog=document.getElementById(button.dataset.shareOpen||'');if(!(dialog instanceof HTMLDialogElement))return;const url=absolute(dialog.dataset.shareUrl);dialog.querySelectorAll('[data-share-url]').forEach(node=>{node.dataset.shareUrl=url});const input=dialog.querySelector('[data-share-copy-value]');if(input)input.value=url;dialog.showModal()}));
  document.querySelectorAll('[data-share-dialog]').forEach(dialog=>{dialog.querySelector('[data-share-close]')?.addEventListener('click',()=>dialog.close());dialog.addEventListener('click',event=>{if(event.target===dialog)dialog.close()})});
  document.querySelectorAll('[data-share-native]').forEach(button=>button.addEventListener('click',async()=>{const url=absolute(button.dataset.shareUrl),title=button.dataset.shareTitle||document.title;try{if(navigator.share)await navigator.share({title,url});else await copy(url)}catch{}}));
  document.querySelectorAll('[data-share-copy]').forEach(button=>button.addEventListener('click',async()=>{const input=button.closest('[data-share-dialog]')?.querySelector('[data-share-copy-value]');await copy(input?.value);const original=button.dataset.copyLabel||button.textContent;button.textContent=button.dataset.copiedLabel||original;setTimeout(()=>{button.textContent=original},1400)}));
  document.querySelectorAll('[data-share-wechat]').forEach(button=>button.addEventListener('click',()=>{const dialog=button.closest('[data-share-dialog]'),panel=dialog?.querySelector('[data-share-qr-panel]'),image=dialog?.querySelector('[data-share-qr]'),status=dialog?.querySelector('[data-share-qr-status]');if(!panel||!image)return;const url=absolute(button.dataset.shareUrl);image.hidden=false;if(status)status.textContent='';image.onload=()=>{image.hidden=false;if(status)status.textContent=''};image.onerror=()=>{image.hidden=true;if(status)status.textContent=dialog.dataset.shareUnavailable||''};image.src='/api/v1/public/share-qr?url='+encodeURIComponent(url);panel.hidden=false}));
  document.querySelectorAll('[data-share-external]').forEach(button=>button.addEventListener('click',()=>{const target=shareURL(button.dataset.shareExternal,button.dataset.shareUrl,button.dataset.shareTitle);if(target.startsWith('mailto:'))location.href=target;else{const opened=window.open(target,'_blank','noopener,noreferrer');if(opened)opened.opener=null}}));
  document.querySelectorAll('[data-image-link="true"]').forEach(link=>link.addEventListener('click',event=>{event.preventDefault();const dialog=document.createElement('dialog'),image=document.createElement('img'),close=document.createElement('button');dialog.className='social-image-dialog';image.src=link.href;image.alt=link.textContent||'';close.type='button';close.textContent='×';close.addEventListener('click',()=>dialog.close());dialog.addEventListener('close',()=>dialog.remove());dialog.append(close,image);document.body.append(dialog);dialog.showModal()}));
})()`;

const statisticsScript = `(() => {
  const roots = document.querySelectorAll('[data-public-stats]');
  const validCount = (value) => Number.isSafeInteger(value) && value >= 0;
  roots.forEach((root) => {
    const locale = root.dataset.statsLocale || '';
    const popular = root.querySelector('[data-popular-posts]');
    const fields = new Map(Array.from(root.querySelectorAll('[data-profile-stat]')).map((node) => [node.dataset.profileStat, node]));
    if (!popular && fields.size === 0) return;
    const unavailable = root.dataset.statisticsUnavailable || '';
    const formatter = new Intl.NumberFormat(locale || undefined);
    const fail = () => {
      if (popular) {
        const message = document.createElement('span');
        message.className = 'statistics-unavailable';
        message.textContent = unavailable;
        popular.replaceChildren(message);
      }
      fields.forEach((node) => { node.textContent = '–'; node.title = unavailable; });
    };
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 6000);
    const parameters = new URLSearchParams({ locale });
    fetch('/api/v1/public/stats?' + parameters, { credentials: 'same-origin', headers: { Accept: 'application/json' }, signal: controller.signal })
      .then((response) => {
        if (!response.ok) throw new Error('statistics request failed');
        return response.json();
      })
      .then((data) => {
        if (!data || data.locale !== locale || !data.profile) throw new Error('invalid statistics response');
        for (const key of ['posts', 'categories', 'visits']) {
          const node = fields.get(key);
          const value = data.profile[key];
          if (node && validCount(value)) { node.textContent = formatter.format(value); node.removeAttribute('title'); }
        }
        const comments = fields.get('comments');
        if (comments) {
          if (data.profile.commentsAvailable === true && validCount(data.profile.comments)) {
            comments.textContent = formatter.format(data.profile.comments);
            comments.removeAttribute('title');
          } else {
            comments.textContent = '–';
            comments.title = unavailable;
          }
        }
        if (popular) {
          const fragment = document.createDocumentFragment();
          for (const item of Array.isArray(data.popularPosts) ? data.popularPosts : []) {
            if (!item || typeof item.title !== 'string' || !validCount(item.visits) || typeof item.url !== 'string') continue;
            let target;
            try { target = new URL(item.url, location.origin); } catch { continue; }
            if (target.origin !== location.origin || !target.pathname.startsWith('/' + locale + '/posts/')) continue;
            const link = document.createElement('a');
            const title = document.createElement('span');
            const count = document.createElement('small');
            link.href = target.pathname;
            title.textContent = item.title;
            count.textContent = formatter.format(item.visits);
            count.title = root.dataset.visitsLabel || '';
            link.append(title, count);
            fragment.append(link);
          }
          popular.replaceChildren(fragment);
        }
      })
      .catch(fail)
      .finally(() => clearTimeout(timeout));
  });
})()`;

const visitScript = `(() => {
  document.querySelectorAll('[data-visit-subject]').forEach((subject) => {
    const kind = subject.dataset.visitKind || '';
    const id = subject.dataset.visitId || '';
    if (!kind || !id) return;
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);
    fetch('/api/v1/public/visits', {
      method: 'POST',
      credentials: 'same-origin',
      keepalive: true,
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ kind, id }),
      signal: controller.signal,
    }).catch(() => {}).finally(() => clearTimeout(timeout));
  });
})()`;

const upvoteScript = `(() => {
  const endpoint = '/api/v1/public/upvotes';
  const request = async (url, options) => {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);
    try {
      const response = await fetch(url, { ...options, credentials: 'same-origin', signal: controller.signal, headers: { Accept: 'application/json', ...(options?.headers || {}) } });
      if (!response.ok) throw new Error('upvote request failed');
      return await response.json();
    } finally {
      clearTimeout(timeout);
    }
  };
  document.querySelectorAll('[data-upvote]').forEach((button) => {
    const count = button.querySelector('output');
    const status = button.parentElement?.querySelector('.upvote-status');
    const kind = button.dataset.kind || '';
    const id = button.dataset.id || '';
    let busy = false;
    let requestVersion = 0;
    const update = (result) => {
      const value = Number(result?.count);
      if (!Number.isSafeInteger(value) || value < 0) throw new Error('invalid upvote response');
      count.textContent = String(value);
      const active = result?.upvoted === true;
      button.dataset.state = active ? 'active' : 'ready';
      button.setAttribute('aria-pressed', String(active));
      status.textContent = active ? (button.dataset.upvotedLabel || '') : '';
    };
    const unavailable = () => {
      button.dataset.state = 'unavailable';
      status.textContent = button.dataset.unavailableLabel || '';
    };
    const params = new URLSearchParams({ kind, id });
    const loadVersion = ++requestVersion;
    request(endpoint + '?' + params.toString())
      .then((result) => { if (loadVersion === requestVersion) update(result); })
      .catch(() => { if (loadVersion === requestVersion) unavailable(); })
      .finally(() => { if (!busy) button.disabled = false; });
    button.addEventListener('click', async () => {
      if (busy || button.dataset.state === 'active') return;
      busy = true;
      const submitVersion = ++requestVersion;
      button.disabled = true;
      status.textContent = '';
      try {
        const result = await request(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kind, id }) });
        if (submitVersion === requestVersion) update(result);
      } catch {
        if (submitVersion === requestVersion) unavailable();
      } finally {
        busy = false;
        button.disabled = false;
      }
    });
  });
})()`;

const searchScript = `(()=>{const i=document.querySelector('[data-search-input]'),r=document.querySelector('[data-search-results]');if(!i||!r)return;let d=[];fetch('./index.json').then(x=>x.json()).then(x=>d=x).catch(()=>{});i.addEventListener('input',()=>{const q=i.value.trim().toLocaleLowerCase();r.replaceChildren();if(!q)return;d.filter(x=>(x.title+' '+(x.summary||'')).toLocaleLowerCase().includes(q)).slice(0,30).forEach(x=>{const a=document.createElement('a');a.href=x.url;a.textContent=x.title;r.append(a)})})})()`;

const commentsScript = `(() => {
  const root = document.querySelector('[data-comments]');
  if (!root) return;
  const list = root.querySelector('.comment-list');
  const message = root.querySelector('.comments-loading');
  const form = root.querySelector('form');
  const output = form?.querySelector('output');
  const button = form?.querySelector('button');
  const more = root.querySelector('.comments-more');
  let page = 1;

  const appendComment = (item) => {
    const article = document.createElement('article');
    const head = document.createElement('header');
    const content = document.createElement('p');
    const name = item.author?.name || '?';
    if (root.dataset.showAvatars === 'true') {
      const avatar = document.createElement('span');
      avatar.className = 'comment-avatar';
      avatar.textContent = Array.from(name.trim())[0] || '?';
      avatar.setAttribute('aria-hidden', 'true');
      head.append(avatar);
    }
    const info = document.createElement('div');
    const author = item.author?.website ? document.createElement('a') : document.createElement('strong');
    author.className = 'comment-author';
    author.textContent = name;
    if (item.author?.website) {
      author.href = item.author.website;
      author.target = '_blank';
      author.rel = 'nofollow ugc noopener';
    }
    info.append(author);
    if (root.dataset.showTimestamps === 'true') {
      const time = document.createElement('time');
      time.dateTime = item.createdAt;
      time.textContent = new Intl.DateTimeFormat(root.dataset.locale, { dateStyle: 'short', timeStyle: 'short', timeZone: root.dataset.timezone || 'UTC' }).format(new Date(item.createdAt));
      info.append(time);
    }
    head.append(info);
    content.textContent = item.content;
    article.append(head, content);
    list.append(article);
  };

  const load = (reset = false) => {
    if (reset) {
      page = 1;
      list.replaceChildren();
    }
    if (!list.children.length) root.dataset.state = 'loading';
    if (more) more.disabled = true;
    const parameters = new URLSearchParams({ kind: root.dataset.subjectKind, id: root.dataset.subjectId, page: String(page) });
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);
    fetch('/api/v1/public/comments?' + parameters, { signal: controller.signal })
      .then((response) => {
        if (!response.ok) throw Error();
        return response.json();
      })
      .then((data) => {
        (data.items || []).forEach(appendComment);
        root.dataset.state = list.children.length ? 'ready' : 'empty';
        message.textContent = list.children.length ? '' : root.dataset.empty;
        const hasMore = Number(data.page) * Number(data.size) < Number(data.total);
        if (more) more.hidden = !hasMore;
        if (hasMore) page = Number(data.page) + 1;
      })
      .catch(() => {
        root.dataset.state = 'unavailable';
        message.textContent = root.dataset.unavailable;
      })
      .finally(() => {
        clearTimeout(timeout);
        if (more) more.disabled = false;
      });
  };

  more?.addEventListener('click', () => load(false));

  if (form && output) {
    form.addEventListener('submit', (event) => {
      event.preventDefault();
      form.dataset.state = 'submitting';
      const data = Object.fromEntries(new FormData(form));
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 5000);
      output.textContent = '…';
      if (button) button.disabled = true;
      fetch('/api/v1/public/comments', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...data, kind: root.dataset.subjectKind, id: root.dataset.subjectId, locale: root.dataset.locale }),
        signal: controller.signal,
      })
        .then(async (response) => {
          if (!response.ok) throw Error();
          const result = await response.json();
          output.textContent = result.status === 'approved' ? '' : root.dataset.pending;
          form.reset();
          load(true);
        })
        .catch(() => {
          output.textContent = root.dataset.unavailable;
        })
        .finally(() => {
          clearTimeout(timeout);
          form.dataset.state = 'idle';
          if (button) button.disabled = false;
        });
    });
  }
  load(true);
})()`;

export const earthCSS = `
:root {
  --accent: #4ccba0;
  --ink: #182037;
  --muted: #6e7688;
  --canvas: #f4f6f8;
  --card: #fff;
  --border: #e7e9ee;
  --card-radius: 1rem;
  --card-shadow: 0 5px 18px rgb(14 23 49 / 4%);
}
html[data-theme="dark"] {
  --ink: #e8ecf4;
  --muted: #a7afc1;
  --canvas: #0f172a;
  --card: #172036;
  --border: #26324a;
}
* { box-sizing: border-box; }
html { scroll-behavior: smooth; }
body {
  margin: 0;
  background: var(--canvas);
  color: var(--ink);
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, sans-serif;
  line-height: 1.65;
}
body.body-font-humanist { font-family: "Trebuchet MS", "Segoe UI", ui-sans-serif, system-ui, sans-serif; }
body.body-font-serif { font-family: ui-serif, Georgia, "Times New Roman", serif; }
a { color: inherit; text-decoration: none; }
.site-header {
  position: sticky;
  top: 0;
  z-index: 20;
  height: 4rem;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--card) 90%, transparent);
  backdrop-filter: blur(12px);
}
.site-header-static { position: relative; }
.header-inner {
  display: flex;
  max-width: 75rem;
  height: 100%;
  margin: auto;
  align-items: center;
  gap: 2rem;
  padding: 0 1.2rem;
}
.site-brand { display: flex; align-items: center; gap: .6rem; min-width: 0; }
.site-brand strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.brand-image { display: block; width: auto; max-width: 11rem; height: 2.35rem; object-fit: contain; }
.brand-symbol {
  display: grid;
  flex: 0 0 auto;
  min-width: 2rem;
  height: 2rem;
  place-items: center;
  border-radius: .55rem;
  background: #0e1731;
  color: var(--accent);
  font-weight: 800;
  padding: 0 .4rem;
}
.main-nav { display: flex; align-items: center; gap: 1.4rem; font-size: .9rem; }
.main-nav a:hover, .menu-children summary:hover { color: var(--accent); }
.menu-children { position: relative; }
.menu-children summary { cursor: pointer; list-style: none; }
.menu-children > div {
  position: absolute;
  top: 100%;
  display: flex;
  min-width: 10rem;
  flex-direction: column;
  gap: .25rem;
  border: 1px solid var(--border);
  border-radius: .7rem;
  background: var(--card);
  box-shadow: var(--card-shadow);
  padding: .55rem;
}
.menu-children > div a { padding: .3rem .45rem; }
.header-actions { display: flex; margin-left: auto; align-items: center; gap: .6rem; }
.icon-action, .locale-picker summary {
  border: 0;
  background: transparent;
  color: var(--ink);
  cursor: pointer;
  font: inherit;
  font-size: .82rem;
}
.mobile-nav { position: relative; display: none; }
.mobile-nav > summary { list-style: none; }
.mobile-nav > summary::-webkit-details-marker { display: none; }
.mobile-nav > nav {
  position: absolute;
  top: calc(100% + .8rem);
  right: 0;
  display: grid;
  width: min(18rem, calc(100vw - 2rem));
  max-height: calc(100vh - 5.5rem);
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: .75rem;
  background: var(--card);
  box-shadow: var(--card-shadow);
  padding: .65rem;
  gap: .2rem;
}
.mobile-nav > nav > a, .mobile-nav .menu-children > summary, .mobile-nav .menu-children a { display: block; padding: .55rem .65rem; }
.mobile-nav .menu-children > div { position: static; border: 0; background: transparent; box-shadow: none; padding: 0 0 0 .7rem; }
.locale-picker { position: relative; }
.locale-picker div {
  position: absolute;
  right: 0;
  display: flex;
  min-width: 8rem;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: .6rem;
  background: var(--card);
  box-shadow: var(--card-shadow);
  padding: .45rem;
}
.locale-picker div a { padding: .35rem .5rem; }
.hero {
  display: grid;
  min-height: 22rem;
  place-items: center;
  background: radial-gradient(circle at 25% 25%, color-mix(in srgb, var(--accent) 28%, transparent), transparent 35%), linear-gradient(145deg, #0e1731, #202d52);
  padding: 3rem 1.2rem;
  text-align: center;
  color: #fff;
}
.hero-compact { min-height: 15rem; }
.hero-standard { min-height: 22rem; }
.hero-tall { min-height: min(34rem, 70vh); }
.hero-kicker { color: var(--accent); font-weight: 750; letter-spacing: .16em; text-transform: uppercase; }
.hero h1 { margin: .4rem 0; font-size: clamp(2.4rem, 6vw, 4.5rem); line-height: 1.05; }
.hero p { max-width: 40rem; margin: .8rem auto; color: #d8deeb; }
.hero-latest { display: grid; width: min(66rem, 100%); align-items: center; grid-template-columns: minmax(15rem, 1fr) minmax(0, 1.25fr); gap: clamp(1.2rem, 4vw, 3rem); text-align: left; }
.hero-latest img { width: 100%; max-height: 22rem; border-radius: var(--card-radius); box-shadow: 0 20px 55px rgb(0 0 0 / 30%); object-fit: cover; }
.hero-latest p { margin-left: 0; }
.hero-post-grid { display: grid; width: min(72rem, 100%); grid-template-columns: repeat(6, minmax(0, 1fr)); gap: .8rem; }
.hero-post-grid a { display: flex; min-height: 10rem; align-items: flex-end; grid-column: span 2; border: 1px solid rgb(255 255 255 / 20%); border-radius: var(--card-radius); background: rgb(255 255 255 / 9%) center / cover; box-shadow: 0 10px 30px rgb(0 0 0 / 20%); padding: 1rem; text-align: left; }
.hero-post-grid a:first-child, .hero-post-grid a:nth-child(2) { grid-column: span 3; }
.hero-post-grid strong { text-shadow: 0 1px 5px rgb(0 0 0 / 55%); }
.taxonomy-hero { min-height: 13rem; }
.earth-layout {
  display: grid;
  max-width: 75rem;
  margin: 2rem auto;
  grid-template-columns: minmax(0, 1fr) 18rem;
  gap: 1.5rem;
  padding: 0 1.2rem;
}
.earth-layout.sidebar-left { grid-template-columns: 18rem minmax(0, 1fr); }
.earth-layout.sidebar-left > .post-grid, .earth-layout.sidebar-left > .article-shell, .earth-layout.sidebar-left > .list-content { order: 2; }
.earth-layout.sidebar-left > .sidebar-card { order: 1; }
.earth-layout-no-sidebar { grid-template-columns: minmax(0, 1fr); }
.list-content { display: grid; min-width: 0; align-content: start; gap: 1.2rem; }
.list-heading { display: flex; align-items: center; gap: 1rem; border-radius: var(--card-radius); background: var(--card); box-shadow: var(--card-shadow); padding: 1.1rem 1.25rem; }
.list-heading h1 { margin: 0; font-size: clamp(1.5rem, 3vw, 2.15rem); }
.list-heading p { margin: .35rem 0 0; color: var(--muted); }
.taxonomy-heading > img { width: 5rem; height: 5rem; flex: 0 0 auto; border-radius: min(var(--card-radius), .8rem); object-fit: cover; }
.taxonomy-filter { min-width: 0; }
.taxonomy-filter > ul { display: flex; min-width: 0; flex-wrap: wrap; gap: .45rem; margin: 0; padding: 0; list-style: none; }
.taxonomy-filter li { list-style: none; }
.taxonomy-filter li > div { display: flex; align-items: stretch; }
.taxonomy-filter a { display: inline-flex; min-height: 2.25rem; align-items: center; gap: .25rem; border-radius: .5rem; color: var(--muted); padding: .4rem .75rem; font-weight: 650; }
.taxonomy-filter a:hover, .taxonomy-filter a[aria-current="page"] { background: color-mix(in srgb, var(--accent) 13%, var(--card)); color: var(--ink); box-shadow: 0 2px 8px rgb(14 23 49 / 6%); }
.taxonomy-filter details { position: relative; }
.taxonomy-filter summary { display: grid; min-width: 2rem; height: 100%; place-items: center; border-radius: .45rem; color: var(--muted); cursor: pointer; list-style: none; }
.taxonomy-filter summary::-webkit-details-marker { display: none; }
.taxonomy-filter summary:hover, .taxonomy-filter details[open] > summary { background: color-mix(in srgb, var(--accent) 13%, var(--card)); color: var(--ink); }
.taxonomy-filter details > ul { position: absolute; top: calc(100% + .35rem); left: 0; z-index: 12; display: grid; width: max-content; min-width: 12rem; max-width: min(22rem, calc(100vw - 2rem)); gap: .25rem; margin: 0; border: 1px solid var(--border); border-radius: .65rem; background: var(--card); box-shadow: 0 14px 35px rgb(14 23 49 / 16%); padding: .45rem; }
.taxonomy-filter details > ul .taxonomy-filter-branch details > ul { top: 0; left: calc(100% + .35rem); }
.taxonomy-filter-tags { display: flex; flex-wrap: wrap; gap: .45rem; border-radius: var(--card-radius); background: var(--card); box-shadow: var(--card-shadow); padding: .8rem; }
.taxonomy-filter-tags a { border: 1px solid var(--border); border-radius: 999px; }
.taxonomy-filter sup { color: var(--muted); font-size: .62rem; }
.empty-list { margin: 1.5rem 0; color: var(--muted); text-align: center; }
.more-posts { display: inline-flex; margin-left: auto; align-items: center; gap: .4rem; color: var(--muted); font-size: .86rem; }
.more-posts:hover { color: var(--ink); }
.post-grid { display: grid; gap: 1.2rem; }
.post-grid-grid-3 { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.post-grid-grid-2 { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.post-grid-single { grid-template-columns: minmax(0, 1fr); }
.post-card, .sidebar-card, .article-card {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--card-radius);
  background: var(--card);
  box-shadow: var(--card-shadow);
}
.cards-borderless .post-card, .cards-borderless .sidebar-card, .cards-borderless .article-card { border-color: transparent; }
.post-card { transition: transform .2s ease, box-shadow .2s ease; }
.post-card:hover { transform: translateY(-3px); box-shadow: 0 12px 30px rgb(14 23 49 / 9%); }
.post-cover { display: block; overflow: hidden; }
.post-cover-wide { aspect-ratio: 21 / 9; }
.post-cover-landscape { aspect-ratio: 16 / 9; }
.post-cover-square { aspect-ratio: 1; }
.post-cover img { width: 100%; height: 100%; object-fit: cover; transition: transform .3s ease; }
.post-card:hover .post-cover img { transform: scale(1.03); }
.post-card-body { padding: 1.2rem; }
.post-taxonomy { display: flex; flex-wrap: wrap; justify-content: center; gap: .5rem; color: var(--accent); font-size: .72rem; font-weight: 650; }
.post-card .post-taxonomy { justify-content: flex-start; }
.post-card h2 { margin: .5rem 0; font-size: 1.3rem; line-height: 1.35; }
.post-card p {
  display: -webkit-box;
  overflow: hidden;
  margin: .5rem 0;
  color: var(--muted);
  font-size: .9rem;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}
.post-meta { display: flex; justify-content: space-between; margin-top: 1rem; color: var(--muted); font-size: .75rem; }
.sidebar-card { align-self: start; padding: 1.2rem; }
.sidebar-card section + section { margin-top: 1rem; border-top: 1px solid var(--border); padding-top: 1rem; }
.sidebar-card h3 { margin: .3rem 0; }
.sidebar-card p { color: var(--muted); font-size: .85rem; }
.sidebar-card a { display: block; padding: .25rem 0; color: var(--muted); }
.sidebar-sticky { position: sticky; top: 5.25rem; }
.table-of-contents h3 { display: flex; align-items: center; gap: .4rem; }
.table-of-contents h3::before { content: "☷"; color: var(--accent); }
.table-of-contents ol { display: grid; gap: .18rem; max-height: 16rem; margin: .65rem 0 0; overflow: auto; padding: 0; list-style: none; }
.table-of-contents li { min-width: 0; }
.table-of-contents li.toc-level-2 { padding-left: .7rem; }
.table-of-contents li.toc-level-3 { padding-left: 1.4rem; }
.table-of-contents li.toc-level-4 { padding-left: 2.1rem; }
.table-of-contents a { overflow: hidden; border-radius: .45rem; color: var(--muted); font-size: .84rem; text-overflow: ellipsis; white-space: nowrap; }
.table-of-contents a:hover { background: color-mix(in srgb, var(--accent) 12%, transparent); color: var(--ink); }
.sidebar-profile-plain { border-color: transparent; background: transparent; box-shadow: none; }
.sidebar-profile > img { display: block; width: 4rem; height: 4rem; margin-bottom: .7rem; border-radius: 50%; object-fit: cover; }
.profile-stats { display: grid; margin: .85rem 0; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: .35rem; }
.profile-stats > div { min-width: 0; text-align: center; }
.profile-stats dt { overflow: hidden; color: var(--muted); font-size: .68rem; text-overflow: ellipsis; white-space: nowrap; }
.profile-stats dd { margin: .1rem 0 0; color: var(--ink); font-size: .9rem; font-variant-numeric: tabular-nums; font-weight: 750; }
.sidebar-post-list > div { display: grid; }
.sidebar-post-list a { display: flex; min-width: 0; justify-content: space-between; gap: .65rem; }
.sidebar-post-list a > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sidebar-post-list a > small { flex: 0 0 auto; color: var(--muted); font-size: .68rem; font-variant-numeric: tabular-nums; }
.statistics-loading, .statistics-unavailable { color: var(--muted); font-size: .8rem; }
.sidebar-tags { display: flex; flex-wrap: wrap; gap: .35rem; }
.sidebar-tags a { display: inline-flex; border-radius: 999px; background: color-mix(in srgb, var(--accent) 12%, transparent); padding: .18rem .5rem; }
.pagination { display: grid; min-width: 0; align-items: center; grid-template-columns: auto minmax(0, 1fr) auto; gap: .75rem; }
.pagination > a, .pagination > span, .pagination-pages > a, .pagination-pages > strong { display: inline-flex; min-width: 2.25rem; min-height: 2.25rem; align-items: center; justify-content: center; border: 1px solid var(--border); border-radius: .5rem; background: var(--card); color: var(--ink); padding: .35rem .65rem; font-size: .82rem; }
.pagination > span[aria-disabled="true"] { color: var(--muted); opacity: .55; }
.pagination-pages { display: flex; min-width: 0; justify-content: center; gap: .35rem; overflow-x: auto; padding: .15rem; }
.pagination-pages > strong { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 16%, var(--card)); color: var(--accent); }
.article-shell { max-width: 62rem; margin: 2rem auto; padding: 0 1.2rem; }
.earth-layout > .article-shell { width: 100%; margin: 0; padding: 0; }
.earth-layout-no-sidebar > .article-shell { margin-inline: auto; }
.article-width-narrow { max-width: 49rem; }
.article-width-standard { max-width: 62rem; }
.article-width-wide { max-width: 76rem; }
.article-card { padding: clamp(1.2rem, 5vw, 3.5rem); }
.article-card > header { text-align: center; }
.article-card h1 { margin: .7rem 0; font-size: clamp(2rem, 5vw, 3.2rem); line-height: 1.15; }
.article-card header p, .article-card time { color: var(--muted); }
.article-cover { width: 100%; margin: 2rem 0; border-radius: min(var(--card-radius), .8rem); }
.content-cover { display: grid; place-items: center; background-position: center; background-size: cover; color: #fff; padding: 2rem 1.2rem; text-align: center; }
.content-cover-compact { min-height: 16rem; }
.content-cover-standard { min-height: 24rem; }
.content-cover-tall { min-height: min(34rem, 72vh); }
.content-cover header { width: min(58rem, 100%); text-shadow: 0 2px 14px rgb(0 0 0 / 55%); }
.content-cover h1 { margin: .6rem 0; font-size: clamp(2rem, 6vw, 4rem); line-height: 1.12; }
.content-cover p { max-width: 42rem; margin: .6rem auto; }
.content-cover .post-taxonomy { color: #fff; }
.markdown-body { min-width: 0; font-size: 1.03rem; }
.markdown-font-serif { font-family: ui-serif, Georgia, "Times New Roman", serif; }
.markdown-font-sans { font-family: Inter, ui-sans-serif, system-ui, -apple-system, sans-serif; }
.markdown-body a { color: color-mix(in srgb, var(--accent) 82%, var(--ink)); text-decoration: underline; text-underline-offset: .16em; }
.markdown-body img { max-width: 100%; height: auto; border-radius: .6rem; }
.markdown-body pre { overflow: auto; border-radius: .6rem; background: #0e1731; color: #e9eef8; padding: 1rem; }
.markdown-body :not(pre) > code { border-radius: .3rem; background: color-mix(in srgb, var(--accent) 12%, var(--card)); padding: .12em .32em; }
.markdown-body blockquote { margin-left: 0; border-left: 4px solid var(--accent); padding-left: 1rem; color: var(--muted); }
.markdown-body table { display: block; max-width: 100%; overflow-x: auto; border-collapse: collapse; }
.markdown-body th, .markdown-body td { border: 1px solid var(--border); padding: .45rem .65rem; }
.markdown-body hr { border: 0; border-top: 1px solid var(--border); margin: 2rem 0; }
.markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4 { scroll-margin-top: 5.5rem; }
.markdown-style-typography, .markdown-style-tailwind { font-size: 1.08rem; line-height: 1.82; }
.markdown-style-typography h2, .markdown-style-typography h3, .markdown-style-tailwind h2, .markdown-style-tailwind h3 { margin-top: 2.2em; letter-spacing: -.02em; }
.post-cursor { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 1rem; margin-top: 2.4rem; border-top: 1px solid var(--border); padding-top: 1.35rem; }
.post-cursor > a { display: flex; min-width: 0; align-items: center; gap: .65rem; color: var(--muted); }
.post-cursor > a:hover { color: var(--accent); }
.post-cursor > a > span:first-child, .post-cursor-next > span:last-child { flex: 0 0 auto; font-size: 1.2rem; }
.post-cursor-previous > span:last-child, .post-cursor-next > span:first-child { display: grid; min-width: 0; }
.post-cursor small { color: var(--muted); font-size: .7rem; font-weight: 650; text-transform: uppercase; }
.post-cursor-previous { text-align: left; }
.post-cursor-next { justify-content: flex-end; text-align: right; }
.post-cursor-previous > span:last-child, .post-cursor-next > span:first-child { overflow: hidden; font-weight: 700; text-overflow: ellipsis; white-space: nowrap; }
.article-tags { display: flex; flex-wrap: wrap; gap: .5rem; margin-top: 2rem; }
.article-tags a { border-radius: 999px; background: color-mix(in srgb, var(--accent) 13%, transparent); color: var(--accent); padding: .25rem .65rem; font-size: .8rem; }
.upvote-action { display: flex; min-height: 2.4rem; align-items: center; gap: .75rem; margin-top: 1.5rem; }
.upvote-action button { display: inline-flex; align-items: center; gap: .45rem; border: 1px solid var(--border); border-radius: 999px; background: var(--card); color: var(--ink); cursor: pointer; font: inherit; font-weight: 700; padding: .42rem .85rem; }
.upvote-action button:hover, .upvote-action button[data-state="active"] { border-color: var(--accent); color: var(--accent); }
.upvote-action button:disabled { cursor: wait; opacity: .65; }
.upvote-action output { min-width: 1ch; font-variant-numeric: tabular-nums; }
.upvote-icon { transition: transform .16s ease; }
.upvote-action button[data-state="active"] .upvote-icon { transform: scale(1.15); }
.upvote-status { color: var(--muted); font-size: .8rem; }
.share-entry { margin-top: 1.25rem; }
.share-trigger { display: inline-flex; align-items: center; gap: .45rem; border: 1px solid var(--border); border-radius: 999px; background: var(--card); color: var(--ink); cursor: pointer; font: inherit; font-weight: 700; padding: .42rem .85rem; }
.share-trigger:hover { border-color: var(--accent); color: var(--accent); }
.share-dialog { width: min(34rem, calc(100vw - 2rem)); border: 0; border-radius: var(--card-radius); background: var(--card); box-shadow: 0 24px 80px rgb(0 0 0 / 38%); color: var(--ink); padding: 1.1rem; }
.share-dialog::backdrop { background: rgb(5 10 22 / 70%); backdrop-filter: blur(4px); }
.share-dialog > header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; }
.share-dialog > header > button { border: 0; background: transparent; color: var(--ink); cursor: pointer; font-size: 1.45rem; }
.share-actions { display: flex; flex-wrap: wrap; gap: .5rem; margin-top: 1rem; }
.share-actions a, .share-actions button { border: 1px solid var(--border); border-radius: 999px; background: var(--card); color: var(--ink); cursor: pointer; font: inherit; padding: .35rem .75rem; }
.share-actions a:hover, .share-actions button:hover { border-color: var(--accent); color: var(--accent); }
.share-copy-row { display: flex; margin-top: 1rem; gap: .45rem; }
.share-copy-row input { min-width: 0; flex: 1; border: 1px solid var(--border); border-radius: .55rem; background: var(--surface); color: var(--ink); padding: .55rem .65rem; }
.share-copy-row button { flex: 0 0 auto; border: 0; border-radius: .55rem; background: var(--accent); color: #06120f; cursor: pointer; font: inherit; font-weight: 750; padding: .55rem .8rem; }
.share-qr { display: grid; margin-top: 1rem; place-items: center; gap: .6rem; }
.share-qr[hidden] { display: none; }
.share-qr img { width: 15rem; max-width: 100%; aspect-ratio: 1; border-radius: .5rem; background: #fff; object-fit: contain; }
.share-qr output { color: var(--muted); font-size: .8rem; text-align: center; }
.comments { margin-top: 3rem; border-top: 1px solid var(--border); padding-top: 1.75rem; }
.comments > h2 { margin: 0 0 1rem; font-size: 1.45rem; }
.comments-loading, .comments-closed { color: var(--muted); font-size: .9rem; }
.comments[data-state="loading"] .comments-loading { min-height: 1.5rem; animation: comments-pulse 1.2s ease-in-out infinite alternate; }
.comments[data-state="empty"] .comments-loading { border: 1px dashed var(--border); border-radius: .7rem; padding: .85rem; text-align: center; }
.comments[data-state="unavailable"] .comments-loading { border: 1px solid color-mix(in srgb, #e34b4b 34%, var(--border)); border-radius: .7rem; background: color-mix(in srgb, #e34b4b 7%, var(--card)); color: color-mix(in srgb, #e34b4b 78%, var(--ink)); padding: .85rem; }
.comment-list { display: grid; gap: .85rem; }
.comment-list article { border: 1px solid var(--border); border-radius: min(var(--card-radius), .8rem); background: color-mix(in srgb, var(--card) 96%, var(--canvas)); padding: 1rem; }
.comment-list article > header { display: flex; align-items: center; gap: .7rem; text-align: left; }
.comment-list article > header > div { display: flex; min-width: 0; flex-direction: column; }
.comment-author { overflow: hidden; color: var(--ink); font-weight: 700; text-overflow: ellipsis; white-space: nowrap; }
a.comment-author:hover { color: var(--accent); }
.comment-list time { color: var(--muted); font-size: .72rem; }
.comment-list article > p { margin: .7rem 0 0; color: var(--ink); white-space: pre-wrap; overflow-wrap: anywhere; }
.comment-avatar { display: grid; width: 2.35rem; height: 2.35rem; flex: 0 0 auto; place-items: center; border-radius: 50%; background: color-mix(in srgb, var(--accent) 18%, var(--card)); color: var(--accent); font-weight: 800; }
.comments-more { display: block; margin: 1rem auto 0; border: 1px solid var(--border); border-radius: .65rem; background: var(--card); color: var(--ink); cursor: pointer; font: inherit; padding: .55rem 1rem; }
.comments-more[hidden] { display: none; }
.comments-more:disabled { cursor: wait; opacity: .65; }
.comment-form { display: grid; gap: .75rem; margin-top: 1.25rem; }
.comment-form-two-column { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.comment-form input, .comment-form textarea {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: .65rem;
  outline: none;
  background: var(--card);
  color: var(--ink);
  font: inherit;
  padding: .72rem .82rem;
}
.comment-form input:focus, .comment-form textarea:focus { border-color: var(--accent); box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent); }
.comment-form textarea, .comment-form-actions { grid-column: 1 / -1; }
.comment-form textarea { min-height: 8rem; resize: vertical; }
.comment-form-actions { display: flex; align-items: center; gap: .85rem; }
.comment-form button {
  border: 0;
  border-radius: .65rem;
  background: var(--accent);
  color: #081a17;
  cursor: pointer;
  font: inherit;
  font-weight: 750;
  padding: .65rem 1rem;
}
.comment-form button:disabled { cursor: wait; opacity: .65; }
.comment-form[data-state="submitting"] input, .comment-form[data-state="submitting"] textarea { opacity: .7; }
.comment-form output { color: var(--muted); font-size: .85rem; }
.search-card input[type="search"] { width: 100%; border: 1px solid var(--border); border-radius: .7rem; background: var(--card); color: var(--ink); font: inherit; padding: .8rem 1rem; }
.search-card [data-search-results] { display: grid; margin-top: .8rem; }
.search-card [data-search-results] a { border-bottom: 1px solid var(--border); padding: .65rem 0; }
.archive-list > h1 { margin-top: 0; }
.archive-group { display: grid; grid-template-columns: minmax(7rem, 10rem) minmax(0, 1fr); gap: 1rem; }
.archive-group + .archive-group { margin-top: 1.4rem; }
.archive-group > h2 { position: sticky; top: 5.25rem; align-self: start; margin: .45rem 0 0; font-size: 1rem; }
.archive-group > div { display: grid; gap: .65rem; }
.archive-group article { border: 1px solid var(--border); border-radius: min(var(--card-radius), .8rem); background: color-mix(in srgb, var(--card) 96%, var(--canvas)); padding: .9rem 1rem; }
.archive-post-heading { display: flex; min-width: 0; align-items: baseline; justify-content: space-between; gap: 1rem; }
.archive-post-heading h3 { min-width: 0; margin: 0; font-size: 1.05rem; }
.archive-post-heading h3 a { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.archive-post-heading time { flex: 0 0 auto; font-size: .72rem; }
.archive-taxonomy { display: flex; flex-wrap: wrap; gap: .5rem; margin-top: .35rem; }
.archive-taxonomy a { color: var(--muted); font-size: .74rem; }
.archive-group article > p { display: -webkit-box; overflow: hidden; margin: .45rem 0 0; color: var(--muted); font-size: .82rem; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.link-groups { display: grid; gap: 1.2rem; }
.link-card img { width: 2.25rem; height: 2.25rem; object-fit: cover; border-radius: .45rem; }
.not-found { text-align: center; }
.not-found > strong { color: var(--accent); font-size: clamp(4rem, 15vw, 9rem); line-height: 1; }
.site-footer {
  display: flex;
  max-width: 75rem;
  margin: 3rem auto 0;
  justify-content: space-between;
  gap: 1.5rem;
  border-top: 1px solid var(--border);
  padding: 2rem 1.2rem;
  color: var(--muted);
  font-size: .8rem;
}
.site-footer-borderless { border-top-color: transparent; }
.site-footer > div { display: flex; flex-direction: column; }
.site-footer p { margin: 0; }
.site-footer-centered { flex-direction: column; align-items: center; text-align: center; }
.footer-logo { width: auto; max-width: 10rem; height: 2.8rem; margin-bottom: .55rem; object-fit: contain; }
.footer-slogan { max-width: 30rem; margin: .2rem 0; color: var(--ink); font-size: .9rem; white-space: pre-line; }
.footer-menus { display: flex; flex: 1; justify-content: center; gap: clamp(1rem, 4vw, 3rem); }
.footer-menus nav { display: flex; min-width: 7rem; flex-direction: column; gap: .25rem; }
.footer-menus nav > strong { color: var(--ink); }
.footer-menus .menu-children > summary { display: none; }
.footer-menus .menu-children > div { position: static; border: 0; background: transparent; box-shadow: none; padding: 0; }
.footer-meta { align-items: flex-end; }
.site-footer-centered .footer-meta { align-items: center; }
.social-links { display: flex; flex-wrap: wrap; gap: .55rem; }
.social-links a { display: inline-flex; align-items: center; gap: .3rem; color: var(--ink); }
.social-links a > span { display: inline-grid; min-width: 1.35rem; height: 1.35rem; place-items: center; border-radius: 50%; background: color-mix(in srgb, var(--accent) 14%, transparent); color: var(--accent); font-size: .65rem; font-weight: 800; }
.social-image-dialog { position: relative; width: min(42rem, calc(100vw - 2rem)); border: 0; border-radius: var(--card-radius); background: var(--card); box-shadow: 0 24px 80px rgb(0 0 0 / 35%); padding: 2.4rem 1rem 1rem; }
.social-image-dialog::backdrop { background: rgb(5 10 22 / 70%); backdrop-filter: blur(4px); }
.social-image-dialog img { display: block; width: 100%; max-height: calc(100vh - 6rem); object-fit: contain; }
.social-image-dialog button { position: absolute; top: .45rem; right: .55rem; width: 1.8rem; height: 1.8rem; border: 0; background: transparent; color: var(--ink); cursor: pointer; font-size: 1.45rem; line-height: 1; }
.footer-copyright { margin-top: .25rem; }
.scroll-top { position: fixed; right: 1.2rem; bottom: 1.2rem; z-index: 15; display: grid; width: 2.6rem; height: 2.6rem; place-items: center; border: 1px solid var(--border); border-radius: 50%; background: var(--card); box-shadow: var(--card-shadow); color: var(--ink); }
@keyframes comments-pulse { from { opacity: .5; } to { opacity: 1; } }
@media (max-width: 1100px) {
  .post-grid-grid-3 { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 850px) {
  .main-nav { display: none; }
  .mobile-nav { display: block; }
  .earth-layout, .earth-layout.sidebar-left { grid-template-columns: 1fr; }
  .earth-layout.sidebar-left > .post-grid, .earth-layout.sidebar-left > .article-shell, .earth-layout.sidebar-left > .list-content, .earth-layout.sidebar-left > .sidebar-card { order: initial; }
  .sidebar-card { display: none; }
  .sidebar-card.sidebar-has-toc { display: block; }
  .sidebar-card.sidebar-has-toc > :not(.table-of-contents) { display: none; }
  .post-grid-grid-3, .post-grid-grid-2 { grid-template-columns: 1fr; }
  .hero-latest { grid-template-columns: 1fr; text-align: center; }
  .hero-latest p { margin-left: auto; }
  .hero-post-grid { grid-template-columns: 1fr; }
  .hero-post-grid a, .hero-post-grid a:first-child, .hero-post-grid a:nth-child(2) { grid-column: auto; }
  .footer-menus { width: 100%; flex-wrap: wrap; justify-content: flex-start; }
	.taxonomy-filter details > ul .taxonomy-filter-branch details > ul { top: calc(100% + .35rem); left: 0; }
}
@media (max-width: 560px) {
  .header-inner { gap: .75rem; }
  .comment-form-two-column { grid-template-columns: 1fr; }
  .site-footer { flex-direction: column; }
  .footer-meta { align-items: flex-start; }
  .scroll-top { right: .75rem; bottom: .75rem; }
	.archive-group { grid-template-columns: 1fr; }
	.archive-group > h2 { position: static; }
	.archive-post-heading { align-items: flex-start; flex-direction: column; gap: .2rem; }
	.pagination > a span, .pagination > span span { display: none; }
	.taxonomy-heading { align-items: flex-start; }
	.post-cursor { grid-template-columns: 1fr; gap: .8rem; }
	.post-cursor-next { justify-content: flex-start; text-align: left; }
	.post-cursor-next > span:first-child { order: 2; }
}
@media (prefers-reduced-motion: reduce) {
  html { scroll-behavior: auto; }
  *, *::before, *::after { scroll-behavior: auto !important; transition-duration: .01ms !important; animation-duration: .01ms !important; animation-iteration-count: 1 !important; }
}
`;

export type { ThemeContext, ThemeHeading, ThemeLink, ThemeLinkGroup, ThemeMenu, ThemeMenuItem, ThemePagination, ThemePaginationPage, ThemePost, ThemePostCursor, ThemeSite, ThemeTaxonomyCollection, ThemeTaxonomyCollections, ThemeTaxonomyKind, ThemeTaxonomySummary } from "./types.js";
