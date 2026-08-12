import { mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { afterEach, describe, expect, test } from "vitest";
import { buildSite, fallbackChain } from "./build.js";
import type { BuildInput } from "./types.js";

const outputs: string[] = [];
afterEach(async () => { const { rm } = await import("node:fs/promises"); await Promise.all(outputs.splice(0).map((path) => rm(path, { recursive: true, force: true }))); });

async function countOutputFiles(root: string): Promise<number> {
	let total = 0;
	for (const entry of await readdir(root, { withFileTypes: true })) {
		total += entry.isDirectory() ? await countOutputFiles(join(root, entry.name)) : entry.isFile() ? 1 : 0;
	}
	return total;
}

describe("fallbackChain", () => {
  test("enforces the confirmed Chinese fallback order", () => {
    expect(fallbackChain({ sourceLocale: "zh-CN", fallback: ["en", "zh-CN"] }, "ja")).toEqual(["ja", "zh-CN"]);
    expect(fallbackChain({ sourceLocale: "fr" }, "ja")).toEqual(["ja", "zh-CN", "fr"]);
  });
});

test("requires exact content and framework strings for ready locales", async () => {
  const output = join("/tmp", `mutiblog-ready-locale-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    locales: [
      { code: "zh-CN", label: "简体中文", status: "ready" },
      { code: "ja", label: "日本語", status: "ready" },
    ],
    site: {
      locales: {
        "zh-CN": { title: "中文站点" },
        ja: { title: "日本語サイト" },
      },
    },
    dictionaries: {
      "zh-CN": { home: "首页", notFound: "页面不存在" },
      ja: { home: "ホーム", notFound: "ページが見つかりません" },
    },
    theme: {
      id: "earth",
      settings: { layout: { heroKicker: "中文眉题" } },
      localizableSettings: ["layout.heroKicker"],
      localizedSettings: { ja: { "layout.heroKicker": "日本語の眉題" } },
    },
    posts: [
      {
        id: "exact-post",
        status: "published",
        locales: {
          "zh-CN": {
            title: "中文标题",
            summary: "中文摘要",
            markdown: "中文正文",
          },
          ja: {
            title: "日本語のタイトル",
            summary: "日本語の要約",
            markdown: "日本語の本文",
          },
        },
        localeStates: {
          "zh-CN": { state: "current", origin: "source", revision: 2, sourceRevision: 2 },
          ja: { state: "current", origin: "ai", revision: 1, sourceRevision: 2 },
        },
      },
      {
        id: "saved-draft",
        status: "draft",
        locales: {
          "zh-CN": { title: "未发布草稿", markdown: "只保存中文，不触发翻译" },
        },
        localeStates: {
          "zh-CN": { state: "current", origin: "source", revision: 3, sourceRevision: 3 },
        },
      },
    ],
  };

  await buildSite(input, output);
  const japanese = await readFile(join(output, "ja/index.html"), "utf8");
  expect(japanese).toContain("日本語のタイトル");
  expect(japanese).toContain("日本語の眉題");
  expect(japanese).not.toContain("中文标题");
  expect(japanese).not.toContain("中文眉题");

  input.locales[1].status = "building";
  await expect(buildSite(input, output)).rejects.toThrow(
    "non-ready locale cannot be rendered: ja",
  );
  input.locales[1].status = "ready";
  input.posts[0].localeStates!.ja.state = "stale";
  await expect(buildSite(input, output)).rejects.toThrow(
    "post locale is stale: exact-post.ja",
  );
  input.posts[0].localeStates!.ja.state = "current";
  input.posts[0].localeStates!.ja.origin = "manual";
  await buildSite(input, output);
  input.posts[0].localeStates!.ja.state = "stale";
  await expect(buildSite(input, output)).rejects.toThrow(
    "post locale is stale: exact-post.ja",
  );
  input.posts[0].localeStates!.ja.state = "current";
  input.posts[0].localeStates!.ja.origin = "ai";
  await buildSite(input, output);
  delete input.posts[0].locales.ja.summary;
  await expect(buildSite(input, output)).rejects.toThrow(
    "post locale field is missing: exact-post.ja.summary",
  );
  input.posts[0].locales.ja.summary = "日本語の要約";
  input.theme!.localizedSettings!.ja["stale.path"] = "古い設定";
  await expect(buildSite(input, output)).rejects.toThrow(
    "localized theme setting keys do not match: ja",
  );
  delete input.theme!.localizedSettings!.ja["stale.path"];
  delete input.posts[0].locales.ja;
  await expect(buildSite(input, output)).rejects.toThrow(
    "post locale is missing: exact-post.ja",
  );
});

test("rejects a published legacy-source item missing the fixed site-source locale", async () => {
  const output = join("/tmp", `mutiblog-pending-fixed-source-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    locales: [
      { code: "zh-CN", label: "简体中文", status: "ready" },
      { code: "en", label: "English", status: "ready" },
    ],
    site: { locales: { "zh-CN": { title: "固定中文源站点" } } },
    dictionaries: { "zh-CN": { notFound: "页面不存在" } },
    posts: [{
      id: "legacy-source-pending-translation",
      sourceLocale: "en",
      status: "published",
      locales: { en: { title: "Legacy source", markdown: "Pending translation" } },
      localeStates: {
        en: { state: "current", origin: "source", revision: 3, sourceRevision: 3 },
      },
    }],
  };

  await expect(buildSite(input, output)).rejects.toThrow(
    "post locale is missing: legacy-source-pending-translation.zh-CN",
  );
});

test("keeps a legacy entity origin available on the fixed ready source locale", async () => {
  const output = join("/tmp", `mutiblog-legacy-source-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    locales: [
      { code: "zh-CN", label: "简体中文", status: "ready" },
      { code: "en", label: "English" },
    ],
    site: { locales: { "zh-CN": { title: "固定源站点" } } },
    dictionaries: {
      "zh-CN": { notFound: "页面不存在", redirecting: "继续访问" },
    },
    posts: [
      {
        id: "legacy-origin",
        sourceLocale: "en",
        status: "published",
        locales: { en: { title: "Legacy title", markdown: "Legacy body" } },
      },
    ],
  };

  const report = await buildSite(input, output);

  expect(report.redirects).toContainEqual({
    from: "/zh-CN/posts/legacy-origin/",
    to: "/en/posts/legacy-origin/",
    status: 302,
  });
  expect(await readFile(join(output, "zh-CN/index.html"), "utf8")).toContain(
    "Legacy title",
  );
  expect(
    await readFile(join(output, "en/posts/legacy-origin/index.html"), "utf8"),
  ).toContain("Legacy body");
});

test("keeps an entity's original source locale after the site source changes", async () => {
  const output = join("/tmp", `mutiblog-source-switch-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "fr",
    locales: [{ code: "en", label: "English" }, { code: "zh-CN", label: "简体中文" }, { code: "de", label: "Deutsch" }, { code: "fr", label: "Français" }],
    site: { locales: { fr: { title: "Nouveau site" } } },
    posts: [{ id: "legacy-post", sourceLocale: "de", status: "published", locales: { de: { title: "Alt", markdown: "Inhalt" } } }],
  };

  const report = await buildSite(input, output);

  expect(report.redirects).toContainEqual({ from: "/fr/posts/legacy-post/", to: "/de/posts/legacy-post/", status: 302 });
  const legacyPost = await readFile(join(output, "de/posts/legacy-post/index.html"), "utf8");
  expect(legacyPost).toContain("Alt");
  expect(legacyPost).toContain('data-share-external="x"');
  expect(legacyPost).toContain("new URL(value||location.href,location.href)");
  expect(await readFile(join(output, "fr/index.html"), "utf8")).toContain("Alt");
});

test("uses the Chinese site fallback before the current site source", async () => {
  const output = join("/tmp", `mutiblog-site-source-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "fr",
    locales: [
      { code: "en", label: "English" },
      { code: "zh-CN", label: "简体中文" },
      { code: "fr", label: "Français" },
      { code: "ja", label: "日本語" },
    ],
    site: { locales: { en: { title: "Old English site" }, "zh-CN": { title: "中文站点" }, fr: { title: "French site" } } },
    posts: [],
  };

  await buildSite(input, output);

  const japaneseHomepage = await readFile(join(output, "ja/index.html"), "utf8");
  expect(japaneseHomepage).toContain("中文站点");
  expect(japaneseHomepage).not.toContain("French site");
  expect(japaneseHomepage).not.toContain("Old English site");
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
  expect(report.redirects).toContainEqual({ from: "/ja/posts/hello-world/", to: "/zh-CN/posts/hello-world/", status: 302 });
  const englishPost = await readFile(join(output, "en/posts/hello-world/index.html"), "utf8");
  expect(englishPost).toContain("<title>SEO Hello – Test site</title>");
  expect(englishPost).toContain('<meta name="description" content="An SEO summary"/>');
  expect(englishPost).toContain('<link rel="canonical" href="https://blog.example.com/en/posts/hello-world/"/>');
  expect(englishPost).toContain('hrefLang="zh-CN" href="https://blog.example.com/zh-CN/posts/hello-world/"');
  expect(englishPost).not.toContain('hrefLang="ja"');
  const englishPage = await readFile(join(output, "en/pages/about-page/index.html"), "utf8");
  expect(englishPage).toContain("About this site");
  expect(englishPage).toContain('data-visit-kind="Page"');
  expect(englishPage).toContain("/api/v1/public/visits");
  const englishCategory = await readFile(join(output, "en/categories/engineering/index.html"), "utf8");
  expect(englishCategory).toContain("Hello");
  expect(englishCategory).toContain("<title>Engineering topics – Test site</title>");
  expect(englishCategory).toContain('<meta name="description" content="Engineering articles"/>');
  expect(report.redirects).toContainEqual({ from: "/ja/categories/engineering/", to: "/zh-CN/categories/engineering/", status: 302 });
  expect(await readFile(join(output, "ja/posts/hello-world/index.html"), "utf8")).toContain('<html lang="ja">');
  expect(await readFile(join(output, "ja/posts/hello-world/index.html"), "utf8")).toContain("继续访问");
  expect(await readFile(join(output, "en/links/index.html"), "utf8")).toContain("Example");
  expect(await readFile(join(output, "en/index.html"), "utf8")).toContain("/en/pages/about-page/");
  expect(await readFile(join(output, "assets/theme.css"), "utf8")).toContain("mjx-container.MathJax");
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

test("falls through missing requested content to Simplified Chinese", async () => {
  const output = join("/tmp", `mutiblog-render-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    locales: [{ code: "en", label: "English" }, { code: "zh-CN", label: "简体中文" }, { code: "ja", label: "日本語" }],
    site: { locales: { "zh-CN": { title: "测试站" } } },
    posts: [{ id: "hello-world", status: "published", locales: { "zh-CN": { title: "你好", markdown: "# 你好" } } }],
  };
  const report = await buildSite(input, output);
  expect(report.redirects).toContainEqual({ from: "/ja/posts/hello-world/", to: "/zh-CN/posts/hello-world/", status: 302 });
});

test("omits invalid crawler metadata until an old installation has a public URL", async () => {
  const output = join("/tmp", `mutiblog-no-base-url-${crypto.randomUUID()}`);
  outputs.push(output);
  await buildSite({
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    locales: [{ code: "zh-CN", label: "简体中文" }],
    site: { locales: { "zh-CN": { title: "No public URL" } } },
    posts: [],
  }, output);

  const robots = await readFile(join(output, "robots.txt"), "utf8");
  expect(robots).toBe("User-agent: *\nAllow: /\n");
  await expect(readFile(join(output, "sitemap.xml"), "utf8")).rejects.toMatchObject({ code: "ENOENT" });
  await expect(readFile(join(output, "zh-CN/rss.xml"), "utf8")).rejects.toMatchObject({ code: "ENOENT" });
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
    const page = (context) => "CUSTOM:" + context.currentPath + ":" + context.settings.accent + ":" + context.site.timezone;
    export const css = "body{color:rebeccapurple}";
    export const renderIndex = page;
    export const renderPost = page;
    export const renderPage = page;
    export const renderTaxonomy = page;
    export const renderLinks = page;
    export const renderArchive = page;
    export const renderSearch = page;
		export const postTemplates = { gallery: (context) => "GALLERY:" + context.post.title + ":" + context.post.pinned + ":" + context.site.timezone };
		export const categoryTemplates = { masonry: (context, name) => "MASONRY:" + name + ":" + context.taxonomy.cover + ":" + context.taxonomy.template };
	  `);
  await writeFile(join(themeRoot, "assets", "client.js"), "console.log('custom theme')");
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    timezone: "Asia/Shanghai",
    locales: [{ code: "zh-CN", label: "简体中文" }],
    site: { locales: { "zh-CN": { title: "测试站" } } },
	    posts: [{ id: "gallery-post", status: "published", template: "gallery", pinned: true, categories: ["engineering"], locales: { "zh-CN": { title: "相册", markdown: "body" } } }],
    categories: [{ id: "engineering", template: "masonry", cover: "/media/category.webp", locales: { "zh-CN": { name: "工程" } } }],
    theme: { id: "custom-theme", modulePath: join(themeRoot, "server.mjs"), assetsPath: join(themeRoot, "assets"), settings: { accent: "violet" }, postTemplates: [{ id: "gallery", name: "Gallery" }], categoryTemplates: [{ id: "masonry", name: "Masonry" }] },
  };
  await buildSite(input, output);
  expect(await readFile(join(output, "zh-CN/index.html"), "utf8")).toContain("CUSTOM:/:violet:Asia/Shanghai");
  expect(await readFile(join(output, "assets/theme.css"), "utf8")).toContain("rebeccapurple");
  expect(await readFile(join(output, "assets/themes/custom-theme/client.js"), "utf8")).toContain("custom theme");
  expect(await readFile(join(output, "zh-CN/posts/gallery-post/index.html"), "utf8")).toContain("GALLERY:相册:true:Asia/Shanghai");
  expect(await readFile(join(output, "zh-CN/categories/engineering/index.html"), "utf8")).toContain("MASONRY:工程:/media/category.webp:masonry");
	const collection = await readFile(join(output, "zh-CN/categories/index.html"), "utf8");
	expect(collection).toContain('data-taxonomy-collection="categories"');
	expect(collection).toContain("相册");
	expect(collection).toContain('<link rel="canonical" href="/zh-CN/categories/"/>');
	expect(collection).not.toContain("site-header");
});

test("uses an optional custom taxonomy collection renderer", async () => {
	const output = join("/tmp", `mutiblog-collection-${crypto.randomUUID()}`);
	const themeRoot = join("/tmp", `mutiblog-collection-theme-${crypto.randomUUID()}`);
	outputs.push(output, themeRoot);
	await mkdir(themeRoot, { recursive: true });
	await writeFile(join(themeRoot, "server.mjs"), `
		const page = () => "PAGE";
		export const css = "body{}";
		export const renderIndex = page; export const renderPost = page; export const renderPage = page;
		export const renderTaxonomy = page; export const renderLinks = page; export const renderArchive = page; export const renderSearch = page;
		export const renderCollection = (context) => "COLLECTION:" + context.collection.kind + ":" + context.collection.selectedId + ":" + context.posts.length + ":" + context.allPosts.length;
	`);
	const input: BuildInput = {
		schemaVersion: 1,
		sourceLocale: "en",
		locales: [{ code: "en", label: "English" }],
		site: { locales: { en: { title: "Collection theme" } } },
		posts: [{ id: "collection-post", status: "published", tags: ["release"], locales: { en: { title: "Collection post", markdown: "Body" } } }],
		tags: [{ id: "release", locales: { en: { name: "Release" } } }],
		theme: { id: "collection-theme", modulePath: join(themeRoot, "server.mjs") },
	};
	await buildSite(input, output);
	expect(await readFile(join(output, "en/tags/index.html"), "utf8")).toContain("COLLECTION:tags:release:1:1");
});

test("gives the first rendered detail a page-local posts slice and the complete allPosts set", async () => {
  const output = join("/tmp", `mutiblog-complete-detail-context-${crypto.randomUUID()}`);
  const themeRoot = join("/tmp", `mutiblog-complete-detail-theme-${crypto.randomUUID()}`);
  outputs.push(output, themeRoot);
  await mkdir(themeRoot, { recursive: true });
  await writeFile(join(themeRoot, "server.mjs"), `
    const page = () => "PAGE";
    export const css = "body{}";
    export const renderIndex = page;
    export const renderPost = (context) => "CURRENT:" + context.posts.map((post) => post.title).join("|") + ":ALL:" + context.allPosts.map((post) => post.title).join("|") + ":HEADINGS:" + context.post.headings.map((heading) => heading.id).join("|") + ":PREVIOUS:" + (context.cursor.previous?.title || "") + ":NEXT:" + (context.cursor.next?.title || "");
    export const renderPage = page; export const renderTaxonomy = page; export const renderLinks = page;
    export const renderArchive = page; export const renderSearch = page;
  `);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "en",
    locales: [{ code: "en", label: "English" }],
    site: { locales: { en: { title: "Complete context" } } },
    posts: [
      { id: "first-context-post", status: "published", locales: { en: { title: "First", markdown: "## First heading\n\nFirst" } } },
      { id: "second-context-post", status: "published", locales: { en: { title: "Second", markdown: "Second" } } },
    ],
    theme: { id: "complete-context", modulePath: join(themeRoot, "server.mjs") },
  };

  await buildSite(input, output);
  const firstDetail = await readFile(join(output, "en/posts/first-context-post/index.html"), "utf8");
  expect(firstDetail).toContain("CURRENT:First:ALL:First|Second:HEADINGS:first-heading:PREVIOUS::NEXT:Second");
});

test("builds bounded static pagination and discoverable taxonomy collections", async () => {
	const output = join("/tmp", `mutiblog-pagination-${crypto.randomUUID()}`);
	outputs.push(output);
	const suffix = (index: number) => index < 26 ? String.fromCharCode(97 + index) : `a${String.fromCharCode(97 + index - 26)}`;
	const posts: BuildInput["posts"] = Array.from({ length: 37 }, (_, index) => ({
		id: `pagination-${suffix(index)}`,
		status: "published",
		publishedAt: index < 13 ? `2026-08-${String(index + 1).padStart(2, "0")}T08:00:00Z` : `2026-07-${String((index - 13) % 24 + 1).padStart(2, "0")}T08:00:00Z`,
		categories: index < 13 ? ["engineering"] : [],
		tags: index < 13 ? ["release"] : [],
		locales: { en: { title: `Paged Post ${String(index + 1).padStart(2, "0")}`, summary: `Summary ${index + 1}`, markdown: index === 0 ? "## Introduction\n\nBody 1" : `Body ${index + 1}` } },
	}));
	const input: BuildInput = {
		schemaVersion: 1,
		sourceLocale: "en",
		baseUrl: "https://pages.example.com",
		locales: [{ code: "en", label: "English" }, { code: "ja", label: "日本語" }],
		site: { locales: { en: { title: "Paged site" } } },
		dictionaries: { en: {
			home: "Home", archives: "Archives", links: "Links", search: "Search", menu: "Menu", colorScheme: "Color scheme", poweredBy: "Powered by MutiBlog",
			posts: "Posts", categories: "Categories", tags: "Tags", all: "All", noPosts: "No posts", morePosts: "More posts", undated: "Undated", pagination: "Pagination", page: "Page", previousPage: "Previous", nextPage: "Next", tableOfContents: "Table of contents", postNavigation: "Post navigation", previousPost: "Previous post", nextPost: "Next post",
			recentPosts: "Recent posts", statisticsUnavailable: "Statistics unavailable", visits: "Visits", comments: "Comments", scrollTop: "Scroll to top", redirecting: "Continue", notFound: "Not found",
		} },
		posts,
		categories: [{ id: "engineering", cover: "/media/engineering.webp", locales: { en: { name: "Engineering", description: "Engineering articles" } } }, { id: "backend", parentId: "engineering", locales: { en: { name: "Backend", description: "Backend articles" } } }],
		tags: [{ id: "release", cover: "/media/release.webp", locales: { en: { name: "Release", description: "Release notes" } } }],
		linkGroups: [{ id: "friends", order: 0, locales: { en: { name: "Friends" } } }],
		links: [{ id: "example-site", groupId: "friends", order: 0, url: "https://example.com", locales: { en: { name: "Example" } } }],
		theme: { id: "earth", settings: { layout: { showHero: false, showSidebar: true }, sidebar: { widgets: ["profile", "recent-posts"] } } },
	};

	const report = await buildSite(input, output);
	const homeFirst = await readFile(join(output, "en/index.html"), "utf8");
	const homeMiddle = await readFile(join(output, "en/page/2/index.html"), "utf8");
	const homeLast = await readFile(join(output, "en/page/4/index.html"), "utf8");
	expect(homeFirst.match(/<article class="post-card">/g)).toHaveLength(12);
	expect(homeMiddle.match(/<article class="post-card">/g)).toHaveLength(12);
	expect(homeLast.match(/<article class="post-card">/g)).toHaveLength(1);
	expect(homeFirst).toContain('<link rel="canonical" href="https://pages.example.com/en/"/>');
	expect(homeFirst).toContain('<link rel="next" href="https://pages.example.com/en/page/2/"/>');
	expect(homeMiddle).toContain('<link rel="prev" href="https://pages.example.com/en/"/>');
	expect(homeMiddle).toContain('<link rel="next" href="https://pages.example.com/en/page/3/"/>');
	expect(homeMiddle).toContain('hrefLang="ja" href="https://pages.example.com/ja/page/2/"');
	expect(homeLast).toContain('<link rel="prev" href="https://pages.example.com/en/page/3/"/>');
	expect(homeLast).not.toContain('<link rel="next"');
	for (const page of [1, 2, 3, 4]) expect(homeMiddle).toContain(`aria-label="Page ${page}"`);
	expect(homeFirst).toContain('href="/en/categories/engineering/"');
	expect(homeFirst).toContain('aria-current="page">All</a>');
	expect(homeFirst).toContain("Engineering");

	const categoryFirst = await readFile(join(output, "en/categories/engineering/index.html"), "utf8");
	const categoryLast = await readFile(join(output, "en/categories/engineering/page/2/index.html"), "utf8");
	const tagLast = await readFile(join(output, "en/tags/release/page/2/index.html"), "utf8");
	expect(categoryFirst.match(/<article class="post-card">/g)).toHaveLength(12);
	expect(categoryLast.match(/<article class="post-card">/g)).toHaveLength(1);
	expect(categoryFirst).toContain('<link rel="next" href="https://pages.example.com/en/categories/engineering/page/2/"/>');
	expect(categoryLast).toContain('<link rel="canonical" href="https://pages.example.com/en/categories/engineering/page/2/"/>');
	expect(categoryLast).toContain('rel="prev" href="https://pages.example.com/en/categories/engineering/"');
	expect(categoryLast).not.toContain('<link rel="next"');
	expect(categoryFirst).toContain('class="taxonomy-filter-branch"');
	expect(categoryFirst).toContain('href="/en/categories/backend/"');
	expect(tagLast.match(/<article class="post-card">/g)).toHaveLength(1);
	expect(report.redirects).toContainEqual({ from: "/ja/categories/engineering/page/2/", to: "/en/categories/engineering/page/2/", status: 302 });
	expect(report.redirects).not.toContainEqual(expect.objectContaining({ from: "/ja/categories/engineering/page/3/" }));
	expect(await readFile(join(output, "ja/categories/engineering/page/2/index.html"), "utf8")).toContain('href="/en/categories/engineering/page/2/"');
	await expect(readFile(join(output, "en/categories/engineering/page/3/index.html"), "utf8")).rejects.toMatchObject({ code: "ENOENT" });
	await expect(readFile(join(output, "en/page/1/index.html"), "utf8")).rejects.toMatchObject({ code: "ENOENT" });
	await expect(readFile(join(output, "en/page/5/index.html"), "utf8")).rejects.toMatchObject({ code: "ENOENT" });

	const categories = await readFile(join(output, "en/categories/index.html"), "utf8");
	const tags = await readFile(join(output, "en/tags/index.html"), "utf8");
	for (const html of [categories, tags, categoryFirst, await readFile(join(output, "en/archives/index.html"), "utf8"), await readFile(join(output, "en/links/index.html"), "utf8")]) {
		expect(html).toContain('class="sidebar-card');
	}
	expect(categories).toContain('data-taxonomy-collection="categories"');
	expect(categories).toContain('<link rel="canonical" href="https://pages.example.com/en/categories/"/>');
	expect(categories).toContain('hrefLang="ja" href="https://pages.example.com/ja/categories/"');
	expect(categories.match(/<article class="post-card">/g)).toHaveLength(10);
	expect(categories).toContain('aria-current="page">Engineering</a>');
	expect(categories).toContain('class="more-posts" href="/en/categories/engineering/"');
	expect(tags).toContain('data-taxonomy-collection="tags"');
	expect(tags.match(/<article class="post-card">/g)).toHaveLength(10);
	expect(tags).toContain('class="more-posts" href="/en/tags/release/"');

	const firstDetail = await readFile(join(output, "en/posts/pagination-a/index.html"), "utf8");
	expect(firstDetail).toContain('<dd data-profile-stat="posts">37</dd>');
	expect(firstDetail).toContain('<h2 id="introduction">Introduction</h2>');
	expect(firstDetail).toContain('Table of contents');
	expect(firstDetail).toContain('href="#introduction"');
	expect(firstDetail).toContain('href="/en/posts/pagination-b/"');
	expect(firstDetail).toContain('Next post');
	const secondDetail = await readFile(join(output, "en/posts/pagination-b/index.html"), "utf8");
	expect(secondDetail).toContain('href="/en/posts/pagination-a/"');
	expect(secondDetail).toContain('href="/en/posts/pagination-c/"');
	const archiveFirst = await readFile(join(output, "en/archives/index.html"), "utf8");
	const archiveMiddle = await readFile(join(output, "en/archives/page/2/index.html"), "utf8");
	expect(archiveFirst).toContain('<link rel="next" href="https://pages.example.com/en/archives/page/2/"/>');
	expect(archiveMiddle).toContain('<link rel="prev" href="https://pages.example.com/en/archives/"/>');
	expect(archiveMiddle).toContain('<link rel="next" href="https://pages.example.com/en/archives/page/3/"/>');
	expect(archiveMiddle).toContain("August 2026");
	expect(archiveMiddle).toContain("July 2026");
	expect(archiveMiddle).toContain("Engineering");
	expect(archiveMiddle).toContain("#Release");
	expect(archiveMiddle).toContain("Summary 13");
	const archiveLast = await readFile(join(output, "en/archives/page/4/index.html"), "utf8");
	expect(archiveLast.match(/<article>/g)).toHaveLength(1);
	expect(archiveLast).toContain('<link rel="prev" href="https://pages.example.com/en/archives/page/3/"/>');
	expect(archiveLast).not.toContain('<link rel="next"');

	const search = JSON.parse(await readFile(join(output, "en/search/index.json"), "utf8"));
	expect(search).toContainEqual(expect.objectContaining({ kind: "Post", id: "pagination-ak" }));
	expect(search).toContainEqual(expect.objectContaining({ kind: "Category", id: "engineering", url: "/en/categories/engineering/" }));
	expect(search).toContainEqual(expect.objectContaining({ kind: "Tag", id: "release", url: "/en/tags/release/" }));
	const rss = await readFile(join(output, "en/rss.xml"), "utf8");
	expect(rss.match(/<item>/g)).toHaveLength(37);
	expect(rss).toContain("Paged Post 37");
	const sitemap = await readFile(join(output, "sitemap.xml"), "utf8");
	for (const path of ["/en/page/4/", "/en/archives/page/4/", "/en/categories/", "/en/tags/", "/en/categories/engineering/page/2/", "/en/tags/release/page/2/"]) expect(sitemap).toContain(`https://pages.example.com${path}`);
	expect(sitemap).not.toContain("/page/1/");
	expect(sitemap).not.toContain("/page/5/");
	expect(sitemap).not.toContain("https://pages.example.com/ja/categories/engineering/page/2/");
	expect(report.files).toBe(await countOutputFiles(output));
	expect(JSON.parse(await readFile(join(output, "build-report.json"), "utf8")).files).toBe(report.files);
});

test("bounds taxonomy fallback redirects by the destination locale posts", async () => {
	const output = join("/tmp", `mutiblog-taxonomy-fallback-pages-${crypto.randomUUID()}`);
	outputs.push(output);
	const posts: BuildInput["posts"] = Array.from({ length: 12 }, (_, index) => ({
		id: `fallback-post-${String.fromCharCode(97 + index)}`,
		status: "published",
		sourceLocale: "zh-CN",
		categories: ["news"],
		tags: ["release"],
		locales: { "zh-CN": { title: `中文文章 ${index + 1}`, markdown: "正文" } },
	}));
	posts.push({
		id: "legacy-french-only",
		status: "published",
		categories: ["news"],
		tags: ["release"],
		locales: { fr: { title: "Article français", markdown: "Corps" } },
	});
	const input: BuildInput = {
		schemaVersion: 1,
		sourceLocale: "zh-CN",
		locales: [{ code: "zh-CN", label: "简体中文" }, { code: "fr", label: "Français" }],
		site: { locales: { "zh-CN": { title: "分页回退" } } },
		posts,
		categories: [{ id: "news", sourceLocale: "zh-CN", locales: { "zh-CN": { name: "新闻" } } }],
		tags: [{ id: "release", sourceLocale: "zh-CN", locales: { "zh-CN": { name: "发布" } } }],
	};

	const report = await buildSite(input, output);
	for (const [kind, id] of [["categories", "news"], ["tags", "release"]] as const) {
		expect(report.redirects).toContainEqual({ from: `/fr/${kind}/${id}/`, to: `/zh-CN/${kind}/${id}/`, status: 302 });
		expect(report.redirects).not.toContainEqual(expect.objectContaining({ from: `/fr/${kind}/${id}/page/2/` }));
		await expect(readFile(join(output, `fr/${kind}/${id}/page/2/index.html`), "utf8")).rejects.toMatchObject({ code: "ENOENT" });
		await expect(readFile(join(output, `zh-CN/${kind}/${id}/page/2/index.html`), "utf8")).rejects.toMatchObject({ code: "ENOENT" });
	}
});

test("does not generate empty taxonomy collection routes", async () => {
	const output = join("/tmp", `mutiblog-empty-collections-${crypto.randomUUID()}`);
	outputs.push(output);
	const input: BuildInput = {
		schemaVersion: 1,
		sourceLocale: "en",
		baseUrl: "https://empty.example.com",
		locales: [{ code: "en", label: "English" }],
		site: { locales: { en: { title: "Empty collections" } } },
		posts: [],
	};

	await buildSite(input, output);
	await expect(readFile(join(output, "en/categories/index.html"), "utf8")).rejects.toMatchObject({ code: "ENOENT" });
	await expect(readFile(join(output, "en/tags/index.html"), "utf8")).rejects.toMatchObject({ code: "ENOENT" });
	const sitemap = await readFile(join(output, "sitemap.xml"), "utf8");
	expect(sitemap).not.toContain("/en/categories/");
	expect(sitemap).not.toContain("/en/tags/");
});

test("formats Earth publication dates in the configured site timezone", async () => {
  const output = join("/tmp", `mutiblog-timezone-${crypto.randomUUID()}`);
  outputs.push(output);
  const publishedAt = "2026-08-10T20:30:00Z";
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "en",
    timezone: "Asia/Shanghai",
    locales: [{ code: "en", label: "English" }],
    site: { locales: { en: { title: "Timezone site" } } },
    dictionaries: { en: { archives: "Archives", search: "Search", links: "Links", notFound: "Not found", redirecting: "Continue" } },
    posts: [{ id: "timezone-post", status: "published", publishedAt, locales: { en: { title: "Timezone post", markdown: "Body" } } }],
  };
  await buildSite(input, output);
  const homepage = await readFile(join(output, "en/index.html"), "utf8");
  const article = await readFile(join(output, "en/posts/timezone-post/index.html"), "utf8");
  const archive = await readFile(join(output, "en/archives/index.html"), "utf8");
  const expected = new Intl.DateTimeFormat("en", { timeZone: "Asia/Shanghai" }).format(new Date(publishedAt));
  const utcDate = new Intl.DateTimeFormat("en", { timeZone: "UTC" }).format(new Date(publishedAt));
  expect(expected).not.toBe(utcDate);
  for (const html of [homepage, article, archive]) {
    expect(html).toContain(expected);
    expect(html).not.toContain(utcDate);
  }
  expect(article).toContain('data-timezone="Asia/Shanghai"');
});

