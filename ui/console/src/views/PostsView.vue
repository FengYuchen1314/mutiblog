<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { Icon } from "@iconify/vue";
import { VButton, VCard, VEmpty, VPageHeader, VStatusDot, VTag } from "@halo-dev/components";
import { api, type Post } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import { useCodeLabel } from "@/i18n/useCodeLabel";

const session = useSessionStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const codeLabel = useCodeLabel();
const posts = ref<Post[]>([]);
const loading = ref(true);
const error = ref("");
const queryValue = (name: string, fallback: string) => typeof route.query[name] === "string" ? String(route.query[name]) : fallback;
const recycleMode = ref(queryValue("view", "") === "recycled");
const query = ref(queryValue("q", ""));
const statusFilter = ref(queryValue("status", "all"));
const localeFilter = ref(queryValue("locale", "all"));
const sortOrder = ref(queryValue("sort", "updated-desc"));
const page = ref(Math.max(1, Number.parseInt(queryValue("page", "1"), 10) || 1));
const pageSize = 20;
const selectedIDs = ref<string[]>([]);
const bulkBusy = ref(false);
const localeOptions = computed(() => [...new Set(posts.value.flatMap((post) => Object.keys(post.meta.locales)))].sort());
const visiblePosts = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase();
  const filtered = posts.value.filter((post) => {
    if (recycleMode.value ? post.meta.status !== "recycled" : post.meta.status === "recycled") return false;
    if (statusFilter.value !== "all" && post.meta.status !== statusFilter.value) return false;
    if (localeFilter.value !== "all" && !post.meta.locales[localeFilter.value]) return false;
    return !needle || post.meta.id.toLocaleLowerCase().includes(needle) || Object.values(post.content).some((content) => `${content.title}\n${content.summary ?? ""}`.toLocaleLowerCase().includes(needle));
  });
  return [...filtered].sort((left, right) => {
    if (sortOrder.value === "updated-asc") return left.meta.updatedAt.localeCompare(right.meta.updatedAt);
    if (sortOrder.value === "title") return sourceTitle(left).localeCompare(sourceTitle(right));
    return right.meta.updatedAt.localeCompare(left.meta.updatedAt);
  });
});
const pageCount = computed(() => Math.max(1, Math.ceil(visiblePosts.value.length / pageSize)));
const currentPage = computed(() => Math.min(page.value, pageCount.value));
const pagedPosts = computed(() => visiblePosts.value.slice((currentPage.value - 1) * pageSize, currentPage.value * pageSize));
const selectedPosts = computed(() => posts.value.filter((post) => selectedIDs.value.includes(post.meta.id)));
const selectedPublishable = computed(() => selectedPosts.value.filter((post) => post.meta.status === "draft" || post.meta.status === "unpublished"));
const selectedPublished = computed(() => selectedPosts.value.filter((post) => post.meta.status === "published"));
const pageAllSelected = computed(() => pagedPosts.value.length > 0 && pagedPosts.value.every((post) => selectedIDs.value.includes(post.meta.id)));

function syncQuery() {
  void router.replace({ query: {
    ...(query.value ? { q: query.value } : {}),
    ...(!recycleMode.value && statusFilter.value !== "all" ? { status: statusFilter.value } : {}),
    ...(localeFilter.value !== "all" ? { locale: localeFilter.value } : {}),
    ...(sortOrder.value !== "updated-desc" ? { sort: sortOrder.value } : {}),
    ...(recycleMode.value ? { view: "recycled" } : {}),
    ...(page.value > 1 ? { page: String(page.value) } : {}),
  } });
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
    posts.value = (await api.posts()).items;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("contentList.loadPostsFailed");
  } finally {
    loading.value = false;
  }
}

onMounted(load);

function sourceTitle(post: Post) {
  return post.content[post.meta.sourceLocale]?.title ?? post.meta.id;
}

function toggleRecycleMode() {
  recycleMode.value = !recycleMode.value;
}

