// autable app SDK. Vendored into app projects by `autablectl app sdk`; do not edit there.
//
// Every call goes to the autable server that hosts the app, with the
// signed-in user's own permissions. No dependencies; works with any
// framework. Table and field names are checked against ./schema.ts, which
// `autablectl app sdk` generates from the database metadata.

import { DATABASE, TABLE_NAMES } from "./schema.js";
import type { ReadOnlyFields, Tables } from "./schema.js";

export { DATABASE, TABLE_NAMES };
export type { Tables };

/** The SDK version this file implements, sent with every request. */
export const SDK_VERSION = "1.0";

const SDK_HEADER = "X-Autable-SDK";

// Types.

export type TableName = keyof Tables & string;
export type FieldName<T extends TableName> = keyof Tables[T] & string;
/** Field values of one row of table T. */
export type Values<T extends TableName> = Tables[T];
/** Fields usable in filters and sorts: the table's fields and the record id. */
export type QueryField<T extends TableName> = FieldName<T> | "ct_record_id";
type ComputedField<T extends TableName> = T extends keyof ReadOnlyFields ? ReadOnlyFields[T] : never;
/** Values accepted by create, update, and upsert: any subset of the writable fields. */
export type WritableValues<T extends TableName> = Partial<Omit<Tables[T], ComputedField<T>>>;

export interface Row<T extends TableName> {
  record_id: number;
  values: Values<T>;
}

export interface UpsertResult<T extends TableName> extends Row<T> {
  operation: "create" | "update" | "noop";
}

export type Operator =
  | "="
  | "!="
  | "<"
  | "<="
  | ">"
  | ">="
  | "contains"
  | "doesNotContain"
  | "beginsWith"
  | "doesNotBeginWith"
  | "endsWith"
  | "doesNotEndWith"
  | "null"
  | "notNull";

/**
 * One comparison. The value "$current_user" stands for the signed-in
 * user's email. "null" and "notNull" take no value and treat "" as empty.
 */
export interface Condition<T extends TableName> {
  field: QueryField<T>;
  operator: Operator;
  value?: unknown;
}

export interface Group<T extends TableName> {
  combinator: "and" | "or";
  rules: Filter<T>[];
  not?: boolean;
}

export type Filter<T extends TableName> = Condition<T> | Group<T>;

export interface Sort<T extends TableName> {
  field: QueryField<T>;
  direction: "asc" | "desc";
}

export interface ListOptions<T extends TableName> {
  /** Narrows the rows; combined with the view's own filter. */
  filter?: Filter<T>;
  /** Replaces the view's sort order. */
  sorts?: Sort<T>[];
  /** A saved view of the table; the default "all" needs a grant like any other view. */
  view?: string;
  /** Full-text search across the readable fields. */
  search?: string;
  limit?: number;
  /** Rows to skip; needs a limit. */
  offset?: number;
}

export interface PageOptions<T extends TableName> extends ListOptions<T> {
  limit: number;
}

export interface Page<T extends TableName> {
  rows: Row<T>[];
  /** Rows matching the filter, view, and search, ignoring limit and offset. */
  total: number;
}

export interface RowChange<T extends TableName> {
  history_key: string;
  database: string;
  table: string;
  record_id: number;
  /** Milliseconds since the Unix epoch. */
  timestamp: number;
  operation?: string;
  values: Partial<Values<T>>;
  diff?: { [K in FieldName<T>]?: { old: Values<T>[K]; new: Values<T>[K] } };
  actor_id?: string;
}

export interface User {
  id: string;
  email: string;
  display_name: string;
  provider: string;
}

export interface FileRecord {
  id: number;
  name: string;
  size: number;
  content_type: string;
  creator_id?: string;
  database_name: string;
  table_name: string;
  record_id: number;
  created_at: number;
}

export interface WorkflowStep {
  node_id: string;
  node_type?: string;
  runner?: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  error?: string;
}

export interface WorkflowRun {
  history_key: string;
  run: {
    workflow_id: number;
    timestamp: number;
    inputs?: Record<string, unknown>;
    outputs?: Record<string, unknown>;
    steps: WorkflowStep[];
    error?: string;
  };
}

