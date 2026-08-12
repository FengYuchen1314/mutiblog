import { describe, expect, test } from "vitest";
import type { UnifiedTask } from "@/api/client";
import { buildTaskTree } from "./taskTree";

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

describe("buildTaskTree", () => {
  test("groups redundant scheduled, translation, and static-build links exactly once", () => {
    const parent = task("scheduled", {
      kind: "ScheduledPublish",
      buildTaskId: "build",
      translationTaskId: "translation",
      relations: [
        { id: "build", role: "static-build" },
        { id: "translation", role: "translation" },
      ],
    });
    const translation = task("translation", {
      kind: "Translation",
      buildTaskId: "build",
      relations: [
        { id: "scheduled", role: "parent" },
        { id: "build", role: "static-build" },
      ],
    });
    const build = task("build", {
      relations: [{ id: "translation", role: "parent" }],
    });

    const tree = buildTaskTree([parent, translation, build]);

    expect(tree.roots.map((node) => node.task.id)).toEqual(["scheduled"]);
    expect(tree.roots[0].children.map((node) => node.task.id)).toEqual(["translation"]);
    expect(tree.roots[0].children[0].children.map((node) => node.task.id)).toEqual(["build"]);
    expect(tree.parentByTaskId.get("translation")).toBe("scheduled");
    expect(tree.parentByTaskId.get("build")).toBe("translation");
  });

  test("keeps a task visible when its parent record is outside the filtered result", () => {
    const tree = buildTaskTree([
      task("build", { parentTaskId: "not-in-filter", relations: [{ id: "not-in-filter", role: "parent" }] }),
    ]);

    expect(tree.roots.map((node) => node.task.id)).toEqual(["build"]);
  });
});
