# Remote Runner Design

Status: draft, not implemented.

## Problem

Workflow nodes execute inside the autable server process. Nodes that call
external services (`dingtalk.*`, `github.*`, a future `kingdee.*`) therefore
require the target system to be reachable **from the server**. When autable is
deployed on a public host and the target system lives inside a private network
(on-premise ERP such as Kingdee K3Cloud, internal databases, MES systems), the
server cannot reach it and the node model breaks down.

Solving this per integration (standalone sync daemons, VPN tunnels into the
private network) either abandons the shared node model or widens the private
network's attack surface. The general fix is a **remote runner**: a worker
process that runs inside the private network, connects *outbound* to the
autable server, and executes individual node steps on the server's behalf.
Nodes stay ordinary registry nodes that every autable deployment can use;
where they execute becomes a deployment concern.

## Non-goals

- Running workflow orchestration (the goja `run(info)` script) remotely. The
  script, trigger evaluation, history persistence, and secret storage stay on
  the server.
- Running trigger nodes remotely. Triggers respond to server-side events
  (`schedule` ticks, `table.record.changed` history keys) and never leave the
  server.
- Running `table.*` / autable-service nodes remotely. They need direct access
  to server-side stores.
- Per-user or per-database runner permissions. There is a single system-wide
  runner token (autable currently has no system administrator concept; see
  Security).
- Guaranteed (at-least-once) job delivery. A step dispatched to a runner that
  disconnects mid-flight fails visibly, like any other node error.

## Design overview

```
  private network                        public network
 ┌───────────────────────┐             ┌─────────────────────────────┐
 │ Kingdee / internal DB │             │ autable server              │
 │          ▲            │             │                             │
 │          │ local call │             │  goja run(info)             │
 │  autable-runner  ─────┼── outbound ─▶  runner hub (WS endpoint)   │
 │  (node subset)        │  WebSocket  │  workflow.Runner            │
 └───────────────────────┘             └─────────────────────────────┘
```

Every node instance executes on a **runner**. The default is the **server
runner**: the node registry inside the autable server process, which is what
all instances use today — it is not a separate process and has no connection
or token. Remote runners are additional named executors that node instances
can be bound to.

1. `autable-runner` (a new CLI in `cmd/autable-runner`) starts inside the
   private network with an endpoint URL and the runner token. It opens a
   persistent outbound WebSocket connection to the server and advertises its
   name and the node types it can execute.
2. A node instance may be bound to a remote runner in the web UI, in the same
   editor area where instance variables and secrets are already configured.
   When the goja script calls `info.instance("x").exec(input)` on a bound
   instance, the server serializes the node input (plus resolved
   secrets/variables) into a job, sends it to the connected runner, and
   blocks the step until the result message returns.
3. The runner executes the node locally — inside the network where the target
   system is reachable — and sends the output map back. The step result is
   recorded in run history exactly like a local step.

No inbound connectivity, port forwarding, or VPN into the private network is
required. The runner only needs outbound HTTPS/WSS to the server.

## Runner binding

Runner bindings are **per-instance configuration, not part of the workflow
script**. They live exactly where instance variables and secrets already
live: edited in the web UI next to the secrets/variables JSON editors, stored
on the workflow record in `system.sqlite` (a `RunnersJSON` column beside
`SecretsJSON`/`VariablesJSON`), keyed by instance ID:

```jsonc
// runners map on a workflow
{ "pull_orders": "intranet" }
```

- An instance absent from the map runs on the server runner. The UI presents
  the binding as a selector defaulting to "server", listing connected remote
  runners that advertise the instance's node type.
- The workflow script (`instances`, `trigger`, `run`) is unchanged and stays
  deployment-portable: the git-managed repository never references runner
  names, so the same repository works across deployments whose runners are
  named differently — bindings are local deployment data, like secrets.
- Trigger instances and server-only node types cannot be bound to a remote
  runner; the save API rejects such bindings.
- If the bound runner has no live connection when `exec` is called, the step
  fails with an explicit error (`runner "intranet" is not connected`). There
  is no fallback to the server runner, matching the project rule that
  non-normal paths fail visibly instead of degrading.

