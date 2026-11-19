# Workflows v2: integrations without releases

Status: proposal.

## Problem

Today a workflow is one JavaScript file with `instances`, `trigger` and
`run`, evaluated synchronously in goja. The script's only way to reach the
outside world is a **node**: a Go type compiled into the binary and listed
in `internal/workflow/nodes/registry.go`. As a result:

1. **Every integration is a release.** The script has no HTTP, crypto,
   logging or timers, so even one REST call or one signature needs a new
   Go node, a new autable build, and an upgrade. Remote runners do not
   help: they run the same compiled node list, and the server validates
   declarations against its own registry.
2. **Triggers are hard-coded.** Record change, schedule and webhook are
   matched by node-type strings inside the API server. The webhook body
   must be `{token, payload}`, so third-party callbacks (an IM approval
   callback, a payment notification) cannot be received directly.
3. **Nothing is durable.** Runs are written once, at the end; queues live
   in memory; there are no retries, no "running" state and no way to wait.
   Waiting for an approval is done by polling on a schedule and keeping
   state in user tables.
4. **One file, edited in place.** No versions, no rollback, no shared
   code between workflows, and the script is re-evaluated several times
   per event just to read its declarations.

## Direction

Treat a workflow like an app (see `apps.md`): **a code project with
versions, built by autablectl, running against a small, stable, versioned
platform API.** autable provides primitives; integrations become ordinary
TypeScript that lives with the deployment and changes without an autable
release.

### 1. Primitives instead of nodes

The runtime gives workflow code a fixed set of capabilities, versioned as
an SDK with the same MAJOR.MINOR compatibility rules as apps:

| Capability | Notes |
|---|---|
| `fetch` | HTTP with timeouts and size limits; optional `via: "<runner>"` to egress through a remote runner for intranet systems |
| `crypto` | hashes, HMAC, signatures (ed25519, ECDSA incl. secp curves), random bytes, AES |
| `secrets`, `vars` | per-workflow, read-only, never logged |
| `tables`, `users`, `files` | typed from generated `schema.ts`, executed as the workflow subject with its grants |
| `log` | structured lines stored with the run |
| `step`, `sleep`, `waitFor`, `signal` | durable execution (section 3) |

With `fetch` and `crypto`, an IM, ERP or Git hosting integration is a
TypeScript module of a few hundred lines. Those modules live in the
deployment's own repository, or in a shared package the deployment
vendors. Adding or fixing one is a workflow push, not an autable release.
The existing Go nodes are ported to such a library and then deleted.

**Remote runners become egress proxies.** Instead of executing nodes, a
runner relays `fetch` calls for hosts only it can reach. It holds no code,
so it never needs upgrading for a new integration.

### 2. Workflows as projects

```
autablectl workflow init DIR --database DB --name NAME
autablectl workflow push [DIR] [--publish]
autablectl workflow publish | rollback | versions | pull | runs | logs
```

- `push` bundles the TypeScript with esbuild into one script, then uploads
  the bundle, its source and a manifest stating the SDK version. Versions
  are stored and published exactly like app versions.
- Triggers are declared in code and read once, when a version is
  published:

```ts
export const triggers = [
  schedule({ cron: "0 9 * * 1-5", timezone: "Asia/Shanghai" }),
  recordChanged({ table: "orders", operations: ["create", "update"] }),
  webhook({ path: "approval-callback" }), // handler gets the raw request
];

export default async function run(event: TriggerEvent) { ... }
```

- A `webhook` trigger hands the code the raw method, headers and body. The
  code verifies signatures itself, so any third-party callback format
  works.

### 3. Durable execution

Borrowed from Temporal, Inngest, Cloudflare Workflows, Vercel's Workflow
DevKit and DBOS, in the smallest form that fits autable:

```ts
const instance = await step("create approval", () => im.createApproval(...));
const result = await waitFor(`approval:${instance.id}`, { timeout: "7d" });
await step("write result", () => tables.orders.update(id, { status: result.status }));
```

- `step(name, fn)` runs `fn` once and journals its result in the run
  record, with per-step retries and backoff.
- `waitFor(key)` suspends the run: it is stored as *waiting* and its goja
  state is discarded.
- `signal(key, payload)`, usually called from a webhook trigger, resumes
  the run. Resuming re-executes the code from the top and replays
  journaled step results, so code between steps must be deterministic.
  `now()` and `random()` are journaled for that reason.
- Runs get a persistent status (queued, running, waiting, succeeded,
  failed), a log, and their step journal. Queues are stored, so a restart
  resumes them instead of losing them.

Approvals become callbacks instead of polling, and a run can span days.

### Runtime choice

Keep goja, inside the single binary.

- **Why:**
  - Deployments stay one binary.
  - The sandbox is simple: the code can only use what we expose.
  - CPU and memory are bounded by `Interrupt` and per-run limits.
- **Language level:** the goja version autable pins runs async/await
  and generators natively; esbuild only strips types and bundles.
  Today's synchronous `exec` needs an event loop around promises, which
  `fetch` needs anyway.
- **Libraries:** pure-JS npm packages bundle fine. Packages that need
  Node APIs do not; the platform's `fetch` and `crypto` cover what
  integrations need.
- **If this ever becomes the bottleneck:** the alternative is a Node or
  Deno worker process per deployment. It needs no API change, because
  the primitives are the contract.

## Phases

1. **Primitives in the current runtime.** Add `fetch`, `crypto`, `log`,
   `secrets` and `vars` to today's scripts, plus raw-request webhooks.
   - Small change.
   - From here on, most new integrations need no release.
2. **Projects and versions.**
   - Workflow projects, versions, publish and rollback, reusing the app
     bundle and SDK machinery.
   - Triggers declared in code.
   - Typed tables.
   - `autablectl workflow` commands.
3. **Durable execution.**
   - Persistent runs and queues, steps, retries, `waitFor` / `signal`.
   - Port the Go nodes to a TypeScript integrations library, then
     remove nodes and the node registry.

Each phase is useful on its own; phase 1 alone removes most releases.
