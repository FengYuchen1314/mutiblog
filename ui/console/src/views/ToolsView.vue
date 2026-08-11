<script setup lang="ts">
import { ref } from "vue";
import { useI18n } from "vue-i18n";
import { VButton, VCard, VEmpty, VPageHeader, VTag } from "@halo-dev/components";
import { api, type IndexStats, type StaticBuildReport } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import { useCodeLabel } from "@/i18n/useCodeLabel";

type SearchItem = { kind: "Post" | "Page"; id: string; locale: string; status: string; title: string; summary?: string };

const session = useSessionStore();
const { t } = useI18n();
const codeLabel = useCodeLabel();
const query = ref("");
const results = ref<SearchItem[]>([]);
const searched = ref(false);
const busy = ref<"index" | "site" | "search" | "">("");
const error = ref("");
const notice = ref("");

async function search() {
  if (!query.value.trim()) return;
  busy.value = "search"; error.value = "";
  try { results.value = (await api.searchIndex(query.value.trim())).items; searched.value = true; }
  catch (caught) { error.value = caught instanceof Error ? caught.message : t("toolsPage.searchFailed"); }
  finally { busy.value = ""; }
}

async function rebuildIndex() {
  if (!session.session) return;
  busy.value = "index"; error.value = ""; notice.value = "";
  try {
    const result: IndexStats = await api.rebuildIndex(session.session.csrfToken);
    notice.value = t("toolsPage.indexRebuilt", { count: result.documents });
  } catch (caught) { error.value = caught instanceof Error ? caught.message : t("toolsPage.indexFailed"); }
  finally { busy.value = ""; }
}

async function rebuildSite() {
  if (!session.session) return;
  busy.value = "site"; error.value = ""; notice.value = "";
  try {
    const result: { status: "succeeded"; report: StaticBuildReport } = await api.rebuildSite(session.session.csrfToken);
    notice.value = t("toolsPage.siteRebuilt", { count: result.report.files });
  } catch (caught) { error.value = caught instanceof Error ? caught.message : t("toolsPage.siteFailed"); }
  finally { busy.value = ""; }
}
</script>

<template>
  <div class="page">
    <VPageHeader :title="t('toolsPage.title')" />
    <div class="page-body settings-grid">
      <VCard><div class="card-title">{{ t("toolsPage.maintenance") }}</div><p>{{ t("toolsPage.maintenanceHelp") }}</p><div class="tool-actions">
        <VButton :loading="busy === 'index'" :disabled="Boolean(busy)" @click="rebuildIndex">{{ t("toolsPage.rebuildIndex") }}</VButton>
        <VButton type="secondary" :loading="busy === 'site'" :disabled="Boolean(busy)" @click="rebuildSite">{{ t("toolsPage.rebuildSite") }}</VButton>
      </div><div v-if="notice" class="form-success">{{ notice }}</div><div v-if="error" class="form-alert">{{ error }}</div></VCard>
      <VCard><div class="card-title">{{ t("toolsPage.indexSearch") }}</div><form class="filter-bar" @submit.prevent="search"><input v-model="query" :placeholder="t('toolsPage.searchPlaceholder')" /><button type="submit" :disabled="busy === 'search'">{{ busy === "search" ? t("common.loading") : t("search") }}</button></form>
        <div v-if="results.length" class="post-rows"><article v-for="item in results" :key="`${item.kind}:${item.id}:${item.locale}`" class="post-row"><div class="post-row-main"><strong>{{ item.title }}</strong><span>{{ codeLabel(item.kind) }} · {{ item.id }}</span></div><div class="post-row-tags"><VTag>{{ item.locale }}</VTag><VTag>{{ codeLabel(item.status) }}</VTag></div></article></div>
        <VEmpty v-else-if="searched" :title="t('toolsPage.noResults')" />
      </VCard>
    </div>
  </div>
</template>
