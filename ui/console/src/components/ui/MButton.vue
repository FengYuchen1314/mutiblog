<script setup lang="ts">
import { computed } from "vue";
import type { RouteLocationRaw } from "vue-router";

/**
 * `type`, `route`, `circle` and `ghost` deliberately mirror the Halo button
 * API. Keeping those aliases makes page-by-page migration safe while new code
 * can use the more explicit Material `variant` and `to` props.
 */
type ButtonVariant = "filled" | "tonal" | "outlined" | "text" | "elevated" | "danger";
type LegacyButtonType = "default" | "primary" | "secondary" | "danger";
type NativeButtonType = "button" | "submit" | "reset";
type ButtonType = ButtonVariant | LegacyButtonType | NativeButtonType;

const legacyVariants: Record<LegacyButtonType, ButtonVariant> = {
  default: "tonal",
  primary: "filled",
  secondary: "outlined",
  danger: "danger",
};
const materialVariants = new Set<ButtonVariant>(["filled", "tonal", "outlined", "text", "elevated", "danger"]);

const props = withDefaults(
  defineProps<{
    variant?: ButtonVariant;
    disabled?: boolean;
    loading?: boolean;
    to?: RouteLocationRaw;
    /** Compatibility alias for `to`. */
    route?: RouteLocationRaw;
    /** A native button type, or the legacy Halo visual type. */
    type?: ButtonType;
    size?: "xs" | "sm" | "md" | "lg";
    block?: boolean;
    /** Compatibility prop for compact icon-only buttons. */
    circle?: boolean;
    /** Compatibility prop. It selects the text treatment unless variant wins. */
    ghost?: boolean;
    ariaLabel?: string;
  }>(),
  { size: "md" },
);

const emit = defineEmits<{ click: [event: MouseEvent] }>();
const isDisabled = computed(() => props.disabled || props.loading);
const destination = computed(() => props.to ?? props.route);
const nativeType = computed<NativeButtonType>(() => {
  const candidate = props.type;
  return candidate === "button" || candidate === "submit" || candidate === "reset" ? candidate : "button";
});
const visualVariant = computed<ButtonVariant>(() => {
  if (props.variant) return props.variant;
  if (props.ghost) return "text";
  if (props.type && materialVariants.has(props.type as ButtonVariant)) return props.type as ButtonVariant;
  if (props.type && props.type in legacyVariants) return legacyVariants[props.type as LegacyButtonType];
  return "tonal";
});

const classes = computed(() => [
  "m-button",
  `m-button--${visualVariant.value}`,
  `m-button--${props.size}`,
  {
    "m-button--block": props.block,
    "m-button--circle": props.circle,
    "m-button--loading": props.loading,
  },
]);

function handleClick(event: MouseEvent, navigate?: (event: MouseEvent) => void) {
  if (isDisabled.value) {
    event.preventDefault();
    event.stopImmediatePropagation();
    return;
  }
  emit("click", event);
  if (!event.defaultPrevented) navigate?.(event);
}
</script>

<template>
  <RouterLink v-if="destination" v-slot="{ href, navigate }" custom :to="destination">
    <a
      :href="href"
      :class="classes"
      :aria-label="ariaLabel"
      :aria-busy="loading || undefined"
      :aria-disabled="isDisabled || undefined"
      :tabindex="isDisabled ? -1 : undefined"
      @click="handleClick($event, navigate)"
    >
      <span v-if="$slots.icon || loading" class="m-button__icon" aria-hidden="true">
        <span v-if="loading" class="m-button__progress"></span>
        <slot v-else name="icon" />
      </span>
      <span class="m-button__label"><slot /></span>
    </a>
  </RouterLink>
  <button
    v-else
    :class="classes"
    :type="nativeType"
    :disabled="isDisabled"
    :aria-label="ariaLabel"
    :aria-busy="loading || undefined"
    @click="handleClick"
  >
    <span v-if="$slots.icon || loading" class="m-button__icon" aria-hidden="true">
      <span v-if="loading" class="m-button__progress"></span>
      <slot v-else name="icon" />
    </span>
    <span class="m-button__label"><slot /></span>
  </button>
</template>

