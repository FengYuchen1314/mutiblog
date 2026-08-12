<script setup lang="ts">
import { computed } from "vue";

type StatusTone = "neutral" | "success" | "warning" | "danger" | "info";
type LegacyState = "default" | "success" | "warning" | "error";

const legacyStates: Record<LegacyState, StatusTone> = {
  default: "neutral",
  success: "success",
  warning: "warning",
  error: "danger",
};

const props = withDefaults(
  defineProps<{
    tone?: StatusTone;
    /** Compatibility alias for Halo's VStatusDot `state`. */
    state?: LegacyState;
    pulse?: boolean;
    /** Compatibility alias for `pulse`. */
    animate?: boolean;
    /** VStatusDot-compatible label, useful without a default slot. */
    text?: string;
  }>(),
  { tone: "neutral" },
);

const resolvedTone = computed<StatusTone>(() => (props.state ? legacyStates[props.state] : props.tone));
const shouldPulse = computed(() => props.pulse || props.animate);
</script>

<template>
  <span class="m-status" :class="[`m-status--${resolvedTone}`, { 'm-status--pulse': shouldPulse }]">
    <span class="m-status__dot" aria-hidden="true"></span>
    <span v-if="text || $slots.text || $slots.default" class="m-status__label">
      <slot name="text"
        ><slot>{{ text }}</slot></slot
      >
    </span>
  </span>
</template>

<style scoped>
.m-status {
  --m-status-color: var(--m-sys-color-outline);
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 0.4375rem;
  color: var(--m-sys-color-on-surface-variant);
  font-size: var(--m-sys-typescale-label-medium-size);
  font-weight: 600;
  line-height: var(--m-sys-typescale-label-medium-line-height);
  vertical-align: middle;
}

.m-status--neutral {
  --m-status-color: var(--m-sys-color-outline);
}

.m-status--success {
  --m-status-color: var(--m-sys-color-success);
}

.m-status--warning {
  --m-status-color: var(--m-sys-color-warning);
}

.m-status--danger {
  --m-status-color: var(--m-sys-color-error);
}

.m-status--info {
  --m-status-color: var(--m-sys-color-info);
}

.m-status__dot {
  position: relative;
  display: inline-block;
  width: 0.625rem;
  height: 0.625rem;
  flex: 0 0 auto;
  border-radius: 50%;
  background: var(--m-status-color);
}

.m-status--pulse .m-status__dot::after {
  position: absolute;
  inset: -0.25rem;
  border: 1px solid var(--m-status-color);
  border-radius: inherit;
  content: "";
  animation: m-status-pulse 1.6s var(--m-sys-motion-easing-standard) infinite;
}

.m-status__label {
  min-width: 0;
}

@keyframes m-status-pulse {
  0% {
    opacity: 0.8;
    transform: scale(0.75);
  }

  70%,
  100% {
    opacity: 0;
    transform: scale(1.35);
  }
}

@media (prefers-reduced-motion: reduce) {
  .m-status--pulse .m-status__dot::after {
    animation: none;
  }
}
</style>
