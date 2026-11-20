// autable workflow SDK. Vendored into workflow projects by `autablectl workflow sdk`; do not edit there.
//
// HTTP requests to other systems, performed by the server (or, with `via`,
// by one of the database's runners). There is no browser `fetch` in a
// workflow; this is the one to use.

import { host } from "./host.js";
import type { Bytes, HostFetchRequest, HostFetchResponse } from "./host.js";
import { utf8 } from "./crypto.js";

export interface FetchInit {
  /** Default "GET". */
  method?: string;
  headers?: Record<string, string> | [string, string][];
  /** Sent as is; a string is sent as UTF-8. Use `json` for JSON bodies. */
  body?: string | Bytes;
  /**
   * A JSON-able value sent as the body, with content-type
   * application/json unless headers set one. Not together with `body`.
   */
  json?: unknown;
  /** Default 30000; never longer than the time left in the run. */
  timeoutMs?: number;
  /**
   * Name of a runner of this database that performs the request, for
   * systems only reachable from inside a network. Fails when that runner
   * is not connected.
   */
  via?: string;
}

/** Response headers; names are case-insensitive. */
export class FetchHeaders implements Iterable<[string, string]> {
  /** Lower-case names in arrival order; repeated headers repeat. */
  readonly raw: readonly [string, string][];

  constructor(raw: [string, string][]) {
    this.raw = raw;
  }

  /** The header's values joined with ", ", or null when absent. */
  get(name: string): string | null {
    const values = this.getAll(name);
    return values.length > 0 ? values.join(", ") : null;
  }

  getAll(name: string): string[] {
    const wanted = name.toLowerCase();
    return this.raw.filter(([key]) => key === wanted).map(([, value]) => value);
  }

  has(name: string): boolean {
    return this.getAll(name).length > 0;
  }

  [Symbol.iterator](): Iterator<[string, string]> {
    return this.raw[Symbol.iterator]();
  }
}

/**
 * A completed response. Any HTTP status resolves (check `ok` or `status`);
 * only network failures, timeouts and bodies over 20 MiB reject. The body
 * is already read, so text(), json() and bytes() are synchronous (awaiting
 * them, as browser code does, works too).
 */
export class FetchResponse {
  readonly status: number;
  readonly statusText: string;
  /** The final URL, after redirects (at most 10 are followed). */
  readonly url: string;
  readonly headers: FetchHeaders;
  private readonly body: ArrayBuffer;

  constructor(response: HostFetchResponse) {
    this.status = response.status;
    this.statusText = response.statusText;
    this.url = response.url;
    this.headers = new FetchHeaders(response.headers);
    this.body = response.body;
  }

  /** True for a 2xx status. */
  get ok(): boolean {
    return this.status >= 200 && this.status < 300;
  }

  text(): string {
    return utf8.decode(this.body);
  }

  /** Parses the body as JSON; throws a SyntaxError when it is not JSON. */
  json<T = unknown>(): T {
    return JSON.parse(this.text()) as T;
  }

  bytes(): Uint8Array {
    return new Uint8Array(this.body.slice(0));
  }
}

/**
 * Sends an HTTP request.
 *
 * ```ts
 * const response = await fetch("https://api.example.com/items", {
 *   method: "POST",
 *   headers: { authorization: `Bearer ${secrets.require("api_token")}` },
 *   json: { name: "Widget" },
 * });
 * if (!response.ok) throw new Error(`items: ${response.status} ${response.text()}`);
 * const item = response.json<{ id: string }>();
 * ```
 */
export async function fetch(url: string | URL, init: FetchInit = {}): Promise<FetchResponse> {
  if (init.json !== undefined && init.body !== undefined) {
    throw new TypeError("fetch: pass either body or json, not both");
  }
  let headers = init.headers;
  let body = init.body;
  if (init.json !== undefined) {
    body = JSON.stringify(init.json);
    const pairs: [string, string][] = Array.isArray(headers) ? [...headers] : Object.entries(headers ?? {});
    if (!pairs.some(([name]) => name.toLowerCase() === "content-type")) {
      pairs.push(["content-type", "application/json"]);
    }
    headers = pairs;
  }
  const request: HostFetchRequest = { url: String(url) };
  if (init.method !== undefined) request.method = init.method;
  if (headers !== undefined) request.headers = headers;
  if (body !== undefined) request.body = body;
  if (init.timeoutMs !== undefined) request.timeoutMs = init.timeoutMs;
  if (init.via !== undefined) request.via = init.via;
  return new FetchResponse(await host().fetch(request));
}
