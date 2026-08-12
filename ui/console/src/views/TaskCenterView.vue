<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { RouteLocationRaw } from "vue-router";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { ApiError, api, type TaskRelationRole, type TaskStatus, type UnifiedTask } from "@/api/client";
import { buildTaskTree, type TaskTreeNode } from "@/composables/taskTree";
import { useCodeLabel } from "@/i18n/useCodeLabel";
import { useTaskPresentation } from "@/i18n/useTaskPresentation";

interface TaskRow {
  node: TaskTreeNode;
  hasChildren: boolean;
}

const { t } = useI18n();
const route = useRoute();
const codeLabel = useCodeLabel();
const { kindLabel, operationLabel, progressLabel, progressPercent, taskErrorLabel, taskWarnings } =
  useTaskPresentation();
const tasks = ref<UnifiedTask[]>([]);
const loading = ref(true);
const error = ref("");
const kind = ref<"" | UnifiedTask["kind"]>("");
const status = ref<"" | TaskStatus>("");
const total = ref(0);
const activeTaskCount = ref(0);
const announcedSummary = ref("");
const expandedTaskIDs = ref<Set<string>>(new Set());
let timer: number | undefined;
let controller: AbortController | undefined;
let requestSequence = 0;
let disposed = false;

const focusedTaskId = computed(() => (typeof route.query.focus === "string" ? route.query.focus : ""));
const tree = computed(() => buildTaskTree(tasks.value));
const active = computed(() => tasks.value.some((task) => task.status === "queued" || task.status === "running"));
const taskRows = computed<TaskRow[]>(() => {
  const rows: TaskRow[] = [];
  const append = (nodes: TaskTreeNode[]) => {
    for (const node of nodes) {
      const hasChildren = node.children.length > 0;
      rows.push({ node, hasChildren });
      if (hasChildren && expandedTaskIDs.value.has(node.task.id)) append(node.children);
    }
  };
  append(tree.value.roots);
  return rows;
});

function stopPolling() {
  window.clearTimeout(timer);
  controller?.abort();
  controller = undefined;
  requestSequence++;
}

function pollDelay() {
  if (typeof document !== "undefined" && document.visibilityState === "hidden") return 6000;
  return active.value ? 900 : 5000;
}

function taskTitle(task: UnifiedTask) {
  if (task.subject) return `${codeLabel(task.subject.kind)} · ${task.subject.id}`;
  if (task.backupId) return `${kindLabel("Backup")} · ${task.backupId}`;
  return kindLabel(task.kind);
}

function taskDestination(task: UnifiedTask): RouteLocationRaw | undefined {
  if (task.subject?.kind === "Post") return { name: "post-editor", params: { id: task.subject.id } };
  if (task.subject?.kind === "Page") return { name: "page-editor", params: { id: task.subject.id } };
  switch (task.kind) {
    case "LocaleProvision":
      return { name: "locales" };
    case "StaticBuild":
    case "IndexRebuild":
      return { name: "tools" };
    case "Backup":
      return { name: "backup" };
    default:
      return undefined;
  }
}

function taskActionLabel(task: UnifiedTask) {
  if (task.subject) return t(task.status === "needs-review" ? "taskCenter.reviewContent" : "taskCenter.openContent");
  switch (task.kind) {
    case "LocaleProvision":
      return t("taskCenter.openLocales");
    case "StaticBuild":
    case "IndexRebuild":
      return t("taskCenter.openTools");
    case "Backup":
      return t("taskCenter.openBackups");
    default:
      return "";
  }
}

function outcomeLabel(task: UnifiedTask) {
  if (!task.outcome) return "";
  const key = `taskCenter.outcomes.${task.outcome}`;
  const translated = t(key);
  return translated === key ? task.outcome : translated;
}

function relationLabel(role?: TaskRelationRole) {
  if (!role) return "";
  return t(`taskCenter.relationships.${role}`);
}

function taskChipTone(status: string): "neutral" | "success" | "warning" | "danger" {
  if (status === "succeeded") return "success";
  if (status === "failed" || status === "needs-review") return "danger";
  if (status === "queued" || status === "running") return "warning";
  return "neutral";
}

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString() : "";
}

function toggleExpanded(taskID: string) {
  const next = new Set(expandedTaskIDs.value);
  if (next.has(taskID)) next.delete(taskID);
  else next.add(taskID);
  expandedTaskIDs.value = next;
}

