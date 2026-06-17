export type FormElement =
  | {
      kind: "input";
      name: string;
      label: string;
      inputType: "text" | "email" | "search" | "tel" | "url" | "password";
    }
  | { kind: "select"; name: string; label: string; options: string[] }
  | { kind: "submit"; label: string }
  | { kind: "html"; html: string };

type InputType = Extract<FormElement, { kind: "input" }>["inputType"];

export type FormRenderResult = {
  elements: FormElement[];
  table?: string;
  fields?: Record<string, string>;
  error?: string;
};

type InputConfig = {
  name: string;
  label?: string;
  type?: string;
};

type SelectConfig = {
  name: string;
  label?: string;
  options?: string[];
};

const inputTypes = new Set<InputType>(["text", "email", "search", "tel", "url", "password"]);

export function renderFormScript(script: string): FormRenderResult {
  const elements: FormElement[] = [];
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
    input: (config: InputConfig): FormElement => ({
      kind: "input",
      name: String(config.name),
      label: config.label ?? config.name,
      inputType: normalizeInputType(config.type)
    }),
    select: (config: SelectConfig): FormElement => ({
      kind: "select",
      name: String(config.name),
      label: config.label ?? config.name,
      options: Array.isArray(config.options) ? config.options.map(String) : []
    }),
    submit: (label: string): FormElement => ({
      kind: "submit",
      label: String(label)
    })
  };

  try {
    const run = new Function("api", "root", `"use strict";\n${script}\nreturn render(api, root);`);
    const returned = run(api, root);
    const definition = formDefinitionFromValue(returned);
    if (rootElement && rootElement.childNodes.length > 0) {
      elements.push({ kind: "html", html: rootElement.innerHTML });
    }
    return { elements, table: definition.table, fields: definition.fields };
  } catch (error) {
    return {
      elements: [],
      error: error instanceof Error ? error.message : "Form script failed"
    };
  }
}

function formDefinitionFromValue(value: unknown): Required<Pick<FormRenderResult, "table" | "fields">> {
  if (!value || typeof value !== "object") {
    throw new Error("form render must return a definition object");
  }
  const maybeDefinition = value as { table?: unknown; fields?: unknown };
  if (typeof maybeDefinition.table !== "string" || !maybeDefinition.fields || typeof maybeDefinition.fields !== "object") {
    throw new Error("form render must return table and fields");
  }
  const fields = Object.fromEntries(
    Object.entries(maybeDefinition.fields as Record<string, unknown>)
      .filter(([, fieldName]) => typeof fieldName === "string")
      .map(([inputID, fieldName]) => [inputID, String(fieldName)])
  );
  if (maybeDefinition.table === "" || Object.keys(fields).length === 0) {
    throw new Error("form render must return table and fields");
  }
  return { table: maybeDefinition.table, fields };
}

function normalizeInputType(value: string | undefined): InputType {
  if (value && inputTypes.has(value as InputType)) {
    return value as InputType;
  }
  return "text";
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
  return kind === "input" || kind === "select" || kind === "submit" || kind === "html";
}
