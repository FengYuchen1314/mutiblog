import { readFile } from "node:fs/promises";
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

test("builds localized pages and redirect records", async () => {
  const output = join("/tmp", `mutiblog-render-${crypto.randomUUID()}`);
  outputs.push(output);
  const input: BuildInput = {
    schemaVersion: 1,
    sourceLocale: "zh-CN",
    locales: [{ code: "zh-CN", label: "简体中文" }, { code: "en", label: "English" }, { code: "ja", label: "日本語" }],
    site: { locales: { "zh-CN": { title: "测试站" }, en: { title: "Test site" } } },
    posts: [{ id: "hello-world", status: "published", locales: { "zh-CN": { title: "你好", markdown: "# 你好" }, en: { title: "Hello", markdown: "# Hello" } } }],
  };
  const report = await buildSite(input, output);
  expect(report.redirects).toContainEqual({ from: "/ja/posts/hello-world/", to: "/en/posts/hello-world/", status: 302 });
  expect(await readFile(join(output, "en/posts/hello-world/index.html"), "utf8")).toContain("Hello");
  expect(JSON.parse(await readFile(join(output, "redirects.json"), "utf8"))).toContainEqual({ from: "/", to: "/zh-CN/", status: 302 });
});
