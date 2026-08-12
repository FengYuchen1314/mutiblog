<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { Icon } from "@iconify/vue";
import { api, type Post, type UnifiedTask } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import { useCodeLabel } from "@/i18n/useCodeLabel";
import { useBuildTasks } from "@/composables/useBuildTasks";
import TaskProgress from "@/components/TaskProgress.vue";

const session = useSessionStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const codeLabel = useCodeLabel();
const pages = ref<Post[]>([]);
const loading = ref(true);
const error = ref("");
const queryValue = (name: string, fallback: string) =>
  typeof route.query[name] === "string" ? String(route.query[name]) : fallback;
const recycleMode = ref(queryValue("view", "") === "recycled");
const query = ref(queryValue("q", ""));
const statusFilter = ref(queryValue("status", "all"));
const localeFilter = ref(queryValue("locale", "all"));
const sortOrder = ref(queryValue("sort", "updated-desc"));
const page = ref(Math.max(1, Number.parseInt(queryValue("page", "1"), 10) || 1));
const pageSize = 20;
const selectedIDs = ref<string[]>([]);
const bulkBusy = ref(false);
const {
  taskIds: buildTaskIds,
  taskLabels: taskSubjects,
  createTask,
  trackTaskChildren,
  discardIfMissing,
  beginOperation,
  reconcile,
} = useBuildTasks();
const bulkCurrent = ref(0);
const bulkTotal = ref(0);
const localeOptions = computed(() =>
  [...new Set(pages.value.flatMap((page) => Object.keys(page.meta.locales)))].sort(),
);
const visiblePages = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase();
  const filtered = pages.value.filter((page) => {
    if (recycleMode.value ? page.meta.status !== "recycled" : page.meta.status === "recycled") return false;
    if (statusFilter.value !== "all" && page.meta.status !== statusFilter.value) return false;
    if (localeFilter.value !== "all" && !page.meta.locales[localeFilter.value]) return false;
    return (
      !needle ||
      page.meta.id.toLocaleLowerCase().includes(needle) ||
      Object.values(page.content).some((content) =>
        `${content.title}\n${content.summary ?? ""}`.toLocaleLowerCase().includes(needle),
      )
    );
  });
  return [...filtered].sort((left, right) => {
    if (sortOrder.value === "updated-asc") return left.meta.updatedAt.localeCompare(right.meta.updatedAt);
    if (sortOrder.value === "title") return sourceTitle(left).localeCompare(sourceTitle(right));
    return right.meta.updatedAt.localeCompare(left.meta.updatedAt);
  });
});
const pageCount = computed(() => Math.max(1, Math.ceil(visiblePages.value.length / pageSize)));
const currentPage = computed(() => Math.min(page.value, pageCount.value));
const pagedPages = computed(() =>
  visiblePages.value.slice((currentPage.value - 1) * pageSize, currentPage.value * pageSize),
);
const selectedPages = computed(() => pages.value.filter((pageItem) => selectedIDs.value.includes(pageItem.meta.id)));
const selectedPublishable = computed(() =>
  selectedPages.value.filter((pageItem) => pageItem.meta.status === "draft" || pageItem.meta.status === "unpublished"),
);
const selectedPublished = computed(() =>
  selectedPages.value.filter((pageItem) => pageItem.meta.status === "published"),
);
const pageAllSelected = computed(
  () =>
    pagedPages.value.length > 0 && pagedPages.value.every((pageItem) => selectedIDs.value.includes(pageItem.meta.id)),
);

function syncQuery() {
  void router.replace({
    query: {
      ...(query.value ? { q: query.value } : {}),
      ...(!recycleMode.value && statusFilter.value !== "all" ? { status: statusFilter.value } : {}),
      ...(localeFilter.value !== "all" ? { locale: localeFilter.value } : {}),
      ...(sortOrder.value !== "updated-desc" ? { sort: sortOrder.value } : {}),
      ...(recycleMode.value ? { view: "recycled" } : {}),
      ...(page.value > 1 ? { page: String(page.value) } : {}),
    },
  });
}

