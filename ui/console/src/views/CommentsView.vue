<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { VButton, VCard, VEmpty, VPageHeader, VStatusDot, VTag } from "@halo-dev/components";
import { api, type CommentRecord } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import { useCodeLabel } from "@/i18n/useCodeLabel";

const session = useSessionStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const codeLabel = useCodeLabel();
const comments = ref<CommentRecord[]>([]);
const status = ref<"all" | CommentRecord["status"]>((route.query.status as "all" | CommentRecord["status"]) || "all");
const kind = ref((route.query.kind as string) || "all");
const query = ref((route.query.q as string) || "");
const page = ref(Math.max(1, Number(route.query.page) || 1));
const size = 20;
const total = ref(0);
const error = ref("");
const loading = ref(true);
const pages = computed(() => Math.max(1, Math.ceil(total.value / size)));
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const result = await api.comments({
      status: status.value,
      kind: kind.value,
      q: query.value.trim(),
      page: page.value,
      size,
    });
    comments.value = result.items;
    total.value = result.total;
    if (page.value > pages.value) {
      page.value = pages.value;
      return load();
    }
    await router.replace({
      query: {
        ...(status.value !== "all" ? { status: status.value } : {}),
        ...(kind.value !== "all" ? { kind: kind.value } : {}),
        ...(query.value.trim() ? { q: query.value.trim() } : {}),
        ...(page.value > 1 ? { page: String(page.value) } : {}),
      },
    });
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("commentsPage.loadFailed");
  } finally {
    loading.value = false;
  }
}
onMounted(load);
function applyFilters() {
  page.value = 1;
  void load();
}
function setStatus(value: string) {
  status.value = value as typeof status.value;
  applyFilters();
}
function changePage(next: number) {
  page.value = Math.min(pages.value, Math.max(1, next));
  void load();
}
async function moderate(comment: CommentRecord, status: CommentRecord["status"]) {
  if (!session.session) return;
  try {
    await api.moderateComment(session.session.csrfToken, comment, status);
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("commentsPage.moderateFailed");
  }
}
async function remove(comment: CommentRecord) {
  if (!session.session || !window.confirm(t("commentsPage.confirmDelete"))) return;
  try {
    await api.deleteComment(session.session.csrfToken, comment);
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("commentsPage.deleteFailed");
  }
}
</script>

<template>
  <div class="page">
    <VPageHeader :title="t('commentsPage.title')"
      ><template #actions
        ><VButton @click="load">{{ t("common.refresh") }}</VButton></template
      ></VPageHeader
    >
    <div class="page-body">
      <VCard>
        <div class="filter-bar">
          <input v-model="query" :placeholder="t('commentsPage.search')" @keyup.enter="applyFilters" /><select
            v-model="kind"
            @change="applyFilters"
          >
            <option value="all">{{ t("common.all") }}</option>
            <option value="post">{{ codeLabel("Post") }}</option>
            <option value="page">{{ codeLabel("Page") }}</option></select
          ><button
            v-for="value in ['all', 'pending', 'approved', 'spam']"
            :key="value"
            :class="{ active: status === value }"
            @click="setStatus(value)"
          >
            {{ codeLabel(value) }}
          </button>
        </div>
        <div v-if="error" class="form-alert resource-alert">{{ error }}</div>
        <div v-if="comments.length" class="comment-admin-list">
          <article v-for="comment in comments" :key="comment.id" class="comment-admin-item">
            <header>
              <strong>{{ comment.author.name }}</strong
              ><VTag>{{ comment.locale }}</VTag
              ><span
                ><VStatusDot
                  :state="comment.status === 'approved' ? 'success' : comment.status === 'spam' ? 'error' : 'warning'"
                />{{ codeLabel(comment.status) }}</span
              ><time>{{ new Date(comment.createdAt).toLocaleString() }}</time>
            </header>
            <p>{{ comment.content }}</p>
            <small>{{ codeLabel(comment.subject.kind) }} · {{ comment.subject.id }}</small>
            <footer>
              <VButton size="sm" @click="moderate(comment, 'approved')">{{ t("commentsPage.approve") }}</VButton
              ><VButton size="sm" @click="moderate(comment, 'pending')">{{ t("commentsPage.pending") }}</VButton
              ><VButton size="sm" @click="moderate(comment, 'spam')">{{ t("commentsPage.spam") }}</VButton
              ><button class="text-danger" @click="remove(comment)">{{ t("common.deleteForever") }}</button>
            </footer>
          </article>
        </div>
        <div v-else-if="loading" class="resource-loading">{{ t("commentsPage.loading") }}</div>
        <VEmpty v-else :title="t('commentsPage.empty')" />
        <nav v-if="total" class="pagination-bar" :aria-label="t('commentsPage.pagination')">
          <button :disabled="page <= 1" @click="changePage(page - 1)">{{ t("contentList.previous") }}</button
          ><span>{{ t("contentList.pageStatus", { page, pages, total }) }}</span
          ><button :disabled="page >= pages" @click="changePage(page + 1)">{{ t("contentList.next") }}</button>
        </nav>
      </VCard>
    </div>
  </div>
</template>
