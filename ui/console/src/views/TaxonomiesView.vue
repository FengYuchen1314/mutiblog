<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { VButton, VCard, VEmpty, VPageHeader, VTag } from "@halo-dev/components";
import { api, type LocalesConfig, type Taxonomy } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import { useCodeLabel } from "@/i18n/useCodeLabel";

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
const error = ref("");
const notice = ref("");
const form = reactive({ name: "", description: "", seoTitle: "", seoDescription: "" });

async function load() {
  error.value = "";
  try {
    locales.value = await api.locales();
    items.value = (await api.taxonomies(kind)).items;
    activeLocale.value ||= locales.value.sourceLocale;
  } catch (caught) { error.value = caught instanceof Error ? caught.message : t("taxonomyPage.loadFailed", { name: title() }); }
}
onMounted(load);

function edit(item: Taxonomy) {
  selected.value = item;
  creating.value = false;
  activeLocale.value = item.sourceLocale;
	parentID.value = item.parentId ?? "";
  loadLocale();
}
function startCreate() {
  selected.value = undefined; creating.value = true; customID.value = ""; parentID.value = "";
  activeLocale.value = locales.value?.sourceLocale ?? "";
  Object.assign(form, { name: "", description: "", seoTitle: "", seoDescription: "" });
}
function selectLocale(locale: string) { activeLocale.value = locale; loadLocale(); }
function loadLocale() {
  const value = selected.value?.locales[activeLocale.value];
  Object.assign(form, { name: value?.name ?? "", description: value?.description ?? "", seoTitle: value?.seoTitle ?? "", seoDescription: value?.seoDescription ?? "" });
}
async function save() {
  if (!session.session) return;
  error.value = ""; notice.value = "";
  try {
    if (!selected.value) {
      selected.value = await api.createTaxonomy(session.session.csrfToken, kind, { id: customID.value || undefined, name: form.name, description: form.description, parentId: kind === "categories" ? parentID.value || undefined : undefined });
      creating.value = false;
    } else {
	  if (kind === "categories" && parentID.value !== (selected.value.parentId ?? "")) {
		selected.value = await api.updateTaxonomyStructure(session.session.csrfToken, kind, selected.value.id, selected.value.revision, parentID.value || undefined);
	  }
      selected.value = await api.updateTaxonomyLocale(session.session.csrfToken, kind, selected.value.id, activeLocale.value, { revision: selected.value.revision, ...form });
    }
    notice.value = t("taxonomyPage.saved", { name: entityName() });
    await load();
  } catch (caught) { error.value = caught instanceof Error ? caught.message : t("taxonomyPage.saveFailed"); }
}
async function removeSelected() {
  if (!session.session || !selected.value || !window.confirm(t("taxonomyPage.confirmDelete", { name: selected.value.locales[selected.value.sourceLocale]?.name ?? selected.value.id }))) return;
  try {
    await api.deleteTaxonomy(session.session.csrfToken, kind, selected.value.id, selected.value.revision);
    selected.value = undefined;
    notice.value = t("taxonomyPage.deleted");
    await load();
  } catch (caught) { error.value = caught instanceof Error ? caught.message : t("taxonomyPage.deleteFailed"); }
}
</script>

<template>
  <div class="page">
    <VPageHeader :title="title()"><template #actions><VButton type="secondary" @click="startCreate">{{ t("taxonomyPage.new", { name: entityName() }) }}</VButton></template></VPageHeader>
    <div class="page-body settings-grid">
      <VCard><div class="resource-list"><button v-for="item in items" :key="item.id" type="button" class="provider-item" @click="edit(item)"><strong>{{ item.locales[item.sourceLocale]?.name }}</strong><span>{{ item.id }}</span><VTag v-if="item.parentId">{{ t("taxonomyPage.childCategory") }}</VTag></button><VEmpty v-if="!items.length" :title="t(kind === 'categories' ? 'taxonomyPage.emptyCategories' : 'taxonomyPage.emptyTags')" /></div></VCard>
      <VCard v-if="selected || creating">
        <div class="provider-form">
          <h3>{{ t(selected ? "taxonomyPage.edit" : "taxonomyPage.new", { name: entityName() }) }}</h3>
          <div v-if="notice" class="form-success">{{ notice }}</div><div v-if="error" class="form-alert">{{ error }}</div>
          <label v-if="!selected"><span>{{ t("taxonomyPage.customId") }}</span><input v-model="customID" placeholder="engineering" /></label>
          <label v-if="kind === 'categories'"><span>{{ t("taxonomyPage.parentCategory") }}</span><select v-model="parentID"><option value="">{{ t("taxonomyPage.none") }}</option><option v-for="item in items.filter(candidate => candidate.id !== selected?.id)" :key="item.id" :value="item.id">{{ item.locales[item.sourceLocale]?.name }}</option></select></label>
          <div v-if="selected && locales" class="locale-tabs"><button v-for="locale in locales.enabled.filter(item => item.enabled)" :key="locale.code" type="button" :class="{ active: activeLocale === locale.code }" @click="selectLocale(locale.code)">{{ locale.label }} <span v-if="selected.locales[locale.code]" class="locale-origin-badge">{{ codeLabel(selected.locales[locale.code].origin) }}</span></button></div>
          <label><span>{{ t("taxonomyPage.name") }}</span><input v-model="form.name" /></label><label><span>{{ t("taxonomyPage.description") }}</span><textarea v-model="form.description" rows="4" /></label><label><span>{{ t("taxonomyPage.seoTitle") }}</span><input v-model="form.seoTitle" /></label><label><span>{{ t("taxonomyPage.seoDescription") }}</span><textarea v-model="form.seoDescription" rows="3" /></label>
          <VButton type="secondary" @click="save">{{ t("common.save") }}</VButton><button v-if="selected" type="button" class="text-danger" @click="removeSelected">{{ t("common.deleteForever") }}</button>
        </div>
      </VCard>
    </div>
  </div>
</template>