function expandFocusedTask() {
  const taskID = focusedTaskId.value;
  if (!taskID) return;
  const next = new Set(expandedTaskIDs.value);
  const seen = new Set<string>();
  let cursor = tree.value.parentByTaskId.get(taskID);
  while (cursor && !seen.has(cursor)) {
    seen.add(cursor);
    next.add(cursor);
    cursor = tree.value.parentByTaskId.get(cursor);
  }
  expandedTaskIDs.value = next;
  void nextTick(() => document.getElementById(`task-${taskID}`)?.focus({ preventScroll: false }));
}

async function load() {
  window.clearTimeout(timer);
  if (disposed) return;
  controller?.abort();
  const requestController = new AbortController();
  controller = requestController;
  const sequence = ++requestSequence;
  try {
    const response = await api.tasks(
      { kind: kind.value || undefined, status: status.value || undefined, limit: 200 },
      requestController.signal,
    );
    if (disposed || sequence !== requestSequence) return;
    tasks.value = response.items;
    total.value = response.total;
    activeTaskCount.value = response.active;
    error.value = "";
    const summary = t("taskCenter.summary", { total: response.total, active: response.active });
    if (announcedSummary.value !== summary) announcedSummary.value = summary;
  } catch (caught) {
    if (disposed || sequence !== requestSequence || (caught instanceof Error && caught.name === "AbortError")) return;
    error.value = caught instanceof ApiError ? caught.message : t("taskCenter.loadFailed");
  } finally {
    if (!disposed && sequence === requestSequence) {
      loading.value = false;
      timer = window.setTimeout(load, pollDelay());
    }
  }
}

function reloadFiltered() {
  loading.value = true;
  void load();
}

function handleVisibilityChange() {
  if (document.visibilityState === "visible") reloadFiltered();
}

watch([tasks, focusedTaskId], expandFocusedTask, { flush: "post" });
watch([kind, status], reloadFiltered);

onMounted(() => {
  document.addEventListener("visibilitychange", handleVisibilityChange);
  void load();
});
onBeforeUnmount(() => {
  disposed = true;
  document.removeEventListener("visibilitychange", handleVisibilityChange);
  stopPolling();
});
</script>

