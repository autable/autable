// The library's public API: everything exported here is what workflows get
// from `import { ... } from "@lib/__LIB_NAME__"`. Keep it backward
// compatible: publishing a version changes every workflow that imports the
// library at its next run.
//
// Import the SDK as "@autable/workflow" (not "./autable"), so the published
// type declarations refer to the importing workflow's SDK.
import { fetch, log } from "@autable/workflow";

export interface PostOptions {
  /** Sent as a bearer token when given; read it with secrets.require in the workflow. */
  token?: string;
  /** Name of a runner of the workflow's database that sends the request. */
  via?: string;
}

/**
 * Posts a JSON body to url and returns the parsed JSON answer. Throws an
 * error with the status and body when the answer is not 2xx.
 */
export async function postJSON<T = unknown>(url: string, body: unknown, options: PostOptions = {}): Promise<T> {
  const headers: Record<string, string> = {};
  if (options.token) headers.authorization = `Bearer ${options.token}`;
  const response = await fetch(url, { method: "POST", headers, json: body, via: options.via });
  if (!response.ok) {
    throw new Error(`POST ${url}: ${response.status} ${response.text().slice(0, 500)}`);
  }
  log.info("posted", { url, status: response.status });
  return response.json<T>();
}
