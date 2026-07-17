export type FormElement =
  | {
      kind: "input";
      field: string;
      label: string;
      inputType: "text" | "email" | "search" | "tel" | "url" | "password";
      scanner?: boolean | ScannerConfig;
      onChangeActionID?: string;
    }
  | { kind: "select"; field: string; label: string; options: string[] }
  | { kind: "file"; field: string; label: string }
  | { kind: "relation"; field: string; label: string; table: string; view?: string; fields?: string[] }
  | { kind: "button"; id: string; label: string; actionID: string }
  | { kind: "submit"; id: string; label: string; actionID: string }
  | { kind: "html"; html: string };

type InputType = Extract<FormElement, { kind: "input" }>["inputType"];
export type ScannerConfig = {
  confirm?: boolean;
};
export type FormAction = (api: FormActionAPI) => unknown | Promise<unknown>;

export type FormRenderResult = {
  elements: FormElement[];
  table?: string;
  fields?: Record<string, string>;
  actions: Record<string, FormAction>;
  error?: string;
};

type InputConfig = {
  field: string;
  label?: string;
  type?: string;
  scanner?: boolean | ScannerConfig;
  onChange?: FormAction;
};

type SelectConfig = {
  field: string;
  label?: string;
  options?: string[];
};

type FileConfig = {
  field: string;
  label?: string;
};

type RelationConfig = {
  field: string;
  label?: string;
  table: string;
  view?: string;
  fields?: string[];
};

type ButtonConfig = {
  id?: string;
  label: string;
  action: FormAction;
};

export type FormRowsAPI = {
  create(table: string, values: Record<string, unknown>): Promise<unknown>;
  update(table: string, recordID: number, values: Record<string, unknown>): Promise<unknown>;
  upsert(table: string, input: { match_field: string; values: Record<string, unknown> }): Promise<unknown>;
  list(table: string, options?: unknown): Promise<unknown>;
};

export type FormActionAPI = {
  value(field: string): string;
  values(): Record<string, string>;
  setValue(field: string, value: string): void;
  rows: FormRowsAPI;
  show(value: unknown): void;
};

const inputTypes = new Set<InputType>(["text", "email", "search", "tel", "url", "password"]);

export function renderFormScript(script: string): FormRenderResult {
  const elements: FormElement[] = [];
  const actions: Record<string, FormAction> = {};
  let nextActionID = 1;
  const rootElement = typeof document === "undefined" ? undefined : document.createElement("div");
  const root = {
    element: rootElement,
    append: (...items: Array<FormElement | FormElement[] | string | Node>) => {
      for (const item of items.flat()) {
        appendFormItem(elements, rootElement, item);
      }
    },
    appendChild: (item: FormElement | string | Node) => {
      appendFormItem(elements, rootElement, item);
    }
  };
  const api = {
    input: (config: InputConfig): FormElement => {
      const field = formControlField(config);
      const onChangeActionID = typeof config.onChange === "function" ? registerAction(actions, `change_${field}`, config.onChange) : undefined;
      return {
        kind: "input",
        field,
        label: config.label ?? field,
        inputType: normalizeInputType(config.type),
        scanner: normalizeScannerConfig(config.scanner),
        onChangeActionID
      };
    },
    select: (config: SelectConfig): FormElement => {
      const field = formControlField(config);
      return {
        kind: "select",
        field,
        label: config.label ?? field,
        options: Array.isArray(config.options) ? config.options.map(String) : []
      };
    },
    file: (config: FileConfig): FormElement => {
      const field = formControlField(config);
      return {
        kind: "file",
        field,
        label: config.label ?? field
      };
    },
    relation: (config: RelationConfig): FormElement => {
      const field = formControlField(config);
      const fields = normalizeRelationFields(config.fields);
      return {
        kind: "relation",
        field,
        label: config.label ?? field,
        table: String(config.table),
        view: config.view ? String(config.view) : undefined,
        ...(fields ? { fields } : {})
      };
    },
    button: (labelOrConfig: string | ButtonConfig, action?: FormAction): FormElement => {
      const config = normalizeButtonConfig(labelOrConfig, action);
      const actionID = registerAction(actions, config.id ?? `button_${nextActionID++}`, config.action);
      return {
        kind: "button",
        id: config.id ?? actionID,
        label: config.label,
        actionID
      };
    },
    submit: (label: string): FormElement => ({
      kind: "submit",
      id: "submit",
      label: String(label),
      actionID: "submit"
    })
  };

  try {
    const run = new Function("api", "root", "stableStringify", `"use strict";\n${script}\nreturn render(api, root);`);
    const returned = run(api, root, stableStringify);
    const definition = formDefinitionFromValue(returned);
    if (rootElement && rootElement.childNodes.length > 0) {
      elements.push({ kind: "html", html: rootElement.innerHTML });
    }
    const fields = Object.fromEntries(elements.flatMap((element) => ("field" in element ? [[element.field, element.field]] : [])));
    if (!actions.submit && definition.table) {
      actions.submit = async (actionAPI) => actionAPI.rows.create(definition.table, actionAPI.values());
    }
    return { elements, table: definition.table, fields, actions };
  } catch (error) {
    return {
      elements: [],
      actions: {},
      error: error instanceof Error ? error.message : "Form script failed"
    };
  }
}

