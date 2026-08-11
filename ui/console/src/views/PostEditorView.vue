<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { VButton, VPageHeader } from "@halo-dev/components";
import { api, ApiError, type ContentRevision, type LocalesConfig, type LocalizedMarkdown, type Post, type Taxonomy, type ThemeRecord } from "@/api/client";
import MarkdownEditor from "@/components/MarkdownEditor.vue";
import { useSessionStore } from "@/stores/session";
import { useCodeLabel } from "@/i18n/useCodeLabel";

const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const { t } = useI18n();
const codeLabel = useCodeLabel();
const isPage = route.name === "page-editor";
const entityLabel = () => t(isPage ? "editorPage.page" : "editorPage.post");
const editor = ref<InstanceType<typeof MarkdownEditor>>();
const post = ref<Post>();
const locales = ref<LocalesConfig>();
const activeLocale = ref("");
const saving = ref(false);
const publishing = ref(false);
const uploading = ref(false);
const translating = ref(false);
const saveState = ref(t("editorPage.unsaved"));
const error = ref("");
const customID = ref("");
const settingsOpen = ref(false);
const revisionsOpen = ref(false);
const revisions = ref<ContentRevision[]>([]);
const revisionComparison = ref<{ summary: ContentRevision; snapshot: Post }>();
const availableCategories = ref<Taxonomy[]>([]);
const availableTags = ref<Taxonomy[]>([]);
const activeTheme = ref<ThemeRecord>();
const postSettings = reactive({ categories: [] as string[], tags: [] as string[], cover: "", commentPolicy: "open" as "open" | "closed", template: isPage ? "page" : "post" });
const drafts = reactive<Record<string, LocalizedMarkdown>>({});
const dirtyLocales = reactive<Record<string, boolean>>({});
const form = ref<LocalizedMarkdown>({ title: "", summary: "", seoTitle: "", seoDescription: "", markdown: "" });
const templateOptions = computed(() => {
  const defaultID = isPage ? "page" : "post";
  const custom = isPage ? activeTheme.value?.pageTemplates : activeTheme.value?.postTemplates;
  const result = [{ id: defaultID, name: t("editorPage.defaultTemplate") }, ...(custom ?? [])];
  if (postSettings.template && !result.some((template) => template.id === postSettings.template)) {
    result.push({ id: postSettings.template, name: t("editorPage.unavailableTemplate", { id: postSettings.template }) });
  }
  return result;
});
const sourceComparison = computed(() => {
  if (!post.value || activeLocale.value === post.value.meta.sourceLocale) return undefined;
  const locale = post.value.meta.sourceLocale;
  const source = drafts[locale] ?? post.value.content[locale];
  return source ? { locale, title: source.title, markdown: source.markdown } : undefined;
});
const revisionComparisonLocale = computed(() => {
  if (!revisionComparison.value) return "";
  if (revisionComparison.value.snapshot.content[activeLocale.value]) return activeLocale.value;
  return revisionComparison.value.snapshot.meta.sourceLocale;
});

onMounted(async () => {
  try {
    const [localeConfig, themeResult] = await Promise.all([api.locales(), api.themes()]);
    locales.value = localeConfig;
    activeTheme.value = themeResult.items.find((theme) => theme.active);
    activeLocale.value = locales.value.sourceLocale;
    const id = typeof route.params.id === "string" ? route.params.id : "";
    if (id) {
      post.value = isPage ? await api.page(id) : await api.post(id);
      for (const [locale, value] of Object.entries(post.value.content)) {
        drafts[locale] = { ...value };
        dirtyLocales[locale] = false;
      }
      activeLocale.value = post.value.meta.sourceLocale;
      Object.assign(postSettings, { categories: [...post.value.meta.categories], tags: [...post.value.meta.tags], cover: post.value.meta.cover ?? "", commentPolicy: post.value.meta.commentPolicy, template: post.value.meta.template });
      loadDraft(activeLocale.value);
      saveState.value = t("editorPage.saved");
    }
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("editorPage.loadingFailed");
  }
});

