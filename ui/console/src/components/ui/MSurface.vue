<script setup lang="ts">
import { computed } from "vue";

type SurfaceVariant = "surface" | "container" | "outlined" | "elevated" | "low" | "high";
type SurfacePadding = "none" | "sm" | "md" | "lg";

const props = withDefaults(
  defineProps<{
    variant?: SurfaceVariant;
    as?: "article" | "section" | "div" | "main" | "aside";
    padding?: SurfacePadding;
    /** VCard-compatible heading prop. Prefer the header slot for rich markup. */
    title?: string;
    /** VCard-compatible class hook for the default content area. */
    bodyClass?: string | string[];
    /** Enables the M3 state layer for a clickable/card-like surface. */
    interactive?: boolean;
  }>(),
  { variant: "container", as: "section", padding: "none" },
);

const classes = computed(() => [
  "m-surface",
  `m-surface--${props.variant}`,
  `m-surface--padding-${props.padding}`,
  { "m-surface--interactive": props.interactive },
]);

const emit = defineEmits<{ click: [event: MouseEvent] }>();

function handleClick(event: MouseEvent) {
  emit("click", event);
}

function handleKeydown(event: KeyboardEvent) {
  if (!props.interactive || (event.key !== "Enter" && event.key !== " ")) return;
  event.preventDefault();
  (event.currentTarget as HTMLElement).click();
}
</script>

<template>
  <component
    :is="as"
    :class="classes"
    :role="interactive ? 'button' : undefined"
    :tabindex="interactive ? 0 : undefined"
    @click="handleClick"
    @keydown="handleKeydown"
  >
    <header v-if="$slots.header || $slots.actions || title" class="m-surface__header">
      <div class="m-surface__heading">
        <slot name="header"
          ><h2 v-if="title">{{ title }}</h2></slot
        >
      </div>
      <div v-if="$slots.actions" class="m-surface__actions"><slot name="actions" /></div>
    </header>
    <div class="m-surface__body" :class="bodyClass"><slot /></div>
    <footer v-if="$slots.footer" class="m-surface__footer"><slot name="footer" /></footer>
  </component>
</template>

<style scoped>
.m-surface {
  --m-surface-background: var(--m-sys-color-surface-container);
  --m-surface-color: var(--m-sys-color-on-surface);
  --m-surface-border: transparent;
  position: relative;
  overflow: hidden;
  border: 1px solid var(--m-surface-border);
  border-radius: var(--m-sys-shape-corner-large);
  background: var(--m-surface-background);
  box-shadow: var(--m-sys-elevation-level0);
  color: var(--m-surface-color);
  transition:
    background-color var(--m-sys-motion-duration-medium1) var(--m-sys-motion-easing-emphasized),
    box-shadow var(--m-sys-motion-duration-medium1) var(--m-sys-motion-easing-emphasized),
    transform var(--m-sys-motion-duration-medium1) var(--m-sys-motion-easing-emphasized);
}

.m-surface--surface {
  --m-surface-background: var(--m-sys-color-surface);
}

.m-surface--low {
  --m-surface-background: var(--m-sys-color-surface-container-low);
}

.m-surface--container {
  --m-surface-background: var(--m-sys-color-surface-container);
}

.m-surface--high {
  --m-surface-background: var(--m-sys-color-surface-container-high);
}

.m-surface--outlined {
  --m-surface-background: var(--m-sys-color-surface);
  --m-surface-border: var(--m-sys-color-outline-variant);
}

.m-surface--elevated {
  --m-surface-background: var(--m-sys-color-surface-container-low);
  box-shadow: var(--m-sys-elevation-level1);
}

.m-surface--interactive {
  cursor: pointer;
}

.m-surface--interactive:hover {
  box-shadow: var(--m-sys-elevation-level2);
  transform: translateY(-1px);
}

.m-surface--interactive:focus-visible {
  outline: none;
  box-shadow: var(--m-sys-focus-ring), var(--m-sys-elevation-level2);
}

.m-surface__header,
.m-surface__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.m-surface__header {
  min-height: 3.75rem;
  border-bottom: 1px solid var(--m-sys-color-outline-variant);
  padding: 0.875rem 1rem;
}

.m-surface__heading {
  min-width: 0;
  color: var(--m-sys-color-on-surface);
  font-family: var(--m-sys-font-family);
  font-size: var(--m-sys-typescale-title-medium-size);
  font-weight: 650;
  line-height: var(--m-sys-typescale-title-medium-line-height);
}

.m-surface__heading :deep(h1),
.m-surface__heading :deep(h2),
.m-surface__heading :deep(h3),
.m-surface__heading :deep(p) {
  margin: 0;
  font: inherit;
}

.m-surface__actions {
  display: flex;
  flex: 0 0 auto;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.5rem;
}

.m-surface__body {
  min-width: 0;
}

.m-surface--padding-none .m-surface__body {
  padding: 0;
}

.m-surface--padding-sm .m-surface__body {
  padding: 0.75rem;
}

.m-surface--padding-md .m-surface__body {
  padding: 1rem;
}

.m-surface--padding-lg .m-surface__body {
  padding: 1.5rem;
}

.m-surface__footer {
  border-top: 1px solid var(--m-sys-color-outline-variant);
  padding: 0.75rem 1rem;
}

@media (max-width: 38rem) {
  .m-surface__header,
  .m-surface__footer {
    align-items: flex-start;
    flex-direction: column;
  }

  .m-surface__actions {
    width: 100%;
    justify-content: flex-start;
  }
}
</style>