/** A failed request. message is the server's explanation. */
export class AutableError extends Error {
  readonly status: number;
  readonly body: unknown;

  constructor(status: number, message: string, body?: unknown) {
    super(message);
    this.name = "AutableError";
    this.status = status;
    this.body = body;
  }
}

// Requests.

function servedByAutable(): boolean {
  return new URL(document.baseURI).pathname.startsWith("/apps/");
}

async function failure(response: Response): Promise<AutableError> {
  const text = await response.text();
  let body: unknown = text;
  try {
    body = JSON.parse(text);
  } catch {
    // Not JSON: keep the text.
  }
  const serverMessage =
    body && typeof body === "object" && typeof (body as { error?: unknown }).error === "string"
      ? (body as { error: string }).error
      : text.trim() || `${response.status} ${response.statusText}`;
  if (response.status === 401) {
    if (servedByAutable()) {
      const here = window.location.pathname + window.location.search + window.location.hash;
      window.location.assign("/login?redirect=" + encodeURIComponent(here));
      return new AutableError(401, "Not signed in; redirecting to the login page.", body);
    }
    return new AutableError(401, "Not signed in: start the dev server with `autablectl app dev`, and run `autablectl login` if your login expired.", body);
  }
  if (response.status === 410) {
    return new AutableError(410, `This app needs to be rebuilt with a newer SDK: ${serverMessage}`, body);
  }
  return new AutableError(response.status, serverMessage, body);
}

async function call<R>(method: string, path: string, body?: unknown): Promise<R> {
  const headers: Record<string, string> = { [SDK_HEADER]: SDK_VERSION, Accept: "application/json" };
  let payload: BodyInit | undefined;
  if (body instanceof FormData) {
    payload = body;
  } else if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const response = await fetch(path, { method, headers, body: payload, credentials: "same-origin" });
  if (!response.ok) {
    throw await failure(response);
  }
  if (response.status === 204) {
    return undefined as R;
  }
  return (await response.json()) as R;
}

const segment = encodeURIComponent;

function rowsPath(name: string, ...rest: (string | number)[]): string {
  return ["/api/tables", segment(DATABASE), segment(name), "rows", ...rest.map((part) => segment(String(part)))].join("/");
}

function toQuery<T extends TableName>(filter: Filter<T> | undefined): Group<T> | undefined {
  if (!filter) {
    return undefined;
  }
  return "rules" in filter ? filter : { combinator: "and", rules: [filter] };
}

function listBody<T extends TableName>(options: ListOptions<T>): Record<string, unknown> {
  return {
    view: options.view,
    query: toQuery(options.filter),
    sorts: options.sorts && options.sorts.length > 0 ? options.sorts : undefined,
    search: options.search || undefined,
    limit: options.limit,
    offset: options.offset || undefined,
  };
}

// Tables.

export interface TableClient<T extends TableName> {
  readonly name: T;
  /** Rows of the table (or of options.view), filtered and sorted. */
  list(options?: ListOptions<T>): Promise<Row<T>[]>;
  /** Shorthand for list({ ...options, filter }). */
  query(filter: Filter<T>, options?: Omit<ListOptions<T>, "filter">): Promise<Row<T>[]>;
  /** One page of rows plus the total count, for paginated tables. */
  page(options: PageOptions<T>): Promise<Page<T>>;
  /** The row with this record id, or null when it does not exist or is not visible. */
  get(recordId: number): Promise<Row<T> | null>;
  create(values: WritableValues<T>): Promise<Row<T>>;
  /** Changes the given fields; the others keep their values. */
  update(recordId: number, values: WritableValues<T>): Promise<Row<T>>;
  /** Updates the row whose matchField equals values[matchField], or creates one. */
  upsert(matchField: FieldName<T>, values: WritableValues<T>): Promise<UpsertResult<T>>;
  /** Deletes the row and returns its last values. */
  delete(recordId: number): Promise<Row<T>>;
  /** Every change to the row, oldest first. */
  history(recordId: number): Promise<RowChange<T>[]>;
}