<template>
  <div class="page">
    <MPageHeader :title="t('taskCenter.title')" />
    <div class="page-body">
      <div v-if="error" class="form-alert">{{ error }}</div>
      <MSurface>
        <div class="filter-bar task-center-filter-bar">
          <span class="task-center-guidance">{{ t("taskCenter.guidance") }}</span>
          <label class="task-center-sr-only" for="task-kind-filter">{{ t("taskCenter.typeFilter") }}</label>
          <select id="task-kind-filter" v-model="kind">
            <option value="">{{ t("taskCenter.allTypes") }}</option>
            <option value="Translation">{{ kindLabel("Translation") }}</option>
            <option value="LocaleProvision">{{ kindLabel("LocaleProvision") }}</option>
            <option value="StaticBuild">{{ kindLabel("StaticBuild") }}</option>
            <option value="IndexRebuild">{{ kindLabel("IndexRebuild") }}</option>
            <option value="Backup">{{ kindLabel("Backup") }}</option>
            <option value="ScheduledPublish">{{ kindLabel("ScheduledPublish") }}</option>
          </select>
          <label class="task-center-sr-only" for="task-status-filter">{{ t("taskCenter.statusFilter") }}</label>
          <select id="task-status-filter" v-model="status">
            <option value="">{{ t("taskCenter.allStatuses") }}</option>
            <option value="queued">{{ codeLabel("queued") }}</option>
            <option value="running">{{ codeLabel("running") }}</option>
            <option value="succeeded">{{ codeLabel("succeeded") }}</option>
            <option value="failed">{{ codeLabel("failed") }}</option>
            <option value="needs-review">{{ codeLabel("needs-review") }}</option>
          </select>
          <button type="button" @click="reloadFiltered">{{ t("common.refresh") }}</button>
        </div>
        <p class="task-center-summary">{{ t("taskCenter.summary", { total, active: activeTaskCount }) }}</p>
        <p class="task-center-sr-only" aria-live="polite">{{ announcedSummary }}</p>
        <div v-if="loading" class="resource-loading">{{ t("taskCenter.loading") }}</div>
        <div v-else-if="taskRows.length" class="task-center-list">
          <article
            v-for="row in taskRows"
            :id="`task-${row.node.task.id}`"
            :key="row.node.task.id"
            class="task-center-row"
            :class="{
              'task-center-row--focused': row.node.task.id === focusedTaskId,
              'task-center-row--warning':
                row.node.task.outcome === 'published-with-warning' || taskWarnings(row.node.task).length,
            }"
            :style="{ marginInlineStart: `${Math.min(row.node.depth, 4) * 1.15}rem` }"
            tabindex="-1"
          >
            <div class="task-center-heading">
              <div class="task-center-copy">
                <div class="task-center-title-line">
                  <button
                    v-if="row.hasChildren"
                    type="button"
                    class="task-center-expand"
                    :aria-expanded="expandedTaskIDs.has(row.node.task.id)"
                    :aria-label="
                      t(expandedTaskIDs.has(row.node.task.id) ? 'taskCenter.collapseTask' : 'taskCenter.expandTask')
                    "
                    @click="toggleExpanded(row.node.task.id)"
                  >
                    {{ expandedTaskIDs.has(row.node.task.id) ? "−" : "+" }}
                  </button>
                  <span v-else class="task-center-branch" aria-hidden="true"></span>
                  <strong>{{ taskTitle(row.node.task) }}</strong>
                </div>
                <span>
                  {{ kindLabel(row.node.task.kind) }} · {{ operationLabel(row.node.task.operation) }}
                  <template v-if="row.node.task.providerId">
                    · {{ row.node.task.providerId }} / {{ row.node.task.model }}</template
                  >
                </span>
                <span v-if="row.node.relationRole" class="task-center-relation">
                  {{ relationLabel(row.node.relationRole) }}
                </span>
                <code>{{ row.node.task.id }}</code>
              </div>
              <div class="task-center-status">
                <MChip :tone="taskChipTone(row.node.task.status)">{{ codeLabel(row.node.task.status) }}</MChip>
                <MChip
                  v-if="outcomeLabel(row.node.task)"
                  :tone="row.node.task.outcome === 'published-with-warning' ? 'warning' : 'success'"
                >
                  {{ outcomeLabel(row.node.task) }}
                </MChip>
                <time>{{
                  formatTime(row.node.task.completedAt ?? row.node.task.startedAt ?? row.node.task.createdAt)
                }}</time>
              </div>
            </div>
            <div class="task-center-progress-line">
              <div
                class="task-center-track"
                role="progressbar"
                :aria-label="taskTitle(row.node.task)"
                :aria-valuetext="`${progressLabel(row.node.task.progress.message || row.node.task.progress.phase || row.node.task.status)} ${progressPercent(row.node.task.progress.percent)}%`"
                :aria-valuenow="progressPercent(row.node.task.progress.percent)"
                aria-valuemin="0"
                aria-valuemax="100"
              >
                <span
                  :class="{ failed: row.node.task.status === 'failed' || row.node.task.status === 'needs-review' }"
                  :style="{ width: `${progressPercent(row.node.task.progress.percent)}%` }"
                />
              </div>
              <strong>{{ progressPercent(row.node.task.progress.percent) }}%</strong>
            </div>
            <div class="task-center-phase">
              <span>{{
                progressLabel(row.node.task.progress.message || row.node.task.progress.phase || row.node.task.status)
              }}</span>
              <span v-if="row.node.task.progress.total"
                >{{ row.node.task.progress.current }} / {{ row.node.task.progress.total }}</span
              >
            </div>
            <div v-if="row.node.task.dueAt" class="task-center-phase">
              <span>{{ t("taskCenter.dueAt", { time: formatTime(row.node.task.dueAt) }) }}</span>
            </div>
            <div
              v-if="row.node.task.targets?.length"
              class="task-center-targets"
              :aria-label="t('taskCenter.targetResults')"
            >
              <div v-for="target in row.node.task.targets" :key="target.locale" class="task-center-target">
                <MChip :tone="taskChipTone(target.status)">{{ target.locale }} · {{ codeLabel(target.status) }}</MChip>
                <span>{{ progressPercent(target.progress?.percent) }}%</span>
                <span v-if="target.attempts">{{ t("taskCenter.attempts", { count: target.attempts }) }}</span>
                <time v-if="target.completedAt">{{ formatTime(target.completedAt) }}</time>
                <span v-if="target.error" class="translation-target-error">
                  {{ taskErrorLabel(target.error, "tasksPage.errors.unknown") }}
                </span>
              </div>
            </div>
            <details v-if="row.node.task.report" class="task-center-report">
              <summary>{{ t("taskCenter.buildReport", { files: row.node.task.report.files }) }}</summary>
              <span>{{ t("taskCenter.buildLocales", { locales: row.node.task.report.locales.join(", ") }) }}</span>
              <span v-if="row.node.task.report.redirects.length">
                {{ t("taskCenter.buildRedirects", { count: row.node.task.report.redirects.length }) }}
              </span>
            </details>
            <div v-for="warning in taskWarnings(row.node.task)" :key="warning" class="task-center-warning">
              {{ warning }}
            </div>
            <div v-if="row.node.task.error" class="translation-task-error">
              {{ taskErrorLabel(row.node.task.error, "taskCenter.taskFailed") }}
            </div>
            <RouterLink
              v-if="taskDestination(row.node.task)"
              class="task-center-action"
              :to="taskDestination(row.node.task)!"
            >
              {{ taskActionLabel(row.node.task) }}
            </RouterLink>
          </article>
        </div>
        <div v-else class="empty-resource"><MEmptyState :title="t('taskCenter.empty')" /></div>
      </MSurface>
    </div>
  </div>
