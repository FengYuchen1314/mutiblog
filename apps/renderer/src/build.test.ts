import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { afterEach, describe, expect, test } from "vitest";
import { buildSite, fallbackChain } from "./build.js";
import type { BuildInput } from "./types.js";

const outputs: string[] = [];
afterEach(async () => { const { rm } = await import("node:fs/promises"); await Promise.all(outputs.splice(0).map((path) => rm(path, { recursive: true, force: true }))); });

describe("fallbackChain", () => {
  test("deduplicates the confirmed fallback order", () => {
    expect(fallbackChain({ sourceLocale: "zh-CN", fallback: ["en", "zh-CN"] }, "ja")).toEqual(["ja", "en", "zh-CN"]);
  });
});

test("keeps an entity's original source locale after the site source changes", async () => {
  const output = join("/tmp", `mutiblog-source-switch-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "fr",
    locales: [{ code: "de", label: "Deutsch" }, { code: "fr", label: "Français" }],
    site: { locales: { fr: { title: "Nouveau site" } } },
    posts: [{ id: "legacy-post", sourceLocale: "de", status: "published", locales: { de: { title: "Alt", markdown: "Inhalt" } } }],
  };

  const report = await buildSite(input, output);

  expect(report.redirects).toContainEqual({ from: "/fr/posts/legacy-post/", to: "/de/posts/legacy-post/", status: 302 });
  expect(await readFile(join(output, "de/posts/legacy-post/index.html"), "utf8")).toContain("Alt");
});

test("builds localized pages and redirect records", async () => {
  const output = join("/tmp", `mutiblog-render-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    locales: [{ code: "zh-CN", label: "简体中文" }, { code: "en", label: "English" }, { code: "ja", label: "日本語" }],
    baseUrl: "https://blog.example.com",
    primaryMenu: "primary",
    site: { locales: { "zh-CN": { title: "测试站" }, en: { title: "Test site" } } },
    dictionaries: { en: { archives: "Archives", search: "Search", links: "Links", notFound: "Not found", redirecting: "Continue" }, "zh-CN": { archives: "归档", search: "搜索", links: "友链", notFound: "页面不存在", redirecting: "继续访问" } },
    posts: [{ id: "hello-world", status: "published", categories: ["engineering"], tags: ["release"], locales: { "zh-CN": { title: "你好", markdown: "# 你好" }, en: { title: "Hello", seoTitle: "SEO Hello", seoDescription: "An SEO summary", markdown: "# Hello" } } }],
    pages: [{ id: "about-page", status: "published", locales: { "zh-CN": { title: "关于", markdown: "# 关于本站" }, en: { title: "About", markdown: "# About this site" } } }],
    categories: [{ id: "engineering", locales: { "zh-CN": { name: "工程" }, en: { name: "Engineering", seoTitle: "Engineering topics", seoDescription: "Engineering articles" } } }],
    tags: [{ id: "release", locales: { "zh-CN": { name: "发布" }, en: { name: "Release" } } }],
    linkGroups: [{ id: "friends", order: 0, locales: { "zh-CN": { name: "朋友" }, en: { name: "Friends" } } }],
    links: [{ id: "example-site", groupId: "friends", url: "https://example.com", order: 0, locales: { "zh-CN": { name: "示例" }, en: { name: "Example" } } }],
    menus: [{ id: "secondary", locales: { "zh-CN": { label: "次要" }, en: { label: "Secondary" } }, items: [] }, { id: "primary", locales: { "zh-CN": { label: "主菜单" }, en: { label: "Primary" } }, items: [{ id: "about", targetKind: "internal", url: "/pages/about-page/", openInNew: false, order: 0, locales: { "zh-CN": { label: "关于" }, en: { label: "About" } } }] }],
  };
  const report = await buildSite(input, output);
  expect(report.redirects).toContainEqual({ from: "/ja/posts/hello-world/", to: "/en/posts/hello-world/", status: 302 });
  const englishPost = await readFile(join(output, "en/posts/hello-world/index.html"), "utf8");
  expect(englishPost).toContain("<title>SEO Hello – Test site</title>");
  expect(englishPost).toContain('<meta name="description" content="An SEO summary"/>');
  expect(englishPost).toContain('<link rel="canonical" href="https://blog.example.com/en/posts/hello-world/"/>');
  expect(englishPost).toContain('hrefLang="zh-CN" href="https://blog.example.com/zh-CN/posts/hello-world/"');
  expect(englishPost).not.toContain('hrefLang="ja"');
  expect(await readFile(join(output, "en/pages/about-page/index.html"), "utf8")).toContain("About this site");
  const englishCategory = await readFile(join(output, "en/categories/engineering/index.html"), "utf8");
  expect(englishCategory).toContain("Hello");
  expect(englishCategory).toContain("<title>Engineering topics – Test site</title>");
  expect(englishCategory).toContain('<meta name="description" content="Engineering articles"/>');
  expect(report.redirects).toContainEqual({ from: "/ja/categories/engineering/", to: "/en/categories/engineering/", status: 302 });
  expect(await readFile(join(output, "ja/posts/hello-world/index.html"), "utf8")).toContain('<html lang="ja">');
  expect(await readFile(join(output, "ja/posts/hello-world/index.html"), "utf8")).toContain("继续访问");
  expect(await readFile(join(output, "en/links/index.html"), "utf8")).toContain("Example");
  expect(await readFile(join(output, "en/index.html"), "utf8")).toContain("/en/pages/about-page/");
  expect(await readFile(join(output, "en/archives/index.html"), "utf8")).toContain("Hello");
  expect(await readFile(join(output, "en/search/index.html"), "utf8")).toContain("data-search-input");
	const searchIndex = JSON.parse(await readFile(join(output, "en/search/index.json"), "utf8"));
	expect(searchIndex).toContainEqual(expect.objectContaining({ kind: "Post", id: "hello-world", url: "/en/posts/hello-world/" }));
	expect(searchIndex).toContainEqual(expect.objectContaining({ kind: "Page", id: "about-page", url: "/en/pages/about-page/" }));
	const notFound = await readFile(join(output, "en/404.html"), "utf8");
	expect(notFound).toContain("<title>Not found – Test site</title>");
	expect(notFound).toContain("site-header");
  expect(await readFile(join(output, "ja/404.html"), "utf8")).toContain("页面不存在");
  expect(await readFile(join(output, "en/rss.xml"), "utf8")).toContain("<rss version=\"2.0\">");
  const sitemap = await readFile(join(output, "sitemap.xml"), "utf8");
  expect(sitemap).toContain("https://blog.example.com/en/archives/");
  expect(sitemap).toContain("https://blog.example.com/en/links/");
  expect(sitemap).toContain("https://blog.example.com/en/categories/engineering/");
  expect(sitemap).toContain("https://blog.example.com/en/tags/release/");
  expect(sitemap).not.toContain("https://blog.example.com/ja/posts/hello-world/");
  expect(JSON.parse(await readFile(join(output, "redirects.json"), "utf8"))).toContainEqual({ from: "/", to: "/zh-CN/", status: 302 });
});

