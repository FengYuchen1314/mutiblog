<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { VCard, VEmpty, VPageHeader, VTag } from "@halo-dev/components";
import { api, type TaskStatus, type UnifiedTask } from "@/api/client";
import { useCodeLabel } from "@/i18n/useCodeLabel";

const { t, te } = useI18n();
const route = useRoute();
const codeLabel = useCodeLabel();
const tasks = ref<UnifiedTask[]>([]);
const loading = ref(true);
const error = ref("");
const kind = ref<"" | UnifiedTask["kind"]>("");
const status = ref<"" | TaskStatus>("");
let timer: number | undefined;

const active = computed(() => tasks.value.some((task) => task.status === "queued" || task.status === "running"));
const focusedTaskId = computed(() => typeof route.query.focus === "string" ? route.query.focus : "");

async function load() {
  window.clearTimeout(timer);
  try {
    const response = await api.tasks({ kind: kind.value || undefined, status: status.value || undefined, limit: 200 });
    tasks.value = response.items;
    error.value = "";
  } catch (caught) {
    error.value = t("taskCenter.loadFailed");
  } finally {
    loading.value = false;
  }
  timer = window.setTimeout(load, active.value ? 750 : 4000);
}

function reloadFiltered() {
  loading.value = true;
  void load();
}

function statusType(value: string) {
  if (value === "succeeded") return "success";
  if (value === "failed" || value === "needs-review") return "danger";
  return "default";
}

function taskTitle(task: UnifiedTask) {
  if (task.subject) return `${codeLabel(task.subject.kind)} · ${task.subject.id}`;
  if (task.backupId) return `${kindLabel("Backup")} · ${task.backupId}`;
  return kindLabel(task.kind);
}

function translated(key: string, fallback: string) {
  return te(key) ? String(t(key)) : fallback;
}

function taskProgressTranslated(key: string, fallback: string) {
  const indexKey = `taskProgressIndex.${key}`;
  return te(indexKey) ? String(t(indexKey)) : translated(`taskProgress.${key}`, fallback);
}

function kindLabel(value: string) {
  return taskProgressTranslated(`kinds.${value}`, value);
}

function operationLabel(value: string) {
  return taskProgressTranslated(`operations.${value}`, value);
}

function progressLabel(value: string) {
  const translatedChunk = /^translated-chunk-(\d+)-of-(\d+)$/.exec(value);
  if (translatedChunk) return String(t("taskProgressTranslatedChunk", { current: translatedChunk[1], total: translatedChunk[2] }));
  const indexKey = `taskProgressIndex.messages.${value}`;
  if (te(indexKey)) return String(t(indexKey));
  const messageKey = `taskProgress.messages.${value}`;
  if (te(messageKey)) return String(t(messageKey));
  const extraKey = `taskProgressExtraMessages.${value}`;
  if (te(extraKey)) return String(t(extraKey));
  const statusKey = `codes.${value}`;
  return te(statusKey) ? String(t(statusKey)) : String(t("taskProgress.working"));
}

function taskErrorLabel(value: string) {
  const indexKey = `taskProgressIndex.messages.${value}`;
  if (te(indexKey)) return String(t(indexKey));
  const taskKey = `tasksPage.errors.${value}`;
  if (te(taskKey)) return String(t(taskKey));
  const backupKey = `backupPage.taskErrors.${value}`;
  if (te(backupKey)) return String(t(backupKey));
  const progressKey = `taskProgress.messages.${value}`;
  if (te(progressKey)) return String(t(progressKey));
  const extraKey = `taskProgressExtraMessages.${value}`;
  return te(extraKey) ? String(t(extraKey)) : String(t("taskCenter.taskFailed"));
}

function taskWarnings(task: UnifiedTask) {
  const warnings: string[] = [];
  if (["failed", "unavailable"].includes(task.buildStatus ?? "")) {
    warnings.push(String(t("taskProgressWarnings.buildFailed")));
  }
  if (["failed", "needs-review"].includes(task.translationStatus ?? "")) {
    warnings.push(String(t("taskProgressWarnings.translationFailed")));
  }
  return warnings;
}

onMounted(load);
onBeforeUnmount(() => window.clearTimeout(timer));
</script>

