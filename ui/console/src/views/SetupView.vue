<script setup lang="ts">
import { reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { VButton } from "@halo-dev/components";
import { ApiError, api, createStaticBuildTaskId } from "@/api/client";
import { markSetupComplete } from "@/router";
import { useSessionStore } from "@/stores/session";

const router = useRouter();
const session = useSessionStore();
const { t } = useI18n();
const submitting = ref(false);
const error = ref("");
const fields = ref<Record<string, string>>({});
const form = reactive({
  siteTitle: "MutiBlog",
  baseUrl: window.location.origin,
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
    const result = await api.setup({ ...form }, createStaticBuildTaskId());
    session.establish(result.session);
    markSetupComplete();
    if (result.build.status === "failed" && !result.build.taskId) {
      await router.push({ name: "tools", query: { setupBuildFailed: "1" } });
      return;
    }
    await router.push({ name: "tasks", query: result.build.taskId ? { focus: result.build.taskId } : undefined });
  } catch (caught) {
    if (caught instanceof ApiError) {
      error.value = caught.message;
      fields.value = Object.fromEntries(
        Object.keys(caught.fields ?? {}).map((field) => [field, t("setupPage.invalidField")]),
      );
    } else {
      error.value = t("setupPage.unavailable");
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
        <h1>{{ t("setupPage.title") }}</h1>
        <p>{{ t("setupPage.subtitle") }}</p>
      </div>
      <form class="form-grid" @submit.prevent="submit">
        <label class="field field--wide">
          <span>{{ t("setupPage.siteName") }}</span>
          <input v-model="form.siteTitle" autocomplete="organization" />
          <small v-if="fields.siteTitle" class="field-error">{{ fields.siteTitle }}</small>
        </label>
        <label class="field field--wide">
          <span>{{ t("setupPage.publicBaseUrl") }}</span>
          <input v-model="form.baseUrl" type="url" autocomplete="url" placeholder="https://blog.example.com" />
          <small>{{ t("setupPage.publicBaseUrlHelp") }}</small>
          <small v-if="fields.baseUrl" class="field-error">{{ fields.baseUrl }}</small>
        </label>
        <label class="field">
          <span>{{ t("setupPage.sourceLocale") }}</span>
          <input v-model="form.sourceLocale" placeholder="zh-CN" />
          <small v-if="fields.sourceLocale" class="field-error">{{ fields.sourceLocale }}</small>
        </label>
        <label class="field">
          <span>{{ t("setupPage.consoleLocale") }}</span>
          <select v-model="form.adminLocale">
            <option value="zh-CN">简体中文</option>
            <option value="en">English</option>
          </select>
          <small v-if="fields.adminLocale" class="field-error">{{ fields.adminLocale }}</small>
        </label>
        <label class="field field--wide">
          <span>{{ t("setupPage.timezone") }}</span>
          <input v-model="form.timezone" placeholder="Asia/Shanghai" />
          <small v-if="fields.timezone" class="field-error">{{ fields.timezone }}</small>
        </label>
        <div class="section-divider field--wide">
          <span>{{ t("setupPage.administrator") }}</span>
        </div>
        <label class="field">
          <span>{{ t("setupPage.username") }}</span>
          <input v-model="form.username" autocomplete="username" />
          <small v-if="fields.username" class="field-error">{{ fields.username }}</small>
        </label>
        <label class="field">
          <span>{{ t("setupPage.password") }}</span>
          <input v-model="form.password" type="password" autocomplete="new-password" />
          <small v-if="fields.password" class="field-error">{{ fields.password }}</small>
        </label>
        <div v-if="error" class="form-alert field--wide">{{ error }}</div>
        <VButton class="field--wide" type="secondary" block :loading="submitting" @click="submit">
          {{ t("setupPage.submit") }}
        </VButton>
      </form>
    </div>
  </div>
</template>