async function toggleSettings() {
  settingsOpen.value = !settingsOpen.value;
  if (!settingsOpen.value || isPage) return;
  try {
    [availableCategories.value, availableTags.value] = await Promise.all([
      api.taxonomies("categories").then((result) => result.items),
      api.taxonomies("tags").then((result) => result.items),
    ]);
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("editorPage.settingsLoadFailed");
  }
}

async function toggleRevisions() {
  if (!post.value) return;
  revisionsOpen.value = !revisionsOpen.value;
  if (!revisionsOpen.value) return;
  try { revisions.value = (isPage ? await api.pageRevisions(post.value.meta.id) : await api.postRevisions(post.value.meta.id)).items; }
  catch (caught) { error.value = caught instanceof Error ? caught.message : t("editorPage.revisionsLoadFailed"); }
}

async function restoreRevision(revision: ContentRevision) {
  if (!session.session || !post.value || !window.confirm(t("editorPage.revisionConfirm", { revision: revision.revision }))) return;
  try {
    const result = isPage
      ? await api.restorePageRevision(session.session.csrfToken, post.value.meta.id, revision.id, post.value.meta.revision)
      : await api.restorePostRevision(session.session.csrfToken, post.value.meta.id, revision.id, post.value.meta.revision);
    post.value = result.post;
    for (const locale of Object.keys(drafts)) delete drafts[locale];
    for (const [locale, value] of Object.entries(post.value.content)) { drafts[locale] = { ...value }; dirtyLocales[locale] = false; }
    activeLocale.value = post.value.meta.sourceLocale;
    loadDraft(activeLocale.value);
    saveState.value = t("editorPage.revisionRestored");
    revisionsOpen.value = false;
    revisionComparison.value = undefined;
  } catch (caught) { error.value = caught instanceof Error ? caught.message : t("editorPage.revisionRestoreFailed"); }
}

async function compareRevision(revision: ContentRevision) {
  if (!post.value) return;
  try {
    const snapshot = isPage ? await api.pageRevision(post.value.meta.id, revision.id) : await api.postRevision(post.value.meta.id, revision.id);
    revisionComparison.value = { summary: revision, snapshot };
  } catch (caught) { error.value = caught instanceof Error ? caught.message : t("revisionCompare.failed"); }
}

async function saveSettings() {
	if (!session.session) return;
  try {
	const current = await save();
	if (!current) return;
    post.value = isPage
		? await api.updatePageSettings(session.session.csrfToken, current.meta.id, { revision: current.meta.revision, cover: postSettings.cover, commentPolicy: postSettings.commentPolicy, template: postSettings.template })
		: await api.updatePostSettings(session.session.csrfToken, current.meta.id, { revision: current.meta.revision, ...postSettings });
    saveState.value = t("editorPage.settingsSaved");
    settingsOpen.value = false;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("editorPage.settingsSaveFailed");
  }
}

function stashDraft() {
  if (activeLocale.value) drafts[activeLocale.value] = { ...form.value };
}

function loadDraft(locale: string) {
  const value = drafts[locale] ?? post.value?.content[locale];
  form.value = {
    title: value?.title ?? "",
    summary: value?.summary ?? "",
    seoTitle: value?.seoTitle ?? "",
    seoDescription: value?.seoDescription ?? "",
    markdown: value?.markdown ?? "",
  };
}

function selectLocale(locale: string) {
  stashDraft();
  activeLocale.value = locale;
  loadDraft(locale);
  saveState.value = t(dirtyLocales[locale] ? "editorPage.unsaved" : post.value?.content[locale] ? "editorPage.saved" : "editorPage.newTranslation");
}

function markDirty() {
  if (!activeLocale.value) return;
  dirtyLocales[activeLocale.value] = true;
  saveState.value = t("editorPage.unsaved");
}

