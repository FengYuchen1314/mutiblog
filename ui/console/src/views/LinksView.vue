<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { api, type LinkGroup, type LocalesConfig, type SiteLink } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import { useCodeLabel } from "@/i18n/useCodeLabel";
import { useBuildTasks } from "@/composables/useBuildTasks";
import TaskProgress from "@/components/TaskProgress.vue";
import MediaPickerField from "@/components/MediaPickerField.vue";

const session = useSessionStore();
const { t } = useI18n();
const codeLabel = useCodeLabel();
const locales = ref<LocalesConfig>();
const groups = ref<LinkGroup[]>([]);
const items = ref<SiteLink[]>([]);
const selectedGroup = ref<LinkGroup>();
const selectedLink = ref<SiteLink>();
const activeLocale = ref("");
const error = ref("");
const busy = ref<"" | "create-group" | "create-link" | "save-locale" | "save-structure" | "delete">("");
const { taskIds: buildTaskIds, createTask, discardIfMissing, beginOperation, reconcile } = useBuildTasks();
const groupForm = reactive({ id: "", name: "", description: "", order: 0 });
const linkForm = reactive({ id: "", groupId: "", url: "", logo: "", name: "", description: "", order: 0 });
const localeForm = reactive({ name: "", description: "" });
const structureForm = reactive({ groupId: "", url: "", logo: "", order: 0 });

