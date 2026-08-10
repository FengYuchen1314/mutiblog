<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { VButton, VCard, VEmpty, VPageHeader } from "@halo-dev/components";

const route = useRoute();
const { t } = useI18n();
const title = computed(() => t(String(route.meta.title ?? "brand")));
</script>

<template>
  <div class="page">
    <VPageHeader :title="title"><template #actions><VButton type="secondary">新建</VButton></template></VPageHeader>
    <div class="page-body">
      <VCard>
        <div class="filter-bar"><input placeholder="输入关键词搜索" /><button>状态：全部</button><button>排序：默认</button><button>刷新</button></div>
        <div class="empty-resource"><VEmpty :title="`还没有${title}`" /></div>
      </VCard>
    </div>
  </div>
</template>