test("never redirects to a disabled fallback locale", async () => {
  const output = join("/tmp", `mutiblog-render-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    locales: [{ code: "zh-CN", label: "简体中文" }, { code: "ja", label: "日本語" }],
    site: { locales: { "zh-CN": { title: "测试站" } } },
    posts: [{ id: "hello-world", status: "published", locales: { "zh-CN": { title: "你好", markdown: "# 你好" }, en: { title: "Hello", markdown: "# Hello" } } }],
  };
  const report = await buildSite(input, output);
  expect(report.redirects).toContainEqual({ from: "/ja/posts/hello-world/", to: "/zh-CN/posts/hello-world/", status: 302 });
});

test("accepts system-generated millisecond timestamp IDs", async () => {
  const output = join("/tmp", `mutiblog-render-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "en",
    locales: [{ code: "en", label: "English" }],
    site: { locales: { en: { title: "Timestamp site" } } },
    posts: [{ id: "1754899200000", status: "published", locales: { en: { title: "Timestamp post", markdown: "Post" } } }],
    pages: [{ id: "1754899200001", status: "published", locales: { en: { title: "Timestamp page", markdown: "Page" } } }],
  };
  await buildSite(input, output);
  expect(await readFile(join(output, "en/posts/1754899200000/index.html"), "utf8")).toContain("Timestamp post");
  expect(await readFile(join(output, "en/pages/1754899200001/index.html"), "utf8")).toContain("Timestamp page");
});

