import { cp, mkdir, readFile, readdir, rm, stat, writeFile } from "node:fs/promises";
import { dirname, join, resolve, sep } from "node:path";
import { pathToFileURL } from "node:url";
import { renderToStaticMarkup } from "react-dom/server";
import { earthCSS, renderArchive, renderIndex, renderLinks, renderNotFound, renderPage, renderPost, renderSearch, renderTaxonomy, type ThemeContext, type ThemeLinkGroup, type ThemeMenuItem, type ThemePost } from "@mutiblog/theme-earth";
import { renderMarkdown } from "@mutiblog/markdown";
import type { BuildInput, BuildReport, LocalizedPostInput, RedirectRecord } from "./types.js";

export async function buildSite(input: BuildInput, outputDirectory: string): Promise<BuildReport> {
  validateInput(input);
	const theme = await loadTheme(input);
  const output = resolve(outputDirectory);
  await rm(output, { recursive: true, force: true });
  await mkdir(output, { recursive: true });
  const redirects: RedirectRecord[] = [];
  let files = 0;

  await emit(output, "assets/theme.css", theme.css);
  files += 1;
	if (input.theme?.assetsPath) {
		const assetTarget = join(output, "assets", "themes", input.theme.id);
		await cp(input.theme.assetsPath, assetTarget, { recursive: true, errorOnExist: true });
		files += await countFiles(assetTarget);
	}

  for (const localeDefinition of input.locales) {
    const locale = localeDefinition.code;
    const siteCopy = selectSite(input, locale);
    const localizedPosts: ThemePost[] = [];
	const localizedPages: ThemePost[] = [];
    for (const post of input.posts.filter((candidate) => candidate.status === "published")) {
      const selection = selectPost(input, post.locales, locale, post.sourceLocale);
      if (!selection) continue;
      const themedPost = toThemePost(input, post, selection.value, locale);
      localizedPosts.push(themedPost);
      if (selection.locale !== locale) {
        redirects.push({ from: `/${locale}/posts/${post.id}/`, to: `/${selection.locale}/posts/${post.id}/`, status: 302 });
        await emit(output, `${locale}/posts/${post.id}/index.html`, redirectHTML(`/${selection.locale}/posts/${post.id}/`, dictionary(input, locale).redirecting, locale));
        files += 1;
        continue;
      }
      const context = makeContext(input, locale, siteCopy, localizedPosts, `/posts/${post.id}/`, themedPost, exactLocaleCodes(input, post.locales));
      await emit(output, `${locale}/posts/${post.id}/index.html`, doctype(renderToStaticMarkup(contentRenderer(theme, "post", post.template, input.theme?.postTemplates)(context))));
      files += 1;
    }

    for (const page of (input.pages ?? []).filter((candidate) => candidate.status === "published")) {
      const selection = selectPost(input, page.locales, locale, page.sourceLocale);
      if (!selection) continue;
	  const themedPage = toThemePost(input, page, selection.value, locale);
	  localizedPages.push(themedPage);
      if (selection.locale !== locale) {
        redirects.push({ from: `/${locale}/pages/${page.id}/`, to: `/${selection.locale}/pages/${page.id}/`, status: 302 });
		await emit(output, `${locale}/pages/${page.id}/index.html`, redirectHTML(`/${selection.locale}/pages/${page.id}/`, dictionary(input, locale).redirecting, locale));
        files += 1;
        continue;
      }
      const context = makeContext(input, locale, siteCopy, localizedPosts, `/pages/${page.id}/`, themedPage, exactLocaleCodes(input, page.locales));
      await emit(output, `${locale}/pages/${page.id}/index.html`, doctype(renderToStaticMarkup(contentRenderer(theme, "page", page.template, input.theme?.pageTemplates)(context))));
      files += 1;
    }

    for (const [kind, taxonomies] of [["categories", input.categories ?? []], ["tags", input.tags ?? []]] as const) {
      for (const item of taxonomies) {
        const selection = selectPost(input, item.locales, locale, item.sourceLocale);
        if (!selection) continue;
        if (selection.locale !== locale) {
          redirects.push({ from: `/${locale}/${kind}/${item.id}/`, to: `/${selection.locale}/${kind}/${item.id}/`, status: 302 });
          await emit(output, `${locale}/${kind}/${item.id}/index.html`, redirectHTML(`/${selection.locale}/${kind}/${item.id}/`, dictionary(input, locale).redirecting, locale));
          files += 1;
          continue;
        }
        const posts = localizedPosts.filter((post) => (kind === "categories" ? post.categories : post.tags)?.some(({ id }) => id === item.id));
        const context = {
			...makeContext(input, locale, siteCopy, posts, `/${kind}/${item.id}/`, undefined, exactLocaleCodes(input, item.locales)),
			pageTitle: selection.value.seoTitle || selection.value.name,
			pageDescription: selection.value.seoDescription || selection.value.description,
		};
        await emit(output, `${locale}/${kind}/${item.id}/index.html`, doctype(renderToStaticMarkup(theme.renderTaxonomy(context, selection.value.name, selection.value.description))));
        files += 1;
      }
    }

    const linkGroups: ThemeLinkGroup[] = (input.linkGroups ?? []).flatMap((group) => {
      const selectedGroup = selectPost(input, group.locales, locale, group.sourceLocale);
      if (!selectedGroup) return [];
      const links = (input.links ?? []).filter((link) => link.groupId === group.id).flatMap((link) => {
        const selected = selectPost(input, link.locales, locale, link.sourceLocale);
        return selected ? [{ id: link.id, name: selected.value.name, description: selected.value.description, url: link.url, logo: link.logo }] : [];
      });
      return [{ id: group.id, name: selectedGroup.value.name, description: selectedGroup.value.description, links }];
    });
    const linksContext = makeContext(input, locale, siteCopy, localizedPosts, "/links/");
    await emit(output, `${locale}/links/index.html`, doctype(renderToStaticMarkup(theme.renderLinks(linksContext, linkGroups))));
    files += 1;

    const indexContext = makeContext(input, locale, siteCopy, localizedPosts, "/");
    const archiveContext = makeContext(input, locale, siteCopy, localizedPosts, "/archives/");
    const searchContext = makeContext(input, locale, siteCopy, localizedPosts, "/search/");
    await emit(output, `${locale}/index.html`, doctype(renderToStaticMarkup(theme.renderIndex(indexContext))));
    await emit(output, `${locale}/archives/index.html`, doctype(renderToStaticMarkup(theme.renderArchive(archiveContext))));
    await emit(output, `${locale}/search/index.html`, doctype(renderToStaticMarkup(theme.renderSearch(searchContext))));
    await emit(output, `${locale}/search/index.json`, JSON.stringify([
	  ...localizedPosts.map(({ id, title, summary }) => ({ kind: "Post", id, title, summary, url: `/${locale}/posts/${id}/` })),
	  ...localizedPages.map(({ id, title, summary }) => ({ kind: "Page", id, title, summary, url: `/${locale}/pages/${id}/` })),
	]));
    await emit(output, `${locale}/rss.xml`, rss(input, locale, siteCopy, localizedPosts));
	const notFoundContext = makeContext(input, locale, siteCopy, localizedPosts, "/404.html");
	const notFound = theme.renderNotFound ? theme.renderNotFound(notFoundContext) : basicPage(siteCopy.title, dictionary(input, locale).notFound ?? "Not found");
    await emit(output, `${locale}/404.html`, typeof notFound === "string" ? ensureDoctype(notFound) : doctype(renderToStaticMarkup(notFound)));
    files += 6;
  }

  const rootTarget = `/${input.sourceLocale}/`;
  redirects.unshift({ from: "/", to: rootTarget, status: 302 });
  await emit(output, "index.html", redirectHTML(rootTarget, dictionary(input, input.sourceLocale).redirecting, input.sourceLocale));
  await emit(output, "redirects.json", JSON.stringify(redirects, null, 2));
  await emit(output, "robots.txt", `User-agent: *\nAllow: /\nSitemap: ${input.baseUrl ? input.baseUrl.replace(/\/$/, "") : ""}/sitemap.xml\n`);
  await emit(output, "sitemap.xml", sitemap(input));
  files += 4;

  const report: BuildReport = { schemaVersion: 1, generatedAt: new Date().toISOString(), files, locales: input.locales.map((locale) => locale.code), redirects };
  await emit(output, "build-report.json", JSON.stringify(report, null, 2));
  return { ...report, files: files + 1 };
}

