// @vitest-environment jsdom

import { flushPromises, mount } from "@vue/test-utils";
import { createI18n } from "vue-i18n";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { api, type UnifiedTask } from "@/api/client";
import en from "@/i18n/locales/en";
import TaskProgress from "./TaskProgress.vue";

vi.mock("@/api/client", () => {
  class MockApiError extends Error {
    constructor(
      public status: number,
      public code: string,
      message: string,
      public fields?: Record<string, string>,
      public details?: Record<string, unknown>,
    ) {
      super(message);
    }
  }

  return { ApiError: MockApiError, api: { task: vi.fn() } };
});

const taskMock = vi.mocked(api.task);

function task(overrides: Partial<UnifiedTask> = {}): UnifiedTask {
  return {
    schemaVersion: 1,
    id: "locale-provision-example",
    kind: "LocaleProvision",
    operation: "localize-site",
    status: "failed",
    progress: { phase: "site-localization-failed", current: 2, total: 2, percent: 100 },
    error: "site-localization-failed",
    buildTaskId: "build-child-example",
    createdAt: "2026-08-12T00:00:00Z",
    ...overrides,
  };
}

function mountProgress(taskId: string) {
  const i18n = createI18n({ legacy: false, locale: "en", messages: { en } });
  return mount(TaskProgress, { props: { taskId }, global: { plugins: [i18n] } });
}

describe("TaskProgress", () => {
  beforeEach(() => taskMock.mockReset());

  test("shows a durable locale-provision failure and emits its build child metadata", async () => {
    const durableTask = task();
    taskMock.mockResolvedValue(durableTask);

    const wrapper = mountProgress(durableTask.id);
    await flushPromises();

    expect(wrapper.text()).toContain("Failed");
    expect(wrapper.text()).toContain("Whole-site localization failed.");
    expect(wrapper.emitted("update")).toEqual([[durableTask]]);
    expect(wrapper.emitted("terminal")).toEqual([[durableTask]]);
    wrapper.unmount();
  });
});