test("loads a packaged SSR theme and isolates its public assets", async () => {
  const output = join("/tmp", `mutiblog-render-${crypto.randomUUID()}`);
  const themeRoot = join("/tmp", `mutiblog-theme-${crypto.randomUUID()}`);
  outputs.push(output, themeRoot);
  await mkdir(join(themeRoot, "assets"), { recursive: true });
  await writeFile(join(themeRoot, "server.mjs"), `
    const page = (context) => "CUSTOM:" + context.currentPath + ":" + context.settings.accent;
    export const css = "body{color:rebeccapurple}";
    export const renderIndex = page;
    export const renderPost = page;
    export const renderPage = page;
    export const renderTaxonomy = page;
    export const renderLinks = page;
    export const renderArchive = page;
    export const renderSearch = page;
	export const postTemplates = { gallery: (context) => "GALLERY:" + context.post.title };
  `);
  await writeFile(join(themeRoot, "assets", "client.js"), "console.log('custom theme')");
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    locales: [{ code: "zh-CN", label: "简体中文" }],
    site: { locales: { "zh-CN": { title: "测试站" } } },
    posts: [{ id: "gallery-post", status: "published", template: "gallery", locales: { "zh-CN": { title: "相册", markdown: "body" } } }],
    theme: { id: "custom-theme", modulePath: join(themeRoot, "server.mjs"), assetsPath: join(themeRoot, "assets"), settings: { accent: "violet" }, postTemplates: [{ id: "gallery", name: "Gallery" }] },
  };
  await buildSite(input, output);
  expect(await readFile(join(output, "zh-CN/index.html"), "utf8")).toContain("CUSTOM:/:violet");
  expect(await readFile(join(output, "assets/theme.css"), "utf8")).toContain("rebeccapurple");
  expect(await readFile(join(output, "assets/themes/custom-theme/client.js"), "utf8")).toContain("custom theme");
  expect(await readFile(join(output, "zh-CN/posts/gallery-post/index.html"), "utf8")).toContain("GALLERY:相册");
});

test("rejects a declared theme template without a renderer", async () => {
  const output = join("/tmp", `mutiblog-render-${crypto.randomUUID()}`);
  const themeRoot = join("/tmp", `mutiblog-theme-${crypto.randomUUID()}`);
  outputs.push(output, themeRoot);
  await mkdir(themeRoot, { recursive: true });
  await writeFile(join(themeRoot, "server.mjs"), `
    const page = () => "PAGE";
    export const css = "body{}";
    export const renderIndex = page; export const renderPost = page; export const renderPage = page;
    export const renderTaxonomy = page; export const renderLinks = page; export const renderArchive = page; export const renderSearch = page;
  `);
  const input: BuildInput = {
    schemaVersion: 1, sourceLocale: "en", locales: [{ code: "en", label: "English" }],
    site: { locales: { en: { title: "Site" } } }, posts: [],
    theme: { id: "broken-theme", modulePath: join(themeRoot, "server.mjs"), postTemplates: [{ id: "gallery", name: "Gallery" }] },
  };
  await expect(buildSite(input, output)).rejects.toThrow("theme post template is missing renderer: gallery");
});
