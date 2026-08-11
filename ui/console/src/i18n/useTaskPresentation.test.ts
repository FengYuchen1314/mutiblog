import { describe, expect, test } from "vitest";
import { createTaskPresentation, normalizeTaskError } from "./useTaskPresentation";

const dictionary: Record<string, string> = {
  taskProgressTranslatedChunk: "chunk {current}/{total}",
  "taskProgress.working": "working",
  "taskProgress.kinds.StaticBuild": "static build",
  "taskProgress.operations.publish": "publish",
  "taskProgress.messages.render": "rendering",
  "taskProgressIndex.kinds.IndexRebuild": "index rebuild",
  "taskProgressIndex.messages.preparing-search-index": "preparing index",
  "taskProgressExtraMessages.storing-imported-backup": "storing backup",
  "codes.running": "running",
  "tasksPage.errors.provider-key-missing": "provider key missing",
  "tasksPage.errors.unknown": "unknown translation error",
  "backupPage.taskErrors.create-failed": "backup creation failed",
  "taskCenter.taskFailed": "task failed",
  "taskProgressWarnings.buildFailed": "build warning",
  "taskProgressWarnings.translationFailed": "translation warning",
  "taskProgressWarnings.translationNotConfigured": "translation configuration warning",
};

function presentation() {
  return createTaskPresentation({
    exists: (key) => Object.hasOwn(dictionary, key),
    translate: (key, values) =>
      Object.entries(values ?? {}).reduce(
        (message, [name, value]) => message.replace(`{${name}}`, String(value)),
        dictionary[key] ?? `missing:${key}`,
      ),
  });
}

describe("task presentation", () => {
  test("shares kind, operation, progress, and status presentation", () => {
    const labels = presentation();

    expect(labels.kindLabel("IndexRebuild")).toBe("index rebuild");
    expect(labels.kindLabel("StaticBuild")).toBe("static build");
    expect(labels.kindLabel("FutureTask")).toBe("FutureTask");
    expect(labels.operationLabel("publish")).toBe("publish");
    expect(labels.progressLabel("translated-chunk-2-of-5")).toBe("chunk 2/5");
    expect(labels.progressLabel("preparing-search-index")).toBe("preparing index");
    expect(labels.progressLabel("render")).toBe("rendering");
    expect(labels.progressLabel("storing-imported-backup")).toBe("storing backup");
    expect(labels.progressLabel("running")).toBe("running");
    expect(labels.progressLabel("future-phase")).toBe("working");
    expect(labels.statusType("succeeded")).toBe("success");
    expect(labels.statusType("needs-review")).toBe("danger");
    expect(labels.statusType("running")).toBe("default");
    expect(labels.progressPercent(-4)).toBe(0);
    expect(labels.progressPercent(108)).toBe(100);
  });

  test("normalizes legacy translation errors and preserves error fallbacks", () => {
    const labels = presentation();

    expect(normalizeTaskError(" provider API key is missing ")).toBe("provider-key-missing");
    expect(labels.taskErrorLabel("provider API key is missing", "taskCenter.taskFailed")).toBe("provider key missing");
    expect(labels.taskErrorLabel("create-failed", "taskCenter.taskFailed")).toBe("backup creation failed");
    expect(labels.taskErrorLabel("future-target-error", "tasksPage.errors.unknown")).toBe("unknown translation error");
    expect(labels.taskErrorLabel("future-task-error", "taskCenter.taskFailed")).toBe("task failed");
  });

  test("distinguishes missing AI configuration from translation failure", () => {
    const labels = presentation();

    expect(labels.taskWarnings({ buildStatus: "failed", translationStatus: "not-configured" })).toEqual([
      "build warning",
      "translation configuration warning",
    ]);
    expect(labels.taskWarnings({ translationStatus: "needs-review" })).toEqual(["translation warning"]);
    expect(labels.taskWarnings({ buildStatus: "succeeded", translationStatus: "queued" })).toEqual([]);
  });
});
