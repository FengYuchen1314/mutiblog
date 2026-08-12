<script setup lang="ts">
withDefaults(
  defineProps<{
    /** Optional for VEmpty compatibility and custom heading slots. */
    title?: string;
    message?: string;
    /** VEmpty-compatible decorative image URL. */
    image?: string;
    compact?: boolean;
  }>(),
  { compact: false },
);
</script>

<template>
  <section class="m-empty-state" :class="{ 'm-empty-state--compact': compact }">
    <div v-if="$slots.image || image || $slots.icon" class="m-empty-state__visual" aria-hidden="true">
      <slot name="image"><img v-if="image" :src="image" alt="" /><slot v-else name="icon" /></slot>
    </div>
    <h2 v-if="title || $slots.title">
      <slot name="title">{{ title }}</slot>
    </h2>
    <p v-if="message || $slots.message" class="m-empty-state__message">
      <slot name="message">{{ message }}</slot>
    </p>
    <div v-if="$slots.actions" class="m-empty-state__actions"><slot name="actions" /></div>
  </section>
</template>

<style scoped>
.m-empty-state {
  display: grid;
  min-height: min(23rem, 55vh);
  place-items: center;
  align-content: center;
  gap: 0.75rem;
  padding: clamp(2rem, 7vw, 4.5rem) 1.25rem;
  color: var(--m-sys-color-on-surface);
  text-align: center;
}

.m-empty-state--compact {
  min-height: 13rem;
  padding: 2rem 1rem;
}

.m-empty-state__visual {
  display: grid;
  width: 5rem;
  height: 5rem;
  place-items: center;
  border-radius: var(--m-sys-shape-corner-extra-large);
  background: var(--m-sys-color-secondary-container);
  color: var(--m-sys-color-on-secondary-container);
}

.m-empty-state__visual :deep(svg) {
  width: 2.5rem;
  height: 2.5rem;
}

.m-empty-state__visual img {
  display: block;
  width: 100%;
  height: 100%;
  border-radius: inherit;
  object-fit: contain;
}

.m-empty-state h2,
.m-empty-state__message {
  margin: 0;
}

.m-empty-state h2 {
  max-width: 34rem;
  font-family: var(--m-sys-font-family);
  font-size: var(--m-sys-typescale-title-large-size);
  font-weight: 650;
  line-height: var(--m-sys-typescale-title-large-line-height);
}

.m-empty-state__message {
  max-width: 34rem;
  color: var(--m-sys-color-on-surface-variant);
  font-size: var(--m-sys-typescale-body-medium-size);
  line-height: var(--m-sys-typescale-body-medium-line-height);
}

.m-empty-state__actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.5rem;
  margin-top: 0.5rem;
}
</style>
