// autable workflow SDK. Vendored into workflow projects by `autablectl workflow sdk`; do not edit there.
//
// Secrets, variables, the run log, and what is running.

import { host } from "./host.js";
import type { HostInfo } from "./host.js";

/**
 * Per-workflow secrets, set by admins in the web UI or with
 * `autablectl workflow secret set`. The API never returns their values;
 * do not log or return them either.
 */
export const secrets = {
  /** The secret's value, or undefined when it is not set. */
  get(name: string): string | undefined {
    return host().secret(name);
  },
  /** The secret's value; throws an error naming the secret when it is not set. */
  require(name: string): string {
    const value = host().secret(name);
    if (value === undefined) {
      throw new Error(`secret ${JSON.stringify(name)} is not set; set it with: autablectl workflow secret set <database> <workflow> ${name}`);
    }
    return value;
  },
};

/** Per-workflow variables: plain configuration, readable by admins. */
export const vars = {
  /** The variable's value, or undefined when it is not set. */
  get(name: string): string | undefined {
    return host().variable(name);
  },
  /** The variable's value; throws an error naming it when it is not set. */
  require(name: string): string {
    const value = host().variable(name);
    if (value === undefined) {
      throw new Error(`var ${JSON.stringify(name)} is not set; set it with: autablectl workflow var set <database> <workflow> ${name} <value>`);
    }
    return value;
  },
};

/**
 * The run log, shown with the run (`autablectl workflow run-log`). data is
 * any JSON-able value. At most 1000 entries per run; messages are cut at
 * 8 KiB and data over 64 KiB is replaced by a note. console.log and
 * friends write here too.
 */
export const log = {
  info(message: string, data?: unknown): void {
    write("info", message, data);
  },
  warn(message: string, data?: unknown): void {
    write("warn", message, data);
  },
  error(message: string, data?: unknown): void {
    write("error", message, data);
  },
};

function write(level: "info" | "warn" | "error", message: string, data: unknown): void {
  if (data === undefined) {
    host().log(level, message);
  } else {
    host().log(level, message, data);
  }
}

export type RunInfo = HostInfo;

/** The database, workflow, published version, run id, and trigger of the current run. */
export function runInfo(): RunInfo {
  return host().info;
}

/** True for the "PermissionError" a table, users or files call rejects with outside the workflow's grants. */
export function isPermissionError(error: unknown): error is Error {
  return error instanceof Error && error.name === "PermissionError";
}

/** True for the "NotFoundError" a call rejects with for a record outside the workflow's row set. */
export function isNotFoundError(error: unknown): error is Error {
  return error instanceof Error && error.name === "NotFoundError";
}
