<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { VButton, VCard, VEmpty, VPageHeader } from "@halo-dev/components";
import { api, type MediaAsset } from "@/api/client";
import { useSessionStore } from "@/stores/session";

const session = useSessionStore();
const { t } = useI18n();
const assets = ref<MediaAsset[]>([]);
const loading = ref(true);
const uploading = ref(false);
const deleting = ref("");
const copied = ref("");
const error = ref("");
const picker = ref<HTMLInputElement>();
const query = ref("");

type MediaTypeFilter = "all" | MediaAsset["mimeType"];
type MediaSort = "newest" | "oldest" | "name" | "size-desc" | "size-asc";

const mediaType = ref<MediaTypeFilter>("all");
const sort = ref<MediaSort>("newest");
const visibleAssets = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase();
  const filtered = assets.value.filter((asset) => {
    const matchesQuery = !needle || `${asset.originalName}\n${asset.filename}\n${asset.mimeType}`.toLocaleLowerCase().includes(needle);
    return matchesQuery && (mediaType.value === "all" || asset.mimeType === mediaType.value);
  });
  return [...filtered].sort((left, right) => {
    switch (sort.value) {
      case "oldest": return Date.parse(left.createdAt) - Date.parse(right.createdAt);
      case "name": return left.originalName.localeCompare(right.originalName);
      case "size-desc": return right.size - left.size;
      case "size-asc": return left.size - right.size;
      default: return Date.parse(right.createdAt) - Date.parse(left.createdAt);
    }
  });
});

async function refresh() {
  loading.value = true;
  try {
    assets.value = (await api.attachments()).items;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("attachmentsPage.loadFailed");
  } finally {
    loading.value = false;
  }
}

async function upload(event: Event) {
  const files = [...((event.target as HTMLInputElement).files ?? [])];
  if (!session.session || files.length === 0) return;
  uploading.value = true;
  error.value = "";
  try {
    for (const file of files) await api.uploadAttachment(session.session.csrfToken, file);
    await refresh();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("attachmentsPage.uploadFailed");
  } finally {
    uploading.value = false;
    if (picker.value) picker.value.value = "";
  }
}

async function remove(asset: MediaAsset) {
  if (!session.session || !window.confirm(t("attachmentsPage.confirmDelete", { name: asset.originalName }))) return;
  deleting.value = asset.id;
  error.value = "";
  try {
    await api.deleteAttachment(session.session.csrfToken, asset.id);
    assets.value = assets.value.filter((candidate) => candidate.id !== asset.id);
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("attachmentsPage.deleteFailed");
  } finally {
    deleting.value = "";
  }
}

async function copyURL(asset: MediaAsset) {
  try {
    await navigator.clipboard.writeText(new URL(asset.url, window.location.origin).toString());
    copied.value = asset.id;
    window.setTimeout(() => { if (copied.value === asset.id) copied.value = ""; }, 1600);
  } catch {
    error.value = t("attachmentsPage.copyFailed");
  }
}

function formatSize(size: number) {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KiB`;
  return `${(size / 1024 / 1024).toFixed(1)} MiB`;
}

onMounted(refresh);
</script>

<template>
  <div class="page">
    <VPageHeader :title="t('attachmentsPage.title')">
      <template #actions>
        <input ref="picker" hidden type="file" accept="image/jpeg,image/png,image/gif,image/webp" multiple @change="upload" />
        <VButton type="secondary" :loading="uploading" @click="picker?.click()">{{ t("attachmentsPage.upload") }}</VButton>
      </template>
    </VPageHeader>
    <div class="page-body">
      <div v-if="error" class="form-alert">{{ error }}</div>
      <VCard>
        <div class="filter-bar">
          <input v-model="query" :placeholder="t('attachmentsPage.filenameSearch')" />
          <select v-model="mediaType" :aria-label="t('attachmentsPage.type')">
            <option value="all">{{ t("attachmentsPage.allTypes") }}</option>
            <option value="image/jpeg">JPEG</option><option value="image/png">PNG</option><option value="image/gif">GIF</option><option value="image/webp">WebP</option>
          </select>
          <select v-model="sort" :aria-label="t('attachmentsPage.sort')">
            <option value="newest">{{ t("attachmentsPage.newest") }}</option><option value="oldest">{{ t("attachmentsPage.oldest") }}</option>
            <option value="name">{{ t("attachmentsPage.nameSort") }}</option><option value="size-desc">{{ t("attachmentsPage.largest") }}</option><option value="size-asc">{{ t("attachmentsPage.smallest") }}</option>
          </select>
          <button type="button" @click="refresh">{{ t("common.refresh") }}</button>
        </div>
        <div v-if="loading" class="empty-resource">{{ t("attachmentsPage.loading") }}</div>
        <div v-else-if="visibleAssets.length" class="attachment-grid">
          <article v-for="asset in visibleAssets" :key="asset.id" class="attachment-card">
            <a :href="asset.url" target="_blank" rel="noopener"><img :src="asset.url" :alt="asset.originalName" /></a>
            <div><strong :title="asset.originalName">{{ asset.originalName }}</strong><span>{{ formatSize(asset.size) }} · {{ asset.mimeType }}</span><button type="button" @click="copyURL(asset)">{{ copied === asset.id ? t("attachmentsPage.copied") : t("attachmentsPage.copyUrl") }}</button><button class="text-danger" type="button" :disabled="deleting === asset.id" @click="remove(asset)">{{ t("common.delete") }}</button></div>
          </article>
        </div>
        <div v-else class="empty-resource"><VEmpty :title="t(query.trim() || mediaType !== 'all' ? 'attachmentsPage.noMatches' : 'attachmentsPage.empty')" /></div>
      </VCard>
    </div>
  </div>
</template>
