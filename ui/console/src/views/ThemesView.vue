<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { VButton, VCard, VEmpty, VPageHeader, VTag } from "@halo-dev/components";
import { useRoute, useRouter } from "vue-router";
import { api, type ThemeRecord, type ThemeSettings, type ThemeSettingsSchema } from "@/api/client";
import TaskProgress from "@/components/TaskProgress.vue";
import ThemeSettingsField from "@/components/ThemeSettingsField.vue";
import { useBuildTasks } from "@/composables/useBuildTasks";
import { useSessionStore } from "@/stores/session";

const session = useSessionStore();
const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const themes = ref<ThemeRecord[]>([]);
const busy = ref("");
const error = ref("");
const notice = ref("");
const picker = ref<HTMLInputElement>();
const settingsPicker = ref<HTMLInputElement>();
const selectedSettings = ref<ThemeSettings>();
const remoteURL = ref("");
const {
  taskIds: installTaskIds,
  createTask: createInstallTask,
  discardIfMissing: discardInstallTaskIfMissing,
  beginOperation: beginInstallOperation,
  reconcile: reconcileInstallTask,
} = useBuildTasks();
const {
  taskIds: themeTaskIds,
  createTask: createThemeTask,
  discardIfMissing: discardThemeTaskIfMissing,
  beginOperation: beginThemeOperation,
  reconcile: reconcileThemeTask,
} = useBuildTasks();
const {
  taskIds: settingsTaskIds,
  createTask: createSettingsTask,
  discardIfMissing: discardSettingsTaskIfMissing,
  beginOperation: beginSettingsOperation,
  reconcile: reconcileSettingsTask,
} = useBuildTasks();
const activeTheme = computed(() => themes.value.find((theme) => theme.active));
const installedThemes = computed(() => themes.value.filter((theme) => !theme.active));
const managingThemes = computed(() => route.name === "themes");

async function load() {
  try {
    themes.value = (await api.themes()).items;
  } catch (caught) {
    error.value = message(caught, t("themesPage.loadFailed"));
  }
}

async function install(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file || !session.session) return;
  const csrfToken = session.session.csrfToken;
  busy.value = "install";
  error.value = "";
  notice.value = "";
  await beginInstallOperation();
  const taskId = createInstallTask();
  try {
    const result = await api.installTheme(csrfToken, file, taskId);
    await reconcileInstallTask(taskId);
    notice.value = t(result.theme.active ? "themesPage.upgraded" : "themesPage.installed", { name: result.theme.name });
    await load();
  } catch (caught) {
    await discardInstallTaskIfMissing(taskId);
    error.value = message(caught, t("themesPage.installFailed"));
  } finally {
    busy.value = "";
    if (picker.value) picker.value.value = "";
  }
}

async function installFromURL() {
  if (!session.session || !remoteURL.value.trim()) return;
  const csrfToken = session.session.csrfToken;
  const url = remoteURL.value.trim();
  busy.value = "install-url";
  error.value = "";
  notice.value = "";
  await beginInstallOperation();
  const taskId = createInstallTask();
  try {
    const result = await api.installThemeURL(csrfToken, url, taskId);
    await reconcileInstallTask(taskId);
    notice.value = t(result.theme.active ? "themesPage.upgraded" : "themesPage.installed", { name: result.theme.name });
    remoteURL.value = "";
    await load();
  } catch (caught) {
    await discardInstallTaskIfMissing(taskId);
    error.value = message(caught, t("themesPage.installFailed"));
  } finally {
    busy.value = "";
  }
}

async function activate(theme: ThemeRecord) {
  if (!session.session || theme.active) return;
  const csrfToken = session.session.csrfToken;
  busy.value = theme.id;
  error.value = "";
  notice.value = "";
  await beginThemeOperation();
  const taskId = createThemeTask();
  try {
    await api.activateTheme(csrfToken, theme.id, taskId);
    await reconcileThemeTask(taskId);
    notice.value = t("themesPage.activated", { name: theme.name });
    await load();
  } catch (caught) {
    await discardThemeTaskIfMissing(taskId);
    error.value = message(caught, t("themesPage.activateFailed"));
  } finally {
    busy.value = "";
  }
}

async function reload(theme: ThemeRecord) {
  if (!session.session) return;
  const csrfToken = session.session.csrfToken;
  busy.value = `reload-${theme.id}`;
  error.value = "";
  notice.value = "";
  await beginThemeOperation();
  const taskId = createThemeTask();
  try {
    await api.reloadTheme(csrfToken, theme.id, taskId);
    await reconcileThemeTask(taskId);
    notice.value = t(theme.active ? "themesPage.reloadedPublished" : "themesPage.reloaded", { name: theme.name });
    await load();
  } catch (caught) {
    await discardThemeTaskIfMissing(taskId);
    error.value = message(caught, t("themesPage.reloadFailed"));
  } finally {
    busy.value = "";
  }
}

