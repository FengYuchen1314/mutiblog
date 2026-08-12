<script setup lang="ts">
withDefaults(
  defineProps<{
    /** Optional so this can replace VPageHeader without a template rewrite. */
    title?: string;
    subtitle?: string;
    eyebrow?: string;
    compact?: boolean;
  }>(),
  { compact: false },
);
</script>

<template>
  <header class="m-page-header" :class="{ 'm-page-header--compact': compact }">
    <div class="m-page-header__leading">
      <span v-if="$slots.icon" class="m-page-header__icon"><slot name="icon" /></span>
      <div class="m-page-header__copy">
        <p v-if="eyebrow" class="m-page-header__eyebrow">{{ eyebrow }}</p>
        <h1 v-if="title || $slots.title">
          <slot name="title">{{ title }}</slot>
        </h1>
        <p v-if="subtitle || $slots.subtitle" class="m-page-header__subtitle">
          <slot name="subtitle">{{ subtitle }}</slot>
        </p>
      </div>
    </div>
    <div v-if="$slots.actions" class="m-page-header__actions"><slot name="actions" /></div>
  </header>
</template>

<style scoped>
.m-page-header {
  display: flex;
  min-height: 7rem;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1.5rem;
  border-bottom: 1px solid var(--m-sys-color-outline-variant);
  background: color-mix(in srgb, var(--m-sys-color-surface) 88%, transparent);
  padding: clamp(1.25rem, 3vw, 2.25rem) clamp(1rem, 4vw, 2.75rem) 1.25rem;
}

.m-page-header--compact {
  min-height: auto;
  padding-top: 1rem;
  padding-bottom: 1rem;
}

.m-page-header__leading {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 1rem;
}

.m-page-header__icon {
  display: grid;
  width: 3rem;
  height: 3rem;
  flex: 0 0 auto;
  place-items: center;
  border-radius: var(--m-sys-shape-corner-medium);
  background: var(--m-sys-color-primary-container);
  color: var(--m-sys-color-on-primary-container);
}

.m-page-header__icon :deep(svg) {
  width: 1.5rem;
  height: 1.5rem;
}

.m-page-header__copy {
  min-width: 0;
}

.m-page-header h1,
.m-page-header__eyebrow,
.m-page-header__subtitle {
  margin: 0;
}

.m-page-header h1 {
  color: var(--m-sys-color-on-surface);
  font-family: var(--m-sys-font-family);
  font-size: var(--m-sys-typescale-headline-small-size);
  font-weight: 650;
  letter-spacing: -0.025em;
  line-height: var(--m-sys-typescale-headline-small-line-height);
}

.m-page-header__eyebrow {
  margin-bottom: 0.25rem;
  color: var(--m-sys-color-primary);
  font-family: var(--m-sys-font-family);
  font-size: var(--m-sys-typescale-label-medium-size);
  font-weight: 700;
  letter-spacing: 0.08em;
  line-height: var(--m-sys-typescale-label-medium-line-height);
  text-transform: uppercase;
}

.m-page-header__subtitle {
  max-width: 46rem;
  margin-top: 0.375rem;
  color: var(--m-sys-color-on-surface-variant);
  font-size: var(--m-sys-typescale-body-medium-size);
  line-height: var(--m-sys-typescale-body-medium-line-height);
}

.m-page-header__actions {
  display: flex;
  flex: 0 0 auto;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.5rem;
}

@media (max-width: 42rem) {
  .m-page-header {
    min-height: auto;
    align-items: flex-start;
    flex-direction: column;
    gap: 1rem;
  }

  .m-page-header__actions {
    width: 100%;
    justify-content: flex-start;
  }
}
</style>