test("rejects an invalid site timezone", async () => {
  const output = join("/tmp", `mutiblog-timezone-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "en",
    timezone: "Mars/Olympus_Mons",
    locales: [{ code: "en", label: "English" }],
    site: { locales: { en: { title: "Invalid timezone" } } },
    posts: [],
  };
  await expect(buildSite(input, output)).rejects.toThrow("invalid site timezone");
});

test("rejects non-local or traversal taxonomy collection covers", async () => {
	const output = join("/tmp", `mutiblog-taxonomy-cover-${crypto.randomUUID()}`);
	outputs.push(output);
	const input: BuildInput = {
		schemaVersion: 1,
		sourceLocale: "en",
		locales: [{ code: "en", label: "English" }],
		site: { locales: { en: { title: "Safe covers" } } },
		posts: [],
		tags: [{ id: "unsafe-cover", cover: "https://tracker.example/cover.webp", locales: { en: { name: "Unsafe" } } }],
	};
	await expect(buildSite(input, output)).rejects.toThrow("invalid taxonomy cover: unsafe-cover");
	input.tags![0].cover = "/media/%2e%2e/secrets.txt";
	await expect(buildSite(input, output)).rejects.toThrow("invalid taxonomy cover: unsafe-cover");
	input.tags![0] = { id: "../unsafe", locales: { en: { name: "Unsafe" } } };
	await expect(buildSite(input, output)).rejects.toThrow("invalid taxonomy id: ../unsafe");
});

test("applies the built-in Earth setting groups to generated markup and CSS", async () => {
  const output = join("/tmp", `mutiblog-earth-settings-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    locales: [{ code: "zh-CN", label: "简体中文" }, { code: "en", label: "English" }],
    site: { locales: { "zh-CN": { title: "地球站", description: "站点说明" }, en: { title: "Earth site", description: "Site description" } } },
    comments: { moderation: "pending", pageSize: 20, maxLength: 4321 },
    dictionaries: {
      "zh-CN": { home: "首页", archives: "归档", links: "友链", search: "搜索", colorScheme: "明暗", about: "关于", languages: "语言", poweredBy: "由 MutiBlog 驱动", posts: "文章", categories: "分类", tags: "文章标签", visits: "访问", popularPosts: "热门文章", recentPosts: "最新文章", statisticsUnavailable: "统计不可用", redirecting: "正在前往页面…", comments: "评论", loadingComments: "加载中", commentsUnavailable: "不可用", commentEmpty: "暂无评论", commentPending: "待审核", commentsMore: "更多评论", commentName: "姓名", commentEmail: "邮箱", commentWebsite: "网站", commentContent: "内容", commentSubmit: "提交", commentsClosed: "已关闭" },
    },
    posts: [{
      id: "earth-settings",
      status: "published",
      cover: "/media/hidden-cover.webp",
      publishedAt: "2026-08-11T00:00:00Z",
      categories: ["engineering"],
      tags: ["release"],
      locales: {
        "zh-CN": { title: "设置文章", summary: "不应显示的摘要", markdown: "正文" },
        en: { title: "Settings post", summary: "Hidden summary", markdown: "Body" },
      },
    }],
    categories: [{ id: "engineering", locales: { "zh-CN": { name: "不应显示的分类" }, en: { name: "Hidden category" } } }],
    tags: [{ id: "release", locales: { "zh-CN": { name: "不应显示的标签" }, en: { name: "Hidden tag" } } }],
    theme: {
      id: "earth",
      settings: {
        global: { sticky: false, showBrandSymbol: true, brandSymbol: "地", showSearch: false, showLanguageSwitcher: false, showColorSchemeToggle: false },
        layout: { showHero: true, heroKicker: "自定义眉题", heroHeight: "compact", postListLayout: "single", showSidebar: true, showPostCovers: false, coverRatio: "square", showPostSummaries: false, showCategories: false, showPublishedAt: false },
        post: { contentWidth: "wide", contentFont: "sans", showCover: false, showSummary: false, showCategories: false, showTags: false, showPublishedAt: false, showUpvoteButton: false },
        style: { accentColor: "#ff3366", defaultColorScheme: "dark", cardRadius: "square", cardShadow: "none", bodyFont: "serif", showCardBorder: false },
        comments: { showSection: true, showAvatars: false, showEmailField: false, showWebsiteField: false, formLayout: "stacked", showTimestamps: false },
        sidebar: { widgets: ["profile", "popular-posts", "categories", "tags", "languages"], position: "left", sticky: true, profileStyle: "plain", showAbout: false, showLanguages: true },
        footer: { showFooter: true, layout: "centered", showSiteDescription: false, showPoweredBy: false, showTopBorder: false, copyright: "保留所有权利" },
      },
    },
  };

  await buildSite(input, output);
  const homepage = await readFile(join(output, "zh-CN/index.html"), "utf8");
  expect(homepage).toContain("site-header-static");
  expect(homepage).toContain('<meta name="view-transition" content="same-origin"/>');
  expect(homepage).toContain('data-page-loading-label="正在前往页面…"');
  expect(homepage).toContain('data-page-loading-status');
  expect(homepage).toContain("pageNavigationQualifies");
  expect(homepage).toContain('<span class="brand-symbol" aria-hidden="true">地</span>');
  expect(homepage).not.toContain('class="locale-picker"');
  // The initialization script keeps its selector and configured default even without a visible toggle.
  expect(homepage).not.toMatch(/<button\b[^>]*\bdata-color-scheme(?:\s|=|>)/);
  expect(homepage).toContain('const d="dark"');
  expect(homepage).not.toContain("/zh-CN/search/");
  expect(homepage).toContain("hero hero-compact");
  expect(homepage).toContain("自定义眉题");
  expect(homepage).toContain("post-grid post-grid-single");
  expect(homepage).toContain("earth-layout sidebar-left");
  expect(homepage).toContain("sidebar-card sidebar-sticky sidebar-profile-plain");
  expect(homepage).toContain('class="sidebar-profile"');
  expect(homepage).toContain('class="profile-stats"');
  expect(homepage).toContain('data-profile-stat="comments"');
  expect(homepage).toContain("/api/v1/public/stats");
  expect(homepage).not.toContain("hidden-cover.webp");
  expect(homepage).not.toContain("不应显示的摘要");
  expect(homepage).toContain("body-font-serif cards-borderless");
  expect(homepage).toContain('data-visual-preset="material-glass"');
  expect(homepage).toContain("--accent:#ff3366");
  expect(homepage).toContain("--card-radius:0");
  expect(homepage).toContain("--card-shadow:none");
  expect(homepage).toContain("--material-surface-shadow:none");
  expect(homepage).toContain("site-footer site-footer-centered site-footer-style-1 site-footer-borderless");
  expect(homepage).toContain("保留所有权利");
  expect(homepage).not.toContain("由 MutiBlog 驱动");

  const article = await readFile(join(output, "zh-CN/posts/earth-settings/index.html"), "utf8");
  expect(article).toContain("article-shell article-width-wide");
  expect(article).toContain("earth-layout sidebar-left");
  expect(article).toContain('data-visit-kind="Post"');
  expect(article).toContain("/api/v1/public/visits");
  expect(article).toContain("markdown-body markdown-font-sans");
  expect(article).not.toContain('class="article-cover"');
  expect(article).not.toContain("<p>不应显示的摘要</p>");
  expect(article).not.toContain('class="post-taxonomy"');
  expect(article).not.toContain('class="article-tags"');
  expect(article).not.toMatch(/<button\b[^>]*\bdata-upvote(?:\s|=|>)/);
  expect(article).toContain("comment-form comment-form-stacked");
  expect(article).toContain('data-show-avatars="false"');
  expect(article).toContain('data-show-timestamps="false"');
  expect(article).not.toContain('name="email"');
  expect(article).not.toContain('name="website"');
  expect(article).toContain('maxLength="4321"');
  expect(article).toContain('class="comments-more"');
  expect(article).toContain("更多评论");
  expect(article).toContain('href="/assets/theme.css"');

  const css = await readFile(join(output, "assets/theme.css"), "utf8");
  expect(css).toContain(".upvote-action");
  expect(css).toContain(".comment-list article");
  expect(css).toContain(".comment-avatar");
  expect(css).toContain('.comments[data-state="empty"] .comments-loading');
  expect(css).toContain('.comments[data-state="unavailable"] .comments-loading');
  expect(css).toContain(".comments-more[hidden]");
  expect(css).toContain(".comment-form input");
  expect(css).toContain(".comment-form input:focus");
  expect(css).toContain('.comment-form[data-state="submitting"]');
  expect(css).toContain(".comment-form-two-column { grid-template-columns: 1fr; }");
  expect(css).toContain("@view-transition { navigation: auto; }");
  expect(css).toContain(".site-navigation-progress");
  expect(css).toContain("@view-transition { navigation: none; }");
  expect(css).toContain('body.cards-borderless[data-visual-preset="material-glass"]');
  expect(css).toContain('body[data-visual-preset="material-glass"] .sidebar-profile-plain');
  expect(css).toContain("mjx-container.MathJax");
});

