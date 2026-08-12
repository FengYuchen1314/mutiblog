<script setup lang="ts">
import { computed, type CSSProperties } from "vue";

type ChipTone = "neutral" | "primary" | "secondary" | "success" | "warning" | "danger" | "info";
type LegacyTheme = "default" | "primary" | "secondary" | "danger";

const legacyThemes: Record<LegacyTheme, ChipTone> = {
  default: "neutral",
  primary: "primary",
  secondary: "secondary",
  danger: "danger",
};

const props = withDefaults(
  defineProps<{
    tone?: ChipTone;
    /** Compatibility alias for Halo's VTag `theme`. */
    theme?: LegacyTheme;
    outlined?: boolean;
    /** VTag-compatible shape switch. M3 chips are rounded by default. */
    rounded?: boolean;
    /** VTag-compatible inline style escape hatch. */
    styles?: CSSProperties;
  }>(),
  { tone: "neutral", rounded: true },
);

const resolvedTone = computed<ChipTone>(() => (props.theme ? legacyThemes[props.theme] : props.tone));
</script>

<template>
  <span
    class="m-chip"
    :class="[`m-chip--${resolvedTone}`, { 'm-chip--outlined': outlined, 'm-chip--rounded': rounded }]"
    :style="styles"
  >
    <span v-if="$slots.leftIcon || $slots.icon" class="m-chip__icon m-chip__icon--left" aria-hidden="true">
      <slot name="leftIcon"><slot name="icon" /></slot>
    </span>
    <span class="m-chip__label"><slot /></span>
    <span v-if="$slots.rightIcon" class="m-chip__icon m-chip__icon--right" aria-hidden="true"
      ><slot name="rightIcon"
    /></span>
  </span>
</template>

<style scoped>
.m-chip {
  --m-chip-background: var(--m-sys-color-surface-container-high);
  --m-chip-color: var(--m-sys-color-on-surface-variant);
  --m-chip-border: transparent;
  display: inline-flex;
  min-height: 2rem;
  align-items: center;
  gap: 0.375rem;
  border: 1px solid var(--m-chip-border);
  border-radius: var(--m-sys-shape-corner-small);
  background: var(--m-chip-background);
  color: var(--m-chip-color);
  font-family: var(--m-sys-font-family);
  font-size: var(--m-sys-typescale-label-medium-size);
  font-weight: 650;
  line-height: var(--m-sys-typescale-label-medium-line-height);
  vertical-align: middle;
}

.m-chip--rounded {
  border-radius: var(--m-sys-shape-corner-full);
}

.m-chip--neutral {
  --m-chip-background: var(--m-sys-color-surface-container-high);
  --m-chip-color: var(--m-sys-color-on-surface-variant);
}

.m-chip--primary {
  --m-chip-background: var(--m-sys-color-primary-container);
  --m-chip-color: var(--m-sys-color-on-primary-container);
}

.m-chip--secondary {
  --m-chip-background: var(--m-sys-color-secondary-container);
  --m-chip-color: var(--m-sys-color-on-secondary-container);
}

.m-chip--success {
  --m-chip-background: var(--m-sys-color-success-container);
  --m-chip-color: var(--m-sys-color-on-success-container);
}

.m-chip--warning {
  --m-chip-background: var(--m-sys-color-warning-container);
  --m-chip-color: var(--m-sys-color-on-warning-container);
}

.m-chip--danger {
  --m-chip-background: var(--m-sys-color-error-container);
  --m-chip-color: var(--m-sys-color-on-error-container);
}

.m-chip--info {
  --m-chip-background: var(--m-sys-color-info-container);
  --m-chip-color: var(--m-sys-color-on-info-container);
}

.m-chip--outlined {
  --m-chip-background: transparent;
  --m-chip-border: color-mix(in srgb, var(--m-chip-color) 45%, var(--m-sys-color-outline-variant));
}

.m-chip__label {
  min-width: 0;
  padding: 0.25rem 0.625rem;
}

.m-chip__icon {
  display: inline-flex;
  width: 0.875rem;
  height: 0.875rem;
  align-items: center;
  justify-content: center;
}

.m-chip__icon--left {
  margin-left: 0.5rem;
  margin-right: -0.25rem;
}

.m-chip__icon--right {
  margin-right: 0.5rem;
  margin-left: -0.25rem;
}

.m-chip__icon :deep(svg) {
  width: 100%;
  height: 100%;
}
</style>
