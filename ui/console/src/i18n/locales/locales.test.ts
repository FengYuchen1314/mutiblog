import { describe, expect, test } from "vitest";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import en from "./en";
import zhCN from "./zh-CN";

function keys(value: unknown, prefix = ""): string[] {
  if (!value || typeof value !== "object") return [prefix];
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    keys(child, prefix ? `${prefix}.${key}` : key),
  );
}

describe("console dictionaries", () => {
  test("have the same message keys", () => {
    expect(keys(zhCN).sort()).toEqual(keys(en).sort());
  });

  test("keeps console copy in dictionaries instead of view literals", async () => {
    const sourceRoot = join(dirname(fileURLToPath(import.meta.url)), "../..");
    const files = await sourceFiles(sourceRoot);
    const violations: string[] = [];
    for (const file of files) {
      const name = relative(sourceRoot, file);
      if (name.startsWith("i18n/locales/")) continue;
      const lines = (await readFile(file, "utf8")).split("\n");
      lines.forEach((line, index) => {
        if (!/[一-龥]/.test(line) || allowedNativeLanguageName(name, line)) return;
        violations.push(`${name}:${index + 1}`);
      });
    }
    expect(violations).toEqual([]);
  });

  test("keeps the setup source locale read-only and submits zh-CN", async () => {
    const source = await viewSource("SetupView.vue");

    expect(source).toContain('const SOURCE_LOCALE = "zh-CN";');
    expect(source).toContain("sourceLocale: SOURCE_LOCALE,");
    expect(source).toMatch(/api\.setup\(\{ \.\.\.form, sourceLocale: SOURCE_LOCALE \}/);
    expect(source).toContain('readonly aria-readonly="true"');
    expect(source).not.toContain('v-model="form.sourceLocale"');
  });

  test("keeps added locales permanent and delegates whole-site translation to the backend", async () => {
    const source = await viewSource("LocalesView.vue");

    expect(source).toContain("api.updateLocales(csrfToken, enabled, SOURCE_LOCALE, taskId)");
    expect(source).toContain("{ ...entry, enabled: true }");
    expect(source).toContain('"localesPage.permanent"');
    expect(source).toContain('"localesPage.pendingSave"');
    expect(source).toContain('t("localesPage.saveAndTranslate")');
    expect(source).toContain('t("localesPage.translationWorkflowHelp")');
    expect(source).not.toContain("window.confirm");
    expect(source).not.toContain("removeLocale");
    expect(source).not.toContain('v-model="sourceLocale"');
    expect(source).not.toContain('v-model="entry.enabled"');
    expect(source).not.toContain("api.dictionaries");
    expect(source).not.toContain("api.updateDictionary");
  });
});

async function viewSource(name: string) {
  const sourceRoot = join(dirname(fileURLToPath(import.meta.url)), "../..");
  return readFile(join(sourceRoot, "views", name), "utf8");
}

async function sourceFiles(root: string): Promise<string[]> {
  const result: string[] = [];
  for (const entry of await readdir(root, { withFileTypes: true })) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) result.push(...(await sourceFiles(path)));
    else if (/\.(?:ts|vue)$/.test(entry.name)) result.push(path);
  }
  return result;
}

function allowedNativeLanguageName(file: string, line: string) {
  if (file === "views/LocalesView.vue" && /^\s+(?:"(?:zh-CN|zh-TW)"|ja):/.test(line)) return true;
  return (
    (file === "views/SetupView.vue" || file === "views/SettingsView.vue") &&
    line.includes('<option value="zh-CN">简体中文</option>')
  );
}
