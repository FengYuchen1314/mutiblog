import { ref, type Ref } from "vue";
import { ApiError, api, createStaticBuildTaskId, type TaskStatus, type UnifiedTask } from "@/api/client";

const terminalStatuses = new Set<TaskStatus>(["succeeded", "failed", "needs-review"]);

export interface BuildTasksTracker {
  taskIds: Ref<string[]>;
  taskLabels: Ref<Record<string, string>>;
  track: (id?: string, label?: string) => string | undefined;
  untrack: (id: string) => void;
  createTask: (label?: string) => string;
  trackTaskChildren: (task: Pick<UnifiedTask, "id" | "buildTaskId" | "translationTaskId">) => string[];
  discardIfMissing: (id: string) => Promise<void>;
  beginOperation: () => Promise<void>;
  reconcile: (provisionalId: string, ...actualIds: Array<string | undefined>) => Promise<void>;
}

export function useBuildTasks(): BuildTasksTracker {
  const taskIds = ref<string[]>([]);
  const taskLabels = ref<Record<string, string>>({});
  // The task endpoint may return 404 until the mutation carrying this provisional ID settles.
  const pendingTaskIds = new Set<string>();

  function track(id?: string, label?: string): string | undefined {
    if (!id) return id;
    if (!taskIds.value.includes(id)) taskIds.value = [...taskIds.value, id];
    if (label && taskLabels.value[id] !== label) taskLabels.value = { ...taskLabels.value, [id]: label };
    return id;
  }

  function untrack(id: string): void {
    pendingTaskIds.delete(id);
    if (!taskIds.value.includes(id)) return;
    taskIds.value = taskIds.value.filter((trackedId) => trackedId !== id);
    if (taskLabels.value[id]) {
      const remaining = { ...taskLabels.value };
      delete remaining[id];
      taskLabels.value = remaining;
    }
  }

  function createTask(label?: string): string {
    const id = createStaticBuildTaskId();
    track(id, label);
    pendingTaskIds.add(id);
    return id;
  }

  function trackTaskChildren(task: Pick<UnifiedTask, "id" | "buildTaskId" | "translationTaskId">): string[] {
    const childIds = [...new Set([task.buildTaskId, task.translationTaskId].filter((id): id is string => Boolean(id)))];
    const label = taskLabels.value[task.id];
    for (const id of childIds) track(id, label);
    return childIds;
  }

  async function discardIfMissing(id: string): Promise<void> {
    pendingTaskIds.delete(id);
    try {
      await api.task(id);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 404) untrack(id);
    }
  }

  async function beginOperation(): Promise<void> {
    const snapshot = taskIds.value.filter((id) => !pendingTaskIds.has(id));
    await Promise.all(
      snapshot.map(async (id) => {
        try {
          const task = await api.task(id);
          if (terminalStatuses.has(task.status)) untrack(id);
        } catch (caught) {
          if (caught instanceof ApiError && caught.status === 404) untrack(id);
        }
      }),
    );
  }

  async function reconcile(provisionalId: string, ...actualIds: Array<string | undefined>): Promise<void> {
    track(provisionalId);
    const label = taskLabels.value[provisionalId];
    for (const id of actualIds) track(id, label);
    await discardIfMissing(provisionalId);
  }

  return {
    taskIds,
    taskLabels,
    track,
    untrack,
    createTask,
    trackTaskChildren,
    discardIfMissing,
    beginOperation,
    reconcile,
  };
}
