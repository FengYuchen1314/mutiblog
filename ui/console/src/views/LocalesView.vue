<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { VButton, VCard, VPageHeader, VTag } from "@halo-dev/components";
import { ApiError, api, createStaticBuildTaskId, type FrameworkDictionary, type LocalesConfig } from "@/api/client";
import TaskProgress from "@/components/TaskProgress.vue";
import { useSessionStore } from "@/stores/session";

const session = useSessionStore();
const { t } = useI18n();
const config = ref<LocalesConfig>();
const entries = ref<LocalesConfig["enabled"]>([]);
const sourceLocale = ref("");
const loadedSourceLocale = ref("");
const code = ref("");
const label = ref("");
const saving = ref(false);
const message = ref("");
const error = ref("");
const dictionaries = ref<FrameworkDictionary[]>([]);
const selectedDictionary = ref<FrameworkDictionary>();
const dictionaryValues = ref<Record<string, string>>({});
const dictionarySaving = ref(false);
const localesBuildTaskId = ref("");
const dictionaryTaskId = ref("");
const fallbackChain = computed(() => {
  const configured = config.value?.fallback?.length ? config.value.fallback : ["zh-CN"];
  return configured.filter((locale, index, values) => values.indexOf(locale) === index);
});

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

async function load() {
  try {
    const [localeConfig, dictionaryResult] = await Promise.all([api.locales(), api.dictionaries()]);
    config.value = localeConfig;
    dictionaries.value = dictionaryResult.items;
    entries.value = config.value.enabled.map((entry) => ({ ...entry }));
    sourceLocale.value = config.value.sourceLocale;
    loadedSourceLocale.value = config.value.sourceLocale;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("localesPage.loadFailed");
  }
}

function dictionaryFor(locale: string) { return dictionaries.value.find((item) => item.locale === locale); }
function editDictionary(locale: string) {
  selectedDictionary.value = dictionaryFor(locale);
  dictionaryValues.value = { ...(selectedDictionary.value?.values ?? {}) };
}

async function discardTaskIfMissing(taskId: string, clear: () => void) {
  try {
    await api.task(taskId);
  } catch (caught) {
    if (caught instanceof ApiError && caught.status === 404) clear();
  }
}