watch([query, statusFilter, localeFilter, sortOrder, recycleMode], () => {
  page.value = 1;
  selectedIDs.value = [];
  syncQuery();
});
watch(page, syncQuery);

async function load() {
  loading.value = true;
  error.value = "";
  try {
    pages.value = (await api.pages()).items;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("contentList.loadPagesFailed");
  } finally {
    loading.value = false;
  }
}

onMounted(load);
const sourceTitle = (page: Post) => page.content[page.meta.sourceLocale]?.title ?? page.meta.id;

function toggleRecycleMode() {
  recycleMode.value = !recycleMode.value;
}

function toggleSelected(id: string) {
  selectedIDs.value = selectedIDs.value.includes(id)
    ? selectedIDs.value.filter((value) => value !== id)
    : [...selectedIDs.value, id];
}

function togglePageSelection() {
  const ids = pagedPages.value.map((pageItem) => pageItem.meta.id);
  selectedIDs.value = pageAllSelected.value
    ? selectedIDs.value.filter((id) => !ids.includes(id))
    : [...new Set([...selectedIDs.value, ...ids])];
}

async function beginTrackedOperation() {
  await beginOperation();
}

function startBuildTask(pageItem: Post, bulk = false) {
  const taskId = createTask(`${sourceTitle(pageItem)} · ${pageItem.meta.id}`);
  if (!bulk) {
    bulkCurrent.value = 0;
    bulkTotal.value = 0;
  }
  return taskId;
}

async function reconcileBuildTask(taskId: string, ...actualIds: Array<string | undefined>) {
  await reconcile(taskId, ...actualIds);
}

async function discardBuildTaskIfMissing(taskId: string) {
  await discardIfMissing(taskId);
}

function handleTaskUpdate(task: UnifiedTask) {
  trackTaskChildren(task);
}

async function transition(page: Post, action: "unpublish" | "recycle" | "restore") {
  if (!session.session) return;
  if (action === "recycle" && !window.confirm(t("contentList.confirmRecyclePage", { title: sourceTitle(page) })))
    return;
  error.value = "";
  await beginTrackedOperation();
  const taskId = startBuildTask(page);
  try {
    const result = await api.changePageStatus(
      session.session.csrfToken,
      page.meta.id,
      action,
      page.meta.revision,
      taskId,
    );
    await reconcileBuildTask(taskId);
    await load();
    if (result.build.status === "failed") error.value = t("common.publicationFailed");
  } catch (caught) {
    await discardBuildTaskIfMissing(taskId);
    error.value = caught instanceof Error ? caught.message : t("contentList.updatePageFailed");
  }
}

async function removeForever(page: Post) {
  if (!session.session || !window.confirm(t("contentList.confirmDelete", { title: sourceTitle(page) }))) return;
  await beginTrackedOperation();
  const taskId = startBuildTask(page);
  try {
    const result = await api.deletePage(session.session.csrfToken, page.meta.id, page.meta.revision, taskId);
    await reconcileBuildTask(taskId);
    await load();
    if (result.build.status === "failed") error.value = t("common.publicationFailed");
  } catch (caught) {
    await discardBuildTaskIfMissing(taskId);
    error.value = caught instanceof Error ? caught.message : t("contentList.deleteFailed");
  }
}

