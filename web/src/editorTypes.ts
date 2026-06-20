import type { WorkflowInstanceDeclaration, WorkflowNodeInfo, WorkflowPort } from "./api";
import type { WorkflowTriggerDeclaration } from "./workflowInstances";

export type EditorExtraLib = {
  filePath: string;
  content: string;
};

export function workflowEditorExtraLibs(options: {
  workflowNodes: WorkflowNodeInfo[];
  workflowInstances?: Record<string, WorkflowInstanceDeclaration>;
  workflowTrigger?: WorkflowTriggerDeclaration;
}): EditorExtraLib[] {
  return [
    {
      filePath: "inmemory://autable/workflow-runtime.d.ts",
      content: workflowRuntimeTypes()
    },
    {
      filePath: "inmemory://autable/workflow-nodes.generated.d.ts",
      content: workflowNodeTypes(options.workflowNodes)
    },
    {
      filePath: "inmemory://autable/workflow-instances.generated.d.ts",
      content: workflowInstanceTypes(
        options.workflowInstances ?? {},
        options.workflowNodes,
        options.workflowTrigger
      )
    }
  ];
}

export function formEditorExtraLibs(): EditorExtraLib[] {
  return [
    {
      filePath: "inmemory://autable/form-runtime.d.ts",
      content: `export {};

declare global {
  type AutableFormInputType = "text" | "email" | "search" | "tel" | "url" | "password";
  type AutableFormScannerConfig = { confirm?: boolean };

  type AutableFormElement =
    | { kind: "input"; field: string; label: string; inputType: AutableFormInputType; scanner?: boolean | AutableFormScannerConfig; onChangeActionID?: string }
    | { kind: "select"; field: string; label: string; options: string[] }
    | { kind: "relation"; field: string; label: string; table: string; view?: string }
    | { kind: "button"; id: string; label: string; actionID: string }
    | { kind: "submit"; id: string; label: string; actionID: string }
    | { kind: "html"; html: string };

  interface AutableRowRecord {
    record_id: number;
    values: Record<string, unknown>;
  }

  interface AutableFormRowsAPI {
    create(table: string, values: Record<string, unknown>): Promise<AutableRowRecord>;
    update(table: string, recordID: number, values: Record<string, unknown>): Promise<AutableRowRecord>;
    upsert(table: string, input: { match_field: string; values: Record<string, unknown> }): Promise<AutableRowRecord & { operation: "create" | "update" | "noop" }>;
    list(table: string, options?: {
      view?: string;
      query?: { field: string; op?: string; operator?: string; value?: unknown } | {
        combinator: "and" | "or";
        rules: Array<{ field?: string; operator?: string; value?: unknown; combinator?: "and" | "or"; rules?: unknown[]; not?: boolean }>;
        not?: boolean;
      };
      sorts?: Array<{ field: string; direction: "asc" | "desc" }>;
      limit?: number;
    }): Promise<AutableRowRecord[]>;
  }

  interface AutableFormActionAPI {
    value(field: string): string;
    values(): Record<string, string>;
    setValue(field: string, value: string): void;
    rows: AutableFormRowsAPI;
    show(value: unknown): void;
  }

  type AutableFormAction = (api: AutableFormActionAPI) => unknown | Promise<unknown>;

  interface AutableFormAPI {
    input(config: { field: string; label?: string; type?: AutableFormInputType; scanner?: boolean | AutableFormScannerConfig; onChange?: AutableFormAction }): AutableFormElement;
    relation(config: { field: string; label?: string; table: string; view?: string }): AutableFormElement;
    select(config: { field: string; label?: string; options: string[] }): AutableFormElement;
    button(label: string, action: AutableFormAction): AutableFormElement;
    button(config: { id?: string; label: string; action: AutableFormAction }): AutableFormElement;
    submit(label: string): AutableFormElement;
  }

  interface AutableFormRoot {
    element?: HTMLDivElement;
    append(...items: Array<AutableFormElement | AutableFormElement[] | string | Node>): void;
    appendChild(item: AutableFormElement | string | Node): void;
  }

  interface AutableFormDefinition {
    table: string;
  }

  function stableStringify(value: unknown): string;
  function render(api: AutableFormAPI, root: AutableFormRoot): AutableFormDefinition;
}
`
    }
  ];
}

