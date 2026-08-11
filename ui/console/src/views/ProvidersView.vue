<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { VButton, VCard, VEmpty, VPageHeader, VTag } from "@halo-dev/components";
import { api, type AIProvider, type AIProviderInput } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import { nextQwenProviderId, preferredProvider, qwenProviderTemplate } from "./providerDefaults";

const session = useSessionStore();
const { t } = useI18n();
const providers = ref<AIProvider[]>([]);
const selected = ref("");
const saving = ref(false);
const testing = ref(false);
const message = ref("");
const error = ref("");
const form = reactive({
  id: "qwen-free",
  ...qwenProviderTemplate,
  apiKey: "",
  default: true,
  clearKey: false,
});

async function load(preferredId = "") {
  try {
    providers.value = (await api.providers()).items;
    const provider = providers.value.find((item) => item.id === preferredId) ?? preferredProvider(providers.value);
    if (provider) edit(provider);
    else resetForm();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("providersPage.loadFailed");
  }
}

function resetForm() {
  selected.value = "";
  Object.assign(form, { id: nextQwenProviderId(providers.value), ...qwenProviderTemplate, apiKey: "", default: providers.value.length === 0, clearKey: false });
}

function edit(provider: AIProvider) {
  selected.value = provider.id;
  Object.assign(form, { ...provider, apiKey: "", clearKey: false });
  message.value = "";
  error.value = "";
}

async function save() {
  if (!session.session) return;
  saving.value = true;
  message.value = "";
  error.value = "";
  try {
    const body: Omit<AIProviderInput, "id"> = {
      name: form.name,
      kind: form.kind,
      baseUrl: form.baseUrl,
      model: form.model,
      enabled: form.enabled,
      default: form.default,
      timeoutSeconds: Number(form.timeoutSeconds),
      maxOutputTokens: Number(form.maxOutputTokens),
    };
    if (form.apiKey) body.apiKey = form.apiKey;
    if (form.clearKey) body.apiKey = "";
    const provider = await api.saveProvider(session.session.csrfToken, form.id, body);
    form.apiKey = "";
    form.clearKey = false;
    await load(provider.id);
    message.value = t("providersPage.saved");
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("providersPage.saveFailed");
  } finally {
    saving.value = false;
  }
}

async function testConnection() {
  if (!session.session || !selected.value) return;
  testing.value = true;
  message.value = "";
  error.value = "";
  try {
    const result = await api.testProvider(session.session.csrfToken, selected.value);
    message.value = t("providersPage.testSuccess", { model: result.model, latency: result.latencyMs, response: result.response });
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("providersPage.testFailed");
  } finally {
    testing.value = false;
  }
}

async function remove() {
  if (!session.session || !selected.value || !window.confirm(t("providersPage.confirmDelete", { id: selected.value }))) return;
  await api.deleteProvider(session.session.csrfToken, selected.value);
  await load();
  resetForm();
  message.value = t("providersPage.deleted");
}

onMounted(load);
</script>

<template>
  <div class="page">
    <VPageHeader :title="t('providersPage.title')"><template #actions><VButton type="secondary" @click="resetForm">{{ t("providersPage.new") }}</VButton></template></VPageHeader>
    <div class="page-body provider-layout">
      <VCard class="provider-list-card">
        <div class="settings-section-title"><div><strong>{{ t("providersPage.provider") }}</strong><span>{{ t("providersPage.guidance") }}</span></div></div>
        <div v-if="providers.length" class="provider-list">
          <button v-for="provider in providers" :key="provider.id" type="button" :class="{ active: selected === provider.id }" @click="edit(provider)">
            <div><strong>{{ provider.name }}</strong><span>{{ provider.model }}</span></div>
            <VTag v-if="provider.default">{{ t("providersPage.default") }}</VTag><VTag v-if="provider.hasKey">{{ provider.maskedKey }}</VTag>
          </button>
        </div>
        <div v-else class="provider-empty"><VEmpty :title="t('providersPage.empty')" /></div>
      </VCard>
      <VCard>
        <div class="settings-section-title"><div><strong>{{ selected ? t('providersPage.edit') : t('providersPage.new') }}</strong><span>{{ t("providersPage.keyHelp") }}</span></div></div>
        <div v-if="message" class="form-success provider-message">{{ message }}</div>
        <div v-if="error" class="form-alert provider-message">{{ error }}</div>
        <div class="provider-form">
          <label><span>{{ t("providersPage.stableId") }}</span><input v-model="form.id" :disabled="Boolean(selected)" placeholder="qwen-free" /></label>
          <label><span>{{ t("providersPage.displayName") }}</span><input v-model="form.name" placeholder="Qwen Free (OpenRouter)" /></label>
          <label class="field--wide"><span>{{ t("providersPage.baseUrl") }}</span><input v-model="form.baseUrl" placeholder="https://openrouter.ai/api/v1" /></label>
          <label><span>{{ t("providersPage.model") }}</span><input v-model="form.model" placeholder="qwen/qwen3-32b:free" /></label>
          <label><span>{{ t("providersPage.apiKey") }}</span><input v-model="form.apiKey" type="password" autocomplete="new-password" :placeholder="selected ? t('providersPage.keyKeep') : t('providersPage.keyEnter')" /></label>
          <label><span>{{ t("providersPage.timeout") }}</span><input v-model.number="form.timeoutSeconds" type="number" min="5" max="300" /></label>
          <label><span>{{ t("providersPage.maxTokens") }}</span><input v-model.number="form.maxOutputTokens" type="number" min="256" max="65536" /></label>
          <label class="provider-check"><input v-model="form.enabled" type="checkbox" />{{ t("providersPage.enabled") }}</label>
          <label class="provider-check"><input v-model="form.default" type="checkbox" />{{ t("providersPage.setDefault") }}</label>
          <label v-if="selected" class="provider-check text-danger"><input v-model="form.clearKey" type="checkbox" />{{ t("providersPage.clearKey") }}</label>
        </div>
        <div class="provider-actions">
          <VButton :loading="saving" @click="save">{{ t("providersPage.save") }}</VButton>
          <VButton :disabled="!selected" :loading="testing" @click="testConnection">{{ t("providersPage.test") }}</VButton>
          <button v-if="selected" class="text-danger" type="button" @click="remove">{{ t("providersPage.delete") }}</button>
        </div>
      </VCard>
    </div>
  </div>
</template>
