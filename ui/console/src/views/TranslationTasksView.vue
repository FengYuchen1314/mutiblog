<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { VCard, VEmpty, VPageHeader, VTag } from "@halo-dev/components";
import { api, type TranslationTask } from "@/api/client";
import { useCodeLabel } from "@/i18n/useCodeLabel";

const tasks = ref<TranslationTask[]>([]);
const { t } = useI18n();
const codeLabel = useCodeLabel();
const error = ref("");
const loading = ref(true);
let timer: number | undefined;

const hasRunning = computed(() => tasks.value.some((task) => task.status === "queued" || task.status === "running"));
const legacyErrors: Record<string, string> = {
  "source content changed before translation started": "source-changed-before-start",
  "one or more target languages failed": "target-failed",
  "translations saved but static rebuild failed": "rebuild-failed",
  "source content changed during translation": "source-changed",
  "manual translation is protected": "manual-protected",
  "provider API key is missing": "provider-key-missing",
  "provider request failed": "provider-request-failed",
  "provider configuration is unavailable": "provider-unavailable",
  "provider output did not preserve protected Markdown": "unsafe-output",
  "translation failed": "translation-failed",
};
const knownErrors = new Set(["source-changed-before-start", "target-needs-review", "target-failed", "rebuild-failed", "source-changed", "target-changed", "manual-protected", "provider-key-missing", "provider-request-failed", "provider-unavailable", "unsafe-output", "translation-timeout", "translation-failed"]);

async function load() {
  try {
    tasks.value = (await api.translationTasks()).items;
    error.value = "";
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("tasksPage.loadFailed");
  } finally {
    loading.value = false;
  }
  window.clearTimeout(timer);
  if (hasRunning.value) timer = window.setTimeout(load, 1500);
}

function statusType(status: string) {
  if (status === "succeeded") return "success";
  if (status === "failed" || status === "needs-review") return "danger";
  return "default";
}

function taskErrors(task: TranslationTask) {
  const values = [task.error, ...task.targets.map((target) => target.error)].filter((value): value is string => Boolean(value));
  return [...new Set(values.map((value) => legacyErrors[value] ?? value))]
    .map((code) => knownErrors.has(code) ? t(`tasksPage.errors.${code}`) : t("tasksPage.errors.unknown"))
    .join(" · ");
}

onMounted(load);
onBeforeUnmount(() => window.clearTimeout(timer));
</script>

<template>
  <div class="page">
    <VPageHeader :title="t('tasksPage.title')" />
    <div class="page-body">
      <div v-if="error" class="form-alert">{{ error }}</div>
      <VCard>
        <div class="filter-bar"><span>{{ t("tasksPage.guidance") }}</span><button type="button" @click="load">{{ t("common.refresh") }}</button></div>
        <div v-if="loading" class="resource-loading">{{ t("tasksPage.loading") }}</div>
        <div v-else-if="tasks.length" class="translation-task-list">
          <article v-for="task in tasks" :key="task.id" class="translation-task-row">
            <div class="translation-task-main"><strong>{{ codeLabel(task.entityKind) }} · {{ task.entityId }}</strong><span>{{ task.providerId }} / {{ task.model }} · {{ t("tasksPage.sourceRevision") }} {{ task.sourceRevision }}</span><code>{{ task.id }}</code></div>
            <div class="translation-targets"><VTag v-for="target in task.targets" :key="target.locale" :type="statusType(target.status)">{{ target.locale }} · {{ codeLabel(target.status) }}</VTag></div>
            <div class="translation-task-status"><VTag :type="statusType(task.status)">{{ codeLabel(task.status) }}</VTag><span>{{ new Date(task.createdAt).toLocaleString() }}</span></div>
            <div v-if="task.error || task.targets.some((target) => target.error)" class="translation-task-error">{{ taskErrors(task) }}</div>
          </article>
        </div>
        <div v-else class="empty-resource"><VEmpty :title="t('tasksPage.empty')" /></div>
      </VCard>
    </div>
  </div>
</template>