export async function buildFromFile(inputFile: string, outputDirectory: string) {
  const input = JSON.parse(await readFile(inputFile, "utf8")) as BuildInput;
  return buildSite(input, outputDirectory);
}

function makeContext(input: BuildInput, locale: string, siteCopy: LocalizedSiteInput, posts: ThemePost[], currentPath: string, post?: ThemePost, availableLocales = input.locales.map(({ code }) => code)): ThemeContext {
  const baseUrl = input.baseUrl?.replace(/\/$/, "");
  const enabled = new Set(availableLocales);
  const alternates = input.locales
    .filter(({ code }) => enabled.has(code))
    .map(({ code, label }) => ({ locale: code, label, href: `${baseUrl ?? ""}/${code}${currentPath}` }));
  return {
    site: { ...siteCopy, locale, sourceLocale: input.sourceLocale, locales: input.locales, baseUrl },
    currentPath,
    posts,
    post,
    pageTitle: post?.seoTitle || post?.title,
    pageDescription: post?.seoDescription || post?.summary,
    canonicalUrl: `${baseUrl ?? ""}/${locale}${currentPath}`,
    alternates,
    strings: dictionary(input, locale),
    settings: input.theme?.settings,
    navigation: makeNavigation(input, locale),
  };
}

function exactLocaleCodes(input: BuildInput, locales: Record<string, unknown>) {
  return input.locales.map(({ code }) => code).filter((code) => Boolean(locales[code]));
}

