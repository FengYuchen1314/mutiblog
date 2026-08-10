<script setup lang="ts">
import { reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { VButton } from "@halo-dev/components";
import { ApiError, api } from "@/api/client";
import { markSetupComplete } from "@/router";

const router = useRouter();
const submitting = ref(false);
const error = ref("");
const fields = ref<Record<string, string>>({});
const form = reactive({
  siteTitle: "MutiBlog",
  sourceLocale: "zh-CN",
  adminLocale: "zh-CN",
  timezone: "Asia/Shanghai",
  username: "admin",
  password: "",
});

async function submit() {
  submitting.value = true;
  error.value = "";
  fields.value = {};
  try {
    await api.setup({ ...form });
    markSetupComplete();
    await router.push({ name: "login" });
  } catch (caught) {
    if (caught instanceof ApiError) {
      error.value = caught.message;
      fields.value = caught.fields ?? {};
    } else {
      error.value = "无法连接 MutiBlog 服务。";
    }
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="auth-page">
    <div class="auth-panel setup-panel">
      <div class="auth-brand"><span class="brand-mark">M</span><strong>MutiBlog</strong></div>
      <div class="auth-heading">
        <h1>创建你的站点</h1>
        <p>源语言首次确认后固定；后台语言之后可随时切换。</p>
      </div>
      <form class="form-grid" @submit.prevent="submit">
        <label class="field field--wide">
          <span>站点名称</span>
          <input v-model="form.siteTitle" autocomplete="organization" />
          <small v-if="fields.siteTitle" class="field-error">{{ fields.siteTitle }}</small>
        </label>
        <label class="field">
          <span>源语言（BCP 47）</span>
          <input v-model="form.sourceLocale" placeholder="zh-CN" />
          <small v-if="fields.sourceLocale" class="field-error">{{ fields.sourceLocale }}</small>
        </label>
        <label class="field">
          <span>后台语言</span>
          <select v-model="form.adminLocale">
            <option value="zh-CN">简体中文</option>
            <option value="en">English</option>
          </select>
          <small v-if="fields.adminLocale" class="field-error">{{ fields.adminLocale }}</small>
        </label>
        <label class="field field--wide">
          <span>时区</span>
          <input v-model="form.timezone" placeholder="Asia/Shanghai" />
          <small v-if="fields.timezone" class="field-error">{{ fields.timezone }}</small>
        </label>
        <div class="section-divider field--wide"><span>唯一管理员</span></div>
        <label class="field">
          <span>用户名</span>
          <input v-model="form.username" autocomplete="username" />
          <small v-if="fields.username" class="field-error">{{ fields.username }}</small>
        </label>
        <label class="field">
          <span>密码</span>
          <input v-model="form.password" type="password" autocomplete="new-password" />
          <small v-if="fields.password" class="field-error">{{ fields.password }}</small>
        </label>
        <div v-if="error" class="form-alert field--wide">{{ error }}</div>
        <VButton class="field--wide" type="secondary" block :loading="submitting" @click="submit">
          初始化 MutiBlog
        </VButton>
      </form>
    </div>
  </div>
</template>