async function bulkAction(action: "publish" | "unpublish" | "recycle" | "restore" | "delete") {
  if (!session.session) return;
  const csrfToken = session.session.csrfToken;
  const targets =
    action === "publish"
      ? selectedPublishable.value
      : action === "unpublish"
        ? selectedPublished.value
        : selectedPages.value;
  if (targets.length === 0) return;
  const count = targets.length;
  const labels = {
    publish: "contentList.bulkPublish",
    unpublish: "contentList.bulkUnpublish",
    recycle: "contentList.bulkRecycle",
    restore: "contentList.bulkRestore",
    delete: "contentList.bulkDelete",
  } as const;
  const actionLabel = t(labels[action]);
  if (
    !window.confirm(
      t(action === "delete" ? "contentList.confirmBulkDelete" : "contentList.confirmBulkAction", {
        count,
        action: actionLabel,
      }),
    )
  )
    return;
  bulkBusy.value = true;
  error.value = "";
  await beginTrackedOperation();
  bulkCurrent.value = 0;
  bulkTotal.value = count;
  let failed = 0;
  for (const [index, pageItem] of targets.entries()) {
    bulkCurrent.value = index + 1;
    const taskId = startBuildTask(pageItem, true);
    try {
      if (action === "delete") {
        const result = await api.deletePage(csrfToken, pageItem.meta.id, pageItem.meta.revision, taskId);
        await reconcileBuildTask(taskId);
        if (result.build.status === "failed") failed += 1;
      } else if (action === "publish") {
        const result = await api.publishPage(csrfToken, pageItem.meta.id, pageItem.meta.revision, taskId);
        await reconcileBuildTask(taskId, result.build.taskId, result.translation.taskId);
        if (
          result.build.status === "failed" ||
          result.build.status === "blocked" ||
          result.translation.status === "failed" ||
          result.translation.status === "not-configured"
        )
          failed += 1;
      } else {
        const result = await api.changePageStatus(csrfToken, pageItem.meta.id, action, pageItem.meta.revision, taskId);
        await reconcileBuildTask(taskId);
        if (result.build.status === "failed") failed += 1;
      }
    } catch {
      await discardBuildTaskIfMissing(taskId);
      failed += 1;
    }
  }
  selectedIDs.value = [];
  await load();
  if (failed) error.value = t("contentList.bulkFailed", { failed, count });
  bulkBusy.value = false;
}
</script>