async function save() {
  if (!session.session) return;
  const csrfToken = session.session.csrfToken;
  stashDraft();
  saving.value = true;
  error.value = "";
  saveState.value = t("editorPage.saving");
  try {
    if (!post.value) {
      const sourceLocale = locales.value?.sourceLocale ?? activeLocale.value;
      const body = { id: customID.value || undefined, ...(drafts[sourceLocale] ?? form.value) };
      post.value = isPage ? await api.createPage(csrfToken, body) : await api.createPost(csrfToken, body);
      activeLocale.value = post.value.meta.sourceLocale;
      drafts[activeLocale.value] = { ...post.value.content[activeLocale.value] };
      dirtyLocales[activeLocale.value] = false;
      Object.assign(postSettings, { categories: [...post.value.meta.categories], tags: [...post.value.meta.tags], cover: post.value.meta.cover ?? "", commentPolicy: post.value.meta.commentPolicy, template: post.value.meta.template });
      await router.replace(`/${isPage ? "pages" : "posts"}/editor/${post.value.meta.id}`);
    } else {
      const dirty = Object.keys(dirtyLocales).filter((locale) => dirtyLocales[locale]);
      if (dirty.length === 0) {
        saveState.value = t(post.value.meta.status === "published" ? "editorPage.published" : "editorPage.saved");
        return post.value;
      }
      for (const locale of dirty) {
        const body = { ...(drafts[locale] ?? post.value.content[locale] ?? { title: "", markdown: "" }), revision: post.value.meta.revision };
        post.value = isPage
          ? await api.updatePageLocale(csrfToken, post.value.meta.id, locale, body)
          : await api.updatePostLocale(csrfToken, post.value.meta.id, locale, body);
        drafts[locale] = { ...post.value.content[locale] };
        dirtyLocales[locale] = false;
      }
    }
    loadDraft(activeLocale.value);
    saveState.value = t("editorPage.saved");
  } catch (caught) {
    saveState.value = t(caught instanceof ApiError && caught.status === 409 ? "editorPage.conflict" : "editorPage.saveFailed");
    error.value = caught instanceof Error ? caught.message : t("editorPage.saveFailed");
	return undefined;
  } finally {
    saving.value = false;
  }
  return post.value;
}

function preview() {
	editor.value?.showPreview();
}

async function publish() {
  if (!session.session) return;
  publishing.value = true;
  const current = await save();
  if (current) {
    try {
      const result = isPage
        ? await api.publishPage(session.session.csrfToken, current.meta.id, current.meta.revision)
        : await api.publishPost(session.session.csrfToken, current.meta.id, current.meta.revision);
      post.value = result.post;
      if (result.build.status === "succeeded") {
        if (result.translation.status === "queued") saveState.value = t("editorPage.publishedQueued");
        else if (result.translation.status === "not-configured") saveState.value = t("editorPage.publishedNoAi");
        else saveState.value = t("editorPage.published");
      } else {
        saveState.value = t("editorPage.publishedBuildFailed");
        error.value = t("editorPage.publicBuildFailed", { entity: entityLabel() });
      }
    } catch (caught) {
      error.value = caught instanceof Error ? caught.message : t("editorPage.publishFailed");
    }
  }
  publishing.value = false;
}

