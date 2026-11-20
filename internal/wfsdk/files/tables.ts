// autable workflow SDK. Vendored into workflow projects by `autablectl workflow sdk`; do not edit there.
//
// Tables, users, and files of the workflow's database, executed by the
// server as the workflow's own subject, `workflow:<id>`. That subject has
// only the grants an admin gave it (docs/permissions.md): a call outside
// them rejects with an Error named "PermissionError", and a record outside
// its row set with one named "NotFoundError". Table and field names are
// checked against ./schema.ts, which `autablectl workflow sdk` generates.
// The types match the app SDK's.

import { DATABASE, TABLE_NAMES } from "./schema.js";
import type { ReadOnlyFields, Tables } from "./schema.js";
import { host } from "./host.js";
import type { BytesOrString } from "./host.js";

export { DATABASE, TABLE_NAMES };
export type { Tables };

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
  /** Fields the workflow may not read are missing. */
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

/** One comparison. "null" and "notNull" take no value and treat "" as empty. */
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
  /** A saved view of the table; the workflow needs a grant on it like on any other view. */
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

export type ChangeOperation = "create" | "update" | "delete";

export interface RowChange<T extends TableName> {
  history_key: string;
  database: string;
  table: T;
  record_id: number;
  /** Milliseconds since the Unix epoch. */
  timestamp: number;
  operation?: ChangeOperation;
  /** The row after the change (before it, for a delete). */
  values: Partial<Values<T>>;
  /** Each changed field with its old and new value. */
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

function toQuery<T extends TableName>(filter: Filter<T> | undefined): Group<T> | undefined {
  if (!filter) {
    return undefined;
  }
  return "rules" in filter ? filter : { combinator: "and", rules: [filter] };
}

function listArgs<T extends TableName>(options: ListOptions<T>): Record<string, unknown> {
  const args: Record<string, unknown> = {};
  if (options.view) args.view = options.view;
  const query = toQuery(options.filter);
  if (query) args.query = query;
  if (options.sorts && options.sorts.length > 0) args.sorts = options.sorts;
  if (options.search) args.search = options.search;
  if (options.limit !== undefined) args.limit = options.limit;
  if (options.offset) args.offset = options.offset;
  return args;
}

export interface TableClient<T extends TableName> {
  readonly name: T;
  /** Rows of the table (or of options.view), filtered and sorted. */
  list(options?: ListOptions<T>): Promise<Row<T>[]>;
  /** Shorthand for list({ ...options, filter }). */
  query(filter: Filter<T>, options?: Omit<ListOptions<T>, "filter">): Promise<Row<T>[]>;
  /** One page of rows plus the total count. */
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

/**
 * Access to one table of the workflow's database.
 *
 * ```ts
 * const open = await tables("orders").query({ field: "status", operator: "=", value: "open" });
 * await tables("orders").update(open[0].record_id, { status: "sent" });
 * ```
 */
export function tables<T extends TableName>(name: T): TableClient<T> {
  const call = <R>(op: string, args: Record<string, unknown>) => host().table(op, name, args) as Promise<R>;
  const list = (options: ListOptions<T> = {}) => call<Row<T>[]>("list", listArgs(options));
  return {
    name,
    list,
    query: (filter, options = {}) => list({ ...options, filter }),
    page: (options) => call<Page<T>>("page", listArgs(options)),
    async get(recordId) {
      const rows = await list({ filter: { field: "ct_record_id", operator: "=", value: recordId }, limit: 1 });
      return rows[0] ?? null;
    },
    create: (values) => call<Row<T>>("create", { values }),
    update: (recordId, values) => call<Row<T>>("update", { record_id: recordId, values }),
    upsert: (matchField, values) => call<UpsertResult<T>>("upsert", { match_field: matchField, values }),
    delete: (recordId) => call<Row<T>>("delete", { record_id: recordId }),
    history: (recordId) => call<RowChange<T>[]>("history", { record_id: recordId }),
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

export const users = {
  /** The user with this id, or null. */
  get(id: string): Promise<User | null> {
    return host().users("get", { id }) as Promise<User | null>;
  },
  /** The user with this email address, or null. */
  byEmail(email: string): Promise<User | null> {
    return host().users("byEmail", { email }) as Promise<User | null>;
  },
  /** Users whose name or email matches query; limit defaults to 20, at most 100. */
  search(query = "", limit?: number): Promise<User[]> {
    const args: Record<string, unknown> = { query };
    if (limit !== undefined) args.limit = limit;
    return host().users("search", args) as Promise<User[]>;
  },
};

export interface UploadOptions {
  /** The table whose file field will hold the file. */
  table: TableName;
  /** File name, e.g. "report.pdf". */
  name: string;
  /** The content; a string is stored as UTF-8. */
  body: BytesOrString;
  /** Default: derived from the name. */
  contentType?: string;
  /** The row the file is for, when it already exists. */
  recordId?: number;
}

export const files = {
  /** Names, sizes, and types of files; ids the workflow cannot see are left out. */
  metadata(ids: number[]): Promise<FileRecord[]> {
    return host().files("metadata", { ids }) as Promise<FileRecord[]>;
  },
  /** A file with its content (at most 20 MiB). */
  async read(id: number): Promise<{ file: FileRecord; body: Uint8Array }> {
    const result = (await host().files("read", { id })) as { file: FileRecord; body: ArrayBuffer };
    return { file: result.file, body: new Uint8Array(result.body) };
  },
  /**
   * Stores a file. Put the returned id in a file field with create or
   * update, which binds it to the row.
   */
  upload(options: UploadOptions): Promise<FileRecord> {
    const args: Record<string, unknown> = { table: options.table, name: options.name, body: options.body };
    if (options.contentType !== undefined) args.content_type = options.contentType;
    if (options.recordId !== undefined) args.record_id = options.recordId;
    return host().files("upload", args) as Promise<FileRecord>;
  },
};
