<script setup lang="ts">
import { reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { VButton } from "@halo-dev/components";
import { ApiError } from "@/api/client";
import { useSessionStore } from "@/stores/session";

const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const form = reactive({ username: "", password: "" });
const submitting = ref(false);
const error = ref("");

async function submit() {
  submitting.value = true;
  error.value = "";
  try {
    await session.login(form.username, form.password);
    const redirect = typeof route.query.redirect === "string" ? route.query.redirect : "/dashboard";
    await router.push(redirect);
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : "无法连接 MutiBlog 服务。";
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="auth-page">
    <div class="auth-panel login-panel">
      <div class="auth-brand"><span class="brand-mark">M</span><strong>MutiBlog</strong></div>
      <div class="auth-heading"><h1>登录</h1><p>进入你的单站点控制台</p></div>
      <form class="form-stack" @submit.prevent="submit">
        <label class="field"><span>用户名</span><input v-model="form.username" autofocus autocomplete="username" /></label>
        <label class="field"><span>密码</span><input v-model="form.password" type="password" autocomplete="current-password" /></label>
        <div v-if="error" class="form-alert">{{ error }}</div>
        <VButton type="secondary" block :loading="submitting" @click="submit">登录</VButton>
      </form>
    </div>
  </div>
</template>