async function translate(overwriteManual = false, requestedTargets?: string[]) {
  if (!session.session || !post.value || !locales.value) return;
  const csrfToken = session.session.csrfToken;
  const current = await save();
  if (!current) return;
  const sourceLocale = current.meta.sourceLocale;
  const targets = requestedTargets ?? (activeLocale.value === sourceLocale
    ? locales.value.enabled.filter((item) => item.enabled && item.code !== sourceLocale).map((item) => item.code)
    : [activeLocale.value]);
  if (targets.length === 0) return;
  translating.value = true;
  error.value = "";
  try {
    if (isPage) await api.startPageTranslation(csrfToken, current.meta.id, targets, overwriteManual);
    else await api.startTranslation(csrfToken, current.meta.id, targets, overwriteManual);
    saveState.value = t("editorPage.queued");
  } catch (caught) {
    if (caught instanceof ApiError && caught.code === "manual_translation_confirmation_required" && !overwriteManual) {
      const manualLocales = Array.isArray(caught.details?.locales) ? caught.details.locales.filter((locale): locale is string => typeof locale === "string") : targets;
      const localesToOverwrite = manualLocales.join("、");
      if (window.confirm(t("editorPage.manualOverwrite", { locales: localesToOverwrite }))) {
        await translate(true, targets);
      } else {
        await translate(false, targets.filter((locale) => !manualLocales.includes(locale)));
      }
    } else {
      error.value = caught instanceof Error ? caught.message : t("editorPage.translationStartFailed");
    }
  } finally {
    translating.value = false;
  }
}

async function handleImages(files: File[]) {
  if (!session.session || files.length === 0) return;
  uploading.value = true;
  error.value = "";
  try {
    const markdown: string[] = [];
    for (const file of files) {
      const asset = await api.uploadAttachment(session.session.csrfToken, file);
      const alt = asset.originalName.replace(/[\[\]]/g, "");
      markdown.push(`![${alt}](${asset.url})`);
    }
    editor.value?.insertAtCursor(`\n${markdown.join("\n\n")}\n`);
    saveState.value = t("editorPage.unsaved");
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("editorPage.imageUploadFailed");
  } finally {
    uploading.value = false;
  }
}
</script>