async function preview(theme: ThemeRecord) {
  if (!session.session || theme.status !== "ready") return;
  const previewWindow = window.open("about:blank", "_blank");
  if (previewWindow) previewWindow.opener = null;
  busy.value = `preview-${theme.id}`;
  error.value = "";
  try {
    const result = await api.previewTheme(session.session.csrfToken, theme.id);
    if (previewWindow) previewWindow.location.replace(result.url);
    else window.open(result.url, "_blank", "noopener,noreferrer");
    notice.value = t("themePreview.created", { time: new Date(result.expiresAt).toLocaleTimeString() });
  } catch (caught) {
    previewWindow?.close();
    error.value = message(caught, t("themePreview.failed"));
  } finally {
    busy.value = "";
  }
}

async function uninstall(theme: ThemeRecord, deleteSettings = false) {
  const confirmation = deleteSettings ? "themesPage.confirmUninstallSettings" : "themesPage.confirmUninstall";
  if (!session.session || theme.active || theme.builtIn || !window.confirm(t(confirmation, { name: theme.name })))
    return;
  busy.value = theme.id;
  error.value = "";
  try {
    await api.uninstallTheme(session.session.csrfToken, theme.id, deleteSettings);
    await load();
  } catch (caught) {
    error.value = message(caught, t("themesPage.uninstallFailed"));
  } finally {
    busy.value = "";
  }
}

async function openSettings(theme: ThemeRecord) {
  busy.value = `settings-${theme.id}`;
  error.value = "";
  try {
    selectedSettings.value = await api.themeSettings(theme.id);
  } catch (caught) {
    error.value = message(caught, t("themesPage.settingsLoadFailed"));
  } finally {
    busy.value = "";
  }
}

async function saveSettings() {
  if (!session.session || !selectedSettings.value) return;
  const csrfToken = session.session.csrfToken;
  const settings = selectedSettings.value;
  const values = structuredClone(settings.values);
  busy.value = "settings-save";
  error.value = "";
  notice.value = "";
  await beginSettingsOperation();
  const taskId = createSettingsTask();
  try {
    const result = await api.saveThemeSettings(csrfToken, settings.themeId, values, taskId);
    await reconcileSettingsTask(taskId);
    selectedSettings.value = result.settings;
    notice.value = t(result.settings.active ? "themesPage.settingsPublished" : "themesPage.settingsSaved");
  } catch (caught) {
    await discardSettingsTaskIfMissing(taskId);
    error.value = message(caught, t("themesPage.settingsSaveFailed"));
  } finally {
    busy.value = "";
  }
}

async function resetSettings() {
  if (!session.session || !selectedSettings.value || !window.confirm(t("themesPage.confirmReset"))) return;
  const csrfToken = session.session.csrfToken;
  const themeId = selectedSettings.value.themeId;
  busy.value = "settings-reset";
  error.value = "";
  await beginSettingsOperation();
  const taskId = createSettingsTask();
  try {
    const result = await api.resetThemeSettings(csrfToken, themeId, taskId);
    await reconcileSettingsTask(taskId);
    selectedSettings.value = result.settings;
    notice.value = t(result.settings.active ? "themesPage.resetPublished" : "themesPage.resetDone");
  } catch (caught) {
    await discardSettingsTaskIfMissing(taskId);
    error.value = message(caught, t("themesPage.settingsResetFailed"));
  } finally {
    busy.value = "";
  }
}

