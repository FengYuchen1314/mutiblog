// @vitest-environment jsdom

import { flushPromises, mount } from "@vue/test-utils";
import { createI18n } from "vue-i18n";
import { createPinia, setActivePinia } from "pinia";
import { nextTick } from "vue";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { api, type LocalesConfig, type UnifiedTask } from "@/api/client";
import en from "@/i18n/locales/en";
import { useSessionStore } from "@/stores/session";
import LocalesView from "./LocalesView.vue";

vi.mock("@/api/client", () => ({
  api: {
    locales: vi.fn(),
    tasks: vi.fn(),
    updateLocales: vi.fn(),
  },
}));

vi.mock("@/components/TaskProgress.vue", () => ({
  default: {
    name: "TaskProgress",
    props: ["taskId"],
    emits: ["update"],
    template: '<div :data-task-id="taskId" />',
  },
}));

const localesMock = vi.mocked(api.locales);
const tasksMock = vi.mocked(api.tasks);
const updateLocalesMock = vi.mocked(api.updateLocales);

const localeConfig: LocalesConfig = {
  sourceLocale: "zh-CN",
  enabled: [
    { code: "zh-CN", label: "Simplified Chinese", enabled: true, status: "ready" },
    { code: "ja", label: "Japanese", enabled: true, status: "provisioning" },
  ],
  fallback: ["zh-CN"],
};

function localeTask(overrides: Partial<UnifiedTask> = {}): UnifiedTask {
  return {
    schemaVersion: 1,
    id: "locale-provision-example",
    kind: "LocaleProvision",
    operation: "localize-site",
    status: "queued",
    progress: { phase: "queued", current: 0, total: 2, percent: 0 },
    createdAt: "2026-08-12T00:00:00Z",
    ...overrides,
  };
}

function mountView() {
  const pinia = createPinia();
  setActivePinia(pinia);
  const session = useSessionStore();
  session.session = { username: "admin", csrfToken: "csrf", expiresAt: "2030-01-01T00:00:00Z", adminLocale: "en" };
  const i18n = createI18n({ legacy: false, locale: "en", messages: { en } });
  return mount(LocalesView, {
    global: {
      plugins: [pinia, i18n],
      stubs: {
        MButton: {
          props: ["loading"],
          emits: ["click"],
          template: '<button :disabled="loading" @click="$emit(\'click\')"><slot /></button>',
        },
        MSurface: { template: "<section><slot /></section>" },
        MPageHeader: { template: '<header><slot /><slot name="actions" /></header>' },
        MChip: { template: "<span><slot /></span>" },
        RouterLink: { props: ["to"], template: "<a><slot /></a>" },
      },
    },
  });
}

describe("LocalesView durable localization task tracking", () => {
  beforeEach(() => {
    localesMock.mockReset();
    tasksMock.mockReset();
    updateLocalesMock.mockReset();
    localesMock.mockResolvedValue(localeConfig);
    tasksMock.mockResolvedValue({ items: [], total: 0, active: 0 });
  });

  test("tracks the 202 LocaleProvision parent immediately and adds its final build child", async () => {
    const queued = localeTask();
    updateLocalesMock.mockResolvedValue({
      locales: localeConfig,
      task: queued,
      localization: { status: "queued", taskId: queued.id },
      build: { status: "deferred" },
    });
    const wrapper = mountView();
    await flushPromises();

    await wrapper.get("button").trigger("click");
    await flushPromises();

    expect(updateLocalesMock).toHaveBeenCalledWith("csrf", localeConfig.enabled, "zh-CN");
    expect(wrapper.text()).toContain(en.localesPage.savedQueued);
    expect(wrapper.findAll("[data-task-id]").map((item) => item.attributes("data-task-id"))).toEqual([queued.id]);

    wrapper.findComponent({ name: "TaskProgress" }).vm.$emit("update", {
      ...queued,
      buildTaskId: "final-build-child",
    });
    await nextTick();

    expect(wrapper.findAll("[data-task-id]").map((item) => item.attributes("data-task-id"))).toEqual([
      queued.id,
      "final-build-child",
    ]);
    wrapper.unmount();
  });

  test("restores the newest LocaleProvision task and its known build child after remount", async () => {
    const restored = localeTask({ status: "running", buildTaskId: "restored-build-child" });
    tasksMock.mockResolvedValue({ items: [restored], total: 1, active: 1 });

    const wrapper = mountView();
    await flushPromises();

    expect(tasksMock).toHaveBeenCalledWith({ kind: "LocaleProvision", limit: 1 });
    expect(wrapper.findAll("[data-task-id]").map((item) => item.attributes("data-task-id"))).toEqual([
      restored.id,
      "restored-build-child",
    ]);
    wrapper.unmount();
  });

  test("keeps a no-op locale save synchronous without assuming a task exists", async () => {
    updateLocalesMock.mockResolvedValue({
      locales: localeConfig,
      localization: { status: "idle" },
      build: { status: "skipped" },
    });
    const wrapper = mountView();
    await flushPromises();

    await wrapper.get("button").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(en.localesPage.saved);
    expect(wrapper.findAll("[data-task-id]")).toHaveLength(0);
    wrapper.unmount();
  });
});
