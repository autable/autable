export type FormElement =
  | {
      kind: "input";
      name: string;
      label: string;
      inputType: "text" | "email" | "search" | "tel" | "url" | "password";
      required: boolean;
    }
  | { kind: "select"; name: string; label: string; options: string[] }
  | { kind: "submit"; label: string; tableName?: string }
  | { kind: "html"; html: string };

type InputType = Extract<FormElement, { kind: "input" }>["inputType"];

export type FormRenderResult = {
  elements: FormElement[];
  error?: string;
};

type InputConfig = {
  name: string;
  label?: string;
  type?: string;
  required?: boolean;
};

type SelectConfig = {
  name: string;
  label?: string;
  options?: string[];
};

type SubmitConfig = {
  table?: string;
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
      inputType: normalizeInputType(config.type),
      required: Boolean(config.required)
    }),
    select: (config: SelectConfig): FormElement => ({
      kind: "select",
      name: String(config.name),
      label: config.label ?? config.name,
      options: Array.isArray(config.options) ? config.options.map(String) : []
    }),
    submit: (label: string, config?: SubmitConfig): FormElement => ({
      kind: "submit",
      label: String(label),
      tableName: config?.table ? String(config.table) : undefined
    })
  };

  try {
    const run = new Function("api", "root", `"use strict";\n${script}`);
    const returned = run(api, root);
    if (isFormElement(returned)) {
      root.append(returned);
    }
    if (rootElement && rootElement.childNodes.length > 0) {
      elements.push({ kind: "html", html: rootElement.innerHTML });
    }
    return { elements };
  } catch (error) {
    return {
      elements: [],
      error: error instanceof Error ? error.message : "Form script failed"
    };
  }
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
