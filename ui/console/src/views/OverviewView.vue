<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { api, type IndexStats, type SecurityAuditEvent } from "@/api/client";
import { useCodeLabel } from "@/i18n/useCodeLabel";

type SystemStatus = { initialized: boolean; version: string; dataDir: string; index: IndexStats; publisher: string };

const { t } = useI18n();
const codeLabel = useCodeLabel();
const status = ref<SystemStatus>();
const audit = ref<SecurityAuditEvent[]>([]);
const error = ref("");

async function load() {
  error.value = "";
  try {
    const [system, events] = await Promise.all([api.systemStatus(), api.securityAudit()]);
    status.value = system;
    audit.value = events.items;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("overviewPage.loadFailed");
  }
}

onMounted(load);
</script>

<template>
  <div class="page">
    <MPageHeader :title="t('overviewPage.title')"
      ><template #actions
        ><MButton @click="load">{{ t("common.refresh") }}</MButton></template
      ></MPageHeader
    >
    <div class="page-body">
      <div v-if="error" class="form-alert">{{ error }}</div>
      <div v-if="status" class="settings-grid">
        <MSurface
          ><div class="card-title">{{ t("overviewPage.runtime") }}</div>
          <div class="status-list">
            <div>
              <span>{{ t("overviewPage.version") }}</span
              ><strong>{{ status.version }}</strong>
            </div>
            <div>
              <span>{{ t("overviewPage.publisher") }}</span
              ><strong>{{ codeLabel(status.publisher) }}</strong>
            </div>
            <div>
              <span>{{ t("overviewPage.dataDirectory") }}</span
              ><code>{{ status.dataDir }}</code>
            </div>
          </div></MSurface
        >
        <MSurface
          ><div class="card-title">{{ t("overviewPage.index") }}</div>
          <div class="status-list">
            <div>
              <span>{{ t("overviewPage.status") }}</span
              ><strong>{{ codeLabel(status.index.status) }}</strong>
            </div>
            <div>
              <span>{{ t("overviewPage.documents") }}</span
              ><strong>{{ status.index.documents }}</strong>
            </div>
            <div>
              <span>{{ t("overviewPage.lastIndexed") }}</span
              ><time>{{
                status.index.lastIndexedAt ? new Date(status.index.lastIndexedAt).toLocaleString() : "—"
              }}</time>
            </div>
            <div v-if="status.index.lastError">
              <span>{{ t("overviewPage.lastError") }}</span
              ><code>{{ status.index.lastError }}</code>
            </div>
          </div></MSurface
        >
        <MSurface class="audit-card"
          ><div class="card-title">{{ t("overviewPage.securityAudit") }}</div>
          <div v-if="audit.length" class="post-rows">
            <article v-for="event in audit" :key="`${event.time}:${event.action}:${event.outcome}`" class="post-row">
              <div class="post-row-main">
                <strong>{{ codeLabel(event.action) }} · {{ codeLabel(event.outcome) }}</strong
                ><span>{{ event.actor || "—" }} · {{ t("overviewPage.client") }} {{ event.clientHash || "—" }}</span>
              </div>
              <time>{{ new Date(event.time).toLocaleString() }}</time>
            </article>
          </div>
          <p v-else>{{ t("overviewPage.noAuditEvents") }}</p></MSurface
        >
      </div>
      <div v-else-if="!error" class="resource-loading">{{ t("common.loading") }}</div>
    </div>
  </div>
</template>