function toggleSelected(id: string) {
  selectedIDs.value = selectedIDs.value.includes(id) ? selectedIDs.value.filter((value) => value !== id) : [...selectedIDs.value, id];
}

function togglePageSelection() {
  const ids = pagedPosts.value.map((post) => post.meta.id);
  selectedIDs.value = pageAllSelected.value
    ? selectedIDs.value.filter((id) => !ids.includes(id))
    : [...new Set([...selectedIDs.value, ...ids])];
}

async function transition(post: Post, action: "unpublish" | "recycle" | "restore") {
  if (!session.session) return;
  if (action === "recycle" && !window.confirm(t("contentList.confirmRecyclePost", { title: sourceTitle(post) }))) return;
  error.value = "";
  try { await api.changePostStatus(session.session.csrfToken, post.meta.id, action, post.meta.revision); await load(); }
  catch (caught) { error.value = caught instanceof Error ? caught.message : t("contentList.updatePostFailed"); }
}

async function removeForever(post: Post) {
  if (!session.session || !window.confirm(t("contentList.confirmDelete", { title: sourceTitle(post) }))) return;
  try { await api.deletePost(session.session.csrfToken, post.meta.id, post.meta.revision); await load(); }
  catch (caught) { error.value = caught instanceof Error ? caught.message : t("contentList.deleteFailed"); }
}

async function bulkAction(action: "publish" | "unpublish" | "recycle" | "restore" | "delete") {
  if (!session.session) return;
  const csrfToken = session.session.csrfToken;
  const targets = action === "publish" ? selectedPublishable.value : action === "unpublish" ? selectedPublished.value : selectedPosts.value;
  if (targets.length === 0) return;
  const count = targets.length;
  const labels = { publish: "contentList.bulkPublish", unpublish: "contentList.bulkUnpublish", recycle: "contentList.bulkRecycle", restore: "contentList.bulkRestore", delete: "contentList.bulkDelete" } as const;
  const actionLabel = t(labels[action]);
  if (!window.confirm(t(action === "delete" ? "contentList.confirmBulkDelete" : "contentList.confirmBulkAction", { count, action: actionLabel }))) return;
  bulkBusy.value = true;
  error.value = "";
  let failed = 0;
  for (const post of targets) {
    try {
      if (action === "delete") await api.deletePost(csrfToken, post.meta.id, post.meta.revision);
      else if (action === "publish") await api.publishPost(csrfToken, post.meta.id, post.meta.revision);
      else await api.changePostStatus(csrfToken, post.meta.id, action, post.meta.revision);
    } catch { failed += 1; }
  }
  selectedIDs.value = [];
  await load();
  if (failed) error.value = t("contentList.bulkFailed", { failed, count });
  bulkBusy.value = false;
}
</script>

