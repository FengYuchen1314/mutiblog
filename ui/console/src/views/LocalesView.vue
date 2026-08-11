<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { VButton, VCard, VPageHeader, VTag } from "@halo-dev/components";
import { api, type LocalesConfig } from "@/api/client";
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
  taskIds: localesBuildTaskIds,
  createTask: createLocalesBuildTask,
  discardIfMissing: discardLocalesBuildTaskIfMissing,
  beginOperation: beginLocalesBuildOperation,
  reconcile: reconcileLocalesBuildTask,
} = useBuildTasks();

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
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("localesPage.loadFailed");
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
  const taskId = createLocalesBuildTask();
  try {
    const result = await api.updateLocales(csrfToken, enabled, SOURCE_LOCALE, taskId);
    await reconcileLocalesBuildTask(taskId);
    entries.value = normalizeEntries(result.locales.enabled);
    persistedLocaleCodes.value = new Set(entries.value.map((entry) => entry.code));
    const incomplete =
      result.build.status === "failed" ||
      result.localization?.status === "failed" ||
      result.localization?.status === "partial";
    message.value = t(incomplete ? "localesPage.savedBuildFailed" : "localesPage.saved");
  } catch (caught) {
    await discardLocalesBuildTaskIfMissing(taskId);
    error.value = caught instanceof Error ? caught.message : t("localesPage.saveFailed");
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="page">
    <VPageHeader :title="t('localesPage.title')">
      <template #actions>
        <VButton type="secondary" :loading="saving" @click="save">
          {{ t("localesPage.saveAndTranslate") }}
        </VButton>
      </template>
    </VPageHeader>
    <div class="page-body settings-stack">
      <div v-if="message" class="form-success">{{ message }}</div>
      <div v-if="error" class="form-alert">{{ error }}</div>
      <VCard>
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
            <VTag v-if="entry.code === SOURCE_LOCALE">{{ t("localesPage.source") }}</VTag>
            <VTag v-else>{{ t("localesPage.target") }}</VTag>
            <VTag>{{
              t(persistedLocaleCodes.has(entry.code) ? "localesPage.permanent" : "localesPage.pendingSave")
            }}</VTag>
            <VTag v-if="persistedLocaleCodes.has(entry.code)">
              {{ t("localesPage.status." + (entry.status ?? "legacy")) }}
            </VTag>
          </article>
        </div>
        <TaskProgress v-for="taskId in localesBuildTaskIds" :key="taskId" :task-id="taskId" />
      </VCard>
      <VCard>
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
          <VButton @click="addLocale">{{ t("localesPage.add") }}</VButton>
        </div>
      </VCard>
      <VCard>
        <div class="settings-section-title">
          <div>
            <strong>{{ t("localesPage.translationWorkflow") }}</strong
            ><span>{{ t("localesPage.translationWorkflowHelp") }}</span>
          </div>
        </div>
      </VCard>
    </div>
  </div>
</template>