async function saveDictionary() {
  if (!session.session || !selectedDictionary.value) return;
  dictionarySaving.value = true;
  error.value = "";
  const taskId = createStaticBuildTaskId();
  dictionaryTaskId.value = taskId;
  try {
    const updated = await api.updateDictionary(session.session.csrfToken, selectedDictionary.value.locale, dictionaryValues.value, taskId);
    dictionaries.value = dictionaries.value.map((item) => item.locale === updated.locale ? updated : item);
    selectedDictionary.value = updated;
    dictionaryValues.value = { ...updated.values };
    message.value = t("localesPage.dictionarySaved", { locale: updated.locale });
  } catch (caught) {
    await discardTaskIfMissing(taskId, () => { if (dictionaryTaskId.value === taskId) dictionaryTaskId.value = ""; });
    error.value = caught instanceof Error ? caught.message : t("localesPage.dictionarySaveFailed");
  } finally {
    dictionarySaving.value = false;
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

function removeLocale(locale: string) {
  if (locale === sourceLocale.value || isRequiredFallback(locale)) return;
  entries.value = entries.value.filter((entry) => entry.code !== locale);
}

function isRequiredFallback(locale: string) {
  return locale === "zh-CN";
}

async function save() {
  if (!session.session) return;
  if (sourceLocale.value !== loadedSourceLocale.value && !window.confirm(t("localesPage.switchConfirm"))) return;
  saving.value = true;
  message.value = "";
  error.value = "";
  const taskId = createStaticBuildTaskId();
  localesBuildTaskId.value = taskId;
  try {
    const result = await api.updateLocales(session.session.csrfToken, entries.value, sourceLocale.value, taskId);
    config.value = result.locales;
    entries.value = config.value.enabled.map((entry) => ({ ...entry }));
    sourceLocale.value = config.value.sourceLocale;
    loadedSourceLocale.value = config.value.sourceLocale;
    dictionaries.value = (await api.dictionaries()).items;
    if (selectedDictionary.value) editDictionary(selectedDictionary.value.locale);
    message.value = t(result.build.status === "failed" ? "localesPage.savedBuildFailed" : "localesPage.saved");
  } catch (caught) {
    await discardTaskIfMissing(taskId, () => { if (localesBuildTaskId.value === taskId) localesBuildTaskId.value = ""; });
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
      <template #actions><VButton type="secondary" :loading="saving" @click="save">{{ t("common.saveAndBuild") }}</VButton></template>
    </VPageHeader>
    <div class="page-body settings-stack">
      <div v-if="message" class="form-success">{{ message }}</div>
      <div v-if="error" class="form-alert">{{ error }}</div>
      <VCard>
        <div class="settings-section-title"><div><strong>{{ t("localesPage.siteLanguages") }}</strong><span>{{ t("localesPage.sourceHelp") }}</span></div></div>
        <label><span>{{ t("localesPage.sourceLocale") }}</span><select v-model="sourceLocale"><option v-for="entry in entries.filter((item) => item.enabled)" :key="entry.code" :value="entry.code">{{ entry.label }}（{{ entry.code }}）</option></select></label>
        <div class="locale-settings-list">
          <article v-for="entry in entries" :key="entry.code" class="locale-setting-row">
            <div><strong>{{ entry.label }}</strong><span>{{ entry.code }}</span></div>
            <VTag v-if="entry.code === sourceLocale">{{ t("localesPage.source") }}</VTag>
            <VTag v-else-if="entry.code === 'zh-CN'">{{ t("localesPage.builtIn") }}</VTag>
            <VTag v-else>{{ t("localesPage.target") }}</VTag>
            <VTag v-if="dictionaryFor(entry.code)">{{ t("localesPage.dictionaryProgress", { translated: dictionaryFor(entry.code)?.translated, total: dictionaryFor(entry.code)?.total }) }}</VTag>
            <label class="locale-toggle"><input v-model="entry.enabled" type="checkbox" :disabled="entry.code === sourceLocale || isRequiredFallback(entry.code)" />{{ t("localesPage.enabled") }}</label>
            <button v-if="dictionaryFor(entry.code)" type="button" @click="editDictionary(entry.code)">{{ t("localesPage.manageDictionary") }}</button>
            <button v-if="entry.code !== sourceLocale && !isRequiredFallback(entry.code)" class="text-danger" type="button" @click="removeLocale(entry.code)">{{ t("localesPage.remove") }}</button>
          </article>
        </div>
        <TaskProgress v-if="localesBuildTaskId" :task-id="localesBuildTaskId" />
      </VCard>
      <VCard v-if="selectedDictionary">
        <div class="settings-section-title"><div><strong>{{ t("localesPage.dictionaryEditor", { locale: selectedDictionary.locale }) }}</strong><span>{{ t("dictionaryFallbackHelp", { source: sourceLocale }) }}</span></div></div>
        <div v-if="selectedDictionary.missing.length" class="form-alert">{{ t("localesPage.dictionaryMissing", { count: selectedDictionary.missing.length }) }}</div>
        <div class="provider-form dictionary-grid">
          <label v-for="(_, key) in selectedDictionary.values" :key="key"><span><code>{{ key }}</code></span><textarea v-model="dictionaryValues[key]" rows="2" maxlength="500" /></label>
          <VButton type="secondary" :loading="dictionarySaving" @click="saveDictionary">{{ t("common.saveAndBuild") }}</VButton>
          <TaskProgress v-if="dictionaryTaskId" :task-id="dictionaryTaskId" />
        </div>
      </VCard>
      <VCard>
        <div class="settings-section-title"><div><strong>{{ t("localesPage.addTarget") }}</strong><span>{{ t("localesPage.addTargetHelp") }}</span></div></div>
        <div class="locale-add-form">
          <label><span>{{ t("localesPage.localeCode") }}</span><input v-model="code" list="locale-presets" placeholder="ja" @input="suggestLabel" /></label>
          <datalist id="locale-presets"><option v-for="(presetLabel, presetCode) in presets" :key="presetCode" :value="presetCode">{{ presetLabel }}</option></datalist>
          <label><span>{{ t("localesPage.displayName") }}</span><input v-model="label" placeholder="Japanese" /></label>
          <VButton @click="addLocale">{{ t("localesPage.add") }}</VButton>
        </div>
      </VCard>
      <VCard>
        <div class="settings-section-title"><div><strong>{{ t("localesPage.fallback") }}</strong><span>{{ t("localesPage.fallbackHelp", { source: sourceLocale }) }}</span></div></div>
        <div class="fallback-chain"><code>{{ t("localesPage.requested") }}</code><template v-for="locale in fallbackChain" :key="locale"><span>→</span><code>{{ locale }}</code></template><span>→</span><code>{{ t("localesPage.entitySource") }}</code><span>→</span><code>404</code></div>
      </VCard>
    </div>
  </div>
</template>
