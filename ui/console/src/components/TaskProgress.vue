<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { ApiError, api, type UnifiedTask } from "@/api/client";
import { useCodeLabel } from "@/i18n/useCodeLabel";
import { useTaskPresentation } from "@/i18n/useTaskPresentation";

const props = withDefaults(defineProps<{ taskId?: string; compact?: boolean }>(), { taskId: "", compact: false });
const emit = defineEmits<{ update: [task: UnifiedTask]; terminal: [task: UnifiedTask] }>();
const { t } = useI18n();
const codeLabel = useCodeLabel();
const {
  kindLabel: presentKind,
  operationLabel: presentOperation,
  progressLabel,
  progressPercent,
  taskErrorLabel,
  taskWarnings,
} = useTaskPresentation();
const task = ref<UnifiedTask>();
const waiting = ref(false);
const error = ref("");
const missingTaskPollLimit = 30;
const pollIntervalMilliseconds = 700;
let missingTaskPolls = 0;
let timer: number | undefined;

const terminal = computed(() => task.value && !["queued", "running"].includes(task.value.status));
const percent = computed(() => progressPercent(task.value?.progress.percent));
const label = computed(() => {
  if (!task.value) return error.value || (waiting.value ? t("taskProgress.waiting") : "");
  return progressLabel(task.value.progress.message || task.value.progress.phase || task.value.status);
});
const kindLabel = computed(() => (task.value ? presentKind(task.value.kind) : ""));
const operationLabel = computed(() => (task.value ? presentOperation(task.value.operation) : ""));
const warnings = computed(() => (task.value ? taskWarnings(task.value) : []));
const statusLabel = computed(() => (task.value ? codeLabel(task.value.status) : ""));
const taskFailure = computed(() =>
  task.value?.error ? taskErrorLabel(task.value.error, "taskCenter.taskFailed") : "",
);

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
      error.value =
        caught instanceof ApiError
          ? taskErrorLabel(caught.code)
          : caught instanceof Error
            ? caught.message
            : String(t("taskProgress.unavailable"));
    }
  }
  timer = window.setTimeout(load, pollIntervalMilliseconds);
}

watch(
  () => props.taskId,
  () => {
    window.clearTimeout(timer);
    missingTaskPolls = 0;
    task.value = undefined;
    waiting.value = Boolean(props.taskId);
    error.value = "";
    void load();
  },
  { immediate: true },
);

onBeforeUnmount(() => window.clearTimeout(timer));
</script>

<template>
  <section
    v-if="taskId"
    class="task-progress"
    :class="{
      'task-progress--compact': compact,
      'task-progress--failed': task?.status === 'failed' || task?.status === 'needs-review',
    }"
    :aria-label="label"
  >
    <div class="task-progress__header">
      <span>{{ label }}</span>
      <div class="task-progress__summary">
        <span v-if="statusLabel" class="task-progress__status">{{ statusLabel }}</span>
        <strong>{{ percent }}%</strong>
      </div>
    </div>
    <div class="task-progress__track" role="progressbar" :aria-valuenow="percent" aria-valuemin="0" aria-valuemax="100">
      <span :style="{ width: `${percent}%` }" />
    </div>
    <div v-if="!compact && task" class="task-progress__meta">
      <span>{{ kindLabel }} · {{ operationLabel }}</span>
      <span v-if="task.progress.total">{{ task.progress.current }} / {{ task.progress.total }}</span>
    </div>
    <p v-for="warning in warnings" :key="warning" class="task-progress__warning">{{ warning }}</p>
    <p v-if="taskFailure" class="task-progress__error">{{ taskFailure }}</p>
    <p v-if="error" class="task-progress__error">{{ error }}</p>
  </section>
</template>

<style scoped>
.task-progress {
  display: grid;
  gap: 0.45rem;
  border: 1px solid #e3e7ef;
  border-radius: 0.55rem;
  background: #fff;
  padding: 0.75rem;
}
.task-progress__header,
.task-progress__meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}
.task-progress__summary {
  display: flex;
  align-items: center;
  gap: 0.45rem;
}
.task-progress__header {
  color: #394150;
  font-size: 0.76rem;
  text-transform: capitalize;
}
.task-progress__header strong {
  color: #111827;
  font-variant-numeric: tabular-nums;
}
.task-progress__status {
  color: #687386;
  font-size: 0.68rem;
  text-transform: none;
}
.task-progress__track {
  overflow: hidden;
  height: 0.5rem;
  border-radius: 999px;
  background: #edf0f5;
}
.task-progress__track span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: #4f46e5;
  transition: width 0.55s ease;
}
.task-progress--failed .task-progress__track span {
  background: #dc2626;
}
.task-progress__meta {
  color: #818a9a;
  font-size: 0.68rem;
}
.task-progress__warning {
  margin: 0;
  color: #8a5b08;
  font-size: 0.7rem;
}
.task-progress__error {
  margin: 0;
  color: #b91c1c;
  font-size: 0.7rem;
}
.task-progress--compact {
  border: 0;
  background: transparent;
  padding: 0;
}
</style>