/** Access to one table of the app's database. */
export function table<T extends TableName>(name: T): TableClient<T> {
  const list = (options: ListOptions<T> = {}) => call<Row<T>[]>("POST", rowsPath(name, "query"), listBody(options));
  return {
    name,
    list,
    query: (filter, options = {}) => list({ ...options, filter }),
    page: (options) => call<Page<T>>("POST", rowsPath(name, "page"), listBody(options)),
    async get(recordId) {
      const rows = await list({ filter: { field: "ct_record_id", operator: "=", value: recordId }, limit: 1 });
      return rows[0] ?? null;
    },
    create: (values) => call<Row<T>>("POST", rowsPath(name), { values }),
    update: (recordId, values) => call<Row<T>>("PATCH", rowsPath(name, recordId), { values }),
    upsert: (matchField, values) => call<UpsertResult<T>>("POST", rowsPath(name, "upsert"), { match_field: matchField, values }),
    delete: (recordId) => call<Row<T>>("DELETE", rowsPath(name, recordId)),
    history: (recordId) => call<RowChange<T>[]>("GET", rowsPath(name, recordId, "history")),
  };
}

/** Splits a multi-valued option field ("a,b") into its options. */
export function splitOptions(value: string | null | undefined): string[] {
  return value ? value.split(",") : [];
}

/** Joins options for writing a multi-valued option field. */
export function joinOptions(options: readonly string[]): string {
  return options.join(",");
}

// Files.

export interface UploadOptions {
  /** The table whose file fields will hold the file; the user needs access to it. */
  table: TableName;
  /** The row the file is for, when it already exists. */
  recordId?: number;
  /** File name; defaults to the File's own name. */
  name?: string;
}

export const files = {
  /** Uploads a file. Store the returned id in a file field. */
  upload(file: Blob, options: UploadOptions): Promise<FileRecord> {
    const form = new FormData();
    form.append("file", file, options.name ?? (file instanceof File ? file.name : "file"));
    form.append("database_name", DATABASE);
    form.append("table_name", options.table);
    if (options.recordId !== undefined) {
      form.append("record_id", String(options.recordId));
    }
    return call<FileRecord>("POST", "/api/files", form);
  },
  /** URL that downloads (or, for images and PDFs, shows) a file. */
  url(id: number): string {
    return `/api/files/${segment(String(id))}`;
  },
  /** Names, sizes, and types of files; ids the user cannot see are left out. */
  metadata(ids: number[]): Promise<FileRecord[]> {
    return call<FileRecord[]>("POST", "/api/files/metadata", { ids });
  },
};

// Users.

/** The signed-in user. */
export function currentUser(): Promise<User> {
  return call<User>("GET", "/api/auth/me");
}

/** Users whose name or email matches query (at most 20). */
export function searchUsers(query = ""): Promise<User[]> {
  return call<User[]>("GET", `/api/users?query=${encodeURIComponent(query)}`);
}

// Workflows.

/**
 * Runs a workflow of the app's database by name and waits for it to
 * finish. The user needs write access to the workflow. A failed run
 * throws an AutableError whose body is the WorkflowRun.
 */
export async function runWorkflow(name: string, inputs: Record<string, unknown> = {}): Promise<WorkflowRun> {
  const workflows = await call<{ id: number; name: string }[]>("GET", `/api/databases/${segment(DATABASE)}/workflows`);
  const workflow = workflows.find((candidate) => candidate.name === name);
  if (!workflow) {
    throw new AutableError(404, `Workflow ${JSON.stringify(name)} not found in database ${JSON.stringify(DATABASE)} (or not readable).`);
  }
  try {
    return await call<WorkflowRun>("POST", `/api/workflows/${workflow.id}/runs`, { inputs });
  } catch (error) {
    const run = error instanceof AutableError ? (error.body as Partial<WorkflowRun> | undefined)?.run : undefined;
    if (error instanceof AutableError && run) {
      throw new AutableError(error.status, `Workflow ${JSON.stringify(name)} failed: ${run.error ?? "unknown error"}`, error.body);
    }
    throw error;
  }
}

// Location.

/**
 * The path the app is served under, without a trailing slash: for example
 * "/apps/sales/orders" or, in a preview, "/apps/sales/orders/@3"; "/" on a
 * dev server. Pass it to the router as its base name.
 */
export function basePath(): string {
  const path = new URL(document.baseURI).pathname.replace(/\/+$/, "");
  return path === "" ? "/" : path;
}