function exportSettings() {
  if (!selectedSettings.value) return;
  const data = JSON.stringify(
    { schemaVersion: 1, themeId: selectedSettings.value.themeId, values: selectedSettings.value.values },
    null,
    2,
  );
  const url = URL.createObjectURL(new Blob([`${data}\n`], { type: "application/json" }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `mutiblog-theme-${selectedSettings.value.themeId}-settings.json`;
  anchor.click();
  URL.revokeObjectURL(url);
}

async function importSettings(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file || !selectedSettings.value) return;
  error.value = "";
  try {
    const parsed: unknown = JSON.parse(await file.text());
    if (!parsed || Array.isArray(parsed) || typeof parsed !== "object")
      throw new Error(t("themesPage.settingsImportInvalid"));
    const wrapper = parsed as { schemaVersion?: unknown; themeId?: unknown; values?: unknown };
    const exported =
      wrapper.schemaVersion === 1 && typeof wrapper.themeId === "string" && Object.hasOwn(wrapper, "values");
    if (exported && wrapper.themeId !== selectedSettings.value.themeId)
      throw new Error(t("themesPage.settingsImportWrongTheme"));
    const values = exported ? wrapper.values : parsed;
    if (!values || Array.isArray(values) || typeof values !== "object")
      throw new Error(t("themesPage.settingsImportInvalid"));
    selectedSettings.value.values = structuredClone(values as Record<string, unknown>);
    notice.value = t("themesPage.settingsImported");
  } catch (caught) {
    error.value = message(caught, t("themesPage.settingsImportInvalid"));
  } finally {
    if (settingsPicker.value) settingsPicker.value.value = "";
  }
}

function schemaEntries(schema: ThemeSettingsSchema) {
  return Object.entries(schema.properties ?? {});
}

function schemaTitle(schema: ThemeSettingsSchema, fallback: string) {
  return schema["x-i18n"]?.[locale.value] || schema.title || fallback;
}

function groupValue(groupKey: string) {
  return selectedSettings.value?.values[groupKey];
}

function setGroupValue(groupKey: string, value: unknown) {
  if (!selectedSettings.value) return;
  selectedSettings.value.values[groupKey] = value;
}

function message(caught: unknown, fallback: string) {
  return caught instanceof Error ? caught.message : fallback;
}

onMounted(load);
</script>

<template>
  <div class="page">
    <VPageHeader :title="t(managingThemes ? 'themesPage.themeManagement' : 'themesPage.currentTheme')">
      <template #actions>
        <input ref="picker" class="visually-hidden" type="file" accept=".zip,application/zip" @change="install" />
        <VButton v-if="managingThemes" type="secondary" :loading="busy === 'install'" @click="picker?.click()">{{
          t("themesPage.install")
        }}</VButton>
        <VButton v-else type="secondary" @click="router.push({ name: 'themes' })">{{
          t("dashboard.manageThemes")
        }}</VButton>
      </template>
    </VPageHeader>
    <div class="page-body">
      <div v-if="managingThemes" class="form-success theme-guidance">{{ t("themesPage.guidance") }}</div>
      <div v-if="error" class="form-alert theme-message">{{ error }}</div>
      <div v-if="notice" class="form-success theme-message">{{ notice }}</div>
      <TaskProgress v-for="taskId in installTaskIds" :key="taskId" :task-id="taskId" />
      <TaskProgress v-for="taskId in themeTaskIds" :key="taskId" :task-id="taskId" />
      <section v-if="!managingThemes && activeTheme" class="theme-section">
        <h2>{{ t("themesPage.currentTheme") }}</h2>
        <VCard class="theme-card current-theme-card">
          <div class="theme-preview">
            <img v-if="activeTheme.screenshotUrl" :src="activeTheme.screenshotUrl" :alt="activeTheme.name" /><span
              v-else
              >{{ activeTheme.name.slice(0, 1).toUpperCase() }}</span
            >
          </div>
          <div class="theme-copy">
            <div class="theme-title">
              <strong>{{ activeTheme.name }}</strong
              ><VTag>{{ t("themesPage.active") }}</VTag
              ><VTag v-if="activeTheme.builtIn">{{ t("themesPage.builtIn") }}</VTag
              ><VTag>{{ t(`codes.${activeTheme.status}`) }}</VTag>
            </div>
            <span>{{ activeTheme.id }} · v{{ activeTheme.version }} · {{ activeTheme.engine }}</span>
            <p>{{ t("themesPage.currentThemeHelp") }}</p>
          </div>
          <div class="theme-actions">
            <VButton
              type="secondary"
              size="sm"
              :loading="busy === `preview-${activeTheme.id}`"
              @click="preview(activeTheme)"
              >{{ t("themePreview.open") }}</VButton
            ><VButton
              type="secondary"
              size="sm"
              :loading="busy === `settings-${activeTheme.id}`"
              @click="openSettings(activeTheme)"
              >{{ t("themesPage.settings") }}</VButton
            ><VButton
              type="secondary"
              size="sm"
              :loading="busy === `reload-${activeTheme.id}`"
              @click="reload(activeTheme)"
              >{{ t("themesPage.reload") }}</VButton
            >
          </div>
        </VCard>
      </section>
      <VCard v-else-if="!managingThemes"><VEmpty :title="t('themesPage.empty')" /></VCard>
      <section v-if="managingThemes" class="theme-section">
        <h2>{{ t("themesPage.themeManagement") }}</h2>
        <VCard
          ><div class="filter-bar">
            <input v-model="remoteURL" type="url" :placeholder="t('themesPage.remoteUrlPlaceholder')" /><button
              type="button"
              :disabled="!remoteURL.trim() || busy === 'install-url'"
              @click="installFromURL"
            >
              {{ busy === "install-url" ? t("common.loading") : t("themesPage.installFromUrl") }}
            </button>
          </div>
          <p class="theme-guidance">{{ t("themesPage.remoteUrlHelp") }}</p></VCard
        >
        <div v-if="installedThemes.length" class="theme-grid theme-grid--managed">
          <VCard v-for="theme in installedThemes" :key="theme.id" class="theme-card">
            <div class="theme-preview">
              <img v-if="theme.screenshotUrl" :src="theme.screenshotUrl" :alt="theme.name" loading="lazy" /><span
                v-else
                >{{ theme.name.slice(0, 1).toUpperCase() }}</span
              >
            </div>
            <div class="theme-copy">
              <div class="theme-title">
                <strong>{{ theme.name }}</strong
                ><VTag v-if="theme.builtIn">{{ t("themesPage.builtIn") }}</VTag
                ><VTag>{{ t(`codes.${theme.status}`) }}</VTag>
              </div>
              <span
                >{{ theme.id }} · v{{ theme.version }} · {{ theme.engine
                }}<template v-if="theme.requires"> · API {{ theme.requires }}</template></span
              >
            </div>
            <div class="theme-actions">
              <VButton
                type="secondary"
                size="sm"
                :loading="busy === `settings-${theme.id}`"
                @click="openSettings(theme)"
                >{{ t("themesPage.settings") }}</VButton
              >
              <VButton type="secondary" size="sm" :loading="busy === `reload-${theme.id}`" @click="reload(theme)">{{
                t("themesPage.reload")
              }}</VButton>
              <VButton
                type="secondary"
                size="sm"
                :loading="busy === `preview-${theme.id}`"
                :disabled="theme.status !== 'ready'"
                @click="preview(theme)"
                >{{ t("themePreview.open") }}</VButton
              >
              <VButton
                type="secondary"
                size="sm"
                :loading="busy === theme.id"
                :disabled="theme.status !== 'ready'"
                @click="activate(theme)"
                >{{ t("themesPage.enable") }}</VButton
              >
              <button v-if="!theme.builtIn" class="text-danger" :disabled="busy === theme.id" @click="uninstall(theme)">
                {{ t("themesPage.uninstall") }}
              </button>
              <button
                v-if="!theme.builtIn"
                class="text-danger"
                :disabled="busy === theme.id"
                @click="uninstall(theme, true)"
              >
                {{ t("themesPage.uninstallSettings") }}
              </button>
            </div>
          </VCard>
        </div>
        <VCard v-else><VEmpty :title="t('themesPage.noOtherThemes')" /></VCard>
      </section>
      <VCard v-if="selectedSettings" class="theme-settings-card">
        <div class="settings-section-title">
          <div>
            <strong>{{
              schemaTitle(selectedSettings.schema, `${selectedSettings.themeId} ${t("themesPage.settings")}`)
            }}</strong
            ><span>{{ t("themesPage.schemaHelp") }}</span>
          </div>
          <button class="icon-button" @click="selectedSettings = undefined">{{ t("themesPage.close") }}</button>
        </div>
        <input
          ref="settingsPicker"
          class="visually-hidden"
          type="file"
          accept=".json,application/json"
          @change="importSettings"
        />
        <div class="theme-settings-groups">
          <section
            v-for="[groupKey, group] in schemaEntries(selectedSettings.schema)"
            :key="groupKey"
            class="theme-settings-group"
          >
            <h3>{{ schemaTitle(group, groupKey) }}</h3>
            <div class="theme-settings-fields">
              <ThemeSettingsField
                :schema="group"
                :model-value="groupValue(groupKey)"
                @update:model-value="setGroupValue(groupKey, $event)"
              />
            </div>
          </section>
        </div>
        <div class="provider-actions">
          <VButton :loading="busy === 'settings-save'" @click="saveSettings">{{ t("themesPage.saveSettings") }}</VButton
          ><VButton type="secondary" @click="settingsPicker?.click()">{{ t("themesPage.importSettings") }}</VButton
          ><VButton type="secondary" @click="exportSettings">{{ t("themesPage.exportSettings") }}</VButton
          ><button class="text-danger" :disabled="busy === 'settings-reset'" @click="resetSettings">
            {{ t("themesPage.reset") }}
          </button>
        </div>
        <TaskProgress v-for="taskId in settingsTaskIds" :key="taskId" :task-id="taskId" />
      </VCard>
    </div>
  </div>
</template>