function stableStringify(value: unknown): string {
  if (value === undefined || value === null) {
    return "";
  }
  if (typeof value !== "object") {
    return String(value);
  }
  const seen: unknown[] = [];
  function normalize(input: unknown): unknown {
    if (input === null || typeof input !== "object") {
      return input;
    }
    const toJSON = (input as { toJSON?: unknown }).toJSON;
    if (typeof toJSON === "function") {
      return normalize(toJSON.call(input));
    }
    if (seen.includes(input)) {
      throw new TypeError("Converting circular structure to JSON");
    }
    seen.push(input);
    try {
      if (Array.isArray(input)) {
        return input.map((item) => {
          const normalized = normalize(item);
          if (normalized === undefined || typeof normalized === "function" || typeof normalized === "symbol") {
            return null;
          }
          return normalized;
        });
      }
      return Object.fromEntries(
        Object.keys(input)
          .sort()
          .flatMap((key) => {
            const normalized = normalize((input as Record<string, unknown>)[key]);
            if (normalized === undefined || typeof normalized === "function" || typeof normalized === "symbol") {
              return [];
            }
            return [[key, normalized]];
          })
      );
    } finally {
      seen.pop();
    }
  }
  return JSON.stringify(normalize(value)) ?? "";
}

function formDefinitionFromValue(value: unknown): Required<Pick<FormRenderResult, "table" | "fields">> {
  if (!value || typeof value !== "object") {
    throw new Error("form render must return a definition object");
  }
  const maybeDefinition = value as { table?: unknown };
  if (typeof maybeDefinition.table !== "string" || maybeDefinition.table === "") {
    throw new Error("form render must return table");
  }
  return { table: maybeDefinition.table, fields: {} };
}

function normalizeInputType(value: string | undefined): InputType {
  if (value && inputTypes.has(value as InputType)) {
    return value as InputType;
  }
  return "text";
}

function normalizeScannerConfig(value: InputConfig["scanner"]): FormElement extends infer T
  ? T extends { kind: "input"; scanner?: infer S }
    ? S
    : never
  : never {
  if (!value) {
    return false;
  }
  if (typeof value === "object") {
    return { confirm: Boolean(value.confirm) };
  }
  return true;
}

function normalizeRelationFields(fields: RelationConfig["fields"]): string[] | undefined {
  if (!Array.isArray(fields)) {
    return undefined;
  }
  const seen = new Set<string>();
  const normalized: string[] = [];
  for (const field of fields) {
    const fieldName = String(field).trim();
    if (!fieldName || seen.has(fieldName)) {
      continue;
    }
    seen.add(fieldName);
    normalized.push(fieldName);
  }
  return normalized;
}

function formControlField(config: unknown): string {
  if (!config || typeof config !== "object") {
    throw new Error("form controls require field");
  }
  const field = (config as { field?: unknown }).field;
  if (typeof field !== "string" || field === "") {
    throw new Error("form controls require field");
  }
  return field;
}

function registerAction(actions: Record<string, FormAction>, requestedID: string, action: FormAction): string {
  let actionID = requestedID || "action";
  let suffix = 2;
  while (actions[actionID]) {
    actionID = `${requestedID}_${suffix}`;
    suffix += 1;
  }
  actions[actionID] = action;
  return actionID;
}

function normalizeButtonConfig(labelOrConfig: string | ButtonConfig, action?: FormAction): ButtonConfig {
  if (typeof labelOrConfig === "string") {
    if (typeof action !== "function") {
      throw new Error("button action is required");
    }
    return { label: labelOrConfig, action };
  }
  if (!labelOrConfig || typeof labelOrConfig !== "object" || typeof labelOrConfig.action !== "function") {
    throw new Error("button action is required");
  }
  return { ...labelOrConfig, label: String(labelOrConfig.label) };
}

function appendFormItem(elements: FormElement[], rootElement: HTMLDivElement | undefined, item: FormElement | string | Node) {
  if (isFormElement(item)) {
    elements.push(item);
    return;
  }
  if (typeof item === "string") {
    elements.push({ kind: "html", html: item });
    return;
  }
  if (rootElement && typeof Node !== "undefined" && item instanceof Node) {
    rootElement.appendChild(item);
  }
}

function isFormElement(value: unknown): value is FormElement {
  if (!value || typeof value !== "object") {
    return false;
  }
  const kind = (value as { kind?: unknown }).kind;
  return kind === "input" || kind === "select" || kind === "file" || kind === "relation" || kind === "button" || kind === "submit" || kind === "html";
}