`workflow.Definition` gains `Runners map[string]string` alongside `Secrets`
and `Variables`, populated from the workflow record when the definition is
assembled.

## Node eligibility

`internal/workflow/nodes/registry.go` already separates nodes by their
dependencies. That boundary becomes explicit:

- **Remote-capable**: nodes constructible with no server-side dependencies —
  currently `echo`, `dingtalk.robot.*`, `dingtalk.notable.*`,
  `github.file.content`, and future integration nodes such as `kingdee.*`.
  The registry gains a `Remote() []workflow.Node` constructor listing exactly
  these.
- **Server-only**: trigger nodes (`time.schedule`, `table.record.changed`)
  and everything from `AutableNodes` (needs `autable.Service`).

`cmd/autable-runner` is built from the same module and registers
`nodes.Remote()`. Because server and runner share one registry source, a node
added to `Remote()` is automatically available both locally and remotely;
there is no separate plugin mechanism or node distribution problem. The
handshake carries the runner's node type list and binary version so the
server never dispatches a job the runner cannot handle and version skew is
visible in the runners API.

## Execution path

`workflow.Runner` gains an optional dispatcher, injected from the API server:

```go
type RemoteDispatcher interface {
    Dispatch(ctx context.Context, runnerName string, job RemoteJob) (map[string]any, error)
    NodeTypes(runnerName string) ([]string, bool) // false when not connected
}

type RemoteJob struct {
    RunID      string
    InstanceID string
    NodeType   string
    Input      map[string]any
    Secrets    map[string]string
    Variables  map[string]string
    Runtime    RuntimeInfo
}
```

`runInstance` looks up `definition.Runners[instanceID]`: when a remote runner
is bound it routes to the dispatcher, otherwise it runs the registered node
in-process as today (the server runner). The dispatcher blocks until the
runner returns a result, the per-job timeout elapses, or the runner
disconnects. `history.StepRecord` gains an optional `Runner string` field so
run history shows where a step executed; nothing else about history changes.

The server-side hub lives in a new `internal/runnerhub` package: it owns the
WebSocket endpoint, the set of live connections, job correlation
(`job_id → waiting channel`), and implements `RemoteDispatcher`.

## Protocol

Transport: WebSocket over the existing HTTP server, endpoint
`GET /api/runner/ws`, authenticated by `Authorization: Bearer <token>`.
`gorilla/websocket` is already in the dependency tree (indirect); it becomes
a direct dependency. Messages are JSON text frames:

```jsonc
// runner → server, once after connect
{ "kind": "hello", "name": "intranet", "version": "v0.3.1",
  "node_types": ["kingdee.bill.query", "dingtalk.robot.send", "..."] }

// server → runner
{ "kind": "job", "job_id": "uuid", "node_type": "kingdee.bill.query",
  "input": {}, "secrets": {}, "variables": {}, "runtime": {} }

// runner → server
{ "kind": "result", "job_id": "uuid", "output": {} }
{ "kind": "result", "job_id": "uuid", "error": "message" }
```

Keepalive and reconnection are the runner CLI's responsibility:

- Runner sends WebSocket pings every 30s; the server closes connections with
  no ping for 90s. Intermediate proxies and NAT gateways drop idle
  connections well below common TCP timeouts, so the interval is deliberately
  short.
- On any disconnect the runner reconnects with exponential backoff plus
  jitter (1s doubling to a 60s cap), re-sending `hello` each time.
- Jobs are at-most-once: the server fails all in-flight jobs for a connection
  the moment it drops, and the runner discards results it cannot deliver.
  A reconnect does not resume jobs.

Multiple runner processes may connect under the same name; the hub picks any
live connection per job. This gives basic availability during runner restarts
without a coordination protocol.

Per-job timeout: server-side, default 10 minutes, applied by the dispatcher.

## Token

A single static system token authorizes runner connections.

- Stored in `system.sqlite` as a SHA-256 hash (single-row table), created on
  first reset. The plaintext (`atr_` + 43 base62 chars, ≥256 bits) is
  returned exactly once by the reset call and never displayed again.