function workflowRuntimeTypes() {
  return `export {};

declare global {
  type AutablePrimitive = string | number | boolean | null;
  type AutableJSON = AutablePrimitive | AutableJSON[] | { [key: string]: AutableJSON };
  type AutableRecordValues = Record<string, unknown>;

  interface AutableRowRecord {
    record_id: number;
    values: AutableRecordValues;
  }

  interface AutableTriggerRecord {
    history_key: string;
    database: string;
    table: string;
    record_id: number;
    timestamp: number;
  }

  interface AutableWorkflowDefinitionInfo {
    workflow_id?: number;
    database_name: string;
  }

  interface AutableWorkflowRunInputs extends Record<string, unknown> {}

  interface AutableWorkflowRunInfo extends AutableWorkflowDefinitionInfo {
    inputs: AutableWorkflowRunInputs;
    run_id?: string;
    instance(id: string): AutableWorkflowInstance<Record<string, unknown>, Record<string, unknown>>;
  }

  interface AutableWorkflowInstance<Input, Output> {
    id: string;
    node: string;
    exec(input: Input): Output;
  }

  interface AutableWorkflowPort {
    name: string;
    type: string;
    description?: string;
  }

  interface AutableWorkflowInstanceDeclaration {
    node: string;
    variables?: AutableWorkflowPort[];
    secrets?: AutableWorkflowPort[];
    params?: Record<string, unknown>;
  }

  interface AutableWorkflowTriggerDeclaration {
    instance: string;
    params?: Record<string, unknown>;
  }

  function instances(info: AutableWorkflowDefinitionInfo): Record<string, string | AutableWorkflowInstanceDeclaration>;
  function trigger(info: AutableWorkflowDefinitionInfo): AutableWorkflowTriggerDeclaration;
  function stableStringify(value: unknown): string;
  function run(info: AutableWorkflowRunInfo): Record<string, unknown>;
}
`;
}

function workflowNodeTypes(workflowNodes: WorkflowNodeInfo[]) {
  const blocks = workflowNodes.map((node) => {
    const typeName = nodeTypeName(node.type);
    return [
      `interface ${typeName}Input ${portsToObjectType(node.inputs, "  ")}`,
      `interface ${typeName}Output ${portsToObjectType(node.outputs, "  ")}`
    ].join("\n\n");
  });
  return `export {};

declare global {
${indent(blocks.join("\n\n"), "  ")}
}
`;
}

function workflowInstanceTypes(
  workflowInstances: Record<string, WorkflowInstanceDeclaration>,
  workflowNodes: WorkflowNodeInfo[],
  workflowTrigger: WorkflowTriggerDeclaration | undefined
) {
  const nodeByType = new Map(workflowNodes.map((node) => [node.type, node]));
  const overloads = Object.entries(workflowInstances).flatMap(([instanceID, instance]) => {
    const node = nodeByType.get(instance.node);
    if (!node) {
      return [];
    }
    const typeName = nodeTypeName(node.type);
    return [
      `    instance(id: ${JSON.stringify(instanceID)}): AutableWorkflowInstance<${typeName}Input, ${typeName}Output>;`
    ];
  });
  const runInputType = workflowRunInputType(workflowInstances, nodeByType, workflowTrigger);
  if (overloads.length === 0 && !runInputType) {
    return `export {};
`;
  }
  const inputOverride = runInputType ? [`  interface AutableWorkflowRunInputs extends ${runInputType} {}`] : [];
  const infoBlock = overloads.length > 0
    ? [`  interface AutableWorkflowRunInfo {`, ...overloads, `  }`]
    : [];
  return `export {};

declare global {
${[...inputOverride, ...infoBlock].join("\n")}
}
`;
}

function workflowRunInputType(
  workflowInstances: Record<string, WorkflowInstanceDeclaration>,
  nodeByType: Map<string, WorkflowNodeInfo>,
  workflowTrigger: WorkflowTriggerDeclaration | undefined
) {
  if (!workflowTrigger) {
    return "";
  }
  const triggerInstance = workflowInstances[workflowTrigger.instance];
  if (!triggerInstance) {
    return "";
  }
  const triggerNode = nodeByType.get(triggerInstance.node);
  if (!triggerNode || !triggerNode.trigger) {
    return "";
  }
  return `${nodeTypeName(triggerNode.type)}Output`;
}

function portsToObjectType(ports: WorkflowPort[] | undefined, linePrefix: string) {
  if (!ports || ports.length === 0) {
    return "extends Record<string, never> {}";
  }
  return `{\n${ports.map((port) => `${linePrefix}${propertyName(port.name)}?: ${portType(port.type)};`).join("\n")}\n}`;
}

function portType(type: string): string {
  const trimmed = type.trim();
  if (trimmed.endsWith("[]")) {
    return `${portType(trimmed.slice(0, -2))}[]`;
  }
  switch (trimmed) {
    case "string":
      return "string";
    case "int":
    case "int64":
    case "float":
    case "number":
      return "number";
    case "boolean":
    case "bool":
      return "boolean";
    case "any":
      return "unknown";
    case "object":
      return "Record<string, unknown>";
    case "RowRecord":
      return "AutableRowRecord";
    case "TriggerRecord":
      return "AutableTriggerRecord";
    default:
      return "unknown";
  }
}

function nodeTypeName(nodeType: string) {
  const words = nodeType.split(/[^A-Za-z0-9]+/).filter(Boolean);
  const suffix = words.map((word) => word.charAt(0).toUpperCase() + word.slice(1)).join("");
  return `AutableNode${suffix || "Unknown"}`;
}

function propertyName(name: string) {
  return /^[A-Za-z_$][\w$]*$/.test(name) ? name : JSON.stringify(name);
}

function indent(text: string, prefix: string) {
  return text
    .split("\n")
    .map((line) => (line ? prefix + line : line))
    .join("\n");
}
