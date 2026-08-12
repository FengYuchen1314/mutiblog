<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { api, type MediaAsset } from "@/api/client";
import { useSessionStore } from "@/stores/session";

withDefaults(
  defineProps<{
    modelValue: string;
    label?: string;
    help?: string;
    placeholder?: string;
  }>(),
  { label: "", help: "", placeholder: "/media/…" },
);

const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const session = useSessionStore();
const { t } = useI18n();
const open = ref(false);
const loading = ref(false);
const uploading = ref(false);
const error = ref("");
const query = ref("");
const assets = ref<MediaAsset[]>([]);
const fileInput = ref<HTMLInputElement>();

const visibleAssets = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase();
  return assets.value.filter(
    (asset) =>
      asset.mimeType.startsWith("image/") &&
      (!needle || `${asset.originalName}\n${asset.filename}`.toLocaleLowerCase().includes(needle)),
  );
});

async function load() {
  loading.value = true;
  error.value = "";
  try {
    assets.value = (await api.attachments()).items;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("mediaPicker.loadFailed");
  } finally {
    loading.value = false;
  }
}

function show() {
  open.value = true;
  void load();
}

function updateText(event: Event) {
  emit("update:modelValue", (event.target as HTMLInputElement).value);
}

function choose(asset: MediaAsset) {
  emit("update:modelValue", asset.url);
  open.value = false;
}

async function upload(event: Event) {
  const files = [...((event.target as HTMLInputElement).files ?? [])];
  if (!session.session || files.length === 0) return;
  uploading.value = true;
  error.value = "";
  try {
    let latest: MediaAsset | undefined;
    for (const file of files) latest = await api.uploadAttachment(session.session.csrfToken, file);
    await load();
    if (latest && files.length === 1) choose(latest);
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("mediaPicker.uploadFailed");
  } finally {
    uploading.value = false;
    if (fileInput.value) fileInput.value.value = "";
  }
}
</script>

