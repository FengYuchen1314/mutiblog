import { useI18n } from "vue-i18n";
import type { UnifiedTask } from "@/api/client";

type TranslationValues = Record<string, string | number>;

export interface TaskMessageResolver {
  exists: (key: string) => boolean;
  translate: (key: string, values?: TranslationValues) => string;
}

const legacyTaskErrors: Record<string, string> = {
  "source content changed before translation started": "source-changed-before-start",
  "one or more target languages failed": "target-failed",
  "translations saved but static rebuild failed": "rebuild-failed",
  "source content changed during translation": "source-changed",
  "manual translation is protected": "manual-protected",
  "provider API key is missing": "provider-key-missing",
  "provider request failed": "provider-request-failed",
  "provider configuration is unavailable": "provider-unavailable",
  "provider output did not preserve protected Markdown": "unsafe-output",
  "translation failed": "translation-failed",
};

export function normalizeTaskError(value: string) {
  const trimmed = value.trim();
  return legacyTaskErrors[trimmed] ?? trimmed;
}

export function createTaskPresentation(messages: TaskMessageResolver) {
  function translated(key: string, fallback: string) {
    return messages.exists(key) ? messages.translate(key) : fallback;
  }

  function taskProgressTranslated(key: string, fallback: string) {
    const indexKey = `taskProgressIndex.${key}`;
    return messages.exists(indexKey) ? messages.translate(indexKey) : translated(`taskProgress.${key}`, fallback);
  }

  function kindLabel(value: string) {
    return taskProgressTranslated(`kinds.${value}`, value);
  }

  function operationLabel(value: string) {
    return taskProgressTranslated(`operations.${value}`, value);
  }

  function progressLabel(value: string) {
    const translatedChunk = /^translated-chunk-(\d+)-of-(\d+)$/.exec(value);
    if (translatedChunk) {
      return messages.translate("taskProgressTranslatedChunk", {
        current: translatedChunk[1],
        total: translatedChunk[2],
      });
    }
    const keys = [
      `taskProgressIndex.messages.${value}`,
      `taskProgress.messages.${value}`,
      `taskProgressExtraMessages.${value}`,
      `codes.${value}`,
    ];
    const key = keys.find(messages.exists);
    return key ? messages.translate(key) : messages.translate("taskProgress.working");
  }

  function taskErrorLabel(value: string, fallbackKey = "taskProgress.unavailable") {
    const code = normalizeTaskError(value);
    const keys = [
      `taskProgressIndex.messages.${code}`,
      `tasksPage.errors.${code}`,
      `backupPage.taskErrors.${code}`,
      `taskProgress.messages.${code}`,
      `taskProgressExtraMessages.${code}`,
    ];
    const key = keys.find(messages.exists);
    return messages.translate(key ?? fallbackKey);
  }

  function taskWarnings(task: Pick<UnifiedTask, "buildStatus" | "translationStatus">) {
    const warnings: string[] = [];
    if (["failed", "unavailable"].includes(task.buildStatus ?? "")) {
      warnings.push(messages.translate("taskProgressWarnings.buildFailed"));
    }
    if (["failed", "needs-review"].includes(task.translationStatus ?? "")) {
      warnings.push(messages.translate("taskProgressWarnings.translationFailed"));
    } else if (task.translationStatus === "not-configured") {
      warnings.push(messages.translate("taskProgressWarnings.translationNotConfigured"));
    }
    return warnings;
  }

  function statusType(value: string): "success" | "danger" | "default" {
    if (value === "succeeded") return "success";
    if (value === "failed" || value === "needs-review") return "danger";
    return "default";
  }

  function progressPercent(value?: number) {
    return Math.max(0, Math.min(100, value ?? 0));
  }

  return {
    kindLabel,
    operationLabel,
    progressLabel,
    progressPercent,
    statusType,
    taskErrorLabel,
    taskWarnings,
  };
}

export function useTaskPresentation() {
  const { t, te } = useI18n();
  return createTaskPresentation({
    exists: (key) => te(key),
    translate: (key, values) => String(values ? t(key, values) : t(key)),
  });
}
