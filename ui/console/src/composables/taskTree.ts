import type { TaskRelationRole, UnifiedTask } from "@/api/client";

export interface TaskTreeNode {
  task: UnifiedTask;
  depth: number;
  parentId?: string;
  relationRole?: TaskRelationRole;
  children: TaskTreeNode[];
}

export interface TaskTree {
  roots: TaskTreeNode[];
  parentByTaskId: Map<string, string>;
}

interface ParentReference {
  id: string;
  role: TaskRelationRole;
}

function childReferences(task: UnifiedTask): Array<{ id: string; role: TaskRelationRole }> {
  const references = [
    ...(task.relations ?? []).filter((relation) => relation.role !== "parent"),
    ...(task.buildTaskId ? [{ id: task.buildTaskId, role: "static-build" as const }] : []),
    ...(task.translationTaskId ? [{ id: task.translationTaskId, role: "translation" as const }] : []),
  ];
  return references.filter(
    (reference, index) =>
      reference.id !== task.id && references.findIndex((candidate) => candidate.id === reference.id) === index,
  );
}

function parentReferences(task: UnifiedTask): Array<{ id: string; role: TaskRelationRole }> {
  const references = [
    ...(task.relations ?? []).filter((relation) => relation.role === "parent"),
    ...(task.parentTaskId ? [{ id: task.parentTaskId, role: "parent" as const }] : []),
  ];
  return references.filter(
    (reference, index) =>
      reference.id !== task.id && references.findIndex((candidate) => candidate.id === reference.id) === index,
  );
}

function createsCycle(parentByTaskId: Map<string, ParentReference>, parentID: string, childID: string) {
  let cursor = parentID;
  const seen = new Set<string>();
  while (cursor && !seen.has(cursor)) {
    if (cursor === childID) return true;
    seen.add(cursor);
    cursor = parentByTaskId.get(cursor)?.id ?? "";
  }
  return false;
}

// Build a stable forest from redundant forward and reverse task relations.
// Durable task records can be written in either order, so a missing relation
// simply leaves that task at the root instead of hiding it from the operator.
export function buildTaskTree(tasks: UnifiedTask[]): TaskTree {
  const taskByID = new Map(tasks.map((task) => [task.id, task]));
  const parentByTaskId = new Map<string, ParentReference>();

  function attach(parentID: string, childID: string, role: TaskRelationRole) {
    if (!taskByID.has(parentID) || !taskByID.has(childID) || parentID === childID || parentByTaskId.has(childID))
      return;
    if (createsCycle(parentByTaskId, parentID, childID)) return;
    parentByTaskId.set(childID, { id: parentID, role });
  }

  for (const task of tasks) {
    for (const reference of parentReferences(task)) attach(reference.id, task.id, reference.role);
  }
  for (const task of tasks) {
    for (const reference of childReferences(task)) attach(task.id, reference.id, reference.role);
  }

  const childrenByTaskID = new Map<string, string[]>();
  for (const [childID, parent] of parentByTaskId) {
    const children = childrenByTaskID.get(parent.id) ?? [];
    children.push(childID);
    childrenByTaskID.set(parent.id, children);
  }

  const nodeFor = (task: UnifiedTask, depth: number): TaskTreeNode => {
    const children = (childrenByTaskID.get(task.id) ?? [])
      .map((id) => taskByID.get(id))
      .filter((child): child is UnifiedTask => Boolean(child))
      .map((child) => nodeFor(child, depth + 1));
    const parent = parentByTaskId.get(task.id);
    return { task, depth, parentId: parent?.id, relationRole: parent?.role, children };
  };

  return {
    roots: tasks.filter((task) => !parentByTaskId.has(task.id)).map((task) => nodeFor(task, 0)),
    parentByTaskId: new Map([...parentByTaskId].map(([id, parent]) => [id, parent.id])),
  };
}
