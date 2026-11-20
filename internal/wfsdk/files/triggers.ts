// autable workflow SDK. Vendored into workflow projects by `autablectl workflow sdk`; do not edit there.
//
// Trigger declarations and the events they start runs with. A project
// exports `triggers` (an object of named declarations built with the
// helpers below) and `run(event)`; the type of `event` follows from the
// declarations:
//
//   export const triggers = { hourly: schedule({ cron: "0 * * * *" }) };
//   export async function run(event: TriggerEvent<typeof triggers>) { ... }
//
// The server reads `triggers` when a version is published and rejects
// invalid declarations (unknown keys, unknown tables or fields, bad cron).

import type { Bytes } from "./host.js";
import { utf8 } from "./crypto.js";
import type { ChangeOperation, FieldName, RowChange, TableName } from "./tables.js";

// Declarations.

export interface ScheduleTrigger {
  readonly kind: "schedule";
  readonly cron: string;
  readonly timezone?: string;
}

/** A recordChanged declaration with its table and fields as plain strings. */
export interface RecordChangedDeclaration {
  readonly kind: "recordChanged";
  readonly table: string;
  readonly operations?: readonly ChangeOperation[];
  readonly fields?: readonly string[];
  readonly includeOwnWrites?: boolean;
}

export interface RecordChangedTrigger<T extends TableName = TableName> extends RecordChangedDeclaration {
  readonly table: T;
  readonly fields?: readonly FieldName<T>[];
}

export interface WebhookTrigger {
  readonly kind: "webhook";
  readonly path: string;
}

/** A field of a manual trigger's input form. */
export interface ManualField {
  /** Key of the value in event.input. */
  readonly name: string;
  /** Form label; default: the name. */
  readonly label?: string;
  readonly type: "string" | "number" | "boolean";
}

export interface ManualTrigger<F extends readonly ManualField[] = readonly ManualField[]> {
  readonly kind: "manual";
  readonly fields?: F;
}

export type Trigger = ScheduleTrigger | RecordChangedDeclaration | WebhookTrigger | ManualTrigger;

/** What a project exports as `triggers`: names (letters, digits, _; not starting with a digit) to declarations. */
export type Triggers = Record<string, Trigger>;

/**
 * Runs at the minutes a cron expression matches: five fields (minute hour
 * day-of-month month day-of-week) with `*`, lists, ranges, steps, JAN–DEC
 * and SUN–SAT; 0 and 7 are Sunday. When both day fields are restricted
 * either may match, as in cron. Minutes missed while the server was down
 * are not caught up.
 *
 * ```ts
 * schedule({ cron: "0 9 * * 1-5", timezone: "Asia/Shanghai" }) // 09:00 on weekdays
 * ```
 */
export function schedule(options: {
  cron: string;
  /** An IANA time zone such as "Europe/Berlin"; default "UTC". */
  timezone?: string;
}): ScheduleTrigger {
  const declaration: { kind: "schedule"; cron: string; timezone?: string } = { kind: "schedule", cron: options.cron };
  if (options.timezone !== undefined) declaration.timezone = options.timezone;
  return declaration;
}

/**
 * Runs once for every change to a row of `table`. The event carries the
 * change, who made it, and which fields changed; values the workflow may
 * not read are left out.
 *
 * ```ts
 * recordChanged({ table: "orders", operations: ["update"], fields: ["status"] })
 * ```
 */
export function recordChanged<const T extends TableName>(options: {
  table: T;
  /** Default: all of create, update, and delete. */
  operations?: readonly ChangeOperation[];
  /** Only changes that touch one of these fields fire the trigger. */
  fields?: readonly FieldName<T>[];
  /**
   * Whether changes written by this workflow's own runs fire it. Default
   * false, so a workflow updating the row it reacts to does not loop.
   */
  includeOwnWrites?: boolean;
}): RecordChangedTrigger<T> {
  const declaration: {
    kind: "recordChanged";
    table: T;
    operations?: readonly ChangeOperation[];
    fields?: readonly FieldName<T>[];
    includeOwnWrites?: boolean;
  } = { kind: "recordChanged", table: options.table };
  if (options.operations !== undefined) declaration.operations = [...options.operations];
  if (options.fields !== undefined) declaration.fields = [...options.fields];
  if (options.includeOwnWrites !== undefined) declaration.includeOwnWrites = options.includeOwnWrites;
  return declaration;
}

/**
 * Answers HTTP requests to `/api/hooks/<database>/<workflow>/<path>` (any
 * method, no autable session: the code must authenticate the caller,
 * e.g. with crypto.hmac and crypto.timingSafeEqual). The run's return
 * value is the HTTP response and must come within 15 seconds.
 *
 * ```ts
 * webhook({ path: "orders/created" })
 * ```
 */
