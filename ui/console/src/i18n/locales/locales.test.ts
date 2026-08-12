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

    expect(source).toContain("api.updateLocales(csrfToken, enabled, SOURCE_LOCALE)");
    expect(source).toContain("if (result.task)");
    expect(source).toContain("observeLocaleTask(result.task)");
    expect(source).toContain('api.tasks({ kind: "LocaleProvision", limit: 1 })');
    expect(source).toContain("trackLocaleTaskChildren(task)");
    expect(source).not.toContain("createLocalesBuildTask");
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

  test("presents durable whole-site localization in the unified task tree", async () => {
    const taskCenter = await viewSource("TaskCenterView.vue");
    const progress = await sourceFile("components/TaskProgress.vue");
    const client = await sourceFile("api/client.ts");

    expect(client).toContain('"LocaleProvision"');
    expect(client).toContain('localization: { status: "queued" | "idle"; taskId?: string }');
    expect(client).toContain('build: { status: "deferred" | "skipped" }');
    expect(taskCenter).toContain('<option value="LocaleProvision">');
    expect(taskCenter).toContain("buildTaskTree(tasks.value)");
    expect(taskCenter).toContain("taskCenter.expandTask");
    expect(taskCenter).toContain("taskCenter.targetResults");
    expect(taskCenter).toContain("taskCenter.openLocales");
    expect(progress).toContain("statusLabel");
    expect(progress).toContain("taskFailure");
  });

  test("only starts content translation through publish", async () => {
    const editor = await viewSource("PostEditorView.vue");
    const posts = await viewSource("PostsView.vue");
    const pages = await viewSource("PagesView.vue");
    const client = await sourceFile("api/client.ts");

    expect(editor).toContain('t("editorPage.translationPolicy")');
    expect(editor).toContain("trackTranslationTask(result.translation.taskId)");
    expect(editor).toContain("reconcilePublication(buildTaskId, result.build.taskId)");
    expect(editor).toContain('result.translation.status === "running"');
    expect(editor).toContain('result.build.status === "deferred"');
    expect(editor).toContain('result.build.status === "blocked"');
    expect(editor).not.toContain("async function translate(");
    expect(editor).not.toContain('@click="translate');
    expect(editor).not.toContain("editorPage.translateAll");
    expect(editor).not.toContain("editorPage.translateLocale");
    expect(posts).toContain('result.build.status === "blocked"');
    expect(posts).toContain('result.translation.status === "not-configured"');
    expect(posts).toContain("reconcileBuildTask(taskId, result.build.taskId, result.translation.taskId)");
    expect(pages).toContain('result.build.status === "blocked"');
    expect(pages).toContain('result.translation.status === "not-configured"');
    expect(pages).toContain("reconcileBuildTask(taskId, result.build.taskId, result.translation.taskId)");
    expect(client).toContain('"deferred" | "blocked"');
    expect(client).not.toContain("startTranslation:");
    expect(client).not.toContain("startPageTranslation:");
    expect(zhCN.editorPage.translationPolicy).toContain("不会触发翻译");
    expect(zhCN.editorPage.translationPolicy).toContain("离开此页面后任务仍会继续");
    expect(en.editorPage.translationPolicy).toContain("never starts translation");
    expect(en.editorPage.translationPolicy).toContain("continues after you leave this page");
  });

  test("keeps every non-source content locale AI-managed and read-only", async () => {
    const editor = await viewSource("PostEditorView.vue");
    const markdownEditor = await sourceFile("components/MarkdownEditor.vue");

    expect(editor).toContain('const SOURCE_LOCALE = "zh-CN";');
    expect(editor).toContain("const sourceEditable = computed(");
    expect(editor).toContain("post.value.meta.sourceLocale === sourceLocale.value");
    expect(editor).toContain("const translationReadOnly = computed(");
    expect(editor).toContain("const dirty = dirtyLocales[sourceLocale.value] ? [sourceLocale.value] : [];");
    expect(editor).not.toContain("Object.keys(dirtyLocales)");
    expect(editor).toContain(':readonly="translationReadOnly"');
    expect(editor).toContain(':disabled="!sourceEditable"');
    expect(editor).toContain("'read-only': !localeIsEditable(locale.code)");
    expect(editor.match(/const current = await save\(true\);/g)).toHaveLength(2);
    expect(editor).toContain('"editorPage.revisionSourceOnlyHelp"');
    expect(editor).toContain('"editorPage.legacyRevisionReadOnlyHelp"');
    expect(editor).toContain("!entitySourceEditable.value || (!sourceEditable.value && !allowInactiveSource)");
    expect(editor).toContain("if (!session.session || !sourceEditable.value || files.length === 0) return;");
    expect(markdownEditor).toContain("EditorState.readOnly.of(Boolean(props.readonly))");
    expect(markdownEditor).toContain("EditorView.editable.of(!props.readonly)");
    expect(markdownEditor).toContain("if (!editor || props.readonly) return;");
    expect(markdownEditor.match(/if \(props\.readonly\) \{/g)).toHaveLength(2);
    expect(zhCN.editorPage.aiTranslationReadOnlyHelp).toContain("只能编辑 {locale} 源内容");
    expect(en.editorPage.legacySourceReadOnlyHelp).toContain("Migrate it before editing");
  });
});

async function viewSource(name: string) {
  return sourceFile(join("views", name));
}

async function sourceFile(name: string) {
  const sourceRoot = join(dirname(fileURLToPath(import.meta.url)), "../..");
  return readFile(join(sourceRoot, name), "utf8");
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