async function load() {
  try {
    locales.value = await api.locales();
    const data = await api.links();
    groups.value = data.groups;
    items.value = data.items;
    activeLocale.value ||= locales.value.sourceLocale;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("linksPage.loadFailed");
  }
}
onMounted(load);
async function withBuildTask<T>(request: (taskId: string) => Promise<T>) {
  const taskId = createTask();
  try {
    const result = await request(taskId);
    await reconcile(taskId);
    return result;
  } catch (caught) {
    await discardIfMissing(taskId);
    throw caught;
  }
}
async function createGroup() {
  if (!session.session || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const input = {
    id: groupForm.id || undefined,
    name: groupForm.name,
    description: groupForm.description,
    order: groupForm.order,
  };
  busy.value = "create-group";
  try {
    await beginOperation();
    await withBuildTask((taskId) => api.createLinkGroup(csrfToken, input, taskId));
    if (
      groupForm.id === (input.id ?? "") &&
      groupForm.name === input.name &&
      groupForm.description === input.description &&
      groupForm.order === input.order
    ) {
      Object.assign(groupForm, { id: "", name: "", description: "", order: 0 });
    }
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("linksPage.createGroupFailed");
  } finally {
    if (busy.value === "create-group") busy.value = "";
  }
}
async function createLink() {
  if (!session.session || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const input = {
    id: linkForm.id || undefined,
    groupId: linkForm.groupId,
    url: linkForm.url,
    logo: linkForm.logo,
    name: linkForm.name,
    description: linkForm.description,
    order: linkForm.order,
  };
  busy.value = "create-link";
  try {
    await beginOperation();
    await withBuildTask((taskId) => api.createLink(csrfToken, input, taskId));
    if (
      linkForm.id === (input.id ?? "") &&
      linkForm.groupId === input.groupId &&
      linkForm.url === input.url &&
      linkForm.logo === input.logo &&
      linkForm.name === input.name &&
      linkForm.description === input.description &&
      linkForm.order === input.order
    ) {
      Object.assign(linkForm, {
        id: "",
        groupId: input.groupId,
        url: "",
        logo: "",
        name: "",
        description: "",
        order: 0,
      });
    }
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("linksPage.createLinkFailed");
  } finally {
    if (busy.value === "create-link") busy.value = "";
  }
}
function editGroup(group: LinkGroup) {
  selectedGroup.value = group;
  selectedLink.value = undefined;
  activeLocale.value = group.sourceLocale;
  Object.assign(structureForm, { groupId: "", url: "", logo: "", order: group.order });
  loadLocale();
}
function editLink(link: SiteLink) {
  selectedLink.value = link;
  selectedGroup.value = undefined;
  activeLocale.value = link.sourceLocale;
  Object.assign(structureForm, { groupId: link.groupId, url: link.url, logo: link.logo ?? "", order: link.order });
  loadLocale();
}
function loadLocale() {
  const value = selectedGroup.value?.locales[activeLocale.value] ?? selectedLink.value?.locales[activeLocale.value];
  Object.assign(localeForm, { name: value?.name ?? "", description: value?.description ?? "" });
}
async function saveLocale() {
  if (!session.session || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const group = selectedGroup.value;
  const link = selectedLink.value;
  const locale = activeLocale.value;
  const input = { ...localeForm };
  if (!group && !link) return;
  busy.value = "save-locale";
  try {
    await beginOperation();
    if (group) {
      const updated = await withBuildTask((taskId) =>
        api.updateLinkGroupLocale(csrfToken, group.id, locale, { revision: group.revision, ...input }, taskId),
      );
      if (selectedGroup.value?.id === group.id) selectedGroup.value = updated;
    } else if (link) {
      const updated = await withBuildTask((taskId) =>
        api.updateLinkLocale(csrfToken, link.id, locale, { revision: link.revision, ...input }, taskId),
      );
      if (selectedLink.value?.id === link.id) selectedLink.value = updated;
    }
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("linksPage.saveFailed");
  } finally {
    if (busy.value === "save-locale") busy.value = "";
  }
}
async function saveStructure() {
  if (!session.session || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const group = selectedGroup.value;
  const link = selectedLink.value;
  const input = { ...structureForm };
  if (!group && !link) return;
  busy.value = "save-structure";
  try {
    await beginOperation();
    if (group) {
      const updated = await withBuildTask((taskId) =>
        api.updateLinkGroup(csrfToken, group.id, group.revision, input.order, taskId),
      );
      if (selectedGroup.value?.id === group.id) selectedGroup.value = updated;
    } else if (link) {
      const updated = await withBuildTask((taskId) =>
        api.updateLink(
          csrfToken,
          link.id,
          { revision: link.revision, groupId: input.groupId, url: input.url, logo: input.logo, order: input.order },
          taskId,
        ),
      );
      if (selectedLink.value?.id === link.id) selectedLink.value = updated;
    }
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("linksPage.structureSaveFailed");
  } finally {
    if (busy.value === "save-structure") busy.value = "";
  }
}
async function removeSelected() {
  if (!session.session || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const group = selectedGroup.value;
  const link = selectedLink.value;
  const resource = group ?? link;
  if (
    !resource ||
    !window.confirm(
      t("linksPage.confirmDelete", { name: resource.locales[resource.sourceLocale]?.name ?? resource.id }),
    )
  )
    return;
  busy.value = "delete";
  try {
    await beginOperation();
    if (group) await withBuildTask((taskId) => api.deleteLinkGroup(csrfToken, group.id, group.revision, taskId));
    else if (link) await withBuildTask((taskId) => api.deleteLink(csrfToken, link.id, link.revision, taskId));
    if (group && selectedGroup.value?.id === group.id) selectedGroup.value = undefined;
    if (link && selectedLink.value?.id === link.id) selectedLink.value = undefined;
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("linksPage.deleteFailed");
  } finally {
    if (busy.value === "delete") busy.value = "";
  }
}
</script>

<template>
  <div class="page">
    <MPageHeader :title="t('linksPage.title')"
      ><template #actions
        ><MButton @click="load">{{ t("common.refresh") }}</MButton></template
      ></MPageHeader
    >
    <div class="page-body settings-grid">
      <MSurface
        ><h3>{{ t("linksPage.groups") }}</h3>
        <div class="provider-form">
          <label
            ><span>{{ t("linksPage.idOptional") }}</span
            ><input v-model="groupForm.id" placeholder="friends" /></label
          ><label
            ><span>{{ t("linksPage.name") }}</span
            ><input v-model="groupForm.name" /></label
          ><label
            ><span>{{ t("linksPage.description") }}</span
            ><input v-model="groupForm.description" /></label
          ><label
            ><span>{{ t("linksPage.order") }}</span
            ><input v-model.number="groupForm.order" type="number" /></label
          ><MButton variant="tonal" :loading="busy === 'create-group'" :disabled="Boolean(busy)" @click="createGroup">{{
            t("linksPage.createGroup")
          }}</MButton>
        </div>
        <button v-for="group in groups" :key="group.id" class="provider-item" @click="editGroup(group)">
          <strong>{{ group.locales[group.sourceLocale]?.name }}</strong
          ><span>{{ group.id }}</span>
        </button></MSurface
      >
      <MSurface
        ><h3>{{ t("linksPage.links") }}</h3>
        <div class="provider-form">
          <label
            ><span>{{ t("linksPage.group") }}</span
            ><select v-model="linkForm.groupId">
              <option value="">{{ t("linksPage.choose") }}</option>
              <option v-for="group in groups" :key="group.id" :value="group.id">
                {{ group.locales[group.sourceLocale]?.name }}
              </option>
            </select></label
          ><label
            ><span>{{ t("linksPage.idOptional") }}</span
            ><input v-model="linkForm.id" placeholder="example-site" /></label
          ><label
            ><span>{{ t("linksPage.name") }}</span
            ><input v-model="linkForm.name" /></label
          ><label
            ><span>{{ t("linksPage.url") }}</span
            ><input v-model="linkForm.url" placeholder="https://example.com" /></label
          ><MediaPickerField v-model="linkForm.logo" :label="t('linksPage.localLogo')" /><label
            ><span>{{ t("linksPage.description") }}</span
            ><input v-model="linkForm.description" /></label
          ><label
            ><span>{{ t("linksPage.order") }}</span
            ><input v-model.number="linkForm.order" type="number" /></label
          ><MButton variant="tonal" :loading="busy === 'create-link'" :disabled="Boolean(busy)" @click="createLink">{{
            t("linksPage.createLink")
          }}</MButton>
        </div>
        <button v-for="link in items" :key="link.id" class="provider-item" @click="editLink(link)">
          <strong>{{ link.locales[link.sourceLocale]?.name }}</strong
          ><span>{{ link.url }}</span></button
        ><MEmptyState v-if="!items.length" :title="t('linksPage.empty')"
      /></MSurface>
      <MSurface v-if="selectedGroup || selectedLink"
        ><h3>{{ t("linksPage.resourceSettings") }}</h3>
        <div class="provider-form">
          <label v-if="selectedLink"
            ><span>{{ t("linksPage.group") }}</span
            ><select v-model="structureForm.groupId">
              <option v-for="group in groups" :key="group.id" :value="group.id">
                {{ group.locales[group.sourceLocale]?.name }}
              </option>
            </select></label
          ><label v-if="selectedLink"
            ><span>{{ t("linksPage.url") }}</span
            ><input v-model="structureForm.url" /></label
          ><MediaPickerField v-if="selectedLink" v-model="structureForm.logo" :label="t('linksPage.localLogo')" /><label
            ><span>{{ t("linksPage.order") }}</span
            ><input v-model.number="structureForm.order" type="number" /></label
          ><MButton
            variant="tonal"
            :loading="busy === 'save-structure'"
            :disabled="Boolean(busy)"
            @click="saveStructure"
            >{{ t("linksPage.saveStructure") }}</MButton
          >
        </div>
        <h3>{{ t("linksPage.localizedCopy") }}</h3>
        <div class="locale-tabs">
          <button
            v-for="locale in locales?.enabled.filter((item) => item.enabled)"
            :key="locale.code"
            :class="{ active: activeLocale === locale.code }"
            @click="
              activeLocale = locale.code;
              loadLocale();
            "
          >
            {{ locale.label }}
            <span v-if="(selectedGroup?.locales ?? selectedLink?.locales)?.[locale.code]" class="locale-origin-badge">{{
              codeLabel((selectedGroup?.locales ?? selectedLink?.locales)?.[locale.code]?.origin ?? "")
            }}</span>
          </button>
        </div>
        <div class="provider-form">
          <label
            ><span>{{ t("linksPage.name") }}</span
            ><input v-model="localeForm.name" /></label
          ><label
            ><span>{{ t("linksPage.description") }}</span
            ><textarea v-model="localeForm.description" /></label
          ><MButton variant="tonal" :loading="busy === 'save-locale'" :disabled="Boolean(busy)" @click="saveLocale">{{
            t("linksPage.saveTranslation")
          }}</MButton
          ><button type="button" class="text-danger" :disabled="Boolean(busy)" @click="removeSelected">
            {{ t("common.deleteForever") }}
          </button>
        </div></MSurface
      >
      <MSurface v-if="buildTaskIds.length"
        ><TaskProgress v-for="taskId in buildTaskIds" :key="taskId" :task-id="taskId"
      /></MSurface>
      <div v-if="error" class="form-alert">{{ error }}</div>
    </div>
  </div>
</template>
