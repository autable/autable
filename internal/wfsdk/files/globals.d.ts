// autable workflow SDK. Vendored into workflow projects by `autablectl workflow sdk`; do not edit there.
//
// The standard globals the workflow runtime provides, and nothing else:
// the project's tsconfig uses only the ES library, so anything not
// declared here (window, document, process, require, the browser fetch)
// is a type error instead of a crash at run time.

/** UTF-8 only. */
declare class TextEncoder {
  readonly encoding: "utf-8";
  encode(input?: string): Uint8Array;
}

/** UTF-8 only; invalid sequences become U+FFFD. */
declare class TextDecoder {
  constructor(label?: "utf-8" | "utf8");
  readonly encoding: "utf-8";
  decode(input?: ArrayBuffer | ArrayBufferView): string;
}

/** Decodes base64 into a Latin-1 string, as in browsers. Prefer base64.decode from the SDK for bytes. */
declare function atob(data: string): string;
/** Encodes a Latin-1 string as base64, as in browsers. Prefer base64.encode from the SDK. */
declare function btoa(data: string): string;

declare class URLSearchParams implements Iterable<[string, string]> {
  constructor(init?: string | Record<string, string> | [string, string][] | URLSearchParams);
  append(name: string, value: string): void;
  delete(name: string): void;
  get(name: string): string | null;
  getAll(name: string): string[];
  has(name: string): boolean;
  set(name: string, value: string): void;
  forEach(callback: (value: string, name: string, parent: URLSearchParams) => void): void;
  keys(): IterableIterator<string>;
  values(): IterableIterator<string>;
  entries(): IterableIterator<[string, string]>;
  toString(): string;
  [Symbol.iterator](): IterableIterator<[string, string]>;
}

declare class URL {
  constructor(url: string | URL, base?: string | URL);
  href: string;
  readonly origin: string;
  protocol: string;
  username: string;
  password: string;
  host: string;
  hostname: string;
  port: string;
  pathname: string;
  search: string;
  hash: string;
  readonly searchParams: URLSearchParams;
  toString(): string;
}

/** Runs fn after ms milliseconds, within the current run only; a pending timer keeps the run alive. */
declare function setTimeout<A extends unknown[]>(fn: (...args: A) => void, ms?: number, ...args: A): number;
declare function clearTimeout(id: number | undefined): void;

/** Writes to the run log (log and debug as info); objects are JSON-encoded. */
declare const console: {
  log(...data: unknown[]): void;
  info(...data: unknown[]): void;
  warn(...data: unknown[]): void;
  error(...data: unknown[]): void;
  debug(...data: unknown[]): void;
};
