import type { WorkflowInstanceDeclaration } from "./api";

export type WorkflowInstanceInfo = {
  workflow_id?: number;
  database_name: string;
};

export type WorkflowInstanceResult =
  | { ok: true; value: Record<string, WorkflowInstanceDeclaration> }
  | { ok: false; error: string };

export type WorkflowTriggerDeclaration = {
  instance: string;
  params?: Record<string, unknown>;
};

export type WorkflowTriggerResult =
  | { ok: true; value: WorkflowTriggerDeclaration | null }
  | { ok: false; error: string };

export function evaluateWorkflowInstances(script: string, info: WorkflowInstanceInfo): WorkflowInstanceResult {
  try {
    const instances = Function(
      "info",
      `"use strict";\n${script}\nif (typeof instances !== "function") { throw new Error("workflow script must define function instances(info)"); }\nreturn instances(Object.freeze({ ...info }));`
    )(info);
    return { ok: true, value: normalizeWorkflowInstances(instances) };
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : "workflow instances evaluation failed" };
  }
}

export function evaluateWorkflowTrigger(script: string, info: WorkflowInstanceInfo): WorkflowTriggerResult {
  try {
    const trigger = Function(
      "info",
      `"use strict";\n${script}\nif (typeof trigger !== "function") { return null; }\nreturn trigger(Object.freeze({ ...info }));`
    )(info);
    return { ok: true, value: trigger === null ? null : normalizeWorkflowTrigger(trigger) };
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : "workflow trigger evaluation failed" };
  }
}

function normalizeWorkflowInstances(value: unknown): Record<string, WorkflowInstanceDeclaration> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("workflow instances must return an object");
  }
  const instances: Record<string, WorkflowInstanceDeclaration> = {};
  for (const [instanceID, declaration] of Object.entries(value as Record<string, unknown>)) {
    if (!instanceID) {
      throw new Error("workflow instance id is required");
    }
    if (typeof declaration === "string") {
      instances[instanceID] = { node: declaration };
      continue;
    }
    if (!declaration || typeof declaration !== "object" || Array.isArray(declaration)) {
      throw new Error(`workflow instance ${instanceID} must be a node type or object`);
    }
    const raw = declaration as Record<string, unknown>;
    if (typeof raw.node !== "string" || !raw.node) {
      throw new Error(`workflow instance ${instanceID} node is required`);
    }
    instances[instanceID] = {
      node: raw.node,
      variables: Array.isArray(raw.variables) ? raw.variables as WorkflowInstanceDeclaration["variables"] : undefined,
      secrets: Array.isArray(raw.secrets) ? raw.secrets as WorkflowInstanceDeclaration["secrets"] : undefined,
      params: isRecord(raw.params) ? raw.params : undefined
    };
  }
  if (Object.keys(instances).length === 0) {
    throw new Error("workflow instances must declare at least one node instance");
  }
  return instances;
}

function normalizeWorkflowTrigger(value: unknown): WorkflowTriggerDeclaration {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("workflow trigger must return an object");
  }
  const raw = value as Record<string, unknown>;
  if (typeof raw.instance !== "string" || !raw.instance) {
    throw new Error("workflow trigger instance is required");
  }
  return {
    instance: raw.instance,
    params: isRecord(raw.params) ? raw.params : undefined
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}
