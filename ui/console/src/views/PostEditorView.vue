<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { VButton, VPageHeader } from "@halo-dev/components";
import {
  api,
  ApiError,
  type ContentRevision,
  type LocalesConfig,
  type LocalizedMarkdown,
  type Post,
  type Taxonomy,
  type ThemeRecord,
  type UnifiedTask,
} from "@/api/client";
import MarkdownEditor from "@/components/MarkdownEditor.vue";
import MediaPickerField from "@/components/MediaPickerField.vue";
import TaskProgress from "@/components/TaskProgress.vue";
import { useSessionStore } from "@/stores/session";
import { useCodeLabel } from "@/i18n/useCodeLabel";
import { useBuildTasks } from "@/composables/useBuildTasks";

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
const settingsSaving = ref(false);
const publishing = ref(false);
const uploading = ref(false);
const {
  taskIds: publicationTaskIds,
  createTask: createPublicationTask,
  discardIfMissing: discardPublicationIfMissing,
  beginOperation: beginPublicationOperation,
  reconcile: reconcilePublication,
} = useBuildTasks();
const {
  taskIds: translationTaskIds,
  track: trackTranslationTask,
  beginOperation: beginTranslationOperation,
} = useBuildTasks();
const siteTimezone = ref("UTC");
const saveState = ref(t("editorPage.unsaved"));
const error = ref("");
const publicationWarning = ref("");
const customID = ref("");
const settingsOpen = ref(false);
const revisionsOpen = ref(false);
const revisions = ref<ContentRevision[]>([]);
const revisionComparison = ref<{ summary: ContentRevision; snapshot: Post }>();
const availableCategories = ref<Taxonomy[]>([]);
const availableTags = ref<Taxonomy[]>([]);
const activeTheme = ref<ThemeRecord>();
const postSettings = reactive({
  categories: [] as string[],
  tags: [] as string[],
  cover: "",
  pinned: false,
  visibility: "public" as "public" | "private",
  publishedAt: "",
  commentPolicy: "open" as "open" | "closed",
  template: isPage ? "page" : "post",
});
const drafts = reactive<Record<string, LocalizedMarkdown>>({});
const dirtyLocales = reactive<Record<string, boolean>>({});
const form = ref<LocalizedMarkdown>({ title: "", summary: "", seoTitle: "", seoDescription: "", markdown: "" });
const SOURCE_LOCALE = "zh-CN";
const sourceLocale = computed(() => SOURCE_LOCALE);
const sourceConfigurationLocked = computed(() =>
  Boolean(locales.value && locales.value.sourceLocale !== SOURCE_LOCALE),
);
const legacySourceLocked = computed(
  () => sourceConfigurationLocked.value || Boolean(post.value && post.value.meta.sourceLocale !== sourceLocale.value),
);
const entitySourceEditable = computed(
  () => !sourceConfigurationLocked.value && (!post.value || post.value.meta.sourceLocale === sourceLocale.value),
);
const sourceEditable = computed(
  () => Boolean(activeLocale.value) && activeLocale.value === sourceLocale.value && entitySourceEditable.value,
);
const translationReadOnly = computed(() => Boolean(activeLocale.value) && !sourceEditable.value);
const templateOptions = computed(() => {
  const defaultID = isPage ? "page" : "post";
  const custom = isPage ? activeTheme.value?.pageTemplates : activeTheme.value?.postTemplates;
  const result = [{ id: defaultID, name: t("editorPage.defaultTemplate") }, ...(custom ?? [])];
  if (postSettings.template && !result.some((template) => template.id === postSettings.template)) {
    result.push({
      id: postSettings.template,
      name: t("editorPage.unavailableTemplate", { id: postSettings.template }),
    });
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
    const [localeConfig, themeResult, settings] = await Promise.all([api.locales(), api.themes(), api.settings()]);
    locales.value = localeConfig;
    activeTheme.value = themeResult.items.find((theme) => theme.active);
    siteTimezone.value = settings.site.timezone || "UTC";
    activeLocale.value = locales.value.sourceLocale;
    const id = typeof route.params.id === "string" ? route.params.id : "";
    if (id) {
      post.value = isPage ? await api.page(id) : await api.post(id);
      for (const [locale, value] of Object.entries(post.value.content)) {
        drafts[locale] = { ...value };
        dirtyLocales[locale] = false;
      }
      activeLocale.value = post.value.meta.sourceLocale;
      loadPostSettings();
      loadDraft(activeLocale.value);
      saveState.value = t(legacySourceLocked.value ? "editorPage.legacySourceReadOnly" : "editorPage.saved");
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
  try {
    revisions.value = (
      isPage ? await api.pageRevisions(post.value.meta.id) : await api.postRevisions(post.value.meta.id)
    ).items;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("editorPage.revisionsLoadFailed");
  }
}

async function restoreRevision(revision: ContentRevision) {
  if (
    !session.session ||
    !post.value ||
    !sourceEditable.value ||
    !window.confirm(t("editorPage.revisionConfirm", { revision: revision.revision }))
  )
    return;
  try {
    const result = isPage
      ? await api.restorePageRevision(
          session.session.csrfToken,
          post.value.meta.id,
          revision.id,
          post.value.meta.revision,
        )
      : await api.restorePostRevision(
          session.session.csrfToken,
          post.value.meta.id,
          revision.id,
          post.value.meta.revision,
        );
    post.value = result.post;
    for (const locale of Object.keys(drafts)) delete drafts[locale];
    for (const [locale, value] of Object.entries(post.value.content)) {
      drafts[locale] = { ...value };
      dirtyLocales[locale] = false;
    }
    activeLocale.value = post.value.meta.sourceLocale;
    loadDraft(activeLocale.value);
    saveState.value = t("editorPage.revisionRestored");
    revisionsOpen.value = false;
    revisionComparison.value = undefined;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("editorPage.revisionRestoreFailed");
  }
}

async function compareRevision(revision: ContentRevision) {
  if (!post.value) return;
  try {
    const snapshot = isPage
      ? await api.pageRevision(post.value.meta.id, revision.id)
      : await api.postRevision(post.value.meta.id, revision.id);
    revisionComparison.value = { summary: revision, snapshot };
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("revisionCompare.failed");
  }
}

async function saveSettings() {
  if (!session.session) return;
  settingsSaving.value = true;
  try {
    const current = await save(true);
    if (!current) return;
    const publishedAt = postSettings.publishedAt
      ? zonedLocalToISOString(postSettings.publishedAt, siteTimezone.value)
      : "";
    post.value = isPage
      ? await api.updatePageSettings(session.session.csrfToken, current.meta.id, {
          revision: current.meta.revision,
          cover: postSettings.cover,
          visibility: postSettings.visibility,
          publishedAt,
          commentPolicy: postSettings.commentPolicy,
          template: postSettings.template,
        })
      : await api.updatePostSettings(session.session.csrfToken, current.meta.id, {
          revision: current.meta.revision,
          ...postSettings,
          publishedAt,
        });
    loadPostSettings();
    saveState.value = t("editorPage.settingsSaved");
    settingsOpen.value = false;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("editorPage.settingsSaveFailed");
  } finally {
    settingsSaving.value = false;
  }
}

function loadPostSettings() {
  if (!post.value) return;
  Object.assign(postSettings, {
    categories: [...post.value.meta.categories],
    tags: [...post.value.meta.tags],
    cover: post.value.meta.cover ?? "",
    pinned: post.value.meta.pinned ?? false,
    visibility: post.value.meta.visibility || "public",
    publishedAt: toDatetimeLocal(post.value.meta.publishedAt, siteTimezone.value),
    commentPolicy: post.value.meta.commentPolicy,
    template: post.value.meta.template,
  });
}

function toDatetimeLocal(value: string | undefined, timezone: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return zonedParts(date.getTime(), timezone).key;
}

const zonedFormatters = new Map<string, Intl.DateTimeFormat>();

function zonedFormatter(timezone: string) {
  let formatter = zonedFormatters.get(timezone);
  if (!formatter) {
    formatter = new Intl.DateTimeFormat("en-CA", {
      timeZone: timezone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
    });
    zonedFormatters.set(timezone, formatter);
  }
  return formatter;
}

function zonedParts(instant: number, timezone: string) {
  const values: Record<string, string> = {};
  for (const part of zonedFormatter(timezone).formatToParts(new Date(instant))) {
    if (part.type !== "literal") values[part.type] = part.value;
  }
  const year = Number(values.year);
  const month = Number(values.month);
  const day = Number(values.day);
  const hour = Number(values.hour);
  const minute = Number(values.minute);
  return {
    year,
    month,
    day,
    hour,
    minute,
    key: `${values.year}-${values.month}-${values.day}T${values.hour}:${values.minute}`,
  };
}

function zonedLocalToISOString(value: string, timezone: string) {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value);
  if (!match) throw new Error(String(t("editorPublishTime.invalid", { timezone })));
  const [, rawYear, rawMonth, rawDay, rawHour, rawMinute] = match;
  const year = Number(rawYear);
  const month = Number(rawMonth);
  const day = Number(rawDay);
  const hour = Number(rawHour);
  const minute = Number(rawMinute);
  const wallTime = Date.UTC(year, month - 1, day, hour, minute);
  const parsed = new Date(wallTime);
  if (
    parsed.getUTCFullYear() !== year ||
    parsed.getUTCMonth() !== month - 1 ||
    parsed.getUTCDate() !== day ||
    parsed.getUTCHours() !== hour ||
    parsed.getUTCMinutes() !== minute
  ) {
    throw new Error(String(t("editorPublishTime.invalid", { timezone })));
  }

  // A wall clock has no intrinsic offset. Sample every offset around the date,
  // then retain only instants which round-trip to the exact requested minute.
  // Zero matches is a DST gap; two matches is a DST overlap.
  const offsets = new Set<number>();
  for (let sampleHour = -48; sampleHour <= 48; sampleHour += 6) {
    const sampleInstant = wallTime + sampleHour * 60 * 60 * 1000;
    const sample = zonedParts(sampleInstant, timezone);
    const representedAsUTC = Date.UTC(sample.year, sample.month - 1, sample.day, sample.hour, sample.minute);
    offsets.add(representedAsUTC - Math.floor(sampleInstant / 60_000) * 60_000);
  }
  const matches = [...offsets]
    .map((offset) => wallTime - offset)
    .filter((instant, index, all) => all.indexOf(instant) === index && zonedParts(instant, timezone).key === value)
    .sort((left, right) => left - right);
  if (matches.length === 0) throw new Error(String(t("editorPublishTime.invalid", { timezone })));
  if (matches.length > 1) throw new Error(String(t("editorPublishTime.ambiguous", { timezone })));
  return new Date(matches[0]).toISOString();
}

function stashDraft() {
  if (sourceEditable.value) drafts[activeLocale.value] = { ...form.value };
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
  settingsOpen.value = false;
  activeLocale.value = locale;
  loadDraft(locale);
  saveState.value = t(
    !localeIsEditable(locale)
      ? legacySourceLocked.value
        ? "editorPage.legacySourceReadOnly"
        : "editorPage.aiTranslationReadOnly"
      : dirtyLocales[locale]
        ? "editorPage.unsaved"
        : "editorPage.saved",
  );
}

function markDirty() {
  if (!sourceEditable.value) return;
  dirtyLocales[activeLocale.value] = true;
  saveState.value = t("editorPage.unsaved");
}

async function save(allowInactiveSource = false) {
  if (!session.session || !entitySourceEditable.value || (!sourceEditable.value && !allowInactiveSource))
    return post.value;
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
      await router.replace(`/${isPage ? "pages" : "posts"}/editor/${post.value.meta.id}`);
    } else {
      const dirty = dirtyLocales[sourceLocale.value] ? [sourceLocale.value] : [];
      if (dirty.length === 0) {
        saveState.value = t(post.value.meta.status === "published" ? "editorPage.published" : "editorPage.saved");
        return post.value;
      }
      for (const locale of dirty) {
        const body: LocalizedMarkdown & { revision: number } = {
          ...(drafts[locale] ?? post.value.content[locale] ?? { title: "", markdown: "" }),
          revision: post.value.meta.revision,
        };
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
    saveState.value = t(
      caught instanceof ApiError && caught.status === 409 ? "editorPage.conflict" : "editorPage.saveFailed",
    );
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

function handlePublicationTaskUpdate(task: UnifiedTask) {
  if (task.kind === "ScheduledPublish" && task.translationTaskId) {
    trackTranslationTask(task.translationTaskId);
  }
}

async function publish() {
  if (!session.session) return;
  publishing.value = true;
  publicationWarning.value = "";
  const current = await save(true);
  if (current) {
    await Promise.all([beginPublicationOperation(), beginTranslationOperation()]);
    const buildTaskId = createPublicationTask();
    try {
      const result = isPage
        ? await api.publishPage(session.session.csrfToken, current.meta.id, current.meta.revision, buildTaskId)
        : await api.publishPost(session.session.csrfToken, current.meta.id, current.meta.revision, buildTaskId);
      post.value = result.post;
      const translationActive =
        (result.translation.status === "queued" || result.translation.status === "running") &&
        Boolean(result.translation.taskId);
      trackTranslationTask(result.translation.taskId);
      await reconcilePublication(buildTaskId, result.build.taskId);
      if (result.translation.status === "failed")
        publicationWarning.value = String(t("editorPublishedTranslationFailed"));
      if (result.build.status === "scheduled") {
        saveState.value = t("taskProgress.scheduled");
      } else if (result.build.status === "deferred") {
        saveState.value = t(translationActive ? "editorPage.publishedQueued" : "editorPublishedTranslationFailed");
      } else if (result.build.status === "blocked") {
        saveState.value = t(
          result.translation.status === "not-configured"
            ? "editorPage.publishedNoAi"
            : "editorPublishedTranslationFailed",
        );
      } else if (result.build.status === "succeeded") {
        if (translationActive) {
          saveState.value = t("editorPage.publishedQueued");
        } else if (result.translation.status === "not-configured") saveState.value = t("editorPage.publishedNoAi");
        else if (result.translation.status === "failed") saveState.value = t("editorPublishedTranslationFailed");
        else saveState.value = t("editorPage.published");
      } else {
        saveState.value = t("editorPage.publishedBuildFailed");
        error.value = t("editorPage.publicBuildFailed", { entity: entityLabel() });
      }
    } catch (caught) {
      await discardPublicationIfMissing(buildTaskId);
      error.value = caught instanceof Error ? caught.message : t("editorPage.publishFailed");
    }
  }
  publishing.value = false;
}

async function handleImages(files: File[]) {
  if (!session.session || !sourceEditable.value || files.length === 0) return;
  uploading.value = true;
  error.value = "";
  try {
    const markdown: string[] = [];
    for (const file of files) {
      const asset = await api.uploadAttachment(session.session.csrfToken, file);
      const alt = asset.originalName.replaceAll("[", "").replaceAll("]", "");
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

function localeIsEditable(locale: string) {
  return locale === sourceLocale.value && entitySourceEditable.value;
}
</script>

<template>
  <div class="editor-page">
    <VPageHeader
      :title="
        post
          ? form.title || t('editorPage.untitled', { entity: entityLabel() })
          : t('editorPage.create', { entity: entityLabel() })
      "
    >
      <template #actions>
        <span class="save-state">{{ saveState }}</span>
        <VButton :disabled="!post" @click="toggleRevisions">{{ t("editorPage.revisions") }}</VButton
        ><VButton @click="preview">{{ t("editorPage.preview") }}</VButton
        ><VButton :disabled="!sourceEditable" :loading="saving" @click="save()">{{ t("editorPage.save") }}</VButton
        ><VButton @click="toggleSettings">{{ t("editorPage.settings") }}</VButton>
        <VButton type="secondary" :loading="publishing" @click="publish">{{ t("editorPage.publish") }}</VButton>
      </template>
    </VPageHeader>
    <p class="editor-translation-policy">{{ t("editorPage.translationPolicy") }}</p>
    <div class="editor-header">
      <div class="editor-fields">
        <input
          v-model="form.title"
          class="title-input"
          :readonly="translationReadOnly"
          :aria-readonly="translationReadOnly"
          :placeholder="t('editorPage.titlePlaceholder')"
          @input="markDirty"
        />
        <input
          v-model="form.summary"
          class="summary-input"
          :readonly="translationReadOnly"
          :aria-readonly="translationReadOnly"
          :placeholder="t('editorPage.summaryPlaceholder')"
          @input="markDirty"
        />
      </div>
      <label v-if="!post" class="custom-id"
        ><span>{{ t("editorPage.customId") }}</span
        ><input v-model="customID" :placeholder="isPage ? 'about-me' : 'my-first-post'"
      /></label>
      <div v-if="locales" class="locale-tabs">
        <button
          v-for="locale in locales.enabled.filter(
            (item) => item.enabled && (post || item.code === locales?.sourceLocale),
          )"
          :key="locale.code"
          :class="{ active: locale.code === activeLocale, 'read-only': !localeIsEditable(locale.code) }"
          type="button"
          @click="selectLocale(locale.code)"
        >
          {{ locale.label }}
          <span v-if="post?.meta.locales[locale.code]" class="locale-origin-badge">{{
            codeLabel(post.meta.locales[locale.code].origin)
          }}</span>
          <span v-if="!localeIsEditable(locale.code)" class="locale-readonly-badge">{{
            t(
              legacySourceLocked && locale.code === post?.meta.sourceLocale
                ? "editorPage.legacyReadOnlyBadge"
                : "editorPage.readOnlyBadge",
            )
          }}</span>
        </button>
      </div>
      <div v-if="post" class="revision-pointers">
        <span>{{ t("revisionPointers.base") }} #{{ post.meta.baseRevision }}</span
        ><span>{{ t("revisionPointers.head") }} #{{ post.meta.headRevision }}</span
        ><span
          >{{ t("revisionPointers.release") }}
          {{ post.meta.releaseRevision ? `#${post.meta.releaseRevision}` : "—" }}</span
        >
      </div>
      <div v-if="revisionsOpen" class="provider-form editor-settings revision-panel">
        <h3>{{ t("editorPage.history") }}</h3>
        <p class="revision-source-only-help">
          {{
            t(legacySourceLocked ? "editorPage.legacyRevisionReadOnlyHelp" : "editorPage.revisionSourceOnlyHelp", {
              locale: sourceLocale,
            })
          }}
        </p>
        <p v-if="!revisions.length">{{ t("editorPage.noHistory") }}</p>
        <article v-for="revision in revisions" :key="revision.id" class="revision-row">
          <div>
            <strong>#{{ revision.revision }} · {{ revision.title }}</strong
            ><span
              >{{ new Date(revision.createdAt).toLocaleString() }} · {{ codeLabel(revision.status)
              }}<template v-if="revision.isBase"> · {{ t("revisionPointers.base") }}</template
              ><template v-if="revision.isRelease"> · {{ t("revisionPointers.release") }}</template></span
            >
          </div>
          <VButton size="sm" @click="compareRevision(revision)">{{ t("revisionCompare.compare") }}</VButton
          ><VButton size="sm" :disabled="!sourceEditable" @click="restoreRevision(revision)">{{
            t("editorPage.restoreRevision")
          }}</VButton>
        </article>
        <section v-if="revisionComparison" class="revision-comparison">
          <header>
            <strong>{{
              t("revisionCompare.title", {
                revision: revisionComparison.summary.revision,
                locale: revisionComparisonLocale,
              })
            }}</strong
            ><button type="button" class="icon-button" @click="revisionComparison = undefined">
              {{ t("common.dismiss") }}
            </button>
          </header>
          <div>
            <article>
              <h4>{{ t("revisionCompare.historical") }}</h4>
              <pre>{{ revisionComparison.snapshot.content[revisionComparisonLocale]?.markdown }}</pre>
            </article>
            <article>
              <h4>{{ t("revisionCompare.current") }}</h4>
              <pre>{{ (drafts[revisionComparisonLocale] ?? post?.content[revisionComparisonLocale])?.markdown }}</pre>
            </article>
          </div>
        </section>
      </div>
    </div>
    <div v-if="error" class="form-alert editor-alert">{{ error }}</div>
    <div v-if="publicationWarning" class="editor-publication-warning">{{ publicationWarning }}</div>
    <section v-if="translationReadOnly" class="editor-readonly-notice" role="status">
      <strong>{{
        t(legacySourceLocked ? "editorPage.legacySourceReadOnly" : "editorPage.aiTranslationReadOnly")
      }}</strong>
      <span>{{
        t(legacySourceLocked ? "editorPage.legacySourceReadOnlyHelp" : "editorPage.aiTranslationReadOnlyHelp", {
          locale: sourceLocale,
        })
      }}</span>
    </section>
    <section v-if="publicationTaskIds.length || translationTaskIds.length" class="editor-task-progress">
      <div v-if="publicationTaskIds.length">
        <strong>{{ t("taskProgress.publication") }}</strong
        ><TaskProgress
          v-for="taskId in publicationTaskIds"
          :key="taskId"
          :task-id="taskId"
          @update="handlePublicationTaskUpdate"
        />
      </div>
      <div v-if="translationTaskIds.length">
        <strong>{{ t("taskProgress.translation") }}</strong
        ><TaskProgress v-for="taskId in translationTaskIds" :key="taskId" :task-id="taskId" />
      </div>
    </section>
    <MarkdownEditor
      :key="activeLocale"
      ref="editor"
      v-model="form.markdown"
      :readonly="translationReadOnly"
      :uploading="uploading"
      :source-comparison="sourceComparison"
      @update:model-value="markDirty"
      @image-files="handleImages"
    />
    <Teleport v-if="settingsOpen" to="body">
      <div class="content-settings-backdrop" @click.self="settingsOpen = false">
        <section
          class="content-settings-modal"
          role="dialog"
          aria-modal="true"
          :aria-label="t('editorPage.contentSettings', { entity: entityLabel() })"
        >
          <header class="content-settings-header">
            <h2>{{ t("editorPage.contentSettings", { entity: entityLabel() }) }}</h2>
            <button type="button" class="icon-button" :aria-label="t('common.dismiss')" @click="settingsOpen = false">
              ×
            </button>
          </header>
          <div class="content-settings-body">
            <section class="content-settings-group">
              <h3>{{ t("editorPage.generalSettings") }}</h3>
              <div class="content-settings-fields">
                <label class="field--wide"
                  ><span>{{ t("editorPage.title") }}</span
                  ><input
                    v-model="form.title"
                    :readonly="translationReadOnly"
                    :aria-readonly="translationReadOnly"
                    @input="markDirty"
                /></label>
                <label class="field--wide"
                  ><span>{{ t("editorPage.slug") }}</span
                  ><input v-if="post" :value="post.meta.id" disabled /><input
                    v-else
                    v-model="customID"
                    pattern="[a-z]+(?:-[a-z]+)*"
                    :placeholder="isPage ? 'about-me' : 'my-first-post'"
                /></label>
                <label class="field--wide"
                  ><span>{{ t("editorPage.summary") }}</span
                  ><textarea
                    v-model="form.summary"
                    :readonly="translationReadOnly"
                    :aria-readonly="translationReadOnly"
                    rows="3"
                    @input="markDirty"
                  />
                </label>
                <fieldset v-if="!isPage">
                  <legend>{{ t("editorPage.categories") }}</legend>
                  <label v-for="item in availableCategories" :key="item.id" class="check-row"
                    ><input v-model="postSettings.categories" type="checkbox" :value="item.id" />{{
                      item.locales[item.sourceLocale]?.name
                    }}</label
                  >
                </fieldset>
                <fieldset v-if="!isPage">
                  <legend>{{ t("editorPage.tags") }}</legend>
                  <label v-for="item in availableTags" :key="item.id" class="check-row"
                    ><input v-model="postSettings.tags" type="checkbox" :value="item.id" />{{
                      item.locales[item.sourceLocale]?.name
                    }}</label
                  >
                </fieldset>
                <MediaPickerField v-model="postSettings.cover" class="field--wide" :label="t('editorPage.cover')" />
              </div>
            </section>
            <section class="content-settings-group">
              <h3>{{ t("editorPage.advancedSettings") }}</h3>
              <div class="content-settings-fields">
                <label
                  ><span>{{ t("editorPage.comments") }}</span
                  ><select v-model="postSettings.commentPolicy">
                    <option value="open">{{ t("editorPage.open") }}</option>
                    <option value="closed">{{ t("editorPage.closed") }}</option>
                  </select></label
                >
                <label v-if="!isPage" class="provider-check content-settings-check"
                  ><input v-model="postSettings.pinned" type="checkbox" />{{ t("editorPage.pinned") }}</label
                >
                <label
                  ><span>{{ t("editorPage.visibility") }}</span
                  ><select v-model="postSettings.visibility">
                    <option value="public">{{ t("editorPage.public") }}</option>
                    <option value="private">{{ t("editorPage.private") }}</option>
                  </select></label
                >
                <label
                  ><span>{{ t("editorPage.publishTime") }}</span
                  ><input v-model="postSettings.publishedAt" type="datetime-local" step="60" /><small>{{
                    t("editorPublishTime.timezone", { timezone: siteTimezone })
                  }}</small></label
                >
                <label class="field--wide"
                  ><span>{{ t("editorPage.template") }}</span
                  ><select v-model="postSettings.template">
                    <option v-for="template in templateOptions" :key="template.id" :value="template.id">
                      {{ template.name }}
                    </option>
                  </select></label
                >
              </div>
            </section>
            <section class="content-settings-group">
              <h3>SEO</h3>
              <div class="content-settings-fields">
                <label
                  ><span>{{ t("editorPage.seoTitle") }}</span
                  ><input
                    v-model="form.seoTitle"
                    :readonly="translationReadOnly"
                    :aria-readonly="translationReadOnly"
                    :placeholder="form.title"
                    @input="markDirty"
                /></label>
                <label
                  ><span>{{ t("editorPage.seoDescription") }}</span
                  ><textarea
                    v-model="form.seoDescription"
                    :readonly="translationReadOnly"
                    :aria-readonly="translationReadOnly"
                    :placeholder="form.summary"
                    rows="3"
                    @input="markDirty"
                  />
                </label>
              </div>
            </section>
            <div v-if="error" class="form-alert">{{ error }}</div>
          </div>
          <footer class="content-settings-footer">
            <VButton type="secondary" :loading="settingsSaving" @click="saveSettings">{{
              t("editorPage.saveSettings")
            }}</VButton>
            <VButton @click="settingsOpen = false">{{ t("common.dismiss") }}</VButton>
          </footer>
        </section>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.editor-translation-policy {
  margin: 0;
  border-bottom: 1px solid #e7e9ed;
  background: #f8fafc;
  padding: 0.55rem 1rem;
  color: #64748b;
  font-size: 0.72rem;
}

.editor-publication-warning {
  margin: 0.6rem 1rem 0;
  border: 1px solid #f1c56c;
  border-radius: 0.35rem;
  background: #fff8e7;
  padding: 0.65rem 0.75rem;
  color: #78540d;
  font-size: 0.75rem;
}

.editor-readonly-notice {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem 0.65rem;
  border-bottom: 1px solid #c7d2fe;
  background: #eef2ff;
  padding: 0.7rem 1rem;
  color: #3730a3;
  font-size: 0.75rem;
}

.editor-readonly-notice strong {
  font-weight: 700;
}

.locale-readonly-badge {
  pointer-events: none;
  border-radius: 999px;
  background: #eef2ff;
  padding: 0.08rem 0.3rem;
  color: #4338ca;
  font-size: 0.62rem;
  line-height: 1.2;
}

.revision-source-only-help {
  margin: 0;
  color: #64748b;
  font-size: 0.72rem;
}

.title-input:read-only,
.summary-input:read-only {
  cursor: default;
}
</style>