<template>
  <div class="editor-page">
    <VPageHeader :title="post ? form.title || t('editorPage.untitled', { entity: entityLabel() }) : t('editorPage.create', { entity: entityLabel() })">
      <template #actions>
        <span class="save-state">{{ saveState }}</span>
        <VButton :disabled="!post" @click="toggleRevisions">{{ t("editorPage.revisions") }}</VButton><VButton @click="preview">{{ t("editorPage.preview") }}</VButton><VButton :loading="saving" @click="save">{{ t("editorPage.save") }}</VButton><VButton v-if="post" :loading="translating" @click="translate()">{{ t(activeLocale === post.meta.sourceLocale ? 'editorPage.translateAll' : 'editorPage.translateLocale') }}</VButton><VButton @click="toggleSettings">{{ t("editorPage.settings") }}</VButton>
        <VButton type="secondary" :loading="publishing" @click="publish">{{ t("editorPage.publish") }}</VButton>
      </template>
    </VPageHeader>
    <div class="editor-header">
      <div class="editor-fields">
        <input v-model="form.title" class="title-input" :placeholder="t('editorPage.titlePlaceholder')" @input="markDirty" />
        <input v-model="form.summary" class="summary-input" :placeholder="t('editorPage.summaryPlaceholder')" @input="markDirty" />
      </div>
      <label v-if="!post" class="custom-id"><span>{{ t("editorPage.customId") }}</span><input v-model="customID" :placeholder="isPage ? 'about-me' : 'my-first-post'" /></label>
      <div v-if="locales" class="locale-tabs">
        <button v-for="locale in locales.enabled.filter((item) => item.enabled && (post || item.code === locales?.sourceLocale))" :key="locale.code" :class="{ active: locale.code === activeLocale }" type="button" @click="selectLocale(locale.code)">
          {{ locale.label }}
          <span v-if="post?.meta.locales[locale.code]" class="locale-origin-badge">{{ codeLabel(post.meta.locales[locale.code].origin) }}</span>
        </button>
      </div>
      <div v-if="post" class="revision-pointers"><span>{{ t("revisionPointers.base") }} #{{ post.meta.baseRevision }}</span><span>{{ t("revisionPointers.head") }} #{{ post.meta.headRevision }}</span><span>{{ t("revisionPointers.release") }} {{ post.meta.releaseRevision ? `#${post.meta.releaseRevision}` : "—" }}</span></div>
      <div v-if="settingsOpen" class="provider-form editor-settings">
        <h3>{{ t("editorPage.contentSettings", { entity: entityLabel() }) }}</h3>
        <label><span>{{ t("editorPage.seoTitle") }}</span><input v-model="form.seoTitle" :placeholder="form.title" @input="markDirty" /></label>
        <label><span>{{ t("editorPage.seoDescription") }}</span><textarea v-model="form.seoDescription" :placeholder="form.summary" rows="3" @input="markDirty" /></label>
        <template v-if="post">
          <label><span>{{ t("editorPage.cover") }}</span><input v-model="postSettings.cover" placeholder="/media/2026/08/example.webp" /></label>
          <label><span>{{ t("editorPage.comments") }}</span><select v-model="postSettings.commentPolicy"><option value="open">{{ t("editorPage.open") }}</option><option value="closed">{{ t("editorPage.closed") }}</option></select></label>
          <label><span>{{ t("editorPage.template") }}</span><select v-model="postSettings.template"><option v-for="template in templateOptions" :key="template.id" :value="template.id">{{ template.name }}</option></select></label>
          <fieldset v-if="!isPage"><legend>{{ t("editorPage.categories") }}</legend><label v-for="item in availableCategories" :key="item.id" class="check-row"><input v-model="postSettings.categories" type="checkbox" :value="item.id" />{{ item.locales[item.sourceLocale]?.name }}</label></fieldset>
          <fieldset v-if="!isPage"><legend>{{ t("editorPage.tags") }}</legend><label v-for="item in availableTags" :key="item.id" class="check-row"><input v-model="postSettings.tags" type="checkbox" :value="item.id" />{{ item.locales[item.sourceLocale]?.name }}</label></fieldset>
        </template>
        <VButton type="secondary" @click="saveSettings">{{ t("editorPage.saveSettings") }}</VButton>
      </div>
      <div v-if="revisionsOpen" class="provider-form editor-settings revision-panel">
        <h3>{{ t("editorPage.history") }}</h3><p v-if="!revisions.length">{{ t("editorPage.noHistory") }}</p>
        <article v-for="revision in revisions" :key="revision.id" class="revision-row"><div><strong>#{{ revision.revision }} · {{ revision.title }}</strong><span>{{ new Date(revision.createdAt).toLocaleString() }} · {{ codeLabel(revision.status) }}<template v-if="revision.isBase"> · {{ t("revisionPointers.base") }}</template><template v-if="revision.isRelease"> · {{ t("revisionPointers.release") }}</template></span></div><VButton size="sm" @click="compareRevision(revision)">{{ t("revisionCompare.compare") }}</VButton><VButton size="sm" @click="restoreRevision(revision)">{{ t("editorPage.restoreRevision") }}</VButton></article>
        <section v-if="revisionComparison" class="revision-comparison"><header><strong>{{t("revisionCompare.title",{revision:revisionComparison.summary.revision,locale:revisionComparisonLocale})}}</strong><button type="button" class="icon-button" @click="revisionComparison=undefined">{{t("common.dismiss")}}</button></header><div><article><h4>{{t("revisionCompare.historical")}}</h4><pre>{{revisionComparison.snapshot.content[revisionComparisonLocale]?.markdown}}</pre></article><article><h4>{{t("revisionCompare.current")}}</h4><pre>{{(drafts[revisionComparisonLocale]??post?.content[revisionComparisonLocale])?.markdown}}</pre></article></div></section>
      </div>
    </div>
    <div v-if="error" class="form-alert editor-alert">{{ error }}</div>
    <MarkdownEditor :key="activeLocale" ref="editor" v-model="form.markdown" :uploading="uploading" :source-comparison="sourceComparison" @update:model-value="markDirty" @image-files="handleImages" />
  </div>
</template>
