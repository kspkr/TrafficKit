# Engine control API (v1)

The UI, the CLI and tests all drive the engine through this API. It listens
on `127.0.0.1` only. The version prefix changes only on breaking changes.

## Startup handshake

`traffickit serve` binds the API, then prints exactly one JSON line to stdout:

```json
{"event":"ready","version":"0.1.0","api":"http://127.0.0.1:53817","token":"b1Qm...","proxy":{"running":true,"address":"127.0.0.1:8877"}}
```

Everything else the engine writes goes to stderr as structured logs. The
desktop shell reads this line, keeps the token in memory and returns it to the
webview through the `engine_info` command. With `--exit-on-stdin-close` the
engine exits when its parent closes stdin, so it cannot outlive the app.

For development, `--dev-state <file>` also writes the ready line to a file
(mode 0600) so the Vite dev server can inject the token server-side.

## Authentication and request checks

Every request except CORS preflight must satisfy all of:

| Check | Rule | Failure |
|---|---|---|
| Host | `127.0.0.1:<port>`, `localhost:<port>` or `[::1]:<port>` | 421 |
| Origin | absent, or in the allow-list (`--allow-origin`, Tauri origins) | 403 |
| Token | `Authorization: Bearer <token>`, constant-time compare | 401 |

The Host check defeats DNS rebinding; the Origin check stops other web pages
from reading responses; the token stops other local processes and users.

Request bodies are limited to 1 MiB and decoded with unknown fields rejected.

Clients other than the UI should send `X-TK-Client: <name>`. The engine
lists recent clients in `GET /v1/status` and the `clients` event, and the UI
shows them, so the user always knows when something else is reading
traffic.

## Errors

Non-2xx responses carry:

```json
{"error":{"code":"port_in_use","message":"Port 8877 is already in use.","hint":"Pick another port in Settings or stop the program using it."}}
```

`code` is stable and machine-readable; `message` and `hint` are for people.

## Endpoints

### `GET /v1/status`

```json
{"version":"0.1.0","proxy":{...ProxyStatus},"exchanges":42,"capture":{"maxBodyBytes":4194304,"maxExchanges":50000}}
```

### `POST /v1/proxy/start`

Body (all optional): `{"bind":"127.0.0.1","port":8877}`. Restarts the proxy if
it is running on a different address. Returns `ProxyStatus`.

Errors: `invalid_port`, `invalid_bind`, `remote_bind_disabled`, `port_in_use`,
`listen_failed`.

### `POST /v1/proxy/stop`

Returns `ProxyStatus`. Open tunnels are closed.

### `GET /v1/exchanges?after=<id>&limit=<n>`

Summaries with `id > after`, ascending, `limit` defaults to 1000, max 5000.
Response: `{"items":[Summary...],"more":true}`.

### `GET /v1/query`

Newest-first summaries with server-side filtering, for clients that don't
mirror the whole list. Parameters (all optional): `filter` (quick-filter
syntax), `host`, `method`, `status_min`, `status_max`, `failed=1`, `before`,
`after` (IDs), `limit` (default 50, max 1000). Response:
`{"items":[Summary],"more":bool,"lastId":n}`.

### `GET /v1/search?q=<text>&case=1&limit=<n>`

Searches URLs, header lines and decoded bodies, newest first, one hit per
exchange: `{"items":[{"exchange":Summary,"where":"response body","snippet":"…"}]}`.
`where` is `url`, `request header`, `response header`, `request body` or
`response body`.

### `GET /v1/exchanges/{id}`

Full `Exchange` without body bytes. 404 `not_found`.

### `GET /v1/exchanges/{id}/body/{side}?decode=1`

`side` is `request` or `response`. Returns raw captured bytes as
`application/octet-stream` with `X-Content-Type-Options: nosniff` and
`Content-Security-Policy: sandbox`, regardless of what the captured content
type was. With `decode=1`, `gzip`, `deflate`, `br` and `zstd` content
encodings are removed (output capped at 64 MiB).

Response headers:

- `X-TK-Truncated: 1` capture hit `maxBodyBytes`
- `X-TK-Decoded: gzip` encoding that was removed
- `X-TK-Decode-Error: ...` decoding failed; raw bytes returned instead

### `DELETE /v1/exchanges/{id}` / `DELETE /v1/exchanges`

Delete one / all. 204. Emits `removed` / `cleared`.

### HTTPS

- `GET /v1/https` → `{intercept, passthrough, ca: {subject, notBefore, notAfter, sha256, spki, path}}`
- `POST /v1/https` with `{intercept?, passthrough?}` → same. Errors: `invalid_host`.
- `GET /v1/https/ca.pem` → the CA certificate (never the key).
- `POST /v1/https/ca/regenerate` → new CA; launched browsers are closed.
- `POST /v1/https/ca/reveal` → opens the file manager at the certificate.

### Launching

- `GET /v1/targets` → `{items: [{id, kind, name, description, path}]}`,
  what can be launched on this machine (`kind` is `browser` or `terminal`).
- `GET /v1/sources` → `{items: [Source]}` currently running launches.
- `POST /v1/sources` with `{target, url?}` → 201 `Source`. `url` must be an
  absolute http(s) URL. Errors: `proxy_stopped`, `invalid_url`,
  `launch_failed`, 404 for an unknown target.
- `DELETE /v1/sources/{id}` → closes it. 204.

```
Source { id, target, kind, name, pid, started, tracked }
```

`tracked` is false when the OS hands the window to another process (macOS
Terminal, most Linux terminals); those can't be closed by TrafficKit.

### `GET /v1/events`

`Content-Type: application/x-ndjson`. One JSON object per line:

| `type` | `data` | Meaning |
|---|---|---|
| `hello` | `{"proxy","https","sources","lastId"}` | First line on every connection |
| `exchange` | `Summary` | Created or changed; upsert if `rev` is newer |
| `removed` | `{"ids":[...]}` | Deleted or evicted |
| `cleared` | `{}` | All traffic deleted |
| `proxy` | `ProxyStatus` | Proxy started/stopped |
| `https` | HTTPS status | Settings changed or CA regenerated |
| `sources` | `[Source]` | A launch started or exited |
| `clients` | `[{name, lastSeen}]` | A non-UI client (e.g. an MCP assistant) used the API |
| `resync` | `{}` | This subscriber dropped events; refetch the list |
| `ping` | `{}` | Every 15 s, keeps intermediaries from timing out |

Clients should subscribe first, then fetch `/v1/exchanges`, and merge by
`rev`, so nothing is lost between the two.

## Types

See [data-model.md](data-model.md) for `Exchange` and `Summary`.

```
ProxyStatus {
  running: bool
  address: string      // "127.0.0.1:8877" when running
  bind: string
  port: int
  error?: ApiError     // last start failure, cleared on success
}
```
