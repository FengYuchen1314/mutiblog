<script setup lang="ts">
import { reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { VButton } from "@halo-dev/components";
import { ApiError } from "@/api/client";
import { useSessionStore } from "@/stores/session";

const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const { t } = useI18n();
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
    error.value = caught instanceof ApiError ? caught.message : t("login.unavailable");
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="auth-page">
    <div class="auth-panel login-panel">
      <div class="auth-brand"><span class="brand-mark">M</span><strong>MutiBlog</strong></div>
      <div class="auth-heading"><h1>{{ t("login.title") }}</h1><p>{{ t("login.subtitle") }}</p></div>
      <div v-if="route.query.restored === '1'" class="form-success">{{ t("login.restored") }}</div>
      <div v-if="route.query.passwordChanged === '1'" class="form-success">{{ t("login.passwordChanged") }}</div>
      <div v-if="route.query.setupBuildFailed === '1'" class="form-alert">{{ t("login.setupBuildFailed") }}</div>
      <form class="form-stack" @submit.prevent="submit">
        <label class="field"><span>{{ t("login.username") }}</span><input v-model="form.username" autofocus autocomplete="username" /></label>
        <label class="field"><span>{{ t("login.password") }}</span><input v-model="form.password" type="password" autocomplete="current-password" /></label>
        <div v-if="error" class="form-alert">{{ error }}</div>
        <VButton type="secondary" block :loading="submitting" @click="submit">{{ t("login.submit") }}</VButton>
      </form>
    </div>
  </div>
</template>
