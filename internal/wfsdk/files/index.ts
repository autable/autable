// autable workflow SDK. Vendored into workflow projects by `autablectl workflow sdk`; do not edit there.
//
// Everything a workflow uses, imported from "./autable":
//
//   import { manual, schedule, tables, fetch, secrets, log, type TriggerEvent } from "./autable";
//
// A workflow project exports `triggers` and `run(event)`. Every run
// evaluates the bundle in a fresh JavaScript runtime and calls `run` once;
// nothing survives a run except what it writes (table rows, files, its
// log and output). Keep state that spans runs in table rows.
//
// The runtime is not Node and not a browser: there is no `require`,
// `process`, Node built-in, DOM, or global `fetch`. It has the standard
// globals declared in ./globals.d.ts; everything else comes from here.
// npm packages written in plain JavaScript can be bundled.

/** The SDK version this project was built against; autable.workflow.json records it too. */
export const SDK_VERSION = "1.0";

export type { Bytes, BytesOrString } from "./host.js";

export {
  schedule,
  recordChanged,
  webhook,
  manual,
  requestText,
  requestJSON,
} from "./triggers.js";
export type {
  Trigger,
  Triggers,
  TriggerEvent,
  ScheduleTrigger,
  RecordChangedTrigger,
  RecordChangedDeclaration,
  WebhookTrigger,
  ManualTrigger,
  ManualField,
  ManualInput,
  ScheduleEvent,
  RecordChangedEvent,
  RecordChange,
  Actor,
  WebhookEvent,
  WebhookRequest,
  WebhookResponse,
  ManualEvent,
} from "./triggers.js";

export { fetch, FetchResponse, FetchHeaders } from "./fetch.js";
export type { FetchInit } from "./fetch.js";

export { crypto, base64, base64url, hex, utf8 } from "./crypto.js";
export type { HashAlgorithm, SignatureAlgorithm, CipherAlgorithm, BinaryEncoding } from "./crypto.js";

export { secrets, vars, log, runInfo, isPermissionError, isNotFoundError } from "./runtime.js";
export type { RunInfo } from "./runtime.js";

export { tables, users, files, splitOptions, joinOptions, DATABASE, TABLE_NAMES } from "./tables.js";
export type {
  Tables,
  TableName,
  FieldName,
  Values,
  WritableValues,
  QueryField,
  Row,
  UpsertResult,
  Operator,
  Condition,
  Group,
  Filter,
  Sort,
  ListOptions,
  PageOptions,
  Page,
  ChangeOperation,
  RowChange,
  TableClient,
  User,
  FileRecord,
  UploadOptions,
} from "./tables.js";
