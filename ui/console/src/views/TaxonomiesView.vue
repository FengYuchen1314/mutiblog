<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { api, type LocalesConfig, type Taxonomy, type ThemeRecord } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import { useCodeLabel } from "@/i18n/useCodeLabel";
import { useBuildTasks } from "@/composables/useBuildTasks";
import TaskProgress from "@/components/TaskProgress.vue";
import MediaPickerField from "@/components/MediaPickerField.vue";

const route = useRoute();
const session = useSessionStore();
const { t } = useI18n();
const codeLabel = useCodeLabel();
const kind = route.name === "categories" ? "categories" : "tags";
const title = () => t(kind === "categories" ? "taxonomyPage.categories" : "taxonomyPage.tags");
const entityName = () => t(kind === "categories" ? "taxonomyPage.category" : "taxonomyPage.tag");
const items = ref<Taxonomy[]>([]);
const locales = ref<LocalesConfig>();
const selected = ref<Taxonomy>();
const activeLocale = ref("");
const creating = ref(false);
const customID = ref("");
const parentID = ref("");
const cover = ref("");
const template = ref("category");
const activeTheme = ref<ThemeRecord>();
const error = ref("");
const notice = ref("");
const busy = ref<"" | "save" | "delete">("");
const { taskIds: buildTaskIds, createTask, discardIfMissing, beginOperation, reconcile } = useBuildTasks();
const form = reactive({ name: "", description: "", seoTitle: "", seoDescription: "" });
const categoryTemplates = computed(() => [
  { id: "category", name: t("taxonomyPage.defaultTemplate") },
  ...(activeTheme.value?.categoryTemplates ?? []),
]);

async function load() {
  error.value = "";
  try {
    const [localeConfig, taxonomyResult, themeResult] = await Promise.all([
      api.locales(),
      api.taxonomies(kind),
      api.themes(),
    ]);
    locales.value = localeConfig;
    items.value = taxonomyResult.items;
    activeTheme.value = themeResult.items.find((theme) => theme.active);
    activeLocale.value ||= locales.value.sourceLocale;
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("taxonomyPage.loadFailed", { name: title() });
  }
}
onMounted(load);

function edit(item: Taxonomy) {
  selected.value = item;
  creating.value = false;
  activeLocale.value = item.sourceLocale;
  parentID.value = item.parentId ?? "";
  cover.value = item.cover ?? "";
  template.value = item.template || "category";
  loadLocale();
}
function startCreate() {
  selected.value = undefined;
  creating.value = true;
  customID.value = "";
  parentID.value = "";
  cover.value = "";
  template.value = "category";
  activeLocale.value = locales.value?.sourceLocale ?? "";
  Object.assign(form, { name: "", description: "", seoTitle: "", seoDescription: "" });
}
function selectLocale(locale: string) {
  activeLocale.value = locale;
  loadLocale();
}
function loadLocale() {
  const value = selected.value?.locales[activeLocale.value];
  Object.assign(form, {
    name: value?.name ?? "",
    description: value?.description ?? "",
    seoTitle: value?.seoTitle ?? "",
    seoDescription: value?.seoDescription ?? "",
  });
}

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