- `POST /api/runner-token/reset` generates/replaces the token. Resetting
  invalidates every connected runner immediately (the hub drops their
  connections); this is accepted behavior, runners are reconfigured with the
  new token and reconnect.
- `GET /api/runners` returns token metadata (created timestamp, never the
  value) plus the list of connected runners: name, version, node types,
  connect time.
- Both endpoints require an authenticated session. autable has no system
  administrator concept — database owners are the only elevated role — so any
  authenticated user can reset the token. This is a deliberate simplification
  at the current stage; the endpoints are the natural place to attach an
  admin check if such a concept is introduced later.

The token is deliberately **not** stored in the git-managed repository:
`repository.path` is synced to remote git hosting, and a credential committed
there survives in history even after rotation. `system.sqlite` keeps it
server-local, resettable at runtime without a server restart, and out of
version control.

## Secrets

Server-stored workflow secrets keep working unchanged: the server resolves
the instance's declared secret ports from `Definition.Secrets` and ships the
values inside the job payload over WSS, exactly as they are injected into
local node runs. There is no runner-side secret mechanism and no distinction
between secrets that may or may not be sent: a runner holding the token is
**inside the trust boundary** by definition — it executes arbitrary node jobs
with their inputs, so withholding secrets from it would add complexity
without adding a meaningful boundary. Operators who cannot extend that trust
to a machine should not connect a runner from it.

## Runner CLI

```
autable-runner \
  --endpoint wss://autable.example.com \
  --token atr_... \            # or AUTABLE_RUNNER_TOKEN
  --name intranet \            # default: hostname
  --max-jobs 4                 # concurrent job limit, default 4
```

Behavior: connect, hello, execute jobs concurrently up to `--max-jobs`,
ping/reconnect as described above. Logs to stdout via `slog` like the server.
It is a long-running foreground process; process supervision (systemd,
container restart policy) is the operator's problem, and crash-looping is
safe because the protocol is stateless across connections.

Release builds ship `autable-runner` alongside `autable` for the same
platform matrix.

## Security considerations

- **Inversion of trust**: compared to tunneling the private system out (VPN /
  mesh network to the server), the runner model gives the public server zero
  reachability into the private network. A compromised server can at most
  dispatch jobs to nodes the runner registers, with inputs it controls — it
  cannot open arbitrary connections behind the firewall.
- **Token scope**: possession of the runner token allows connecting as any
  runner name, receiving jobs routed to that name (including their secret
  payloads), and returning forged outputs into workflow runs. It grants no
  other API access. TLS (`wss://`) is required outside local development.
- **Job payloads** can contain secrets and row data; they exist transiently
  in runner memory and are not persisted by the CLI.

## Implementation phases

1. **Server core**: per-instance runner bindings (`RunnersJSON` storage, save
   API validation, `Definition.Runners`), `RemoteDispatcher` seam in
   `workflow.Runner`, `internal/runnerhub` with WS endpoint, token storage +
   `/api/runner-token/reset` + `/api/runners`, `nodes.Remote()`.
2. **Runner CLI**: `cmd/autable-runner` with connect/hello/execute/keepalive,
   release packaging.
3. **Web UI**: runner binding selector beside the instance secrets/variables
   editors (default "server"), runners page (connected runners, token reset),
   runner badge on workflow steps in run history.
4. **First real consumer**: a `kingdee.*` node family.

Phases 1–2 are independently testable: unit tests fake the dispatcher on the
workflow side and fake the hub on the CLI side; an integration test boots the
server with `httptest`, connects a real runner process, and drives a workflow
whose remote step executes an `echo` node.

## Open questions

- Should `GET /api/runners` / token reset be surfaced in the web UI settings
  area from phase 1, or is API-only acceptable until the UI phase?
- Job payload size: row-heavy nodes could return large outputs; is a hard
  frame size limit (e.g. 16 MiB) enough, or do large results need chunking?
- A binding referencing a runner that never connects only surfaces at exec
  time; should the runners page flag bindings whose runner is currently
  absent?
