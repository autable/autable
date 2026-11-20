// autable workflow SDK. Vendored into workflow projects by `autablectl workflow sdk`; do not edit there.
//
// The adapter to the host API, globalThis.__autable_host, which the server
// installs in every run (docs/design/workflows-host-api.md in the autable
// source). This is the only SDK module that touches it; workflow code
// imports from "./autable" instead.

/** Binary data: an ArrayBuffer or any view of one, such as a Uint8Array. */
export type Bytes = ArrayBuffer | ArrayBufferView;

/** Bytes, or a string that is used as its UTF-8 encoding. */
export type BytesOrString = Bytes | string;

export interface HostInfo {
  database: string;
  workflow: string;
  version: number;
  runId: number;
  trigger: string;
}

export interface HostFetchRequest {
  url: string;
  method?: string;
  headers?: Record<string, string> | [string, string][];
  body?: BytesOrString;
  timeoutMs?: number;
  via?: string;
}

export interface HostFetchResponse {
  status: number;
  statusText: string;
  url: string;
  headers: [string, string][];
  body: ArrayBuffer;
}

export type Encoding = "base64" | "base64url" | "hex" | "base58" | "utf8";

export interface Host {
  readonly version: string;
  readonly info: HostInfo;
  fetch(request: HostFetchRequest): Promise<HostFetchResponse>;
  hash(alg: string, data: BytesOrString): ArrayBuffer;
  hmac(alg: string, key: BytesOrString, data: BytesOrString): ArrayBuffer;
  randomBytes(n: number): ArrayBuffer;
  sign(alg: string, privateKeyPem: string, data: BytesOrString): ArrayBuffer;
  verify(alg: string, publicKeyPem: string, data: BytesOrString, signature: Bytes): boolean;
  publicKey(privateKeyPem: string): string;
  encrypt(alg: string, key: Bytes, iv: Bytes, data: BytesOrString, aad?: BytesOrString): ArrayBuffer;
  decrypt(alg: string, key: Bytes, iv: Bytes, data: Bytes, aad?: BytesOrString): ArrayBuffer;
  timingSafeEqual(a: BytesOrString, b: BytesOrString): boolean;
  encode(encoding: Encoding, bytes: Bytes): string;
  decode(encoding: Encoding, text: string): ArrayBuffer;
  secret(name: string): string | undefined;
  variable(name: string): string | undefined;
  log(level: "info" | "warn" | "error", message: string, data?: unknown): void;
  table(op: string, table: string, args: Record<string, unknown>): Promise<unknown>;
  users(op: string, args: Record<string, unknown>): Promise<unknown>;
  files(op: string, args: Record<string, unknown>): Promise<unknown>;
}

/**
 * The host object. Looked up on every call, never at import time, so the
 * bundle can be evaluated while the server reads the triggers (when every
 * host function throws).
 */
export function host(): Host {
  const found = (globalThis as { __autable_host?: Host }).__autable_host;
  if (!found) {
    throw new Error("the autable host API is missing: this code only runs inside an autable workflow run");
  }
  return found;
}