function makeNavigation(input: BuildInput, locale: string): ThemeMenuItem[] {
  const menu = input.menus?.find((candidate) => candidate.id === input.primaryMenu) ?? input.menus?.[0];
  if (!menu) return [];
  const nodes = new Map<string, ThemeMenuItem>();
  for (const item of [...menu.items].sort((a, b) => a.order - b.order)) {
    const selected = selectPost(input, item.locales, locale, menu.sourceLocale);
    if (!selected) continue;
    const href = item.targetKind === "internal" ? `/${locale}${item.url === "/" ? "/" : item.url}` : item.url;
    nodes.set(item.id, { id: item.id, label: selected.value.label, href, openInNew: item.openInNew, children: [] });
  }
  const roots: ThemeMenuItem[] = [];
  for (const item of [...menu.items].sort((a, b) => a.order - b.order)) {
    const node = nodes.get(item.id); if (!node) continue;
    const parent = item.parentId && nodes.get(item.parentId);
    if (parent) parent.children!.push(node); else roots.push(node);
  }
  return roots;
}

type LocalizedSiteInput = BuildInput["site"]["locales"][string];

function selectSite(input: BuildInput, requested: string): LocalizedSiteInput {
  const enabled = new Set(input.locales.map(({ code }) => code));
  for (const locale of fallbackChain(input, requested).filter((candidate) => enabled.has(candidate))) {
    if (input.site.locales[locale]) return input.site.locales[locale];
  }
  throw new Error(`site copy is missing for ${requested}`);
}

function selectPost<T>(input: BuildInput, locales: Record<string, T>, requested: string, sourceLocale = input.sourceLocale) {
	const enabled = new Set(input.locales.map(({ code }) => code));
	for (const locale of fallbackChain(input, requested, sourceLocale).filter((candidate) => enabled.has(candidate))) {
    if (locales[locale]) return { locale, value: locales[locale] };
  }
  return undefined;
}

export function fallbackChain(input: Pick<BuildInput, "sourceLocale" | "fallback">, requested: string, sourceLocale = input.sourceLocale): string[] {
	return [...new Set([requested, ...(input.fallback ?? ["en", "zh-CN"]), sourceLocale])];
}

function toThemePost(input: BuildInput, post: BuildInput["posts"][number], localized: LocalizedPostInput, locale: string): ThemePost {
	return {
    id: post.id, template: post.template, title: localized.title, summary: localized.summary, seoTitle: localized.seoTitle, seoDescription: localized.seoDescription, html: renderMarkdown(localized.markdown), cover: post.cover, publishedAt: post.publishedAt, commentPolicy: post.commentPolicy,
    categories: (post.categories ?? []).flatMap((id) => { const item = (input.categories ?? []).find((candidate) => candidate.id === id); const selected = item && selectPost(input, item.locales, locale, item.sourceLocale); return selected ? [{ id, name: selected.value.name }] : []; }),
		tags: (post.tags ?? []).flatMap((id) => { const item = (input.tags ?? []).find((candidate) => candidate.id === id); const selected = item && selectPost(input, item.locales, locale, item.sourceLocale); return selected ? [{ id, name: selected.value.name }] : []; }),
  };
}