<style scoped>
.m-button {
  --m-button-background: var(--m-sys-color-secondary-container);
  --m-button-color: var(--m-sys-color-on-secondary-container);
  --m-button-border: transparent;
  position: relative;
  display: inline-flex;
  min-width: 5rem;
  min-height: 2.5rem;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  overflow: hidden;
  border: 1px solid var(--m-button-border);
  border-radius: var(--m-sys-shape-corner-full);
  background: var(--m-button-background);
  box-shadow: var(--m-sys-elevation-level0);
  color: var(--m-button-color);
  cursor: pointer;
  font-family: var(--m-sys-font-family);
  font-size: var(--m-sys-typescale-label-large-size);
  font-weight: 650;
  letter-spacing: 0.006em;
  line-height: var(--m-sys-typescale-label-large-line-height);
  text-align: center;
  text-decoration: none;
  transition:
    background-color var(--m-sys-motion-duration-short4) var(--m-sys-motion-easing-standard),
    box-shadow var(--m-sys-motion-duration-short4) var(--m-sys-motion-easing-standard),
    color var(--m-sys-motion-duration-short4) var(--m-sys-motion-easing-standard),
    transform var(--m-sys-motion-duration-short4) var(--m-sys-motion-easing-standard);
  user-select: none;
}

.m-button::before {
  position: absolute;
  inset: 0;
  background: currentcolor;
  content: "";
  opacity: 0;
  pointer-events: none;
  transition: opacity var(--m-sys-motion-duration-short2) var(--m-sys-motion-easing-standard);
}

.m-button:hover::before {
  opacity: var(--m-sys-state-hover-opacity);
}

.m-button:active::before {
  opacity: var(--m-sys-state-pressed-opacity);
}

.m-button:focus-visible {
  outline: none;
  box-shadow: var(--m-sys-focus-ring), var(--m-sys-elevation-level1);
}

.m-button:disabled,
.m-button[aria-disabled="true"] {
  background: color-mix(in srgb, var(--m-sys-color-on-surface) 12%, transparent);
  box-shadow: var(--m-sys-elevation-level0);
  color: color-mix(in srgb, var(--m-sys-color-on-surface) 38%, transparent);
  cursor: not-allowed;
  pointer-events: none;
}

.m-button--filled {
  --m-button-background: var(--m-sys-color-primary);
  --m-button-color: var(--m-sys-color-on-primary);
}

.m-button--tonal {
  --m-button-background: var(--m-sys-color-secondary-container);
  --m-button-color: var(--m-sys-color-on-secondary-container);
}

.m-button--outlined {
  --m-button-background: transparent;
  --m-button-border: var(--m-sys-color-outline);
  --m-button-color: var(--m-sys-color-primary);
}

.m-button--text {
  min-width: auto;
  --m-button-background: transparent;
  --m-button-color: var(--m-sys-color-primary);
}

.m-button--elevated {
  --m-button-background: var(--m-sys-color-surface-container-low);
  --m-button-color: var(--m-sys-color-primary);
  box-shadow: var(--m-sys-elevation-level1);
}

.m-button--danger {
  --m-button-background: var(--m-sys-color-error-container);
  --m-button-color: var(--m-sys-color-on-error-container);
}

.m-button--xs {
  min-width: 3.25rem;
  min-height: 1.875rem;
  padding: 0.25rem 0.625rem;
  font-size: var(--m-sys-typescale-label-medium-size);
}

.m-button--sm {
  min-width: 4.25rem;
  min-height: 2rem;
  padding: 0.375rem 0.875rem;
}

.m-button--md {
  padding: 0.625rem 1.25rem;
}

.m-button--lg {
  min-width: 6.125rem;
  min-height: 3rem;
  padding: 0.75rem 1.5rem;
  font-size: 0.9375rem;
}

.m-button--block {
  display: inline-flex;
  width: 100%;
}

.m-button--circle {
  width: 2.5rem;
  min-width: 2.5rem;
  padding: 0;
}

.m-button--circle.m-button--xs {
  width: 1.875rem;
  min-width: 1.875rem;
}

.m-button--circle.m-button--sm {
  width: 2rem;
  min-width: 2rem;
}

.m-button--circle.m-button--lg {
  width: 3rem;
  min-width: 3rem;
}

.m-button--circle .m-button__label {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  clip-path: inset(50%);
  white-space: nowrap;
}

.m-button__icon {
  z-index: 1;
  display: inline-flex;
  width: 1.125rem;
  height: 1.125rem;
  align-items: center;
  justify-content: center;
  flex: 0 0 auto;
}

.m-button__icon :deep(svg) {
  width: 100%;
  height: 100%;
}

.m-button__label {
  z-index: 1;
}

.m-button__progress {
  display: block;
  width: 1rem;
  height: 1rem;
  border: 2px solid currentcolor;
  border-right-color: transparent;
  border-radius: 50%;
  animation: m-button-spin 750ms linear infinite;
}

@keyframes m-button-spin {
  to {
    transform: rotate(1turn);
  }
}

@media (prefers-reduced-motion: reduce) {
  .m-button__progress {
    animation: none;
  }
}
</style>