export function webhook(options: {
  /** Segments of letters, digits, and . _ ~ - separated by "/"; unique within the workflow. */
  path: string;
}): WebhookTrigger {
  return { kind: "webhook", path: options.path };
}

/**
 * Started on demand: from the web UI (with a form built from `fields`),
 * `autablectl workflow run`, the API, or an app. Users with a run grant
 * may start it; the run still acts with the workflow's own grants.
 *
 * ```ts
 * manual({ fields: [{ name: "orderId", label: "Order", type: "number" }] })
 * ```
 */
export function manual<const F extends readonly ManualField[] = readonly []>(options?: {
  /** The input form; values arrive in event.input, checked against these types when present. */
  fields?: F;
}): ManualTrigger<F> {
  const declaration: { kind: "manual"; fields?: F } = { kind: "manual" };
  if (options?.fields !== undefined) {
    declaration.fields = options.fields.map((field) => {
      const copy: { name: string; label?: string; type: ManualField["type"] } = { name: field.name, type: field.type };
      if (field.label !== undefined) copy.label = field.label;
      return copy;
    }) as unknown as F;
  }
  return declaration;
}

// Events.

interface BaseEvent<K extends string> {
  /** The name of the trigger that started the run. */
  trigger: K;
  runId: number;
}

export interface ScheduleEvent<K extends string = string> extends BaseEvent<K> {
  /** The matched minute, in milliseconds since the Unix epoch. */
  scheduledAt: number;
}

/** Who made a change: a user, or a workflow (id "workflow:<id>"). */
export interface Actor {
  id: string;
  type: "user" | "workflow";
  email?: string;
  display_name?: string;
}

export interface RecordChange<T extends TableName = TableName> extends RowChange<T> {
  actor: Actor;
  /** The fields this change touched (only those the workflow may read). */
  changedFields: FieldName<T>[];
}

export interface RecordChangedEvent<K extends string = string, T extends TableName = TableName> extends BaseEvent<K> {
  change: RecordChange<T>;
}

/** The raw HTTP request a webhook run answers. */
export interface WebhookRequest {
  method: string;
  /** The part of the path after /api/hooks/<database>/<workflow>/. */
  path: string;
  /** The full request URI: path and query. */
  url: string;
  query: Record<string, string[]>;
  /** Lower-case names; repeated headers are joined with ", ". */
  headers: Record<string, string>;
  /** At most 5 MiB. Use requestText or requestJSON to decode it. */
  body: ArrayBuffer;
}

export interface WebhookEvent<K extends string = string> extends BaseEvent<K> {
  request: WebhookRequest;
}

/**
 * What a webhook run returns: the HTTP response. Return undefined for
 * 204 No Content. A string body is sent as text/plain, bytes as
 * application/octet-stream, anything else as JSON, unless headers set a
 * content type. A thrown error answers 500.
 */
export interface WebhookResponse {
  status: number;
  headers?: Record<string, string>;
  body?: string | Bytes | unknown;
}

type FieldValue<F extends ManualField> = F["type"] extends "number" ? number : F["type"] extends "boolean" ? boolean : string;

/** The input of a manual trigger: its declared fields (each optional) plus anything else the caller sent. */
export type ManualInput<F extends readonly ManualField[]> = { [E in F[number] as E["name"]]?: FieldValue<E> } & Record<string, unknown>;

export interface ManualEvent<K extends string = string, I = Record<string, unknown>> extends BaseEvent<K> {
  input: I;
  /** The user who started the run, when a user did. */
  startedBy?: { id: string; email: string; display_name: string };
}

type EventFor<K extends string, D> =
  D extends ScheduleTrigger
    ? ScheduleEvent<K>
    : D extends RecordChangedTrigger<infer T>
      ? RecordChangedEvent<K, T>
      : D extends WebhookTrigger
        ? WebhookEvent<K>
        : D extends ManualTrigger<infer F>
          ? ManualEvent<K, ManualInput<F>>
          : never;

/**
 * The event `run` receives, given the project's triggers: a union keyed
 * by `event.trigger`, so a switch on it narrows to that trigger's fields.
 */
export type TriggerEvent<T extends Triggers> = { [K in keyof T & string]: EventFor<K, T[K]> }[keyof T & string];

/** A webhook request's body decoded as UTF-8. */
export function requestText(request: WebhookRequest): string {
  return utf8.decode(request.body);
}

/** A webhook request's body parsed as JSON; throws a SyntaxError when it is not JSON. */
export function requestJSON<T = unknown>(request: WebhookRequest): T {
  return JSON.parse(requestText(request)) as T;
}