<template>
  <div class="page">
    <VPageHeader :title="t('navigation.posts')">
      <template #actions>
        <VButton route="/categories">{{ t("contentList.categories") }}</VButton><VButton route="/tags">{{ t("contentList.tags") }}</VButton><VButton @click="toggleRecycleMode">{{ recycleMode ? t('contentList.returnPosts') : t('common.recycleBin') }}</VButton>
        <VButton type="secondary" route="/posts/editor">{{ t("common.new") }}</VButton>
      </template>
    </VPageHeader>
    <div class="page-body">
      <VCard>
        <div class="filter-bar"><input v-model="query" :placeholder="t('contentList.keyword')" /><select v-if="!recycleMode" v-model="statusFilter" :aria-label="t('common.status')"><option value="all">{{ t("common.status") }}：{{ t("common.all") }}</option><option v-for="value in ['draft','published','unpublished']" :key="value" :value="value">{{ codeLabel(value) }}</option></select><select v-model="localeFilter" :aria-label="t('common.language')"><option value="all">{{ t("common.language") }}：{{ t("common.all") }}</option><option v-for="locale in localeOptions" :key="locale" :value="locale">{{ locale }}</option></select><select v-model="sortOrder" :aria-label="t('common.sort')"><option value="updated-desc">{{ t("contentList.newest") }}</option><option value="updated-asc">{{ t("contentList.oldest") }}</option><option value="title">{{ t("contentList.byTitle") }}</option></select><button @click="load">{{ t("common.refresh") }}</button></div>
        <div v-if="visiblePosts.length" class="bulk-bar"><label><input type="checkbox" :checked="pageAllSelected" @change="togglePageSelection" />{{ t("contentList.selectPage") }}</label><span>{{ t("contentList.selected", { count: selectedIDs.length }) }}</span><template v-if="!recycleMode"><button :disabled="!selectedPublishable.length || bulkBusy" @click="bulkAction('publish')">{{ t("contentList.bulkPublish") }}</button><button :disabled="!selectedPublished.length || bulkBusy" @click="bulkAction('unpublish')">{{ t("contentList.bulkUnpublish") }}</button><button :disabled="!selectedIDs.length || bulkBusy" @click="bulkAction('recycle')">{{ t("contentList.bulkRecycle") }}</button></template><template v-else><button :disabled="!selectedIDs.length || bulkBusy" @click="bulkAction('restore')">{{ t("contentList.bulkRestore") }}</button><button class="text-danger" :disabled="!selectedIDs.length || bulkBusy" @click="bulkAction('delete')">{{ t("contentList.bulkDelete") }}</button></template></div>
        <div v-if="error" class="form-alert resource-alert">{{ error }}</div>
        <div v-if="loading" class="resource-loading">{{ t("contentList.loadingPosts") }}</div>
        <div v-else-if="visiblePosts.length" class="post-rows">
          <article v-for="post in pagedPosts" :key="post.meta.id" class="post-row">
            <input class="row-selector" type="checkbox" :checked="selectedIDs.includes(post.meta.id)" :aria-label="t('contentList.selectItem', { title: sourceTitle(post) })" @change="toggleSelected(post.meta.id)" />
            <RouterLink :to="`/posts/editor/${post.meta.id}`" class="post-row-link"><div class="post-row-icon"><Icon icon="ri:article-line" /></div>
            <div class="post-row-main">
              <strong>{{ sourceTitle(post) }}</strong>
              <span>{{ post.meta.id }}</span>
              <div class="post-row-tags">
                <VTag v-if="post.meta.hasUnpublishedChanges">{{ t("contentList.unpublishedChanges") }}</VTag>
                <VTag v-for="(locale, code) in post.meta.locales" :key="code">{{ code }} · {{ codeLabel(locale.state) }}</VTag>
              </div>
            </div>
            </RouterLink><div class="post-row-meta"><span><VStatusDot :state="post.meta.status === 'published' ? 'success' : 'default'" /> {{ codeLabel(post.meta.status) }}</span><time>{{ new Date(post.meta.updatedAt).toLocaleString() }}</time></div>
            <div class="post-row-actions"><button v-if="post.meta.status === 'published'" @click="transition(post, 'unpublish')">{{ t("common.unpublish") }}</button><button v-if="post.meta.status !== 'recycled'" @click="transition(post, 'recycle')">{{ t("common.recycle") }}</button><button v-if="post.meta.status === 'recycled'" @click="transition(post, 'restore')">{{ t("common.restore") }}</button><button v-if="post.meta.status === 'recycled'" class="text-danger" @click="removeForever(post)">{{ t("common.deleteForever") }}</button></div>
          </article>
        </div>
        <div v-else class="empty-resource"><VEmpty :title="t('contentList.noPosts')" /></div>
        <nav v-if="visiblePosts.length" class="pagination-bar" :aria-label="t('contentList.pagination')"><button :disabled="currentPage <= 1" @click="page = currentPage - 1">{{ t("contentList.previous") }}</button><span>{{ t("contentList.pageStatus", { page: currentPage, pages: pageCount, total: visiblePosts.length }) }}</span><button :disabled="currentPage >= pageCount" @click="page = currentPage + 1">{{ t("contentList.next") }}</button></nav>
      </VCard>
    </div>
  </div>
</template>
