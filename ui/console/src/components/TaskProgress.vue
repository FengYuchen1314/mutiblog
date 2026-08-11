<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from "vue";
import { ref } from "vue";
import { useI18n } from "vue-i18n";
import { ApiError, api, type UnifiedTask } from "@/api/client";

const props = withDefaults(defineProps<{ taskId?: string; compact?: boolean }>(), { taskId: "", compact: false });
const emit = defineEmits<{ update: [task: UnifiedTask]; terminal: [task: UnifiedTask] }>();
const { t, te } = useI18n();
const task = ref<UnifiedTask>();
const waiting = ref(false);
const error = ref("");
const missingTaskPollLimit = 30;
const pollIntervalMilliseconds = 700;
let missingTaskPolls = 0;
let timer: number | undefined;

const terminal = computed(() => task.value && !["queued", "running"].includes(task.value.status));
const percent = computed(() => Math.max(0, Math.min(100, task.value?.progress.percent ?? 0)));
const label = computed(() => {
  if (!task.value) return error.value || (waiting.value ? t("taskProgress.waiting") : "");
  return progressLabel(task.value.progress.message || task.value.progress.phase || task.value.status);
});
const kindLabel = computed(() => task.value ? taskProgressTranslated(`kinds.${task.value.kind}`, task.value.kind) : "");
const operationLabel = computed(() => task.value ? taskProgressTranslated(`operations.${task.value.operation}`, task.value.operation) : "");
const warnings = computed(() => {
  if (!task.value) return [];
  const values: string[] = [];
  if (["failed", "unavailable"].includes(task.value.buildStatus ?? "")) {
    values.push(String(t("taskProgressWarnings.buildFailed")));
  }
  if (["failed", "needs-review"].includes(task.value.translationStatus ?? "")) {
    values.push(String(t("taskProgressWarnings.translationFailed")));
  }
  return values;
});

function translated(key: string, fallback: string) {
  return te(key) ? String(t(key)) : fallback;
}

function taskProgressTranslated(key: string, fallback: string) {
  const indexKey = `taskProgressIndex.${key}`;
  return te(indexKey) ? String(t(indexKey)) : translated(`taskProgress.${key}`, fallback);
}

function progressLabel(value: string) {
  const translatedChunk = /^translated-chunk-(\d+)-of-(\d+)$/.exec(value);
  if (translatedChunk) return String(t("taskProgressTranslatedChunk", { current: translatedChunk[1], total: translatedChunk[2] }));
  const indexKey = `taskProgressIndex.messages.${value}`;
  if (te(indexKey)) return String(t(indexKey));
  const key = `taskProgress.messages.${value}`;
  if (te(key)) return String(t(key));
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
  return te(extraKey) ? String(t(extraKey)) : String(t("taskProgress.unavailable"));
}

async function load() {
  window.clearTimeout(timer);
  if (!props.taskId) return;
  const requestedTaskId = props.taskId;
  try {
    const loaded = await api.task(requestedTaskId);
    if (props.taskId !== requestedTaskId) return;
    task.value = loaded;
    missingTaskPolls = 0;
    waiting.value = false;
    error.value = "";
    emit("update", task.value);
    if (terminal.value) {
      emit("terminal", task.value);
      return;
    }
  } catch (caught) {
    if (props.taskId !== requestedTaskId) return;
    if (caught instanceof ApiError && caught.status === 404) {
      missingTaskPolls++;
      if (missingTaskPolls >= missingTaskPollLimit) {
        waiting.value = false;
        error.value = String(t("taskProgress.unavailable"));
        return;
      }
      waiting.value = true;
      error.value = "";
    } else {
      error.value = caught instanceof ApiError
        ? taskErrorLabel(caught.code)
        : caught instanceof Error
          ? caught.message
          : String(t("taskProgress.unavailable"));
    }
  }
  timer = window.setTimeout(load, pollIntervalMilliseconds);
}

watch(() => props.taskId, () => {
  window.clearTimeout(timer);
  missingTaskPolls = 0;
  task.value = undefined;
  waiting.value = Boolean(props.taskId);
  error.value = "";
  void load();
}, { immediate: true });

onBeforeUnmount(() => window.clearTimeout(timer));
</script>

<template>
  <section v-if="taskId" class="task-progress" :class="{ 'task-progress--compact': compact, 'task-progress--failed': task?.status === 'failed' || task?.status === 'needs-review' }" :aria-label="label">
    <div class="task-progress__header">
      <span>{{ label }}</span>
      <strong>{{ percent }}%</strong>
    </div>
    <div class="task-progress__track" role="progressbar" :aria-valuenow="percent" aria-valuemin="0" aria-valuemax="100">
      <span :style="{ width: `${percent}%` }" />
    </div>
    <div v-if="!compact && task" class="task-progress__meta">
      <span>{{ kindLabel }} · {{ operationLabel }}</span>
      <span v-if="task.progress.total">{{ task.progress.current }} / {{ task.progress.total }}</span>
    </div>
    <p v-for="warning in warnings" :key="warning" class="task-progress__warning">{{ warning }}</p>
    <p v-if="error" class="task-progress__error">{{ error }}</p>
  </section>
</template>

<style scoped>
.task-progress { display: grid; gap: .45rem; border: 1px solid #e3e7ef; border-radius: .55rem; background: #fff; padding: .75rem; }
.task-progress__header, .task-progress__meta { display: flex; align-items: center; justify-content: space-between; gap: .75rem; }
.task-progress__header { color: #394150; font-size: .76rem; text-transform: capitalize; }
.task-progress__header strong { color: #111827; font-variant-numeric: tabular-nums; }
.task-progress__track { overflow: hidden; height: .5rem; border-radius: 999px; background: #edf0f5; }
.task-progress__track span { display: block; height: 100%; border-radius: inherit; background: #4f46e5; transition: width .55s ease; }
.task-progress--failed .task-progress__track span { background: #dc2626; }
.task-progress__meta { color: #818a9a; font-size: .68rem; }
.task-progress__warning { margin: 0; color: #8a5b08; font-size: .7rem; }
.task-progress__error { margin: 0; color: #b91c1c; font-size: .7rem; }
.task-progress--compact { border: 0; background: transparent; padding: 0; }
</style>