test("maps the Halo Earth source settings to real header, content, sidebar, sharing, and footer output", async () => {
  const output = join("/tmp", `mutiblog-earth-source-settings-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    baseUrl: "https://blog.example.com",
    primaryMenu: "primary",
    locales: [{ code: "zh-CN", label: "简体中文" }],
    site: { locales: { "zh-CN": { title: "地球站", subtitle: "来自源码的设置" } } },
    dictionaries: { "zh-CN": { home: "首页", archives: "归档", links: "友链", search: "搜索", menu: "菜单", colorScheme: "明暗", about: "关于", languages: "语言", poweredBy: "由 MutiBlog 驱动", posts: "文章", categories: "分类", tags: "文章标签", visits: "访问", popularPosts: "热门文章", recentPosts: "最新文章", statisticsUnavailable: "统计不可用", share: "分享", nativeShare: "系统分享", wechatCopyLink: "微信复制", copyLink: "复制链接", copied: "已复制", close: "关闭", wechatScan: "微信扫码", upvote: "点赞", upvoted: "已点赞", upvoteCount: "点赞数", upvotesUnavailable: "点赞不可用", scrollTop: "返回顶部", comments: "评论", loadingComments: "加载中", commentsUnavailable: "不可用", commentEmpty: "暂无评论", commentPending: "待审核", commentsMore: "更多评论", commentName: "姓名", commentEmail: "邮箱", commentWebsite: "网站", commentContent: "内容", commentSubmit: "提交", commentsClosed: "已关闭" } },
    posts: [{ id: "source-settings", status: "published", cover: "/media/post-cover.webp", tags: ["earth"], locales: { "zh-CN": { title: "源码设置文章", summary: "设置摘要", markdown: "# 正文" } } }],
    tags: [{ id: "earth", locales: { "zh-CN": { name: "Earth" } } }],
    menus: [{ id: "primary", locales: { "zh-CN": { label: "主菜单" } }, items: [{ id: "home", targetKind: "internal", url: "/", openInNew: false, order: 0, locales: { "zh-CN": { label: "主页" } } }] }, { id: "footer-links", locales: { "zh-CN": { label: "页脚链接" } }, items: [{ id: "about", targetKind: "internal", url: "/pages/about/", openInNew: false, order: 0, locales: { "zh-CN": { label: "关于" } } }] }],
    theme: { id: "earth", settings: {
      global: { logoType: "image", logoImage: "/media/header-logo.svg", showScrollButton: true },
      layout: { headerWidget: "latest-post", headerBackgroundType: "image", headerBackgroundImage: "/media/hero.webp", headerTitleColor: "#ffeecc", contentHeader: true },
      post: { showCover: true, titlePosition: "cover", coverHeight: "24rem", contentStyle: "typography", showUpvoteButton: true, showShareButton: true, shareItems: ["native", "x"] },
      style: { visualPreset: "earth-classic" },
      sidebar: { widgets: ["tags", "profile"], profileLogo: "/media/profile.webp", socialLinks: [{ icon: "github", name: "", url: "https://github.com/example", kind: "link" }] },
      footer: { showFooter: true, style: "style-2", layout: "centered", logo: "/media/footer.svg", title: "页脚标题", slogan: "页脚标语", menuIds: ["footer-links"], socialLinks: [{ icon: "rss", name: "RSS", url: "/zh-CN/rss.xml", kind: "link" }], copyright: "自定义版权" },
    } },
  };

  await buildSite(input, output);
  const homepage = await readFile(join(output, "zh-CN/index.html"), "utf8");
  expect(homepage).toContain('data-visual-preset="earth-classic"');
  expect(homepage).toContain('class="brand-image"');
  expect(homepage).toContain('/media/header-logo.svg');
  expect(homepage).toContain('hero-widget-latest-post');
  expect(homepage).toContain('/media/hero.webp');
  expect(homepage).toContain('class="scroll-top"');
  expect(homepage).toContain('aria-label="返回顶部"');
  expect(homepage.indexOf("<h3>文章标签")).toBeLessThan(homepage.indexOf('class="sidebar-profile"'));
  expect(homepage).toContain('aria-label="github"');
  expect(homepage).toContain('aria-hidden="true">GH</span>');
  expect(homepage).toContain('/media/footer.svg');
  expect(homepage).toContain("页脚标题");
  expect(homepage).toContain("页脚标语");
  expect(homepage).toContain("页脚链接");
  expect(homepage).toContain("RSS");

  const article = await readFile(join(output, "zh-CN/posts/source-settings/index.html"), "utf8");
  expect(article).toContain('class="content-cover"');
  expect(article).toContain('min-height:24rem');
  expect(article).toContain("markdown-style-typography");
  expect(article).toContain("data-upvote");
  expect(article).toContain('data-kind="Post"');
  expect(article).toContain("/api/v1/public/upvotes");
  expect(article).toContain('class="share-trigger"');
  expect(article).toContain('class="share-dialog"');
  expect(article).toContain("复制链接");
  expect(article).toContain("微信扫码");
  expect(article).toContain("data-share-native");
  expect(article).toContain("data-share-qr");
  expect(article).toContain("/api/v1/public/share-qr");
  expect(article).toContain("https://x.com/intent/post");
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

test("rejects a declared category template without a renderer", async () => {
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
    theme: { id: "broken-theme", modulePath: join(themeRoot, "server.mjs"), categoryTemplates: [{ id: "masonry", name: "Masonry" }] },
  };
  await expect(buildSite(input, output)).rejects.toThrow("theme category template is missing renderer: masonry");
});