<template>
  <div class="page">
    <VPageHeader :title="t('taskCenter.title')" />
    <div class="page-body">
      <div v-if="error" class="form-alert">{{ error }}</div>
      <VCard>
        <div class="filter-bar">
          <span class="task-center-guidance">{{ t("taskCenter.guidance") }}</span>
          <select v-model="kind" @change="reloadFiltered">
            <option value="">{{ t("taskCenter.allTypes") }}</option>
            <option value="Translation">{{ kindLabel("Translation") }}</option>
            <option value="StaticBuild">{{ kindLabel("StaticBuild") }}</option>
            <option value="IndexRebuild">{{ kindLabel("IndexRebuild") }}</option>
            <option value="Backup">{{ kindLabel("Backup") }}</option>
            <option value="ScheduledPublish">{{ kindLabel("ScheduledPublish") }}</option>
          </select>
          <select v-model="status" @change="reloadFiltered">
            <option value="">{{ t("taskCenter.allStatuses") }}</option>
            <option value="queued">{{ codeLabel("queued") }}</option>
            <option value="running">{{ codeLabel("running") }}</option>
            <option value="succeeded">{{ codeLabel("succeeded") }}</option>
            <option value="failed">{{ codeLabel("failed") }}</option>
            <option value="needs-review">{{ codeLabel("needs-review") }}</option>
          </select>
          <button type="button" @click="reloadFiltered">{{ t("common.refresh") }}</button>
        </div>
        <div v-if="loading" class="resource-loading">{{ t("taskCenter.loading") }}</div>
        <div v-else-if="tasks.length" class="task-center-list">
          <article v-for="task in tasks" :key="task.id" class="task-center-row" :class="{ 'task-center-row--focused': task.id === focusedTaskId }">
            <div class="task-center-heading">
              <div class="task-center-copy">
                <strong>{{ taskTitle(task) }}</strong>
                <span>{{ kindLabel(task.kind) }} · {{ operationLabel(task.operation) }}<template v-if="task.providerId"> · {{ task.providerId }} / {{ task.model }}</template></span>
                <code>{{ task.id }}</code>
              </div>
              <div class="task-center-status">
                <VTag :type="statusType(task.status)">{{ codeLabel(task.status) }}</VTag>
                <time>{{ new Date(task.completedAt ?? task.startedAt ?? task.createdAt).toLocaleString() }}</time>
              </div>
            </div>
            <div class="task-center-progress-line">
              <div class="task-center-track" role="progressbar" :aria-valuenow="task.progress.percent" aria-valuemin="0" aria-valuemax="100">
                <span :class="{ failed: task.status === 'failed' || task.status === 'needs-review' }" :style="{ width: `${task.progress.percent}%` }" />
              </div>
              <strong>{{ task.progress.percent }}%</strong>
            </div>
            <div class="task-center-phase">
              <span>{{ progressLabel(task.progress.message || task.progress.phase || task.status) }}</span>
              <span v-if="task.progress.total">{{ task.progress.current }} / {{ task.progress.total }}</span>
            </div>
            <div v-if="task.dueAt" class="task-center-phase">
              <span>{{ t("taskCenter.dueAt", { time: new Date(task.dueAt).toLocaleString() }) }}</span>
            </div>
            <div v-if="task.targets?.length" class="translation-targets">
              <VTag v-for="target in task.targets" :key="target.locale" :type="statusType(target.status)">
                {{ target.locale }} · {{ target.progress?.percent ?? 0 }}%
              </VTag>
            </div>
            <div v-for="warning in taskWarnings(task)" :key="warning" class="task-center-warning">{{ warning }}</div>
            <div v-if="task.error" class="translation-task-error">{{ taskErrorLabel(task.error) }}</div>
          </article>
        </div>
        <div v-else class="empty-resource"><VEmpty :title="t('taskCenter.empty')" /></div>
      </VCard>
    </div>
  </div>
</template>

<style scoped>
.task-center-guidance { margin-right: auto; color: #687386; font-size: .74rem; }
.task-center-list { display: flex; flex-direction: column; }
.task-center-row { display: grid; gap: .65rem; border-bottom: 1px solid #eceef2; padding: 1rem; }
.task-center-row--focused { background: #f5f7ff; box-shadow: inset 3px 0 #4f46e5; }
.task-center-row:last-child { border-bottom: 0; }
.task-center-heading, .task-center-progress-line, .task-center-phase { display: flex; align-items: center; justify-content: space-between; gap: .8rem; }
.task-center-copy { display: grid; min-width: 0; gap: .15rem; }
.task-center-copy span, .task-center-copy code, .task-center-status time, .task-center-phase { color: #818a9a; font-size: .68rem; }
.task-center-copy span, .task-center-copy code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.task-center-status { display: grid; flex: 0 0 auto; justify-items: end; gap: .3rem; }
.task-center-track { overflow: hidden; width: 100%; height: .55rem; border-radius: 999px; background: #edf0f5; }
.task-center-track span { display: block; height: 100%; border-radius: inherit; background: #4f46e5; transition: width .55s ease; }
.task-center-track span.failed { background: #dc2626; }
.task-center-progress-line > strong { width: 2.8rem; color: #293244; font-size: .72rem; text-align: right; font-variant-numeric: tabular-nums; }
.task-center-phase { text-transform: capitalize; }
.task-center-warning { color: #8a5b08; font-size: .72rem; }
@media (max-width: 720px) { .task-center-heading { align-items: flex-start; flex-direction: column; } .task-center-status { justify-items: start; } }
</style>
