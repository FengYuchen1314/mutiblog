<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { api, type LocalesConfig, type UnifiedTask } from "@/api/client";
import TaskProgress from "@/components/TaskProgress.vue";
import { useBuildTasks } from "@/composables/useBuildTasks";
import { useSessionStore } from "@/stores/session";

const session = useSessionStore();
const { t } = useI18n();
const SOURCE_LOCALE = "zh-CN";
const entries = ref<LocalesConfig["enabled"]>([]);
const persistedLocaleCodes = ref<Set<string>>(new Set());
const code = ref("");
const label = ref("");
const saving = ref(false);
const message = ref("");
const error = ref("");
const {
  taskIds: localeTaskIds,
  track: trackLocaleTask,
  trackTaskChildren: trackLocaleTaskChildren,
  beginOperation: beginLocalesBuildOperation,
} = useBuildTasks();
const localeTaskId = ref("");

const presets: Record<string, string> = {
  en: "English",
  "zh-CN": "简体中文",
  "zh-TW": "繁體中文",
  ja: "日本語",
  ko: "한국어",
  de: "Deutsch",
  fr: "Français",
  es: "Español",
};

function normalizeEntries(items: LocalesConfig["enabled"]) {
  const normalized = items.map((entry) =>
    entry.code.toLowerCase() === SOURCE_LOCALE.toLowerCase()
      ? { code: SOURCE_LOCALE, label: t("localesPage.sourceLocaleName"), enabled: true, status: "ready" as const }
      : { ...entry, enabled: true },
  );
  if (!normalized.some((entry) => entry.code === SOURCE_LOCALE)) {
    normalized.unshift({ code: SOURCE_LOCALE, label: t("localesPage.sourceLocaleName"), enabled: true });
  }
  return normalized;
}

async function load() {
  try {
    const localeConfig = await api.locales();
    entries.value = normalizeEntries(localeConfig.enabled);
    persistedLocaleCodes.value = new Set(entries.value.map((entry) => entry.code));
    await restoreLatestLocaleTask();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("localesPage.loadFailed");
  }
}

function observeLocaleTask(task: UnifiedTask) {
  if (task.kind === "LocaleProvision") localeTaskId.value = task.id;
  trackLocaleTask(task.id);
  trackLocaleTaskChildren(task);
}

function handleLocaleTaskUpdate(task: UnifiedTask) {
  observeLocaleTask(task);
}

async function restoreLatestLocaleTask() {
  try {
    const latest = (await api.tasks({ kind: "LocaleProvision", limit: 1 })).items[0];
    if (latest) observeLocaleTask(latest);
  } catch {
    // Locale configuration remains editable when task history is temporarily unavailable.
  }
}

function suggestLabel() {
  if (!label.value && presets[code.value]) label.value = presets[code.value];
}

function addLocale() {
  error.value = "";
  const normalizedCode = code.value.trim();
  const normalizedLabel = label.value.trim() || presets[normalizedCode] || normalizedCode;
  if (!normalizedCode || entries.value.some((entry) => entry.code.toLowerCase() === normalizedCode.toLowerCase())) {
    error.value = t("localesPage.addInvalid");
    return;
  }
  entries.value.push({ code: normalizedCode, label: normalizedLabel, enabled: true });
  code.value = "";
  label.value = "";
}

async function save() {
  if (!session.session) return;
  const csrfToken = session.session.csrfToken;
  const enabled = entries.value.map((entry) => ({ ...entry, enabled: true }));
  saving.value = true;
  message.value = "";
  error.value = "";
  await beginLocalesBuildOperation();
  localeTaskId.value = "";
  try {
    const result = await api.updateLocales(csrfToken, enabled, SOURCE_LOCALE);
    entries.value = normalizeEntries(result.locales.enabled);
    persistedLocaleCodes.value = new Set(entries.value.map((entry) => entry.code));
    if (result.task) {
      observeLocaleTask(result.task);
      message.value = t("localesPage.savedQueued");
    } else {
      message.value = t("localesPage.saved");
    }
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("localesPage.saveFailed");
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="page">
    <MPageHeader :title="t('localesPage.title')">
      <template #actions>
        <MButton variant="tonal" :loading="saving" @click="save">
          {{ t("localesPage.saveAndTranslate") }}
        </MButton>
      </template>
    </MPageHeader>
    <div class="page-body settings-stack">
      <div v-if="message" class="form-success">{{ message }}</div>
      <div v-if="error" class="form-alert">{{ error }}</div>
      <MSurface>
        <div class="settings-section-title">
          <div>
            <strong>{{ t("localesPage.siteLanguages") }}</strong
            ><span>{{ t("localesPage.sourceHelp") }}</span>
          </div>
        </div>
        <label>
          <span>{{ t("localesPage.sourceLocale") }}</span>
          <input :value="t('localesPage.sourceLocaleValue')" readonly aria-readonly="true" />
        </label>
        <div class="locale-settings-list">
          <article v-for="entry in entries" :key="entry.code" class="locale-setting-row">
            <div>
              <strong>{{ entry.label }}</strong
              ><span>{{ entry.code }}</span>
            </div>
            <MChip v-if="entry.code === SOURCE_LOCALE">{{ t("localesPage.source") }}</MChip>
            <MChip v-else>{{ t("localesPage.target") }}</MChip>
            <MChip>{{
              t(persistedLocaleCodes.has(entry.code) ? "localesPage.permanent" : "localesPage.pendingSave")
            }}</MChip>
            <MChip v-if="persistedLocaleCodes.has(entry.code)">
              {{ t("localesPage.status." + (entry.status ?? "legacy")) }}
            </MChip>
          </article>
        </div>
        <TaskProgress
          v-for="taskId in localeTaskIds"
          :key="taskId"
          :task-id="taskId"
          @update="handleLocaleTaskUpdate"
        />
        <RouterLink
          v-if="localeTaskId"
          class="locale-task-center-link"
          :to="{ name: 'tasks', query: { focus: localeTaskId } }"
        >
          {{ t("localesPage.viewTaskCenter") }}
        </RouterLink>
      </MSurface>
      <MSurface>
        <div class="settings-section-title">
          <div>
            <strong>{{ t("localesPage.addTarget") }}</strong
            ><span>{{ t("localesPage.addTargetHelp") }}</span>
          </div>
        </div>
        <div class="locale-add-form">
          <label
            ><span>{{ t("localesPage.localeCode") }}</span
            ><input v-model="code" list="locale-presets" placeholder="ja" @input="suggestLabel"
          /></label>
          <datalist id="locale-presets">
            <option v-for="(presetLabel, presetCode) in presets" :key="presetCode" :value="presetCode">
              {{ presetLabel }}
            </option>
          </datalist>
          <label
            ><span>{{ t("localesPage.displayName") }}</span
            ><input v-model="label" placeholder="Japanese"
          /></label>
          <MButton @click="addLocale">{{ t("localesPage.add") }}</MButton>
        </div>
      </MSurface>
      <MSurface>
        <div class="settings-section-title">
          <div>
            <strong>{{ t("localesPage.translationWorkflow") }}</strong
            ><span>{{ t("localesPage.translationWorkflowHelp") }}</span>
          </div>
        </div>
      </MSurface>
    </div>
  </div>
</template>

<style scoped>
.locale-task-center-link {
  color: #4f46e5;
  font-size: 0.78rem;
  text-decoration: none;
}
.locale-task-center-link:hover {
  text-decoration: underline;
}
</style>