<template>
  <div class="page">
    <MPageHeader :title="t('navigation.pages')"
      ><template #actions
        ><MButton @click="toggleRecycleMode">{{
          recycleMode ? t("contentList.returnPages") : t("common.recycleBin")
        }}</MButton
        ><MButton variant="tonal" to="/pages/editor">{{ t("common.new") }}</MButton></template
      ></MPageHeader
    >
    <div class="page-body">
      <MSurface>
        <div class="filter-bar">
          <input v-model="query" :placeholder="t('contentList.keyword')" /><select
            v-if="!recycleMode"
            v-model="statusFilter"
            :aria-label="t('common.status')"
          >
            <option value="all">{{ t("common.status") }}：{{ t("common.all") }}</option>
            <option v-for="value in ['draft', 'published', 'unpublished']" :key="value" :value="value">
              {{ codeLabel(value) }}
            </option></select
          ><select v-model="localeFilter" :aria-label="t('common.language')">
            <option value="all">{{ t("common.language") }}：{{ t("common.all") }}</option>
            <option v-for="locale in localeOptions" :key="locale" :value="locale">{{ locale }}</option></select
          ><select v-model="sortOrder" :aria-label="t('common.sort')">
            <option value="updated-desc">{{ t("contentList.newest") }}</option>
            <option value="updated-asc">{{ t("contentList.oldest") }}</option>
            <option value="title">{{ t("contentList.byTitle") }}</option></select
          ><button @click="load">{{ t("common.refresh") }}</button>
        </div>
        <div v-if="visiblePages.length" class="bulk-bar">
          <label
            ><input type="checkbox" :checked="pageAllSelected" @change="togglePageSelection" />{{
              t("contentList.selectPage")
            }}</label
          ><span>{{ t("contentList.selected", { count: selectedIDs.length }) }}</span
          ><template v-if="!recycleMode"
            ><button :disabled="!selectedPublishable.length || bulkBusy" @click="bulkAction('publish')">
              {{ t("contentList.bulkPublish") }}</button
            ><button :disabled="!selectedPublished.length || bulkBusy" @click="bulkAction('unpublish')">
              {{ t("contentList.bulkUnpublish") }}</button
            ><button :disabled="!selectedIDs.length || bulkBusy" @click="bulkAction('recycle')">
              {{ t("contentList.bulkRecycle") }}
            </button></template
          ><template v-else
            ><button :disabled="!selectedIDs.length || bulkBusy" @click="bulkAction('restore')">
              {{ t("contentList.bulkRestore") }}</button
            ><button class="text-danger" :disabled="!selectedIDs.length || bulkBusy" @click="bulkAction('delete')">
              {{ t("contentList.bulkDelete") }}
            </button></template
          >
        </div>
        <div v-if="buildTaskIds.length || bulkTotal">
          <span v-if="bulkTotal">{{ bulkCurrent }} / {{ bulkTotal }}</span>
          <div v-for="taskId in buildTaskIds" :key="taskId">
            <strong>{{ taskSubjects[taskId] }}</strong
            ><TaskProgress :task-id="taskId" @update="handleTaskUpdate" />
          </div>
        </div>
        <div v-if="error" class="form-alert resource-alert">{{ error }}</div>
        <div v-if="loading" class="resource-loading">{{ t("contentList.loadingPages") }}</div>
        <div v-else-if="visiblePages.length" class="post-rows">
          <article v-for="page in pagedPages" :key="page.meta.id" class="post-row">
            <input
              class="row-selector"
              type="checkbox"
              :checked="selectedIDs.includes(page.meta.id)"
              :aria-label="t('contentList.selectItem', { title: sourceTitle(page) })"
              @change="toggleSelected(page.meta.id)"
            />
            <RouterLink :to="`/pages/editor/${page.meta.id}`" class="post-row-link"
              ><div class="post-row-icon"><Icon icon="ri:file-text-line" /></div>
              <div class="post-row-main">
                <strong>{{ sourceTitle(page) }}</strong
                ><span>{{ page.meta.id }}</span>
                <div class="post-row-tags">
                  <MChip v-if="page.meta.hasUnpublishedChanges">{{ t("contentList.unpublishedChanges") }}</MChip
                  ><MChip v-for="(locale, code) in page.meta.locales" :key="code"
                    >{{ code }} · {{ codeLabel(locale.state) }}</MChip
                  >
                </div>
              </div></RouterLink
            >
            <div class="post-row-meta">
              <span
                ><MStatus :tone="page.meta.status === 'published' ? 'success' : 'neutral'" />
                {{ codeLabel(page.meta.status) }}</span
              ><time>{{ new Date(page.meta.updatedAt).toLocaleString() }}</time>
            </div>
            <div class="post-row-actions">
              <button v-if="page.meta.status === 'published'" @click="transition(page, 'unpublish')">
                {{ t("common.unpublish") }}</button
              ><button v-if="page.meta.status !== 'recycled'" @click="transition(page, 'recycle')">
                {{ t("common.recycle") }}</button
              ><button v-if="page.meta.status === 'recycled'" @click="transition(page, 'restore')">
                {{ t("common.restore") }}</button
              ><button v-if="page.meta.status === 'recycled'" class="text-danger" @click="removeForever(page)">
                {{ t("common.deleteForever") }}
              </button>
            </div>
          </article>
        </div>
        <div v-else class="empty-resource"><MEmptyState :title="t('contentList.noPages')" /></div>
        <nav v-if="visiblePages.length" class="pagination-bar" :aria-label="t('contentList.pagination')">
          <button :disabled="currentPage <= 1" @click="page = currentPage - 1">{{ t("contentList.previous") }}</button
          ><span>{{
            t("contentList.pageStatus", { page: currentPage, pages: pageCount, total: visiblePages.length })
          }}</span
          ><button :disabled="currentPage >= pageCount" @click="page = currentPage + 1">
            {{ t("contentList.next") }}
          </button>
        </nav>
      </MSurface>
    </div>
  </div>
</template>
