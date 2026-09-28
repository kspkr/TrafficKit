# Architecture

TrafficKit is three programs that talk over a local, authenticated HTTP API:

```
 ┌──────────────────────────────┐
 │ Desktop shell (Tauri, Rust)  │  owns the window, spawns the engine,
 │                              │  hands the UI its API address + token
 └──────────────┬───────────────┘
                │ webview
 ┌──────────────▼───────────────┐
 │ UI (React, apps/desktop/src) │  presentation only; holds summaries,
 │                              │  fetches details and bodies on demand
 └──────────────┬───────────────┘
                │ HTTP + NDJSON event stream on 127.0.0.1, bearer token
 ┌──────────────▼───────────────┐
 │ Engine (Go, cmd/traffickit)  │
 │  api      control API        │
 │  engine   lifecycle, wiring  │
 │  proxy    HTTP/1.1 proxy,    │
 │           CONNECT tunnels    │
 │  traffic  exchange model,    │
 │           store              │
 │  events   fan-out hub        │
 │  config   file/env/flags     │
 └──────────────────────────────┘
```

The engine is a normal Go binary and has no idea whether a webview, a
browser tab or a script is on the other end of its API. The same binary is
also the CLI (`traffickit proxy`, `traffickit serve`, ...), so there is one
network implementation to test and ship.

## Why these boundaries

**The UI never touches sockets.** Everything network-facing lives in Go. The
UI can crash, reload or be replaced without dropping a single proxied
connection.

**HTTP instead of Tauri IPC for the engine API.** Tauri's `invoke` would tie
the engine to Rust glue code and make the API impossible to use from the CLI,
tests or a browser during development. A loopback HTTP API with a random
per-launch token is just as private (see [security model](../security/model.md))
and trivially testable with `curl`. The Tauri side is reduced to spawning the
engine and returning `{apiUrl, token}` from one command.

**NDJSON over a streaming `fetch` rather than WebSockets or SSE.** `EventSource`
and browser WebSockets cannot send an `Authorization` header, which would push
the token into the URL. A streaming `fetch` response can, and NDJSON is easy to
parse incrementally and to debug by eye.

**Summaries in the UI, everything else on demand.** The event stream carries
~300-byte summaries (method, host, path, status, sizes, timing). Headers and
bodies are fetched only for the selected exchange. Bodies are never put into
React state beyond the one being viewed.

## Data flow for one request

1. A client sends `GET http://example.com/a` to the proxy port.
2. `proxy` allocates an ID from the store, records a `pending` exchange and
   forwards the request upstream. The request body is teed into a bounded
   capture buffer while it streams.
3. Response headers arrive: the exchange moves to `streaming` and is recorded
   again. The body is streamed to the client and teed into a second capture
   buffer.
4. On completion the exchange is recorded as `complete` (or `failed` with a
   structured error) with timings from `httptrace`.
5. Each `Record` call replaces the stored copy, bumps its revision and
   publishes an `exchange` event carrying the summary.
6. The UI batches events per animation frame and upserts by `(id, rev)`.

HTTPS (`CONNECT`) is currently tunneled without decryption and recorded as a
`tunnel` exchange with byte counts. Interception arrives with the certificate
authority in phase 2.

## Backpressure

Every event subscriber has a bounded channel. If a subscriber cannot keep up,
events are dropped for that subscriber only and it receives a `resync` event;
the UI then refetches the list. The proxy never blocks on a slow UI.

Captured bodies are capped (`capture.maxBodyBytes`, default 4 MiB per body);
anything past the cap is streamed to the client but not stored, and the
exchange is flagged as truncated. The in-memory store holds at most
`capture.maxExchanges` exchanges and evicts the oldest.

## Repository layout

```
cmd/traffickit/        engine + CLI entry point
internal/api/          control API (auth, routes, event stream)
internal/bodyutil/     content-encoding decoding with size limits
internal/config/       defaults, config file, env, validation
internal/engine/       owns store, hub, proxy lifecycle
internal/events/       pub/sub hub with per-subscriber backpressure
internal/proxy/        forward proxy, tunnels, capture, timings
internal/traffic/      exchange model and store
tests/integration/     engine-level tests through real sockets
apps/desktop/          React UI (src/) and Tauri shell (src-tauri/)
scripts/               dev runner, engine build helper
docs/                  this documentation
```

This departs from the `/services/proxy`, `/services/storage` split in the
original brief. Go works best as one module with `internal/` packages; the
separation between capture, processing, storage and presentation is enforced
by package boundaries rather than by directories that each need their own
module and release process. A `packages/` directory for shared JS will appear
once there is a second JS consumer (the plugin SDK).

Related documents: [IPC protocol](ipc.md), [data model](data-model.md),
[security model](../security/model.md), [roadmap](../roadmap.md).