function dictionary(input: BuildInput, locale: string) {
  const dictionaries = input.dictionaries ?? {};
  return { ...(dictionaries.en ?? {}), ...(dictionaries[input.sourceLocale] ?? {}), ...(dictionaries[locale] ?? {}) };
}

async function emit(root: string, relative: string, contents: string) {
  const target = resolve(root, relative);
  if (target !== root && !target.startsWith(root + sep)) throw new Error("output path escapes build root");
  await mkdir(dirname(target), { recursive: true });
  await writeFile(target, contents, "utf8");
}

function validateInput(input: BuildInput) {
  if (input.schemaVersion !== 1) throw new Error("unsupported build input schema");
  if (!input.sourceLocale || !input.locales.some((locale) => locale.code === input.sourceLocale)) throw new Error("source locale must be enabled");
  if (new Set(input.locales.map((locale) => locale.code)).size !== input.locales.length) throw new Error("locale codes must be unique");
	const enabledLocales = new Set(input.locales.map(({ code }) => code));
	const requireEntitySource = (item: { id: string; sourceLocale?: string; locales: Record<string, unknown> }, kind: string) => {
		if (item.sourceLocale && (!enabledLocales.has(item.sourceLocale) || !item.locales[item.sourceLocale])) throw new Error(`${kind} source locale is unavailable: ${item.id}`);
	};
	if (input.theme && !/^[a-z]+(?:-[a-z]+)*$/.test(input.theme.id)) throw new Error(`invalid theme id: ${input.theme.id}`);
	if (input.theme?.id !== undefined && input.theme.id !== "earth" && !input.theme.modulePath) throw new Error("custom theme module is required");
  for (const post of input.posts) {
    if (!validPublicID(post.id)) throw new Error(`invalid post id: ${post.id}`);
		requireEntitySource(post, "post");
  }
  for (const page of input.pages ?? []) {
    if (!validPublicID(page.id)) throw new Error(`invalid page id: ${page.id}`);
		requireEntitySource(page, "page");
  }
	for (const item of [...(input.categories ?? []), ...(input.tags ?? [])]) requireEntitySource(item, "taxonomy");
	for (const item of [...(input.linkGroups ?? []), ...(input.links ?? [])]) requireEntitySource(item, "link");
	for (const item of input.menus ?? []) requireEntitySource(item, "menu");
}

function validPublicID(id: string) {
	return /^(?:[0-9a-f]{32}|[0-9]{13}|[a-z]+(?:-[a-z]+)*)$/.test(id);
}

type ThemeAdapter = {
	css: string;
	renderIndex: typeof renderIndex;
	renderPost: typeof renderPost;
	renderPage: typeof renderPage;
	renderTaxonomy: typeof renderTaxonomy;
	renderLinks: typeof renderLinks;
	renderArchive: typeof renderArchive;
	renderSearch: typeof renderSearch;
	renderNotFound?: typeof renderNotFound;
	postTemplates?: Record<string, typeof renderPost>;
	pageTemplates?: Record<string, typeof renderPage>;
};

const builtInTheme: ThemeAdapter = { css: earthCSS, renderIndex, renderPost, renderPage, renderTaxonomy, renderLinks, renderArchive, renderSearch, renderNotFound };

async function loadTheme(input: BuildInput): Promise<ThemeAdapter> {
	if (!input.theme || input.theme.id === "earth") return builtInTheme;
	const modulePath = resolve(input.theme.modulePath!);
	const moduleInfo = await stat(modulePath);
	if (!moduleInfo.isFile()) throw new Error("custom theme server module is not a regular file");
	const imported = await import(`${pathToFileURL(modulePath).href}?v=${moduleInfo.mtimeMs}`) as Record<string, unknown>;
	const candidate = ((imported.default && typeof imported.default === "object") ? imported.default : imported) as Partial<ThemeAdapter>;
	const functions: Array<keyof Omit<ThemeAdapter, "css">> = ["renderIndex", "renderPost", "renderPage", "renderTaxonomy", "renderLinks", "renderArchive", "renderSearch"];
	if (typeof candidate.css !== "string" || functions.some((name) => typeof candidate[name] !== "function")) {
		throw new Error("custom theme does not implement the MutiBlog React SSR contract");
	}
	validateTemplateRenderers(candidate.postTemplates, input.theme?.postTemplates ?? [], "post");
	validateTemplateRenderers(candidate.pageTemplates, input.theme?.pageTemplates ?? [], "page");
	return candidate as ThemeAdapter;
}

