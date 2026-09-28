# Data model

The unit of capture is an **exchange**: one request and (maybe) its response,
or one CONNECT tunnel. Field names below are the JSON names used by the API.

## Exchange

```
Exchange {
  id: uint64            // allocated by the store, increasing, never reused
  rev: uint64           // bumped on every write; clients keep the highest
  kind: "http" | "tunnel"
  state: "pending" | "streaming" | "complete" | "failed"
  started: RFC 3339 timestamp
  client: string        // client address as seen by the proxy
  upgraded: bool        // 101 Switching Protocols
  request: Request
  response?: Response   // absent until response headers arrive
  timings: Timings
  error?: Failure
  websocket?: {messages, sent, received, compressed, closeCode, closeReason}
}

Request {
  method, url, scheme, host, path, proto: string
  headers: [{name, value}]
  body: Body
}

Response {
  status: int
  statusText: string    // reason phrase as sent by the server
  proto: string
  headers: [{name, value}]
  body: Body
}

Body {
  size: int64           // bytes that crossed the proxy
  captured: int64       // bytes stored (≤ capture.maxBodyBytes)
  truncated: bool
  encoding?: string     // Content-Encoding as sent
}

Timings {               // milliseconds, -1 when the phase did not happen
  dns, connect, tls, send, wait, receive, total: float64
}

Failure {
  code: "dns" | "refused" | "timeout" | "reset" | "tls" | "loop"
      | "client_closed" | "upstream" | "hijack"
  message: string       // one sentence for people
  detail: string        // Go error text
  hint?: string         // what to try
}
```

`connect` excludes the TLS handshake, which is reported separately. When a
pooled connection is reused, `dns`, `connect` and `tls` are -1.

### Header order

Go's HTTP parser stores headers in a map, so the original order and name
casing are lost by the time the proxy sees them. Headers are reported sorted
by canonical name. Preserving wire order requires our own HTTP/1.1 parser and
is tracked in the roadmap; it matters for fingerprinting-sensitive debugging
but not for most API work.

## Summary

What the list view and the event stream carry. Kept small on purpose.

```
Summary {
  id, rev, kind, state, started, upgraded
  method, scheme, host, path, proto
  status?: int, statusText?: string
  contentType?: string   // response Content-Type, parameters stripped
  reqSize, respSize: int64
  duration: float64      // ms, -1 while in flight
  error?: string         // Failure.code
}
```

## Storage (current)

`traffic.Store` keeps exchanges in memory, ordered by ID, capped at
`capture.maxExchanges` with oldest-first eviction. Body bytes live alongside
the exchange but are only served through the body endpoint.

## Storage (phase 2 plan)

Persistent sessions move to an on-disk layout per session:

```
<dataDir>/sessions/<session-id>/
  meta.sqlite     exchanges, headers, timings, tags, notes, FTS index
  bodies/         append-only segment files, 64 MiB each
```

Metadata rows reference bodies by `(segment, offset, length)`. Bodies are
written once and never edited, so a segment can be dropped as a unit when
retention expires. The `Store` interface stays the same so the proxy and API
do not change.
