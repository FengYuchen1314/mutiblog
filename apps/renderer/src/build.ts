import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { dirname, join, resolve, sep } from "node:path";
import MarkdownIt from "markdown-it";
import { renderToStaticMarkup } from "react-dom/server";
import { earthCSS, renderIndex, renderPost, type ThemeContext, type ThemePost } from "@mutiblog/theme-earth";
import type { BuildInput, BuildReport, LocalizedPostInput, RedirectRecord } from "./types.js";

const markdown = new MarkdownIt({ html: false, linkify: true, typographer: true });

const dictionaries: Record<string, Record<string, string>> = {
  en: { home: "Home", archives: "Archives", links: "Links", search: "Search", colorScheme: "Color scheme", about: "About", languages: "Languages", comments: "Comments", loadingComments: "Loading comments…", commentsUnavailable: "Comments are temporarily unavailable." },
  "zh-CN": { home: "首页", archives: "归档", links: "友链", search: "搜索", colorScheme: "明暗主题", about: "关于", languages: "语言", comments: "评论", loadingComments: "正在加载评论…", commentsUnavailable: "评论服务暂时不可用。" },
};

export async function buildSite(input: BuildInput, outputDirectory: string): Promise<BuildReport> {
  validateInput(input);
  const output = resolve(outputDirectory);
  await rm(output, { recursive: true, force: true });
  await mkdir(output, { recursive: true });
  const redirects: RedirectRecord[] = [];
  let files = 0;

  await emit(output, "assets/earth.css", earthCSS);
  files += 1;

  for (const localeDefinition of input.locales) {
    const locale = localeDefinition.code;
    const siteCopy = selectSite(input, locale);
    const localizedPosts: ThemePost[] = [];
    for (const post of input.posts.filter((candidate) => candidate.status === "published")) {
      const selection = selectPost(input, post.locales, locale);
      if (!selection) continue;
      if (selection.locale !== locale) {
        redirects.push({ from: `/${locale}/posts/${post.id}/`, to: `/${selection.locale}/posts/${post.id}/`, status: 302 });
        await emit(output, `${locale}/posts/${post.id}/index.html`, redirectHTML(`/${selection.locale}/posts/${post.id}/`));
        files += 1;
        continue;
      }
      const themedPost = toThemePost(post, selection.value);
      localizedPosts.push(themedPost);
      const context = makeContext(input, locale, siteCopy, localizedPosts, `/posts/${post.id}/`, themedPost);
      await emit(output, `${locale}/posts/${post.id}/index.html`, doctype(renderToStaticMarkup(renderPost(context))));
      files += 1;
    }

    const indexContext = makeContext(input, locale, siteCopy, localizedPosts, "/");
    await emit(output, `${locale}/index.html`, doctype(renderToStaticMarkup(renderIndex(indexContext))));
    await emit(output, `${locale}/search/index.json`, JSON.stringify(localizedPosts.map(({ id, title, summary }) => ({ id, title, summary, url: `/${locale}/posts/${id}/` }))));
    await emit(output, `${locale}/404.html`, basicPage(siteCopy.title, dictionary(locale).notFound ?? "Not found"));
    files += 3;
  }

  const rootTarget = `/${input.sourceLocale}/`;
  redirects.unshift({ from: "/", to: rootTarget, status: 302 });
  await emit(output, "index.html", redirectHTML(rootTarget));
  await emit(output, "redirects.json", JSON.stringify(redirects, null, 2));
  await emit(output, "robots.txt", "User-agent: *\nAllow: /\nSitemap: /sitemap.xml\n");
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

function makeContext(input: BuildInput, locale: string, siteCopy: LocalizedSiteInput, posts: ThemePost[], currentPath: string, post?: ThemePost): ThemeContext {
  return { site: { ...siteCopy, locale, locales: input.locales }, currentPath, posts, post, strings: dictionary(locale) };
}

type LocalizedSiteInput = BuildInput["site"]["locales"][string];

function selectSite(input: BuildInput, requested: string): LocalizedSiteInput {
  for (const locale of fallbackChain(input, requested)) {
    if (input.site.locales[locale]) return input.site.locales[locale];
  }
  throw new Error(`site copy is missing for ${requested}`);
}

function selectPost(input: BuildInput, locales: Record<string, LocalizedPostInput>, requested: string) {
  for (const locale of fallbackChain(input, requested)) {
    if (locales[locale]) return { locale, value: locales[locale] };
  }
  return undefined;
}

export function fallbackChain(input: Pick<BuildInput, "sourceLocale" | "fallback">, requested: string): string[] {
  return [...new Set([requested, ...(input.fallback ?? ["en", "zh-CN"]), input.sourceLocale])];
}

function toThemePost(post: BuildInput["posts"][number], localized: LocalizedPostInput): ThemePost {
  return { id: post.id, title: localized.title, summary: localized.summary, html: markdown.render(localized.markdown), cover: post.cover, publishedAt: post.publishedAt };
}

function dictionary(locale: string) {
  return dictionaries[locale] ?? dictionaries.en;
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
  for (const post of input.posts) {
    if (!/^(?:[0-9a-f]{32}|[a-z]+(?:-[a-z]+)*)$/.test(post.id)) throw new Error(`invalid post id: ${post.id}`);
  }
}

function doctype(markup: string) { return `<!doctype html>${markup}`; }
function redirectHTML(target: string) { const safe = target.replace(/[&<>"']/g, ""); return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=${safe}"><link rel="canonical" href="${safe}"></head><body><a href="${safe}">Redirect</a></body></html>`; }
function basicPage(title: string, message: string) { return `<!doctype html><html><head><meta charset="utf-8"><title>${title}</title><link rel="stylesheet" href="/assets/earth.css"></head><body><main class="article-shell"><article class="article-card"><h1>404</h1><p>${message}</p></article></main></body></html>`; }
function sitemap(input: BuildInput) { const paths = input.locales.flatMap((locale) => [`/${locale.code}/`, ...input.posts.filter((post) => post.status === "published" && post.locales[locale.code]).map((post) => `/${locale.code}/posts/${post.id}/`)]); return `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${paths.map((path) => `<url><loc>${path}</loc></url>`).join("")}</urlset>`; }