<template>
  <div class="media-picker-field">
    <span v-if="label" class="media-picker-field__label">{{ label }}</span>
    <div class="media-picker-field__control">
      <img v-if="modelValue" :src="modelValue" alt="" />
      <input :value="modelValue" :placeholder="placeholder" @input="updateText" />
      <button type="button" @click="show">{{ t("mediaPicker.choose") }}</button>
      <button v-if="modelValue" type="button" @click="emit('update:modelValue', '')">
        {{ t("mediaPicker.clear") }}
      </button>
    </div>
    <small v-if="help">{{ help }}</small>

    <Teleport to="body">
      <div
        v-if="open"
        class="media-picker-backdrop"
        tabindex="-1"
        @click.self="open = false"
        @keydown.esc="open = false"
      >
        <section class="media-picker-dialog" role="dialog" aria-modal="true" :aria-label="t('mediaPicker.title')">
          <header>
            <div>
              <strong>{{ t("mediaPicker.title") }}</strong
              ><span>{{ t("mediaPicker.help") }}</span>
            </div>
            <button type="button" :aria-label="t('mediaPicker.close')" @click="open = false">×</button>
          </header>
          <div class="media-picker-toolbar">
            <input v-model="query" type="search" :placeholder="t('mediaPicker.search')" />
            <input
              ref="fileInput"
              hidden
              type="file"
              accept="image/jpeg,image/png,image/gif,image/webp"
              multiple
              @change="upload"
            />
            <button type="button" :disabled="uploading" @click="fileInput?.click()">
              {{ uploading ? t("mediaPicker.uploading") : t("mediaPicker.upload") }}
            </button>
            <button type="button" :disabled="loading" @click="load">{{ t("common.refresh") }}</button>
          </div>
          <div v-if="error" class="form-alert">{{ error }}</div>
          <div v-if="loading" class="media-picker-empty">{{ t("mediaPicker.loading") }}</div>
          <div v-else-if="visibleAssets.length" class="media-picker-grid">
            <button
              v-for="asset in visibleAssets"
              :key="asset.id"
              type="button"
              :class="{ selected: asset.url === modelValue }"
              @click="choose(asset)"
            >
              <img :src="asset.url" :alt="asset.originalName" />
              <span :title="asset.originalName">{{ asset.originalName }}</span>
            </button>
          </div>
          <div v-else class="media-picker-empty">{{ t("mediaPicker.empty") }}</div>
        </section>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.media-picker-field {
  display: grid;
  min-width: 0;
  gap: 0.35rem;
}
.media-picker-field__label {
  color: var(--m-sys-color-on-surface-variant);
  font-size: var(--m-sys-typescale-label-large-size);
  font-weight: 650;
}
.media-picker-field__control {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.45rem;
}
.media-picker-field__control > img {
  width: 2.35rem;
  height: 2.35rem;
  flex: 0 0 auto;
  border: 1px solid var(--m-sys-color-outline-variant);
  border-radius: var(--m-sys-shape-corner-small);
  object-fit: cover;
}
.media-picker-field__control > input {
  min-width: 0;
  flex: 1;
}
.media-picker-field__control > input,
.media-picker-toolbar > input {
  min-height: 2.5rem;
  border: 1px solid var(--m-sys-color-outline);
  border-radius: var(--m-sys-shape-corner-small);
  background: var(--m-sys-color-surface-container-lowest);
  box-shadow: inset 0 1px 0 rgb(25 28 32 / 0.03);
  color: var(--m-sys-color-on-surface);
  padding: 0.55rem 0.7rem;
}
.media-picker-field__control > input:focus-visible,
.media-picker-toolbar > input:focus-visible {
  border-color: var(--m-sys-color-primary);
  outline: none;
  box-shadow: var(--m-sys-focus-ring);
}
.media-picker-field__control > button,
.media-picker-toolbar button {
  flex: 0 0 auto;
  min-height: 2.5rem;
  border: 0;
  border-radius: var(--m-sys-shape-corner-full);
  background: var(--m-sys-color-secondary-container);
  color: var(--m-sys-color-on-secondary-container);
  cursor: pointer;
  font-family: var(--m-sys-font-family);
  font-size: var(--m-sys-typescale-label-large-size);
  font-weight: 650;
  padding: 0.5rem 0.8rem;
  transition:
    background-color var(--m-sys-motion-duration-short4) var(--m-sys-motion-easing-standard),
    transform var(--m-sys-motion-duration-short4) var(--m-sys-motion-easing-standard);
}
.media-picker-field__control > button:hover:not(:disabled),
.media-picker-toolbar button:hover:not(:disabled) {
  background: var(--m-sys-color-primary-container);
  color: var(--m-sys-color-on-primary-container);
}
.media-picker-field__control > button:active:not(:disabled),
.media-picker-toolbar button:active:not(:disabled) {
  transform: scale(0.98);
}
.media-picker-field__control > button:disabled,
.media-picker-toolbar button:disabled {
  background: color-mix(in srgb, var(--m-sys-color-on-surface) 12%, transparent);
  color: color-mix(in srgb, var(--m-sys-color-on-surface) 38%, transparent);
  cursor: not-allowed;
}
.media-picker-field small {
  color: var(--m-sys-color-on-surface-variant);
  font-size: var(--m-sys-typescale-label-medium-size);
}
.media-picker-backdrop {
  position: fixed;
  z-index: 150;
  inset: 0;
  display: grid;
  align-items: start;
  justify-items: center;
  overflow: auto;
  background: color-mix(in srgb, var(--m-sys-color-scrim) 48%, transparent);
  padding: clamp(1rem, 6vh, 4rem) 1rem;
}
.media-picker-dialog {
  display: grid;
  width: min(64rem, 100%);
  max-height: calc(100vh - 2rem);
  overflow: hidden;
  border: 1px solid var(--m-sys-color-outline-variant);
  border-radius: var(--m-sys-shape-corner-extra-large);
  background: var(--m-sys-color-surface-container-high);
  box-shadow: var(--m-sys-elevation-level4);
  padding: 1rem;
  gap: 0.85rem;
}
.media-picker-dialog > header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}
.media-picker-dialog > header > div {
  display: grid;
  gap: 0.15rem;
}
.media-picker-dialog > header span {
  color: var(--m-sys-color-on-surface-variant);
  font-size: var(--m-sys-typescale-label-medium-size);
}
.media-picker-dialog > header > button {
  display: inline-grid;
  width: 2.5rem;
  height: 2.5rem;
  place-items: center;
  border: 0;
  border-radius: var(--m-sys-shape-corner-full);
  background: transparent;
  color: var(--m-sys-color-primary);
  cursor: pointer;
  font-size: 1.5rem;
}
.media-picker-dialog > header > button:hover {
  background: color-mix(in srgb, var(--m-sys-color-primary) 10%, transparent);
}
.media-picker-toolbar {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.media-picker-toolbar > input {
  min-width: 8rem;
  flex: 1;
}
.media-picker-grid {
  display: grid;
  overflow: auto;
  grid-template-columns: repeat(auto-fill, minmax(8.5rem, 1fr));
  gap: 0.7rem;
  padding: 0.15rem;
}
.media-picker-grid > button {
  display: grid;
  overflow: hidden;
  border: 2px solid transparent;
  border-radius: var(--m-sys-shape-corner-medium);
  background: var(--m-sys-color-surface-container-low);
  color: var(--m-sys-color-on-surface);
  cursor: pointer;
  padding: 0;
  text-align: left;
}
.media-picker-grid > button.selected {
  border-color: var(--m-sys-color-primary);
  background: var(--m-sys-color-primary-container);
}
.media-picker-grid > button:hover:not(.selected) {
  background: var(--m-sys-color-surface-container-highest);
}
.media-picker-grid img {
  width: 100%;
  aspect-ratio: 4 / 3;
  object-fit: cover;
}
.media-picker-grid span {
  overflow: hidden;
  padding: 0.45rem 0.55rem;
  font-size: 0.7rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.media-picker-empty {
  padding: 3rem 1rem;
  color: var(--m-sys-color-on-surface-variant);
  text-align: center;
}
@media (max-width: 640px) {
  .media-picker-field__control,
  .media-picker-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .media-picker-field__control > img {
    width: 100%;
    height: 8rem;
  }
}
</style>
