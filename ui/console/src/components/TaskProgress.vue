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
let controller: AbortController | undefined;
let requestSequence = 0;
let disposed = false;

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
const taskFailureDetail = computed(() => task.value?.errorDetail ?? "");
const progressValueText = computed(() => `${label.value} ${percent.value}%`);

function stopPolling() {
  window.clearTimeout(timer);
  controller?.abort();
  controller = undefined;
  requestSequence++;
}

function pollDelay() {
  if (typeof document !== "undefined" && document.visibilityState === "hidden") return 5000;
  return pollIntervalMilliseconds;
}

async function load() {
  window.clearTimeout(timer);
  if (!props.taskId || disposed) return;
  const requestedTaskId = props.taskId;
  controller?.abort();
  const requestController = new AbortController();
  controller = requestController;
  const sequence = ++requestSequence;
  try {
    const loaded = await api.task(requestedTaskId, requestController.signal);
    if (disposed || sequence !== requestSequence || props.taskId !== requestedTaskId) return;
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
    if (
      disposed ||
      sequence !== requestSequence ||
      props.taskId !== requestedTaskId ||
      (caught instanceof Error && caught.name === "AbortError")
    )
      return;
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
  if (!disposed && sequence === requestSequence && props.taskId === requestedTaskId)
    timer = window.setTimeout(load, pollDelay());
}

watch(
  () => props.taskId,
  () => {
    stopPolling();
    missingTaskPolls = 0;
    task.value = undefined;
    waiting.value = Boolean(props.taskId);
    error.value = "";
    void load();
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  disposed = true;
  stopPolling();
});
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
    <div
      class="task-progress__track"
      role="progressbar"
      :aria-label="label"
      :aria-valuetext="progressValueText"
      :aria-valuenow="percent"
      aria-valuemin="0"
      aria-valuemax="100"
    >
      <span :style="{ width: `${percent}%` }" />
    </div>
    <div v-if="!compact && task" class="task-progress__meta">
      <span>{{ kindLabel }} · {{ operationLabel }}</span>
      <span v-if="task.progress.total">{{ task.progress.current }} / {{ task.progress.total }}</span>
    </div>
    <p v-for="warning in warnings" :key="warning" class="task-progress__warning">{{ warning }}</p>
    <p v-if="taskFailure" class="task-progress__error">{{ taskFailure }}</p>
    <p v-if="taskFailureDetail" class="task-progress__error-detail">{{ taskFailureDetail }}</p>
    <p v-if="error" class="task-progress__error">{{ error }}</p>
    <p v-if="terminal && (taskFailure || warnings.length)" class="task-progress__live" aria-live="polite">
      {{ taskFailure || warnings[0] }}
    </p>
  </section>
</template>

<style scoped>
.task-progress {
  display: grid;
  gap: 0.45rem;
  border: 1px solid var(--m-sys-color-outline-variant);
  border-radius: var(--m-sys-shape-corner-medium);
  background: var(--m-sys-color-surface-container-lowest);
  box-shadow: var(--m-sys-elevation-level0);
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
  color: var(--m-sys-color-on-surface);
  font-size: var(--m-sys-typescale-label-large-size);
  font-weight: 600;
  text-transform: capitalize;
}
.task-progress__header strong {
  color: var(--m-sys-color-on-surface);
  font-variant-numeric: tabular-nums;
}
.task-progress__status {
  color: var(--m-sys-color-on-surface-variant);
  font-size: var(--m-sys-typescale-label-medium-size);
  text-transform: none;
}
.task-progress__track {
  overflow: hidden;
  height: 0.5rem;
  border-radius: var(--m-sys-shape-corner-full);
  background: var(--m-sys-color-surface-container-highest);
}
.task-progress__track span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--m-sys-color-primary);
  transition: width var(--m-sys-motion-duration-medium2) var(--m-sys-motion-easing-emphasized);
}
.task-progress--failed .task-progress__track span {
  background: var(--m-sys-color-error);
}
.task-progress--failed {
  border-color: color-mix(in srgb, var(--m-sys-color-error) 55%, var(--m-sys-color-outline-variant));
  background: color-mix(in srgb, var(--m-sys-color-error-container) 32%, var(--m-sys-color-surface-container-lowest));
}
.task-progress__meta {
  color: var(--m-sys-color-on-surface-variant);
  font-size: var(--m-sys-typescale-label-medium-size);
}
.task-progress__warning {
  margin: 0;
  border-radius: var(--m-sys-shape-corner-small);
  background: var(--m-sys-color-warning-container);
  color: var(--m-sys-color-on-warning-container);
  font-size: var(--m-sys-typescale-label-medium-size);
  padding: 0.45rem 0.6rem;
}
.task-progress__error {
  margin: 0;
  border-radius: var(--m-sys-shape-corner-small);
  background: var(--m-sys-color-error-container);
  color: var(--m-sys-color-on-error-container);
  font-size: var(--m-sys-typescale-label-medium-size);
  padding: 0.45rem 0.6rem;
}
.task-progress__error-detail {
  margin: 0;
  border: 1px solid color-mix(in srgb, var(--m-sys-color-error) 32%, var(--m-sys-color-outline-variant));
  border-radius: var(--m-sys-shape-corner-small);
  background: var(--m-sys-color-surface-container-lowest);
  color: var(--m-sys-color-on-error-container);
  font-size: var(--m-sys-typescale-label-medium-size);
  line-height: var(--m-sys-typescale-label-medium-line-height);
  padding: 0.45rem 0.6rem;
}
.task-progress__live {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}
.task-progress--compact {
  border: 0;
  background: transparent;
  padding: 0;
}
</style>
