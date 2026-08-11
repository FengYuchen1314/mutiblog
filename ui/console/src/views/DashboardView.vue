<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Icon } from "@iconify/vue";
import { VButton, VCard, VPageHeader } from "@halo-dev/components";
import { api } from "@/api/client";

const totals = ref<{ posts: number | null; pages: number | null; comments: number | null }>({
  posts: null,
  pages: null,
  comments: null,
});
const { t } = useI18n();
onMounted(async () => {
  const [posts, pages, comments] = await Promise.allSettled([api.posts(), api.pages(), api.comments()]);
  if (posts.status === "fulfilled") totals.value.posts = posts.value.total;
  if (pages.status === "fulfilled") totals.value.pages = pages.value.total;
  if (comments.status === "fulfilled") totals.value.comments = comments.value.total;
});

const quickActions = computed(
  () =>
    [
      [t("dashboard.createPost"), "/posts/editor", "ri:quill-pen-line"],
      [t("dashboard.createPage"), "/pages/editor", "ri:file-add-line"],
      [t("dashboard.uploadAttachment"), "/attachments", "ri:upload-cloud-2-line"],
      [t("dashboard.manageThemes"), "/themes", "ri:palette-line"],
    ] as const,
);
const displayTotal = (value: number | null) => (value === null ? "—" : value.toLocaleString());
const metrics = computed(() => [
  [t("dashboard.posts"), displayTotal(totals.value.posts), "ri:article-line"],
  [t("dashboard.pages"), displayTotal(totals.value.pages), "ri:file-list-line"],
  [t("dashboard.comments"), displayTotal(totals.value.comments), "ri:chat-3-line"],
  [t("dashboard.views"), "—", "ri:line-chart-line"],
]);
</script>

<template>
  <div class="page">
    <VPageHeader :title="t('dashboard.title')"
      ><template #actions
        ><RouterLink to="/settings"
          ><VButton>{{ t("dashboard.settings") }}</VButton></RouterLink
        ></template
      ></VPageHeader
    >
    <div class="page-body">
      <div class="metric-grid">
        <VCard v-for="metric in metrics" :key="metric[0]">
          <div class="metric">
            <span class="metric-icon"><Icon :icon="metric[2]" /></span>
            <div>
              <small>{{ metric[0] }}</small
              ><strong>{{ metric[1] }}</strong>
            </div>
          </div>
        </VCard>
      </div>
      <VCard class="dashboard-quick-access">
        <div class="card-title">{{ t("dashboard.quickAccess") }}</div>
        <div class="quick-grid">
          <RouterLink v-for="action in quickActions" :key="action[1]" :to="action[1]" class="quick-action">
            <Icon :icon="action[2]" /><span>{{ action[0] }}</span>
          </RouterLink>
        </div>
      </VCard>
    </div>
  </div>
</template>
