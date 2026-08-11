<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { VButton, VCard, VEmpty, VPageHeader } from "@halo-dev/components";
import { api, type LocalesConfig, type MenuItem, type MenuRecord } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import { useCodeLabel } from "@/i18n/useCodeLabel";
import { useBuildTasks } from "@/composables/useBuildTasks";
import TaskProgress from "@/components/TaskProgress.vue";
const session = useSessionStore();
const { t } = useI18n();
const codeLabel = useCodeLabel();
const locales = ref<LocalesConfig>();
const menus = ref<MenuRecord[]>([]);
const selected = ref<MenuRecord>();
const selectedItem = ref<MenuItem>();
const activeLocale = ref("");
const error = ref("");
const busy = ref<"" | "create-menu" | "add-item" | "save-locale" | "save-structure" | "delete">("");
const { taskIds: buildTaskIds, createTask, discardIfMissing, beginOperation, reconcile } = useBuildTasks();
const menuForm = reactive({ id: "", label: "" });
const itemForm = reactive({
  id: "",
  parentId: "",
  targetKind: "internal" as "internal" | "external",
  url: "/",
  label: "",
  openInNew: false,
  order: 0,
});
const localeLabel = ref("");
const editForm = reactive({
  parentId: "",
  targetKind: "internal" as "internal" | "external",
  url: "/",
  openInNew: false,
  order: 0,
});
async function load() {
  try {
    locales.value = await api.locales();
    menus.value = (await api.menus()).items;
    if (selected.value) {
      const itemID = selectedItem.value?.id;
      selected.value = menus.value.find((x) => x.id === selected.value?.id);
      selectedItem.value = itemID ? selected.value?.items.find((item) => item.id === itemID) : undefined;
    }
    activeLocale.value ||= locales.value.sourceLocale;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("menusPage.loadFailed");
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
async function createMenu() {
  if (!session.session || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const input = { id: menuForm.id || undefined, label: menuForm.label };
  busy.value = "create-menu";
  try {
    await beginOperation();
    const created = await withBuildTask((taskId) => api.createMenu(csrfToken, input, taskId));
    if (!selected.value) selected.value = created;
    if (menuForm.id === (input.id ?? "") && menuForm.label === input.label)
      Object.assign(menuForm, { id: "", label: "" });
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("menusPage.createFailed");
  } finally {
    if (busy.value === "create-menu") busy.value = "";
  }
}
function choose(menu: MenuRecord) {
  selected.value = menu;
  selectedItem.value = undefined;
  activeLocale.value = menu.sourceLocale;
  localeLabel.value = menu.locales[activeLocale.value]?.label ?? "";
}
async function addItem() {
  if (!session.session || !selected.value || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const menu = selected.value;
  const input = {
    id: itemForm.id || undefined,
    parentId: itemForm.parentId || undefined,
    targetKind: itemForm.targetKind,
    url: itemForm.url,
    label: itemForm.label,
    openInNew: itemForm.openInNew,
    order: itemForm.order,
    revision: menu.revision,
  };
  busy.value = "add-item";
  try {
    await beginOperation();
    const updated = await withBuildTask((taskId) => api.addMenuItem(csrfToken, menu.id, input, taskId));
    if (selected.value?.id === menu.id) selected.value = updated;
    if (
      itemForm.id === (input.id ?? "") &&
      itemForm.parentId === (input.parentId ?? "") &&
      itemForm.targetKind === input.targetKind &&
      itemForm.url === input.url &&
      itemForm.label === input.label &&
      itemForm.openInNew === input.openInNew &&
      itemForm.order === input.order
    )
      Object.assign(itemForm, {
        id: "",
        parentId: "",
        targetKind: "internal",
        url: "/",
        label: "",
        openInNew: false,
        order: 0,
      });
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("menusPage.addFailed");
  } finally {
    if (busy.value === "add-item") busy.value = "";
  }
}
function editItem(item: MenuItem) {
  selectedItem.value = item;
  activeLocale.value = selected.value?.sourceLocale ?? "";
  localeLabel.value = item.locales[activeLocale.value]?.label ?? "";
  Object.assign(editForm, {
    parentId: item.parentId ?? "",
    targetKind: item.targetKind,
    url: item.url,
    openInNew: item.openInNew,
    order: item.order,
  });
}
function chooseLocale(code: string) {
  activeLocale.value = code;
  localeLabel.value = (selectedItem.value?.locales ?? selected.value?.locales)?.[code]?.label ?? "";
}
async function saveLocale() {
  if (!session.session || !selected.value || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const menu = selected.value;
  const item = selectedItem.value;
  const locale = activeLocale.value;
  const label = localeLabel.value;
  busy.value = "save-locale";
  try {
    await beginOperation();
    const updated = item
      ? await withBuildTask((taskId) =>
          api.updateMenuItemLocale(csrfToken, menu.id, item.id, locale, { revision: menu.revision, label }, taskId),
        )
      : await withBuildTask((taskId) =>
          api.updateMenuLocale(csrfToken, menu.id, locale, { revision: menu.revision, label }, taskId),
        );
    if (selected.value?.id === menu.id) selected.value = updated;
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("menusPage.saveFailed");
  } finally {
    if (busy.value === "save-locale") busy.value = "";
  }
}
async function saveStructure() {
  if (!session.session || !selected.value || !selectedItem.value || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const menu = selected.value;
  const item = selectedItem.value;
  const input = {
    revision: menu.revision,
    parentId: editForm.parentId || undefined,
    targetKind: editForm.targetKind,
    url: editForm.url,
    openInNew: editForm.openInNew,
    order: editForm.order,
  };
  busy.value = "save-structure";
  try {
    await beginOperation();
    const updated = await withBuildTask((taskId) => api.updateMenuItem(csrfToken, menu.id, item.id, input, taskId));
    if (selected.value?.id === menu.id) selected.value = updated;
    if (selectedItem.value?.id === item.id)
      selectedItem.value = updated.items.find((candidate) => candidate.id === item.id);
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("menusPage.structureSaveFailed");
  } finally {
    if (busy.value === "save-structure") busy.value = "";
  }
}
async function removeSelected() {
  if (!session.session || !selected.value || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const menu = selected.value;
  const item = selectedItem.value;
  const name = (item?.locales ?? menu.locales)[menu.sourceLocale]?.label ?? item?.id ?? menu.id;
  if (!window.confirm(t("menusPage.confirmDelete", { name }))) return;
  busy.value = "delete";
  try {
    await beginOperation();
    if (item) {
      const updated = await withBuildTask((taskId) =>
        api.deleteMenuItem(csrfToken, menu.id, item.id, menu.revision, taskId),
      );
      if (selected.value?.id === menu.id) selected.value = updated;
      if (selectedItem.value?.id === item.id) {
        selectedItem.value = undefined;
        activeLocale.value = updated.sourceLocale;
        localeLabel.value = updated.locales[activeLocale.value]?.label ?? "";
      }
    } else {
      await withBuildTask((taskId) => api.deleteMenu(csrfToken, menu.id, menu.revision, taskId));
      if (selected.value?.id === menu.id) selected.value = undefined;
    }
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("menusPage.deleteFailed");
  } finally {
    if (busy.value === "delete") busy.value = "";
  }
}
</script>
<template>
  <div class="page">
    <VPageHeader :title="t('menusPage.title')"
      ><template #actions
        ><VButton @click="load">{{ t("common.refresh") }}</VButton></template
      ></VPageHeader
    >
    <div class="page-body settings-grid">
      <VCard
        ><h3>{{ t("menusPage.collections") }}</h3>
        <div class="provider-form">
          <label
            ><span>{{ t("menusPage.idOptional") }}</span
            ><input v-model="menuForm.id" placeholder="primary" /></label
          ><label
            ><span>{{ t("menusPage.name") }}</span
            ><input v-model="menuForm.label" /></label
          ><VButton type="secondary" :loading="busy === 'create-menu'" :disabled="Boolean(busy)" @click="createMenu">{{
            t("menusPage.create")
          }}</VButton>
        </div>
        <button v-for="menu in menus" :key="menu.id" class="provider-item" @click="choose(menu)">
          <strong>{{ menu.locales[menu.sourceLocale]?.label }}</strong
          ><span>{{ menu.id }}</span></button
        ><VEmpty v-if="!menus.length" :title="t('menusPage.empty')"
      /></VCard>
      <VCard v-if="selected"
        ><h3>{{ selected.locales[selected.sourceLocale]?.label }} · {{ t("menusPage.items") }}</h3>
        <div class="provider-form">
          <label
            ><span>{{ t("menusPage.parent") }}</span
            ><select v-model="itemForm.parentId">
              <option value="">{{ t("menusPage.none") }}</option>
              <option v-for="item in selected.items" :key="item.id" :value="item.id">
                {{ item.locales[selected.sourceLocale]?.label }}
              </option>
            </select></label
          ><label
            ><span>{{ t("menusPage.type") }}</span
            ><select v-model="itemForm.targetKind">
              <option value="internal">{{ t("menusPage.internal") }}</option>
              <option value="external">{{ t("menusPage.external") }}</option>
            </select></label
          ><label
            ><span>{{ t("menusPage.address") }}</span
            ><input v-model="itemForm.url" placeholder="/pages/about-site/" /></label
          ><label
            ><span>{{ t("menusPage.label") }}</span
            ><input v-model="itemForm.label" /></label
          ><label
            ><span>{{ t("menusPage.order") }}</span
            ><input v-model.number="itemForm.order" type="number" /></label
          ><label class="check-row"
            ><input v-model="itemForm.openInNew" type="checkbox" />{{ t("menusPage.newWindow") }}</label
          ><VButton type="secondary" :loading="busy === 'add-item'" :disabled="Boolean(busy)" @click="addItem">{{
            t("menusPage.addItem")
          }}</VButton>
        </div>
        <button v-for="item in selected.items" :key="item.id" class="provider-item" @click="editItem(item)">
          <strong>{{ item.locales[selected.sourceLocale]?.label }}</strong
          ><span>{{ item.url }}</span>
        </button></VCard
      >
      <VCard v-if="selected"
        ><template v-if="selectedItem"
          ><h3>{{ t("menusPage.structure") }}</h3>
          <div class="provider-form">
            <label
              ><span>{{ t("menusPage.parent") }}</span
              ><select v-model="editForm.parentId">
                <option value="">{{ t("menusPage.none") }}</option>
                <option
                  v-for="item in selected.items.filter((candidate) => candidate.id !== selectedItem?.id)"
                  :key="item.id"
                  :value="item.id"
                >
                  {{ item.locales[selected.sourceLocale]?.label }}
                </option>
              </select></label
            ><label
              ><span>{{ t("menusPage.type") }}</span
              ><select v-model="editForm.targetKind">
                <option value="internal">{{ t("menusPage.internal") }}</option>
                <option value="external">{{ t("menusPage.external") }}</option>
              </select></label
            ><label
              ><span>{{ t("menusPage.address") }}</span
              ><input v-model="editForm.url" /></label
            ><label
              ><span>{{ t("menusPage.order") }}</span
              ><input v-model.number="editForm.order" type="number" /></label
            ><label class="check-row"
              ><input v-model="editForm.openInNew" type="checkbox" />{{ t("menusPage.newWindow") }}</label
            ><VButton
              type="secondary"
              :loading="busy === 'save-structure'"
              :disabled="Boolean(busy)"
              @click="saveStructure"
              >{{ t("menusPage.saveStructure") }}</VButton
            >
          </div></template
        >
        <h3>{{ t(selectedItem ? "menusPage.item" : "menusPage.menu") }}{{ t("menusPage.localized") }}</h3>
        <div class="locale-tabs">
          <button
            v-for="locale in locales?.enabled.filter((x) => x.enabled)"
            :key="locale.code"
            :class="{ active: activeLocale === locale.code }"
            @click="chooseLocale(locale.code)"
          >
            {{ locale.label
            }}<span v-if="(selectedItem?.locales ?? selected.locales)[locale.code]" class="locale-origin-badge">{{
              codeLabel((selectedItem?.locales ?? selected.locales)[locale.code]?.origin ?? "")
            }}</span>
          </button>
        </div>
        <div class="provider-form">
          <label
            ><span>{{ t("menusPage.label") }}</span
            ><input v-model="localeLabel" /></label
          ><VButton type="secondary" :loading="busy === 'save-locale'" :disabled="Boolean(busy)" @click="saveLocale">{{
            t("menusPage.saveTranslation")
          }}</VButton
          ><button type="button" class="text-danger" :disabled="Boolean(busy)" @click="removeSelected">
            {{ t("common.deleteForever") }}
          </button>
        </div></VCard
      ><VCard v-if="buildTaskIds.length"
        ><TaskProgress v-for="taskId in buildTaskIds" :key="taskId" :task-id="taskId"
      /></VCard>
      <div v-if="error" class="form-alert">{{ error }}</div>
    </div>
  </div>
</template>
