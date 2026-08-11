<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { ThemeSettingsSchema } from "@/api/client";
import MediaPickerField from "@/components/MediaPickerField.vue";

const props = withDefaults(
  defineProps<{
    schema: ThemeSettingsSchema;
    modelValue: unknown;
    label?: string;
    depth?: number;
  }>(),
  { label: "", depth: 0 },
);

const emit = defineEmits<{ "update:modelValue": [value: unknown] }>();
const { t, locale } = useI18n();

const objectValue = computed<Record<string, unknown>>(() => {
  return isRecord(props.modelValue) ? props.modelValue : {};
});

const arrayValue = computed<unknown[]>(() => (Array.isArray(props.modelValue) ? props.modelValue : []));
const numericDraft = ref(numericInputText(props.modelValue));

watch(
  () => props.modelValue,
  (value) => {
    numericDraft.value = numericInputText(value);
  },
);

function localizedTitle(schema: ThemeSettingsSchema, fallback: string) {
  return schema["x-i18n"]?.[locale.value] || schema.title || fallback;
}

function enumTitle(schema: ThemeSettingsSchema, value: string) {
  return schema["x-enum-i18n"]?.[locale.value]?.[value] || value;
}

function updateObject(key: string, value: unknown) {
  emit("update:modelValue", { ...objectValue.value, [key]: value });
}

function updatePrimitive(event: Event) {
  const input = event.target as HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement;
  if (props.schema.type === "boolean") {
    emit("update:modelValue", (input as HTMLInputElement).checked);
    return;
  }
  if (props.schema.type === "number" || props.schema.type === "integer") {
    const number = parseNumericValue(input.value);
    if (number !== undefined) emit("update:modelValue", number);
    return;
  }
  emit("update:modelValue", input.value);
}

function updateNumericDraft(event: Event) {
  numericDraft.value = (event.target as HTMLInputElement).value;
}

function commitNumericDraft() {
  const number = parseNumericValue(numericDraft.value);
  if (number === undefined) {
    numericDraft.value = numericInputText(props.modelValue);
    return;
  }
  numericDraft.value = String(number);
  emit("update:modelValue", number);
}

function parseNumericValue(value: string): number | undefined {
  if (!value.trim()) return undefined;
  const number = Number(value);
  if (!Number.isFinite(number)) return undefined;
  if (props.schema.type === "integer" && !Number.isInteger(number)) return undefined;
  return number;
}

function numericInputText(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "number") return Number.isFinite(value) ? String(value) : "";
  return String(value);
}

function appendArrayItem() {
  if (!props.schema.items || (props.schema.maxItems !== undefined && arrayValue.value.length >= props.schema.maxItems))
    return;
  emit("update:modelValue", [...arrayValue.value, initialValue(props.schema.items)]);
}

function updateArrayItem(index: number, value: unknown) {
  const next = [...arrayValue.value];
  next[index] = value;
  emit("update:modelValue", next);
}

function removeArrayItem(index: number) {
  if (props.schema.minItems !== undefined && arrayValue.value.length <= props.schema.minItems) return;
  emit(
    "update:modelValue",
    arrayValue.value.filter((_, itemIndex) => itemIndex !== index),
  );
}

function moveArrayItem(index: number, offset: number) {
  const destination = index + offset;
  if (destination < 0 || destination >= arrayValue.value.length) return;
  const next = [...arrayValue.value];
  [next[index], next[destination]] = [next[destination], next[index]];
  emit("update:modelValue", next);
}

function initialValue(schema: ThemeSettingsSchema): unknown {
  if (schema.default !== undefined) return structuredClone(schema.default);
  if (schema.type === "object") {
    return Object.fromEntries(
      Object.entries(schema.properties ?? {}).map(([key, child]) => [key, initialValue(child)]),
    );
  }
  if (schema.type === "array") return [];
  if (schema.type === "boolean") return false;
  if (schema.type === "number" || schema.type === "integer") return 0;
  return schema.enum?.[0] ?? "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}
</script>

<template>
  <fieldset
    v-if="schema.type === 'object'"
    class="theme-setting-object"
    :class="{ 'theme-setting-object--nested': depth > 0 }"
  >
    <legend v-if="label">{{ label }}</legend>
    <ThemeSettingsField
      v-for="[key, child] in Object.entries(schema.properties ?? {})"
      :key="key"
      :schema="child"
      :label="localizedTitle(child, key)"
      :model-value="objectValue[key]"
      :depth="depth + 1"
      @update:model-value="updateObject(key, $event)"
    />
  </fieldset>

  <section v-else-if="schema.type === 'array'" class="theme-setting-array">
    <div class="theme-setting-array__header">
      <strong>{{ label }}</strong>
      <button
        type="button"
        :disabled="schema.maxItems !== undefined && arrayValue.length >= schema.maxItems"
        @click="appendArrayItem"
      >
        {{ t("themeSettingsForm.addItem") }}
      </button>
    </div>
    <div v-if="arrayValue.length" class="theme-setting-array__items">
      <article v-for="(item, index) in arrayValue" :key="index" class="theme-setting-array__item">
        <div class="theme-setting-array__actions">
          <span>{{ t("themeSettingsForm.item", { number: index + 1 }) }}</span>
          <button
            type="button"
            :disabled="index === 0"
            :title="t('themeSettingsForm.moveUp')"
            @click="moveArrayItem(index, -1)"
          >
            ↑
          </button>
          <button
            type="button"
            :disabled="index === arrayValue.length - 1"
            :title="t('themeSettingsForm.moveDown')"
            @click="moveArrayItem(index, 1)"
          >
            ↓
          </button>
          <button
            type="button"
            class="text-danger"
            :disabled="schema.minItems !== undefined && arrayValue.length <= schema.minItems"
            @click="removeArrayItem(index)"
          >
            {{ t("common.delete") }}
          </button>
        </div>
        <ThemeSettingsField
          v-if="schema.items"
          :schema="schema.items"
          :model-value="item"
          :depth="depth + 1"
          @update:model-value="updateArrayItem(index, $event)"
        />
      </article>
    </div>
    <p v-else class="theme-guidance">{{ t("themeSettingsForm.empty") }}</p>
  </section>

  <div v-else class="field theme-setting-primitive">
    <span>{{ label }}</span>
    <input v-if="schema.type === 'boolean'" type="checkbox" :checked="Boolean(modelValue)" @change="updatePrimitive" />
    <select v-else-if="schema.enum" :value="String(modelValue ?? '')" @change="updatePrimitive">
      <option v-for="option in schema.enum" :key="option" :value="option">{{ enumTitle(schema, option) }}</option>
    </select>
    <textarea
      v-else-if="schema.format === 'textarea'"
      rows="3"
      :maxlength="schema.maxLength"
      :value="String(modelValue ?? '')"
      @input="updatePrimitive"
    />
    <MediaPickerField
      v-else-if="schema.format === 'media'"
      :model-value="String(modelValue ?? '')"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <input
      v-else-if="schema.type === 'number' || schema.type === 'integer'"
      type="number"
      :step="schema.type === 'integer' ? '1' : 'any'"
      :value="numericDraft"
      @input="updateNumericDraft"
      @blur="commitNumericDraft"
    />
    <input
      v-else
      :type="schema.format === 'color' ? 'color' : 'text'"
      :maxlength="schema.maxLength"
      :value="String(modelValue ?? '')"
      @input="updatePrimitive"
    />
  </div>
</template>
