import { cp, mkdir, readFile, readdir, rm, stat, writeFile } from "node:fs/promises";
import { dirname, join, resolve, sep } from "node:path";
import { pathToFileURL } from "node:url";
import { createElement, type ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { earthCSS, renderArchive, renderCollection, renderIndex, renderLinks, renderNotFound, renderPage, renderPost, renderSearch, renderTaxonomy, type ThemeContext, type ThemeLinkGroup, type ThemeMenu, type ThemeMenuItem, type ThemePagination, type ThemePost, type ThemeTaxonomyCollection, type ThemeTaxonomyCollections, type ThemeTaxonomyKind } from "@mutiblog/theme-earth";
import { markdownContentCSS, renderMarkdownWithHeadings } from "@mutiblog/markdown";
import type { BuildInput, BuildReport, LocalizedPostInput, RedirectRecord } from "./types.js";

export const STATIC_PAGE_SIZE = 12;
const COLLECTION_PREVIEW_SIZE = 10;

export async function buildSite(input: BuildInput, outputDirectory: string): Promise<BuildReport> {
  validateInput(input);
	const theme = await loadTheme(input);
  const output = resolve(outputDirectory);
  await rm(output, { recursive: true, force: true });
  await mkdir(output, { recursive: true });
  const redirects: RedirectRecord[] = [];
  const sitemapPaths = new Set<string>();
  let files = 0;
	const emitOutput = async (relative: string, contents: string) => {
		await emit(output, relative, contents);
		files += 1;
	};
	const emitCanonical = async (relative: string, publicPath: string, contents: string) => {
		await emitOutput(relative, contents);
		sitemapPaths.add(publicPath);
	};

	await emitOutput("assets/theme.css", `${theme.css.trimEnd()}\n${markdownContentCSS.trim()}\n`);
	if (input.theme?.assetsPath) {
		const assetTarget = join(output, "assets", "themes", input.theme.id);
		await cp(input.theme.assetsPath, assetTarget, { recursive: true, errorOnExist: true });
		files += await countFiles(assetTarget);
	}

  for (const localeDefinition of input.locales) {
    const locale = localeDefinition.code;
    const siteCopy = selectSite(input, locale);
    const selectedPosts: Array<{ post: BuildInput["posts"][number]; themed: ThemePost; redirectTo?: string }> = [];
    for (const post of input.posts.filter((candidate) => candidate.status === "published")) {
      const selection = selectPost(input, post.locales, locale, post.sourceLocale);
      if (!selection) continue;
      const redirectTo = selection.locale === locale ? undefined : `/${selection.locale}/posts/${post.id}/`;
      selectedPosts.push({ post, themed: toThemePost(input, post, selection.value, locale, "post"), redirectTo });
      if (redirectTo) redirects.push({ from: `/${locale}/posts/${post.id}/`, to: redirectTo, status: 302 });
    }
    // Selection and detail rendering are deliberately separate. The list
    // slice is page-local while allPosts remains complete for every detail and
    // list page, regardless of the current entity's source order.
    const localizedPosts = selectedPosts.map(({ themed }) => themed);
		const taxonomyCollections = makeTaxonomyCollections(input, locale, localizedPosts);
		for (const [index, { post, themed, redirectTo }] of selectedPosts.entries()) {
      if (redirectTo) {
				await emitOutput(`${locale}/posts/${post.id}/index.html`, redirectHTML(redirectTo, dictionary(input, locale).redirecting, locale));
        continue;
      }
			const path = `/posts/${post.id}/`;
			const context = makeContext(input, locale, siteCopy, [themed], path, themed, exactLocaleCodes(input, post.locales), {
				allPosts: localizedPosts,
				taxonomyCollections,
				cursor: { previous: localizedPosts[index - 1], next: localizedPosts[index + 1] },
			});
			await emitCanonical(outputPath(locale, path), `/${locale}${path}`, doctype(renderToStaticMarkup(contentRenderer(theme, "post", post.template, input.theme?.postTemplates)(context))));
    }

    const selectedPages: Array<{ page: NonNullable<BuildInput["pages"]>[number]; themed: ThemePost; redirectTo?: string }> = [];
    for (const page of (input.pages ?? []).filter((candidate) => candidate.status === "published")) {
      const selection = selectPost(input, page.locales, locale, page.sourceLocale);
      if (!selection) continue;
      const redirectTo = selection.locale === locale ? undefined : `/${selection.locale}/pages/${page.id}/`;
      selectedPages.push({ page, themed: toThemePost(input, page, selection.value, locale, "page"), redirectTo });
      if (redirectTo) redirects.push({ from: `/${locale}/pages/${page.id}/`, to: redirectTo, status: 302 });
    }
    const localizedPages = selectedPages.map(({ themed }) => themed);
    for (const { page, themed, redirectTo } of selectedPages) {
      if (redirectTo) {
				await emitOutput(`${locale}/pages/${page.id}/index.html`, redirectHTML(redirectTo, dictionary(input, locale).redirecting, locale));
        continue;
      }
			const path = `/pages/${page.id}/`;
			const context = makeContext(input, locale, siteCopy, [themed], path, themed, exactLocaleCodes(input, page.locales), { allPosts: localizedPosts, taxonomyCollections });
			await emitCanonical(outputPath(locale, path), `/${locale}${path}`, doctype(renderToStaticMarkup(contentRenderer(theme, "page", page.template, input.theme?.pageTemplates)(context))));
    }

    for (const [kind, taxonomies] of [["categories", input.categories ?? []], ["tags", input.tags ?? []]] as const) {
      for (const item of taxonomies) {
        const selection = selectPost(input, item.locales, locale, item.sourceLocale);
        if (!selection) continue;
				const taxonomyPosts = localizedPosts.filter((post) => (kind === "categories" ? post.categories : post.tags)?.some(({ id }) => id === item.id));
				const totalPages = totalPageCount(taxonomyPosts.length);
				const basePath = `/${kind}/${item.id}/`;
        if (selection.locale !== locale) {
					const destinationPostCount = input.posts.filter((post) =>
						post.status === "published"
						&& Boolean(selectPost(input, post.locales, selection.locale, post.sourceLocale))
						&& (kind === "categories" ? post.categories : post.tags)?.includes(item.id),
					).length;
					for (let page = 1; page <= totalPageCount(destinationPostCount); page += 1) {
						const path = paginatedPath(basePath, page);
						const from = `/${locale}${path}`;
						const to = `/${selection.locale}${path}`;
						redirects.push({ from, to, status: 302 });
						await emitOutput(outputPath(locale, path), redirectHTML(to, dictionary(input, locale).redirecting, locale));
					}
          continue;
        }
        const renderer = kind === "categories"
          ? taxonomyRenderer(theme, item.template, input.theme?.categoryTemplates)
          : theme.renderTaxonomy;
				for (let page = 1; page <= totalPages; page += 1) {
					const path = paginatedPath(basePath, page);
					const context = makeContext(input, locale, siteCopy, pageSlice(taxonomyPosts, page), path, undefined, exactLocaleCodes(input, item.locales), {
						allPosts: localizedPosts,
						taxonomyCollections,
						pagination: makePagination(taxonomyPosts.length, page, basePath),
						taxonomy: { kind, id: item.id, cover: item.cover, template: item.template, name: selection.value.name, description: selection.value.description },
					});
					const title = selection.value.seoTitle || selection.value.name;
					context.pageTitle = pageTitle(title, context.strings.page, page);
					context.pageDescription = selection.value.seoDescription || selection.value.description;
					await emitCanonical(outputPath(locale, path), `/${locale}${path}`, doctype(renderToStaticMarkup(renderer(context, selection.value.name, selection.value.description))));
				}
      }
    }

		for (const kind of ["categories", "tags"] as const) {
			const items = taxonomyCollections[kind];
			if (!items.length) continue;
			const path = `/${kind}/`;
			const selectedId = (kind === "categories" ? items.find((item) => !item.parentId || !items.some((candidate) => candidate.id === item.parentId)) : undefined)?.id ?? items[0].id;
			const previewPosts = localizedPosts
				.filter((post) => (kind === "categories" ? post.categories : post.tags)?.some(({ id }) => id === selectedId))
				.slice(0, COLLECTION_PREVIEW_SIZE);
			const collection: ThemeTaxonomyCollection = { kind, title: dictionary(input, locale)[kind] || items[0].name, selectedId, items };
			const context = makeContext(input, locale, siteCopy, previewPosts, path, undefined, undefined, { allPosts: localizedPosts, taxonomyCollections, collection });
			context.pageTitle = collection.title;
			const rendered: unknown = theme.renderCollection ? theme.renderCollection(context) : renderCollectionFallback(context);
			await emitCanonical(outputPath(locale, path), `/${locale}${path}`, typeof rendered === "string" ? ensureDoctype(rendered) : doctype(renderToStaticMarkup(rendered as ReactNode)));
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
		const linksPath = "/links/";
		const linksContext = makeContext(input, locale, siteCopy, [], linksPath, undefined, undefined, { allPosts: localizedPosts, taxonomyCollections });
		await emitCanonical(outputPath(locale, linksPath), `/${locale}${linksPath}`, doctype(renderToStaticMarkup(theme.renderLinks(linksContext, linkGroups))));

		for (let page = 1; page <= totalPageCount(localizedPosts.length); page += 1) {
			const path = paginatedPath("/", page);
			const context = makeContext(input, locale, siteCopy, pageSlice(localizedPosts, page), path, undefined, undefined, {
				allPosts: localizedPosts,
				taxonomyCollections,
				pagination: makePagination(localizedPosts.length, page, "/"),
			});
			if (page > 1) context.pageTitle = `${context.strings.page} ${page}`;
			await emitCanonical(outputPath(locale, path), `/${locale}${path}`, doctype(renderToStaticMarkup(theme.renderIndex(context))));
		}
		for (let page = 1; page <= totalPageCount(localizedPosts.length); page += 1) {
			const path = paginatedPath("/archives/", page);
			const context = makeContext(input, locale, siteCopy, pageSlice(localizedPosts, page), path, undefined, undefined, {
				allPosts: localizedPosts,
				taxonomyCollections,
				pagination: makePagination(localizedPosts.length, page, "/archives/"),
			});
			context.pageTitle = pageTitle(context.strings.archives, context.strings.page, page);
			await emitCanonical(outputPath(locale, path), `/${locale}${path}`, doctype(renderToStaticMarkup(theme.renderArchive(context))));
		}

		const searchContext = makeContext(input, locale, siteCopy, [], "/search/", undefined, undefined, { allPosts: localizedPosts, taxonomyCollections });
		await emitOutput(`${locale}/search/index.html`, doctype(renderToStaticMarkup(theme.renderSearch(searchContext))));
		await emitOutput(`${locale}/search/index.json`, JSON.stringify([
	  ...localizedPosts.map(({ id, title, summary }) => ({ kind: "Post", id, title, summary, url: `/${locale}/posts/${id}/` })),
	  ...localizedPages.map(({ id, title, summary }) => ({ kind: "Page", id, title, summary, url: `/${locale}/pages/${id}/` })),
		...taxonomyCollections.categories.map(({ id, name: title, description: summary, cover, href: url }) => ({ kind: "Category", id, title, summary, cover, url })),
		...taxonomyCollections.tags.map(({ id, name: title, description: summary, cover, href: url }) => ({ kind: "Tag", id, title, summary, cover, url })),
	]));
		// An RSS channel's permalink fields are required to be absolute URLs.
		// Legacy installations without a confirmed public origin deliberately
		// omit the feed rather than publish invalid relative links.
		if (input.baseUrl?.trim()) await emitOutput(`${locale}/rss.xml`, rss(input, locale, siteCopy, localizedPosts));
	const notFoundContext = makeContext(input, locale, siteCopy, [], "/404.html", undefined, undefined, { allPosts: localizedPosts, taxonomyCollections });
	const notFound = theme.renderNotFound ? theme.renderNotFound(notFoundContext) : basicPage(siteCopy.title, dictionary(input, locale).notFound ?? "Not found");
		await emitOutput(`${locale}/404.html`, typeof notFound === "string" ? ensureDoctype(notFound) : doctype(renderToStaticMarkup(notFound)));
  }

  const rootTarget = `/${input.sourceLocale}/`;
  redirects.unshift({ from: "/", to: rootTarget, status: 302 });
	await emitOutput("index.html", redirectHTML(rootTarget, dictionary(input, input.sourceLocale).redirecting, input.sourceLocale));
	await emitOutput("redirects.json", JSON.stringify(redirects, null, 2));
	const baseUrl = input.baseUrl?.replace(/\/$/, "");
	const robots = ["User-agent: *", "Allow: /"];
	if (baseUrl) robots.push(`Sitemap: ${baseUrl}/sitemap.xml`);
	await emitOutput("robots.txt", `${robots.join("\n")}\n`);
	// Legacy installations may not have configured their public origin yet.
	// A relative <loc> is invalid sitemap XML, so omit the file until the site
	// settings provide a canonical absolute base URL instead of publishing bad
	// crawler metadata.
	if (baseUrl) await emitOutput("sitemap.xml", sitemap(baseUrl, [...sitemapPaths]));

	const report: BuildReport = { schemaVersion: 1, generatedAt: new Date().toISOString(), files: files + 1, locales: input.locales.map((locale) => locale.code), redirects };
  await emit(output, "build-report.json", JSON.stringify(report, null, 2));
	return report;
}

export async function buildFromFile(inputFile: string, outputDirectory: string) {
  const input = JSON.parse(await readFile(inputFile, "utf8")) as BuildInput;
  return buildSite(input, outputDirectory);
}

type ContextOptions = {
	allPosts: ThemePost[];
	taxonomyCollections: ThemeTaxonomyCollections;
	cursor?: ThemeContext["cursor"];
	pagination?: ThemePagination;
	taxonomy?: ThemeContext["taxonomy"];
	collection?: ThemeTaxonomyCollection;
};

function makeContext(
	input: BuildInput,
	locale: string,
	siteCopy: LocalizedSiteInput,
	posts: ThemePost[],
	currentPath: string,
	post?: ThemePost,
	availableLocales = input.locales.map(({ code }) => code),
	options: ContextOptions = { allPosts: posts, taxonomyCollections: { categories: [], tags: [] } },
): ThemeContext {
  const baseUrl = input.baseUrl?.replace(/\/$/, "");
  const enabled = new Set(availableLocales);
  const alternates = input.locales
    .filter(({ code }) => enabled.has(code))
    .map(({ code, label }) => ({ locale: code, label, href: `${baseUrl ?? ""}/${code}${currentPath}` }));
  return {
    site: { ...siteCopy, locale, sourceLocale: input.sourceLocale, timezone: siteTimezone(input), locales: input.locales, baseUrl, logo: input.site.logo, commentMaxLength: commentMaxLength(input) },
    currentPath,
    posts,
	allPosts: options.allPosts,
    post,
		cursor: options.cursor,
		taxonomy: options.taxonomy,
		taxonomyCollections: options.taxonomyCollections,
		collection: options.collection,
		pagination: options.pagination,
    pageTitle: post?.seoTitle || post?.title,
    pageDescription: post?.seoDescription || post?.summary,
    canonicalUrl: `${baseUrl ?? ""}/${locale}${currentPath}`,
    alternates,
    strings: dictionary(input, locale),
    settings: localizedThemeSettings(input, locale),
    navigation: makeNavigation(input, locale),
    menus: makeMenus(input, locale),
  };
}

function makeTaxonomyCollections(input: BuildInput, locale: string, allPosts: ThemePost[]): ThemeTaxonomyCollections {
	const summaries = (kind: ThemeTaxonomyKind) => {
		const items = kind === "categories" ? input.categories ?? [] : input.tags ?? [];
		return items.flatMap((item) => {
			const selection = selectPost(input, item.locales, locale, item.sourceLocale);
			if (!selection) return [];
			const count = allPosts.filter((post) => (kind === "categories" ? post.categories : post.tags)?.some(({ id }) => id === item.id)).length;
			return [{
				id: item.id,
				parentId: item.parentId,
				name: selection.value.name,
				description: selection.value.description,
				cover: item.cover,
				count,
				href: `/${locale}/${kind}/${item.id}/`,
			}];
		});
	};
	return { categories: summaries("categories"), tags: summaries("tags") };
}

function totalPageCount(totalItems: number): number {
	return Math.max(1, Math.ceil(totalItems / STATIC_PAGE_SIZE));
}

function paginatedPath(basePath: string, page: number): string {
	if (!basePath.startsWith("/") || !basePath.endsWith("/") || page < 1 || !Number.isInteger(page)) throw new Error("invalid pagination path");
	return page === 1 ? basePath : `${basePath}page/${page}/`;
}

function pageSlice<T>(items: T[], page: number): T[] {
	const start = (page - 1) * STATIC_PAGE_SIZE;
	return items.slice(start, start + STATIC_PAGE_SIZE);
}

function makePagination(totalItems: number, page: number, basePath: string): ThemePagination {
	const totalPages = totalPageCount(totalItems);
	if (page < 1 || page > totalPages) throw new Error("pagination page is out of range");
	return {
		page,
		pageSize: STATIC_PAGE_SIZE,
		totalItems,
		totalPages,
		basePath,
		previousPath: page > 1 ? paginatedPath(basePath, page - 1) : undefined,
		nextPath: page < totalPages ? paginatedPath(basePath, page + 1) : undefined,
		pages: Array.from({ length: totalPages }, (_, index) => ({
			number: index + 1,
			path: paginatedPath(basePath, index + 1),
			current: index + 1 === page,
		})),
	};
}

function outputPath(locale: string, publicPath: string): string {
	if (!publicPath.startsWith("/") || !publicPath.endsWith("/")) throw new Error("invalid public page path");
	return `${locale}${publicPath}index.html`;
}

function pageTitle(title: string, pageLabel: string, page: number): string {
	return page === 1 ? title : `${title} – ${pageLabel} ${page}`;
}

function commentMaxLength(input: BuildInput): number {
  const value = input.comments?.maxLength;
  return typeof value === "number" && Number.isInteger(value) && value >= 100 && value <= 10000 ? value : 2000;
}

function exactLocaleCodes(input: BuildInput, locales: Record<string, unknown>) {
  return input.locales.map(({ code }) => code).filter((code) => Boolean(locales[code]));
}

function makeNavigation(input: BuildInput, locale: string, requestedMenu?: string): ThemeMenuItem[] {
  const menu = requestedMenu
    ? input.menus?.find((candidate) => candidate.id === requestedMenu)
    : input.menus?.find((candidate) => candidate.id === input.primaryMenu) ?? input.menus?.[0];
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

function makeMenus(input: BuildInput, locale: string): ThemeMenu[] {
  return (input.menus ?? []).flatMap((menu) => {
    const selected = selectPost(input, menu.locales, locale, menu.sourceLocale);
    if (!selected) return [];
    return [{ id: menu.id, label: selected.value.label, items: makeNavigation(input, locale, menu.id) }];
  });
}

type LocalizedSiteInput = BuildInput["site"]["locales"][string];

function selectSite(input: BuildInput, requested: string): LocalizedSiteInput {
  if (requiresExactLocale(input, requested)) {
    const exact = input.site.locales[requested];
    if (exact) return exact;
    throw new Error(`site copy is missing for ${requested}`);
  }
  const enabled = new Set(input.locales.map(({ code }) => code));
  // Site-owned data shares the Chinese safety fallback, then ends at the
  // current site source. This keeps an old English copy from silently winning
  // after the source locale changes.
  for (const locale of siteFallbackChain(input, requested).filter((candidate) => enabled.has(candidate))) {
    if (input.site.locales[locale]) return input.site.locales[locale];
  }
  throw new Error(`site copy is missing for ${requested}`);
}

function siteFallbackChain(input: Pick<BuildInput, "sourceLocale">, requested: string): string[] {
  return [...new Set([requested, "zh-CN", input.sourceLocale])];
}

function selectPost<T>(input: BuildInput, locales: Record<string, T>, requested: string, sourceLocale = input.sourceLocale) {
  if (requiresExactLocale(input, requested)) {
    const exact = locales[requested];
    if (exact) return { locale: requested, value: exact };
    // Repositories created before the fixed Chinese-source policy retain each
    // entity's immutable original source. The global source page may redirect
    // to that legacy origin, but every ready target locale remains exact-only.
    const legacySource =
      requested === input.sourceLocale ? locales[sourceLocale] : undefined;
    return legacySource
      ? { locale: sourceLocale, value: legacySource }
      : undefined;
  }
  const enabled = new Set(input.locales.map(({ code }) => code));
  for (const locale of fallbackChain(input, requested, sourceLocale).filter((candidate) => enabled.has(candidate))) {
    if (locales[locale]) return { locale, value: locales[locale] };
  }
  return undefined;
}

export function fallbackChain(input: Pick<BuildInput, "sourceLocale" | "fallback">, requested: string, sourceLocale = input.sourceLocale): string[] {
  // The persisted fallback field is retained in build snapshots for backward
  // compatibility, but renderer selection enforces the current fixed policy
  // even when it is asked to render an old snapshot that still lists English.
  return [...new Set([requested, "zh-CN", sourceLocale])];
}

function toThemePost(input: BuildInput, post: BuildInput["posts"][number], localized: LocalizedPostInput, locale: string, kind: "post" | "page"): ThemePost {
	const rendered = renderMarkdownWithHeadings(localized.markdown);
  return {
    id: post.id,
		kind,
    template: post.template,
    title: localized.title,
    summary: localized.summary,
    seoTitle: localized.seoTitle,
    seoDescription: localized.seoDescription,
		html: rendered.html,
		headings: rendered.headings,
    cover: post.cover,
    pinned: post.pinned,
    publishedAt: post.publishedAt,
    commentPolicy: post.commentPolicy,
    categories: (post.categories ?? []).flatMap((id) => {
      const item = (input.categories ?? []).find((candidate) => candidate.id === id);
      const selected = item && selectPost(input, item.locales, locale, item.sourceLocale);
      return selected ? [{ id, name: selected.value.name }] : [];
    }),
    tags: (post.tags ?? []).flatMap((id) => {
      const item = (input.tags ?? []).find((candidate) => candidate.id === id);
      const selected = item && selectPost(input, item.locales, locale, item.sourceLocale);
      return selected ? [{ id, name: selected.value.name }] : [];
    }),
  };
}

function dictionary(input: BuildInput, locale: string) {
  const dictionaries = input.dictionaries ?? {};
  if (requiresExactLocale(input, locale))
    return { ...(dictionaries[locale] ?? {}) };
  return {
    ...(dictionaries["zh-CN"] ?? {}), ...(dictionaries[input.sourceLocale] ?? {}), ...(dictionaries[locale] ?? {}) };
}

function requiresExactLocale(
  input: Pick<BuildInput, "locales">,
  locale: string,
): boolean {
  return input.locales.some(
    (definition) => definition.code === locale && definition.status === "ready",
  );
}

function localizedThemeSettings(
  input: BuildInput,
  locale: string,
): Record<string, unknown> | undefined {
  const base = input.theme?.settings;
  const localized = input.theme?.localizedSettings?.[locale];
  if (!base || !localized || Object.keys(localized).length === 0) return base;
  const result = JSON.parse(JSON.stringify(base)) as Record<string, unknown>;
  for (const path of input.theme?.localizableSettings ?? []) {
    const value = localized[path];
    if (value === undefined) continue;
    const segments = path.split(".");
    let current: unknown = result;
    for (let index = 0; index < segments.length - 1; index += 1) {
      const segment = segments[index];
      current = Array.isArray(current)
        ? current[Number.parseInt(segment, 10)]
        : typeof current === "object" && current !== null
          ? (current as Record<string, unknown>)[segment]
          : undefined;
    }
    const finalSegment = segments.at(-1)!;
    if (Array.isArray(current)) {
      const position = Number.parseInt(finalSegment, 10);
      if (!Number.isInteger(position) || typeof current[position] !== "string")
        throw new Error(`localized theme setting path is invalid: ${path}`);
      current[position] = value;
    } else if (
      typeof current === "object" &&
      current !== null &&
      typeof (current as Record<string, unknown>)[finalSegment] === "string"
    ) {
      (current as Record<string, unknown>)[finalSegment] = value;
    } else {
      throw new Error(`localized theme setting path is invalid: ${path}`);
    }
  }
  return result;
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
  try {
    new Intl.DateTimeFormat("en", { timeZone: siteTimezone(input) }).format(0);
  } catch {
    throw new Error(`invalid site timezone: ${input.timezone}`);
  }
  const enabledLocales = new Set(input.locales.map(({ code }) => code));
  for (const definition of input.locales) {
    if (definition.status !== undefined && definition.status !== "ready")
      throw new Error(
        `non-ready locale cannot be rendered: ${definition.code}`,
      );
  }
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
	for (const item of [...(input.categories ?? []), ...(input.tags ?? [])]) {
		requireEntitySource(item, "taxonomy");
		if (!validPublicID(item.id)) throw new Error(`invalid taxonomy id: ${item.id}`);
		if (item.cover && !validMediaPath(item.cover)) throw new Error(`invalid taxonomy cover: ${item.id}`);
	}
	for (const item of input.categories ?? []) {
		if (item.template && !/^[a-z]+(?:-[a-z]+)*$/.test(item.template)) throw new Error(`invalid category template: ${item.id}`);
	}
	for (const item of [...(input.linkGroups ?? []), ...(input.links ?? [])]) requireEntitySource(item, "link");
	for (const item of input.menus ?? []) requireEntitySource(item, "menu");

  const sourceDictionary = input.dictionaries?.[input.sourceLocale];
  for (const locale of input.locales
    .filter((definition) => definition.status === "ready")
    .map(({ code }) => code)) {
    const requireLocale = (
      item: {
        id: string;
        sourceLocale?: string;
        locales: Record<string, object>;
      },
      kind: string,
      fields: string[],
      sourceLocale = item.sourceLocale ?? input.sourceLocale,
    ) => {
      const target = item.locales[locale] as
        Record<string, unknown> | undefined;
      if (!target) {
        if (
          locale === input.sourceLocale &&
          sourceLocale !== locale &&
          item.locales[sourceLocale]
        )
          return;
        throw new Error(`${kind} locale is missing: ${item.id}.${locale}`);
      }
      const source = item.locales[sourceLocale] as
        Record<string, unknown> | undefined;
      if (!source)
        throw new Error(
          `${kind} source locale is missing: ${item.id}.${sourceLocale}`,
        );
      for (const field of fields) {
        const sourceValue = source[field];
        const targetValue = target[field];
        if (
          typeof sourceValue === "string" &&
          sourceValue.trim() &&
          (typeof targetValue !== "string" || !targetValue.trim())
        ) {
          throw new Error(
            `${kind} locale field is missing: ${item.id}.${locale}.${field}`,
          );
        }
      }
    };
    const siteCopy = input.site.locales[locale];
    if (!siteCopy || !siteCopy.title?.trim())
      throw new Error(`site copy is missing for ${locale}`);
    const sourceSiteCopy = input.site.locales[input.sourceLocale];
    for (const field of ["subtitle", "description"] as const) {
      if (sourceSiteCopy?.[field]?.trim() && !siteCopy[field]?.trim())
        throw new Error(`site copy field is missing: ${locale}.${field}`);
    }
    const localeDictionary = input.dictionaries?.[locale];
    if (!sourceDictionary || !localeDictionary)
      throw new Error(`framework dictionary is missing for ${locale}`);
    for (const key of Object.keys(sourceDictionary)) {
      if (!localeDictionary[key]?.trim())
        throw new Error(
          `framework dictionary value is missing: ${locale}.${key}`,
        );
    }
    if (Object.keys(localeDictionary).some((key) => !(key in sourceDictionary)))
      throw new Error(
        `framework dictionary contains an unknown key for ${locale}`,
      );
    for (const post of input.posts)
      requireLocale(post, "post", [
        "title",
        "summary",
        "seoTitle",
        "seoDescription",
        "markdown",
      ]);
    for (const page of input.pages ?? [])
      requireLocale(page, "page", [
        "title",
        "summary",
        "seoTitle",
        "seoDescription",
        "markdown",
      ]);
    for (const item of [...(input.categories ?? []), ...(input.tags ?? [])])
      requireLocale(item, "taxonomy", [
        "name",
        "description",
        "seoTitle",
        "seoDescription",
      ]);
    for (const item of [...(input.linkGroups ?? []), ...(input.links ?? [])])
      requireLocale(item, "link", ["name", "description"]);
    for (const menu of input.menus ?? []) {
      requireLocale(menu, "menu", ["label"]);
      for (const item of menu.items)
        requireLocale(
          item,
          `menu item ${menu.id}`,
          ["label"],
          menu.sourceLocale ?? input.sourceLocale,
        );
    }
    if (locale !== input.sourceLocale) {
      const requiredPaths = input.theme?.localizableSettings ?? [];
      const localizedPaths = Object.keys(
        input.theme?.localizedSettings?.[locale] ?? {},
      );
      if (
        localizedPaths.length !== requiredPaths.length ||
        localizedPaths.some((path) => !requiredPaths.includes(path))
      ) {
        throw new Error(`localized theme setting keys do not match: ${locale}`);
      }
      for (const path of requiredPaths) {
        if (!input.theme?.localizedSettings?.[locale]?.[path]?.trim())
          throw new Error(
            `localized theme setting is missing: ${locale}.${path}`,
          );
      }
    }
  }
}

function siteTimezone(input: Pick<BuildInput, "timezone">): string {
  return input.timezone?.trim() || "UTC";
}

function validPublicID(id: string) {
	return /^(?:[0-9a-f]{32}|[0-9]{13}|[a-z]+(?:-[a-z]+)*)$/.test(id);
}

function validMediaPath(value: string) {
	if (!/^\/media\/[A-Za-z0-9][A-Za-z0-9._/-]{0,498}$/.test(value) || value.includes("//")) return false;
	return value.split("/").every((segment) => segment !== "." && segment !== "..");
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
	renderCollection?: typeof renderCollection;
	postTemplates?: Record<string, typeof renderPost>;
	pageTemplates?: Record<string, typeof renderPage>;
	categoryTemplates?: Record<string, typeof renderTaxonomy>;
};

const builtInTheme: ThemeAdapter = { css: earthCSS, renderIndex, renderPost, renderPage, renderTaxonomy, renderLinks, renderArchive, renderSearch, renderNotFound, renderCollection };

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
	if (candidate.renderCollection !== undefined && typeof candidate.renderCollection !== "function") throw new Error("custom theme collection renderer is invalid");
	validateTemplateRenderers(candidate.postTemplates, input.theme?.postTemplates ?? [], "post");
	validateTemplateRenderers(candidate.pageTemplates, input.theme?.pageTemplates ?? [], "page");
	validateTemplateRenderers(candidate.categoryTemplates, input.theme?.categoryTemplates ?? [], "category");
	return candidate as ThemeAdapter;
}

function validateTemplateRenderers(renderers: Record<string, unknown> | undefined, declared: Array<{ id: string }>, kind: "post" | "page" | "category") {
	for (const template of declared) {
		if (!renderers || typeof renderers[template.id] !== "function") throw new Error(`theme ${kind} template is missing renderer: ${template.id}`);
	}
}

function taxonomyRenderer(theme: ThemeAdapter, selected?: string, declared: Array<{ id: string }> = []) {
	if (!selected || selected === "category" || !declared.some((template) => template.id === selected)) return theme.renderTaxonomy;
	// Theme switches retain the stored selection, but undeclared templates
	// safely fall back to the active theme's default category renderer.
	return theme.categoryTemplates?.[selected] ?? theme.renderTaxonomy;
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
function renderCollectionFallback(context: ThemeContext) {
	const collection = context.collection!;
	const selected = collection.items.find((item) => item.id === collection.selectedId)!;
	const title = `${collection.title} – ${context.site.title}`;
	return createElement("html", { lang: context.site.locale },
		createElement("head", null,
			createElement("meta", { charSet: "utf-8" }),
			createElement("meta", { name: "viewport", content: "width=device-width, initial-scale=1" }),
			createElement("title", null, title),
			context.canonicalUrl ? createElement("link", { rel: "canonical", href: context.canonicalUrl }) : null,
			...(context.alternates ?? []).map((alternate) => createElement("link", { key: alternate.locale, rel: "alternate", hrefLang: alternate.locale, href: alternate.href })),
			context.alternates?.length ? createElement("link", { rel: "alternate", hrefLang: "x-default", href: (context.alternates.find(({ locale }) => locale === context.site.sourceLocale) ?? context.alternates[0]).href }) : null,
			createElement("link", { rel: "stylesheet", href: "/assets/theme.css" }),
		),
		createElement("body", null,
			createElement("main", { "data-taxonomy-collection": collection.kind },
				createElement("h1", null, collection.title),
				createElement("nav", { "aria-label": collection.title }, ...collection.items.map((item) => createElement("a", { key: item.id, href: item.href, "aria-current": item.id === collection.selectedId ? "page" : undefined }, item.name, " (", String(item.count), ")"))),
				createElement("section", null, ...context.posts.map((post) => createElement("article", { key: post.id },
					createElement("h2", null, createElement("a", { href: `/${context.site.locale}/posts/${post.id}/` }, post.title)),
					post.summary ? createElement("p", null, post.summary) : null,
				))),
				context.posts.length ? null : createElement("p", null, context.strings.noPosts),
				selected.count > context.posts.length ? createElement("a", { href: selected.href }, context.strings.morePosts) : null,
			),
		),
	);
}
function sitemap(baseUrl: string, paths: string[]) {
	return `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${paths.sort().map((path) => `<url><loc>${escapeXML(baseUrl + path)}</loc></url>`).join("")}</urlset>`;
}
function rss(input: BuildInput, locale: string, site: LocalizedSiteInput, posts: ThemePost[]) { const base = input.baseUrl?.replace(/\/$/, "") ?? ""; return `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>${escapeXML(site.title)}</title><link>${escapeXML(base + `/${locale}/`)}</link><description>${escapeXML(site.description ?? site.subtitle ?? site.title)}</description>${posts.map((post) => `<item><title>${escapeXML(post.title)}</title><link>${escapeXML(base + `/${locale}/posts/${post.id}/`)}</link><guid>${escapeXML(base + `/${locale}/posts/${post.id}/`)}</guid>${post.publishedAt ? `<pubDate>${new Date(post.publishedAt).toUTCString()}</pubDate>` : ""}<description>${escapeXML(post.summary ?? "")}</description></item>`).join("")}</channel></rss>`; }
function escapeXML(value: string) { return value.replace(/[<>&"']/g, (character) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;", "'": "&apos;" })[character]!); }
