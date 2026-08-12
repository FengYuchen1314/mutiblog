import { beforeEach, describe, expect, test, vi } from "vitest";
import { ApiError, api, createStaticBuildTaskId, type TaskStatus, type UnifiedTask } from "@/api/client";
import { useBuildTasks } from "./useBuildTasks";

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

  return {
    ApiError: MockApiError,
    api: { task: vi.fn() },
    createStaticBuildTaskId: vi.fn(),
  };
});

const taskMock = vi.mocked(api.task);
const createTaskIdMock = vi.mocked(createStaticBuildTaskId);

function task(id: string, status: TaskStatus): UnifiedTask {
  return {
    schemaVersion: 1,
    id,
    kind: "StaticBuild",
    operation: "publish",
    status,
    progress: { phase: status, current: 0, total: 1, percent: 0 },
    createdAt: "2026-08-11T00:00:00Z",
  };
}

describe("useBuildTasks", () => {
  beforeEach(() => {
    taskMock.mockReset();
    createTaskIdMock.mockReset();
  });

  test("tracks task IDs additively without duplicates", () => {
    createTaskIdMock.mockReturnValueOnce("generated-task");
    const tracker = useBuildTasks();

    expect(tracker.track()).toBeUndefined();
    expect(tracker.track("existing-task", "Existing article")).toBe("existing-task");
    tracker.track("existing-task");
    expect(tracker.createTask("Generated article")).toBe("generated-task");
    expect(tracker.taskIds.value).toEqual(["existing-task", "generated-task"]);
    expect(tracker.taskLabels.value).toEqual({
      "existing-task": "Existing article",
      "generated-task": "Generated article",
    });

    tracker.untrack("existing-task");
    tracker.untrack("unknown-task");
    expect(tracker.taskIds.value).toEqual(["generated-task"]);
    expect(tracker.taskLabels.value).toEqual({ "generated-task": "Generated article" });
  });

  test("tracks build and translation children reported by a durable parent", () => {
    const tracker = useBuildTasks();
    tracker.track("locale-provision-parent", "Whole-site localization");

    expect(
      tracker.trackTaskChildren({
        id: "locale-provision-parent",
        buildTaskId: "build-child",
        translationTaskId: "translation-child",
      }),
    ).toEqual(["build-child", "translation-child"]);
    expect(
      tracker.trackTaskChildren({
        id: "locale-provision-parent",
        buildTaskId: "build-child",
        translationTaskId: "build-child",
      }),
    ).toEqual(["build-child"]);
    expect(tracker.taskIds.value).toEqual(["locale-provision-parent", "build-child", "translation-child"]);
    expect(tracker.taskLabels.value).toEqual({
      "locale-provision-parent": "Whole-site localization",
      "build-child": "Whole-site localization",
      "translation-child": "Whole-site localization",
    });
  });

  test("discardIfMissing removes only an HTTP 404 task", async () => {
    createTaskIdMock.mockReturnValueOnce("missing-task");
    const tracker = useBuildTasks();
    tracker.createTask();
    tracker.track("server-error-task");
    tracker.track("network-error-task");

    taskMock.mockRejectedValueOnce(new ApiError(404, "task_not_found", "missing"));
    await tracker.discardIfMissing("missing-task");
    taskMock.mockRejectedValueOnce(new ApiError(500, "internal_error", "failed"));
    await tracker.discardIfMissing("server-error-task");
    taskMock.mockRejectedValueOnce(new Error("offline"));
    await tracker.discardIfMissing("network-error-task");

    expect(tracker.taskIds.value).toEqual(["server-error-task", "network-error-task"]);
  });

  test("beginOperation prunes terminal and missing tasks while preserving active and uncertain tasks", async () => {
    const tracker = useBuildTasks();
    const statuses = new Map<string, TaskStatus>([
      ["queued-task", "queued"],
      ["running-task", "running"],
      ["succeeded-task", "succeeded"],
      ["failed-task", "failed"],
      ["review-task", "needs-review"],
    ]);
    for (const id of [...statuses.keys(), "missing-task", "transient-task"]) tracker.track(id);

    taskMock.mockImplementation(async (id) => {
      if (id === "missing-task") throw new ApiError(404, "task_not_found", "missing");
      if (id === "transient-task") throw new ApiError(503, "unavailable", "try again");
      return task(id, statuses.get(id)!);
    });

    await tracker.beginOperation();

    expect(tracker.taskIds.value).toEqual(["queued-task", "running-task", "transient-task"]);
  });

  test("beginOperation cannot prune a provisional task before its request settles", async () => {
    createTaskIdMock.mockReturnValueOnce("provisional-task");
    taskMock.mockRejectedValue(new ApiError(404, "task_not_found", "missing"));
    const tracker = useBuildTasks();
    const provisionalId = tracker.createTask();

    await tracker.beginOperation();

    expect(taskMock).not.toHaveBeenCalled();
    expect(tracker.taskIds.value).toEqual(["provisional-task"]);

    await tracker.reconcile(provisionalId);

    expect(taskMock).toHaveBeenCalledOnce();
    expect(taskMock).toHaveBeenCalledWith("provisional-task");
    expect(tracker.taskIds.value).toEqual([]);
  });

  test("overlapping beginOperation calls never overwrite newer task IDs", async () => {
    let resolveFirstLookup!: (value: UnifiedTask) => void;
    const firstLookup = new Promise<UnifiedTask>((resolve) => {
      resolveFirstLookup = resolve;
    });
    let oldLookupCount = 0;
    taskMock.mockImplementation((id) => {
      if (id === "old-task" && oldLookupCount++ === 0) return firstLookup;
      return Promise.resolve(task(id, id === "old-task" ? "succeeded" : "running"));
    });
    const tracker = useBuildTasks();
    tracker.track("old-task");

    const firstSweep = tracker.beginOperation();
    tracker.track("new-task");
    const secondSweep = tracker.beginOperation();
    await secondSweep;
    tracker.track("latest-task");

    resolveFirstLookup(task("old-task", "succeeded"));
    await firstSweep;

    expect(tracker.taskIds.value).toEqual(["new-task", "latest-task"]);
  });

  test("reconcile retains a real provisional task and adds every reported task", async () => {
    const tracker = useBuildTasks();
    tracker.track("provisional-task", "Article subject");
    taskMock.mockResolvedValueOnce(task("provisional-task", "running"));

    await tracker.reconcile("provisional-task", "scheduled-task", "translation-task", undefined, "");

    expect(tracker.taskIds.value).toEqual(["provisional-task", "scheduled-task", "translation-task"]);
    expect(tracker.taskLabels.value).toEqual({
      "provisional-task": "Article subject",
      "scheduled-task": "Article subject",
      "translation-task": "Article subject",
    });

    tracker.track("unused-provisional-task");
    taskMock.mockRejectedValueOnce(new ApiError(404, "task_not_found", "missing"));
    await tracker.reconcile("unused-provisional-task", "scheduled-task");

    expect(tracker.taskIds.value).toEqual(["provisional-task", "scheduled-task", "translation-task"]);
  });
});
