// @vitest-environment jsdom

import { flushPromises, mount } from "@vue/test-utils";
import { createI18n } from "vue-i18n";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { api, type UnifiedTask } from "@/api/client";
import en from "@/i18n/locales/en";
import TaskCenterView from "./TaskCenterView.vue";

vi.mock("@/api/client", () => {
  class MockApiError extends Error {
    constructor(
      public status: number,
      public code: string,
      message: string,
    ) {
      super(message);
    }
  }
  return { ApiError: MockApiError, api: { tasks: vi.fn() } };
});

vi.mock("@halo-dev/components", () => ({
  VCard: { template: "<section><slot /></section>" },
  VEmpty: { props: ["title"], template: "<p>{{ title }}</p>" },
  VPageHeader: { template: "<header><slot /><slot name='actions' /></header>" },
  VTag: { template: "<span><slot /></span>" },
}));

const tasksMock = vi.mocked(api.tasks);

function task(id: string, overrides: Partial<UnifiedTask> = {}): UnifiedTask {
  return {
    schemaVersion: 1,
    id,
    kind: "StaticBuild",
    operation: "rebuild",
    status: "queued",
    progress: { phase: "queued", current: 0, total: 1, percent: 0 },
    createdAt: "2026-08-12T00:00:00Z",
    ...overrides,
  };
}

function mountView() {
  const i18n = createI18n({ legacy: false, locale: "en", messages: { en } });
  return mount(TaskCenterView, {
    global: {
      plugins: [i18n],
      stubs: {
        RouterLink: { props: ["to"], template: "<a><slot /></a>" },
      },
    },
  });
}

describe("TaskCenterView", () => {
  beforeEach(() => tasksMock.mockReset());

  test("shows related work as an expandable task tree with target details and contextual action", async () => {
    const scheduled = task("scheduled", {
      kind: "ScheduledPublish",
      operation: "scheduled-publish",
      subject: { kind: "Post", id: "hello" },
      translationTaskId: "translation",
      relations: [{ id: "translation", role: "translation" }],
      outcome: "published-with-warning",
    });
    const translation = task("translation", {
      kind: "Translation",
      buildTaskId: "build",
      relations: [
        { id: "scheduled", role: "parent" },
        { id: "build", role: "static-build" },
      ],
      targets: [
        {
          locale: "ja",
          status: "failed",
          attempts: 2,
          error: "translation-failed",
          progress: { phase: "translation-target", current: 1, total: 1, percent: 100 },
        },
      ],
    });
    const build = task("build", { relations: [{ id: "translation", role: "parent" }] });
    tasksMock.mockResolvedValue({ items: [scheduled, translation, build], total: 3, active: 2 });

    const wrapper = mountView();
    await flushPromises();

    expect(wrapper.findAll(".task-center-row")).toHaveLength(1);
    expect(wrapper.text()).toContain("Published with warning");
    expect(wrapper.text()).toContain("Open content");

    await wrapper.get(".task-center-expand").trigger("click");
    await flushPromises();
    expect(wrapper.findAll(".task-center-row")).toHaveLength(2);
    expect(wrapper.text()).toContain("ja · Failed");
    expect(wrapper.text()).toContain("2 attempts");

    await wrapper.findAll(".task-center-expand")[1].trigger("click");
    await flushPromises();
    expect(wrapper.findAll(".task-center-row")).toHaveLength(3);
    wrapper.unmount();
  });
});
