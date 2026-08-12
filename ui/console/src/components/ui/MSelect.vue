<script setup lang="ts" generic="T extends string = string">
withDefaults(
  defineProps<{
    modelValue?: T;
    id?: string;
    name?: string;
    disabled?: boolean;
    required?: boolean;
    ariaLabel?: string;
    invalid?: boolean;
  }>(),
  { disabled: false, required: false, invalid: false },
);

const emit = defineEmits<{
  "update:modelValue": [value: T];
  change: [event: Event];
}>();

function handleChange(event: Event) {
  emit("update:modelValue", (event.target as HTMLSelectElement).value as T);
  emit("change", event);
}
</script>

<template>
  <span class="m-select" :class="{ 'm-select--disabled': disabled, 'm-select--invalid': invalid }">
    <select
      :id="id"
      :name="name"
      :value="modelValue"
      :disabled="disabled"
      :required="required"
      :aria-label="ariaLabel"
      :aria-invalid="invalid || undefined"
      @change="handleChange"
    >
      <slot />
    </select>
  </span>
</template>

<style scoped>
.m-select {
  position: relative;
  display: inline-flex;
  min-width: 9.5rem;
  min-height: 2.5rem;
  overflow: hidden;
  border: 1px solid var(--m-sys-color-outline);
  border-radius: var(--m-sys-shape-corner-small);
  background: var(--m-sys-color-surface-container-lowest);
  color: var(--m-sys-color-on-surface);
  transition:
    border-color var(--m-sys-motion-duration-short4) var(--m-sys-motion-easing-standard),
    box-shadow var(--m-sys-motion-duration-short4) var(--m-sys-motion-easing-standard),
    background-color var(--m-sys-motion-duration-short4) var(--m-sys-motion-easing-standard);
}

.m-select::after {
  position: absolute;
  top: calc(50% - 0.2rem);
  right: 1rem;
  width: 0.45rem;
  height: 0.45rem;
  border-right: 2px solid var(--m-sys-color-on-surface-variant);
  border-bottom: 2px solid var(--m-sys-color-on-surface-variant);
  content: "";
  pointer-events: none;
  transform: rotate(45deg);
}

.m-select:focus-within {
  border-color: var(--m-sys-color-primary);
  box-shadow: var(--m-sys-focus-ring);
}

.m-select:hover:not(.m-select--disabled) {
  background: var(--m-sys-color-surface-container-low);
}

.m-select select {
  width: 100%;
  min-width: 0;
  min-height: 2.5rem;
  appearance: none;
  border: 0;
  outline: 0;
  background: transparent;
  background-image: none;
  color: inherit;
  cursor: pointer;
  font-family: var(--m-sys-font-family);
  font-size: var(--m-sys-typescale-label-large-size);
  font-weight: 600;
  letter-spacing: 0.006em;
  line-height: var(--m-sys-typescale-label-large-line-height);
  padding: 0.5rem 2.75rem 0.5rem 0.875rem;
}

.m-select--disabled {
  border-color: transparent;
  background: color-mix(in srgb, var(--m-sys-color-on-surface) 12%, transparent);
  color: color-mix(in srgb, var(--m-sys-color-on-surface) 38%, transparent);
}

.m-select--disabled select {
  cursor: not-allowed;
}

.m-select--invalid {
  border-color: var(--m-sys-color-error);
}

@media (max-width: 38rem) {
  .m-select {
    width: 100%;
  }
}
</style>
