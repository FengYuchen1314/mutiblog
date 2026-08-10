<script setup lang="ts">
import { onMounted, ref } from "vue";
import { Icon } from "@iconify/vue";
import { VButton, VCard, VPageHeader, VStatusDot } from "@halo-dev/components";
import { api } from "@/api/client";

const status = ref<{ version: string; dataDir: string; index: string; publisher: string } | null>(null);
onMounted(async () => {
  status.value = await api.systemStatus();
});

const quickActions = [
  ["创建文章", "/posts", "ri:quill-pen-line"],
  ["创建页面", "/pages", "ri:file-add-line"],
  ["附件上传", "/attachments", "ri:upload-cloud-2-line"],
  ["主题管理", "/theme", "ri:palette-line"],
] as const;
</script>

<template>
  <div class="page">
    <VPageHeader title="仪表盘"><template #actions><VButton>设置</VButton></template></VPageHeader>
    <div class="page-body">
      <div class="metric-grid">
        <VCard v-for="metric in [['文章', '0', 'ri:article-line'], ['页面', '0', 'ri:file-list-line'], ['评论', '0', 'ri:chat-3-line'], ['浏览量', '0', 'ri:line-chart-line']]" :key="metric[0]">
          <div class="metric"><span class="metric-icon"><Icon :icon="metric[2]" /></span><div><small>{{ metric[0] }}</small><strong>{{ metric[1] }}</strong></div></div>
        </VCard>
      </div>
      <div class="dashboard-grid">
        <VCard>
          <div class="card-title">快捷访问</div>
          <div class="quick-grid">
            <RouterLink v-for="action in quickActions" :key="action[1]" :to="action[1]" class="quick-action">
              <Icon :icon="action[2]" /><span>{{ action[0] }}</span>
            </RouterLink>
          </div>
        </VCard>
        <VCard>
          <div class="card-title">系统状态</div>
          <div v-if="status" class="status-list">
            <div><span><VStatusDot state="success" /> 服务</span><strong>正常</strong></div>
            <div><span>索引</span><strong>{{ status.index }}</strong></div>
            <div><span>发布器</span><strong>{{ status.publisher }}</strong></div>
            <div><span>版本</span><strong>{{ status.version }}</strong></div>
          </div>
          <div v-else class="skeleton">正在读取状态…</div>
        </VCard>
      </div>
    </div>
  </div>
</template>