function validateTemplateRenderers(renderers: ThemeAdapter["postTemplates"], declared: Array<{ id: string }>, kind: "post" | "page") {
	for (const template of declared) {
		if (!renderers || typeof renderers[template.id] !== "function") throw new Error(`theme ${kind} template is missing renderer: ${template.id}`);
	}
}

function contentRenderer(theme: ThemeAdapter, kind: "post" | "page", selected?: string, declared: Array<{ id: string }> = []) {
	const defaultID = kind;
	const fallback = kind === "post" ? theme.renderPost : theme.renderPage;
	if (!selected || selected === defaultID || !declared.some((template) => template.id === selected)) return fallback;
	// Template selections are theme-specific. Switching themes deliberately
	// falls back to the new theme's default when it does not declare the old ID.
	return (kind === "post" ? theme.postTemplates : theme.pageTemplates)?.[selected] ?? fallback;
}

async function countFiles(root: string): Promise<number> {
	let count = 0;
	for (const entry of await readdir(root, { withFileTypes: true })) {
		if (entry.isDirectory()) count += await countFiles(join(root, entry.name));
		else if (entry.isFile()) count += 1;
	}
	return count;
}

function doctype(markup: string) { return `<!doctype html>${markup}`; }
function ensureDoctype(markup: string) { return /^<!doctype html>/i.test(markup) ? markup : doctype(markup); }
function redirectHTML(target: string, label = "Continue", locale = "en") { const safe = target.replace(/[&<>"']/g, ""); return `<!doctype html><html lang="${escapeXML(locale)}"><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=${safe}"><link rel="canonical" href="${safe}"></head><body><a href="${safe}">${escapeXML(label)}</a></body></html>`; }
function basicPage(title: string, message: string) { return `<!doctype html><html><head><meta charset="utf-8"><title>${escapeXML(title)}</title><link rel="stylesheet" href="/assets/theme.css"></head><body><main class="article-shell"><article class="article-card"><h1>404</h1><p>${escapeXML(message)}</p></article></main></body></html>`; }
function sitemap(input: BuildInput) {
	const base = input.baseUrl?.replace(/\/$/, "") ?? "";
	const paths = input.locales.flatMap(({ code }) => [
		`/${code}/`,
		`/${code}/archives/`,
		`/${code}/links/`,
		...input.posts.filter((post) => post.status === "published" && post.locales[code]).map((post) => `/${code}/posts/${post.id}/`),
		...(input.pages ?? []).filter((page) => page.status === "published" && page.locales[code]).map((page) => `/${code}/pages/${page.id}/`),
		...(input.categories ?? []).filter((item) => item.locales[code]).map((item) => `/${code}/categories/${item.id}/`),
		...(input.tags ?? []).filter((item) => item.locales[code]).map((item) => `/${code}/tags/${item.id}/`),
	]);
	return `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${paths.map((path) => `<url><loc>${base}${path}</loc></url>`).join("")}</urlset>`;
}
function rss(input: BuildInput, locale: string, site: LocalizedSiteInput, posts: ThemePost[]) { const base = input.baseUrl?.replace(/\/$/, "") ?? ""; return `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>${escapeXML(site.title)}</title><link>${escapeXML(base + `/${locale}/`)}</link><description>${escapeXML(site.description ?? site.subtitle ?? site.title)}</description>${posts.map((post) => `<item><title>${escapeXML(post.title)}</title><link>${escapeXML(base + `/${locale}/posts/${post.id}/`)}</link><guid>${escapeXML(base + `/${locale}/posts/${post.id}/`)}</guid>${post.publishedAt ? `<pubDate>${new Date(post.publishedAt).toUTCString()}</pubDate>` : ""}<description>${escapeXML(post.summary ?? "")}</description></item>`).join("")}</channel></rss>`; }
function escapeXML(value: string) { return value.replace(/[<>&"']/g, (character) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;", "'": "&apos;" })[character]!); }
