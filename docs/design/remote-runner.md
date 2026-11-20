# Runners: egress proxies for workflows

Status: implemented (`internal/runnerhub`, `internal/runnercli`,
`cmd/autable-runner`). Replaces the node-executing runners of the
node-based workflows.

## Problem

Workflows run inside the autable server. A workflow that calls a system
inside a private network (an on-premise ERP, an internal database API)
cannot reach it when autable is hosted elsewhere.

## Model

A **runner** is a small process inside the private network. It connects
*outbound* to the server over WebSocket and performs HTTP requests on the
server's behalf: `fetch(url, { via: "office" })` in a workflow sends the
request through the runner named `office` of the workflow's database.

```
  private network                        public network
 ┌───────────────────────┐             ┌──────────────────────────┐
 │ internal HTTP service │             │ autable server           │
 │          ▲            │             │  workflow run            │
 │          │ HTTP       │             │   fetch(..., {via})      │
 │  autable-runner  ─────┼── outbound ─▶  runner hub (WebSocket)  │
 └───────────────────────┘  WebSocket  └──────────────────────────┘
```

- A runner executes **no code** and stores nothing. Each request carries
  everything it needs (method, URL, headers, body, timeout), so new
  integrations never require updating a runner.
- The runner applies the same limits as the server's own `fetch`
  (`internal/egress`): 20 MiB response cap, at most 10 redirects, the
  request's timeout (default 30 s).
- A request to a runner that is offline, or that disconnects mid-request,
  fails the `fetch` call visibly. There is no queueing or retry.

## Token

Runner tokens are per database: `POST /api/databases/{db}/runners`
(admins) issues a new token and disconnects that database's runners;
`GET /api/databases/{db}/runners` lists the token's creation time and the
connected runners (`name`, `version`, `connected_at`). The token decides
the database, so a runner can only serve the workflows of its database.
When several runners connect with the same name, the newest connection
receives the requests.

## Protocol

`GET /api/runner/ws` with `Authorization: Bearer <runner token>`, then
JSON text frames:

| Direction | Frame |
|---|---|
| runner → server, first frame | `{"kind": "hello", "name": "office", "version": "v0.1.70"}` |
| server → runner | `{"kind": "fetch", "job_id": "…", "request": {"method", "url", "headers": [[name, value], …], "body": "<base64>", "timeout_ms"}}` |
| runner → server | `{"kind": "result", "job_id": "…", "response": {"status", "status_text", "url", "headers": [[name, value], …], "body": "<base64>"}}` or `{"kind": "result", "job_id": "…", "error": "…"}` |

The runner pings every 30 s; the server drops connections silent for
90 s. The server gives up on a request 15 s after its own timeout.

## Runner CLI

```
autable-runner --endpoint https://autable.example.com --token "$AUTABLE_RUNNER_TOKEN" --name office [--max-jobs 4]
```

It reconnects with exponential backoff and logs each request's method,
URL without query or credentials, status and duration.

## Security considerations

- A runner sends whatever requests the workflows of its database ask for,
  so it should run with network access limited to the systems those
  workflows need.
- Secrets used in a request (an `Authorization` header) travel with that
  request over the runner's TLS connection; the runner neither logs nor
  keeps them.