</template>

<style scoped>
.task-center-filter-bar {
  align-items: center;
}
.task-center-guidance {
  margin-right: auto;
  color: var(--m-sys-color-on-surface-variant);
  font-size: 0.74rem;
}
.task-center-summary {
  margin: 0.25rem 1rem 0.75rem;
  color: var(--m-sys-color-on-surface-variant);
  font-size: 0.72rem;
}
.task-center-sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}
.task-center-list {
  display: flex;
  flex-direction: column;
}
.task-center-row {
  display: grid;
  gap: 0.65rem;
  border-bottom: 1px solid #eceef2;
  padding: 1rem;
  overflow-wrap: anywhere;
}
.task-center-row:focus-visible {
  outline: 2px solid #4f46e5;
  outline-offset: -2px;
}
.task-center-row--focused {
  background: #f5f7ff;
  box-shadow: inset 3px 0 #4f46e5;
}
.task-center-row--warning {
  background: #fffbeb;
}
.task-center-row:last-child {
  border-bottom: 0;
}
.task-center-heading,
.task-center-progress-line,
.task-center-phase,
.task-center-target,
.task-center-report {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.8rem;
}
.task-center-copy {
  display: grid;
  min-width: 0;
  gap: 0.15rem;
}
.task-center-title-line {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 0.45rem;
}
.task-center-title-line strong {
  overflow-wrap: anywhere;
}
.task-center-expand,
.task-center-branch {
  display: inline-grid;
  flex: 0 0 1.45rem;
  width: 1.45rem;
  height: 1.45rem;
  place-items: center;
}
.task-center-expand {
  border: 1px solid #d7dce6;
  border-radius: 999px;
  background: #fff;
  color: #394150;
  font-size: 1rem;
  line-height: 1;
}
.task-center-expand:hover {
  border-color: #4f46e5;
  color: #4338ca;
}
.task-center-copy span,
.task-center-copy code,
.task-center-status time,
.task-center-phase,
.task-center-target,
.task-center-report {
  color: #818a9a;
  font-size: 0.68rem;
}
.task-center-copy code {
  white-space: normal;
}
.task-center-relation {
  color: #596579 !important;
}
.task-center-status {
  display: grid;
  flex: 0 0 auto;
  justify-items: end;
  gap: 0.3rem;
}
.task-center-track {
  overflow: hidden;
  width: 100%;
  height: 0.55rem;
  border-radius: 999px;
  background: #edf0f5;
}
.task-center-track span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: #4f46e5;
  transition: width 0.55s ease;
}
.task-center-track span.failed {
  background: #dc2626;
}
.task-center-progress-line > strong {
  width: 2.8rem;
  color: #293244;
  font-size: 0.72rem;
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.task-center-phase {
  text-transform: capitalize;
}
.task-center-targets {
  display: grid;
  gap: 0.35rem;
  border-left: 2px solid #e1e6f0;
  padding-left: 0.75rem;
}
.task-center-target {
  justify-content: flex-start;
  flex-wrap: wrap;
}
.task-center-report {
  align-items: flex-start;
  flex-wrap: wrap;
  border: 1px solid #e5e8ef;
  border-radius: 0.45rem;
  padding: 0.55rem 0.65rem;
}
.task-center-report summary {
  cursor: pointer;
  color: #394150;
}
.translation-target-error,
.translation-task-error {
  color: #b91c1c;
  font-size: 0.7rem;
}
.task-center-warning {
  color: #8a5b08;
  font-size: 0.72rem;
}
.task-center-action {
  justify-self: start;
  border-radius: 999px;
  background: #eef2ff;
  color: #4338ca;
  padding: 0.4rem 0.7rem;
  font-size: 0.72rem;
  font-weight: 600;
  text-decoration: none;
}
.task-center-action:hover {
  background: #e0e7ff;
}
@media (max-width: 720px) {
  .task-center-filter-bar {
    align-items: stretch;
  }
  .task-center-guidance {
    margin-right: 0;
  }
  .task-center-heading {
    align-items: flex-start;
    flex-direction: column;
  }
  .task-center-status {
    justify-items: start;
  }
  .task-center-row {
    margin-inline-start: 0 !important;
  }
}
</style>