async function save() {
  if (!session.session || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const target = selected.value;
  const locale = activeLocale.value;
  const localeInput = { ...form };
  const createInput = {
    id: customID.value || undefined,
    name: form.name,
    description: form.description,
    parentId: kind === "categories" ? parentID.value || undefined : undefined,
    cover: kind === "categories" ? cover.value : undefined,
    template: kind === "categories" ? template.value : undefined,
  };
  const structureInput = { parentId: parentID.value || undefined, cover: cover.value, template: template.value };
  const structureChanged = Boolean(
    target &&
    kind === "categories" &&
    (parentID.value !== (target.parentId ?? "") ||
      cover.value !== (target.cover ?? "") ||
      template.value !== target.template),
  );
  error.value = "";
  notice.value = "";
  busy.value = "save";
  try {
    await beginOperation();
    if (!target) {
      const created = await withBuildTask((taskId) => api.createTaxonomy(csrfToken, kind, createInput, taskId));
      if (!selected.value && creating.value) {
        selected.value = created;
        creating.value = false;
      }
    } else {
      let updated = target;
      if (structureChanged) {
        updated = await withBuildTask((taskId) =>
          api.updateTaxonomyStructure(csrfToken, kind, target.id, updated.revision, structureInput, taskId),
        );
        if (selected.value?.id === target.id) selected.value = updated;
      }
      updated = await withBuildTask((taskId) =>
        api.updateTaxonomyLocale(
          csrfToken,
          kind,
          target.id,
          locale,
          { revision: updated.revision, ...localeInput },
          taskId,
        ),
      );
      if (selected.value?.id === target.id) selected.value = updated;
    }
    notice.value = t("taxonomyPage.saved", { name: entityName() });
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("taxonomyPage.saveFailed");
  } finally {
    if (busy.value === "save") busy.value = "";
  }
}
async function removeSelected() {
  if (!session.session || !selected.value || busy.value) return;
  const csrfToken = session.session.csrfToken;
  const target = selected.value;
  if (
    !window.confirm(t("taxonomyPage.confirmDelete", { name: target.locales[target.sourceLocale]?.name ?? target.id }))
  )
    return;
  busy.value = "delete";
  try {
    await beginOperation();
    await withBuildTask((taskId) => api.deleteTaxonomy(csrfToken, kind, target.id, target.revision, taskId));
    if (selected.value?.id === target.id) selected.value = undefined;
    notice.value = t("taxonomyPage.deleted");
    await load();
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t("taxonomyPage.deleteFailed");
  } finally {
    if (busy.value === "delete") busy.value = "";
  }
}
</script>

<template>
  <div class="page">
    <MPageHeader :title="title()"
      ><template #actions
        ><MButton variant="tonal" @click="startCreate">{{
          t("taxonomyPage.new", { name: entityName() })
        }}</MButton></template
      ></MPageHeader
    >
    <div class="page-body settings-grid">
      <MSurface
        ><div class="resource-list">
          <button v-for="item in items" :key="item.id" type="button" class="provider-item" @click="edit(item)">
            <strong>{{ item.locales[item.sourceLocale]?.name }}</strong
            ><span>{{ item.id }}</span
            ><MChip v-if="item.parentId">{{ t("taxonomyPage.childCategory") }}</MChip></button
          ><MEmptyState
            v-if="!items.length"
            :title="t(kind === 'categories' ? 'taxonomyPage.emptyCategories' : 'taxonomyPage.emptyTags')"
          /></div
      ></MSurface>
      <MSurface v-if="selected || creating">
        <div class="provider-form">
          <h3>{{ t(selected ? "taxonomyPage.edit" : "taxonomyPage.new", { name: entityName() }) }}</h3>
          <div v-if="notice" class="form-success">{{ notice }}</div>
          <div v-if="error" class="form-alert">{{ error }}</div>
          <label v-if="!selected"
            ><span>{{ t("taxonomyPage.customId") }}</span
            ><input v-model="customID" placeholder="engineering"
          /></label>
          <label v-if="kind === 'categories'"
            ><span>{{ t("taxonomyPage.parentCategory") }}</span
            ><select v-model="parentID">
              <option value="">{{ t("taxonomyPage.none") }}</option>
              <option
                v-for="item in items.filter((candidate) => candidate.id !== selected?.id)"
                :key="item.id"
                :value="item.id"
              >
                {{ item.locales[item.sourceLocale]?.name }}
              </option>
            </select></label
          >
          <MediaPickerField v-if="kind === 'categories'" v-model="cover" :label="t('taxonomyPage.cover')" />
          <label v-if="kind === 'categories'"
            ><span>{{ t("taxonomyPage.template") }}</span
            ><select v-model="template">
              <option v-for="option in categoryTemplates" :key="option.id" :value="option.id">{{ option.name }}</option>
              <option
                v-if="selected && !categoryTemplates.some((option) => option.id === selected?.template)"
                :value="selected?.template ?? 'category'"
              >
                {{ t("taxonomyPage.unavailableTemplate", { id: selected?.template ?? "category" }) }}
              </option>
            </select></label
          >
          <div v-if="selected && locales" class="locale-tabs">
            <button
              v-for="locale in locales.enabled.filter((item) => item.enabled)"
              :key="locale.code"
              type="button"
              :class="{ active: activeLocale === locale.code }"
              @click="selectLocale(locale.code)"
            >
              {{ locale.label }}
              <span v-if="selected.locales[locale.code]" class="locale-origin-badge">{{
                codeLabel(selected.locales[locale.code].origin)
              }}</span>
            </button>
          </div>
          <label
            ><span>{{ t("taxonomyPage.name") }}</span
            ><input v-model="form.name" /></label
          ><label
            ><span>{{ t("taxonomyPage.description") }}</span
            ><textarea v-model="form.description" rows="4" /></label
          ><label
            ><span>{{ t("taxonomyPage.seoTitle") }}</span
            ><input v-model="form.seoTitle" /></label
          ><label
            ><span>{{ t("taxonomyPage.seoDescription") }}</span
            ><textarea v-model="form.seoDescription" rows="3" />
          </label>
          <MButton variant="tonal" :loading="busy === 'save'" :disabled="Boolean(busy)" @click="save">{{
            t("common.save")
          }}</MButton
          ><button v-if="selected" type="button" class="text-danger" :disabled="Boolean(busy)" @click="removeSelected">
            {{ t("common.deleteForever") }}
          </button>
        </div>
      </MSurface>
      <MSurface v-if="buildTaskIds.length"
        ><TaskProgress v-for="taskId in buildTaskIds" :key="taskId" :task-id="taskId"
      /></MSurface>
    </div>
  </div>
</template>
