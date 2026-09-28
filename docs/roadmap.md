# Roadmap

Each phase ends only when its features work end to end, are tested, and are
documented. Features that are not built yet do not appear in the UI.

## Phase 1: foundation (current)

- [x] Monorepo, docs, dev runner
- [x] Go engine: config, structured logging, control API with token/Host/Origin checks
- [x] HTTP/1.1 forward proxy with streaming, bounded body capture, timings
- [x] CONNECT tunnels (recorded, not decrypted)
- [x] Protocol upgrades (101) relayed byte-for-byte
- [x] Structured upstream errors with hints
- [x] In-memory store with eviction, event hub with backpressure
- [x] React UI: virtualized traffic table, sorting, quick filter, inspector,
      settings, light/dark/system themes, keyboard navigation
- [x] Tauri desktop app that spawns the engine; Windows installers (MSI, NSIS) built and tested
- [x] `traffickit serve`, `traffickit proxy`, `traffickit version`

Known gaps carried forward: header order/casing (see data model), no
persistence, proxy settings not saved between launches.

## Phase 2: HTTPS and persistence (in progress)

- [x] Local CA, per-host leaf certificates with an LRU cache, regeneration
- [x] CONNECT interception (HTTP/1.1 to the client, h1/h2 upstream),
      passthrough hosts, clear reporting when a client rejects the certificate
- [x] Connect screen: launch Chrome, Edge, Brave, Chromium, Vivaldi in isolated
      profiles; a terminal preconfigured for curl, Node, Python, git and more
- [ ] HTTP/2 to the client (ALPN h2)
- [ ] Firefox launcher (needs a prepared profile and NSS certificate database)
- [ ] Optional, explicit system trust install/removal with status detection
- Body viewers: JSON tree (collapse, search, copy path/value), XML, HTML
  source, images, hex, form and multipart
- Filter language (`host:api.* status:>=400 method:POST -type:image`), regex,
  saved filters
- On-disk sessions (SQLite metadata + append-only body segments), retention
- Full-text search running in the engine with cancellation

## MCP server (done ahead of schedule)

- [x] `traffickit mcp` (stdio), connecting to the running app or standalone
- [x] Tools: status, list, get, search, wait for request, launch/close
      browser, proxy settings, clear
- [x] Credential redaction by default, visible "AI connected" indicator
- [ ] Replay, mock and rule tools as those features land

## Phase 3: acting on traffic

- Replay and edit-and-replay, response diff
- Request editor with structured ⇄ raw sync (Monaco)
- Code generation: cURL, HTTPie, fetch, Python requests, Go net/http
- Rule engine (match → actions) with a visual builder and an expression editor
- Mock endpoints served by the engine

## Phase 4: protocols and formats

- [x] WebSocket message capture and inspector (text, binary, control frames,
      fragmentation, permessage-deflate), MCP tool
- HAR import/export
- OpenAPI 3.x import, endpoint browser, templates
- Collections
- Advanced search

## Phase 5: distribution and extension

- Full CLI (`replay`, `import`, `export`, `mock`, `rules`, `session`)
- Plugin API (WASM-sandboxed processors and viewers)
- Android / iOS / emulator setup guides
- Performance work for 100k+ exchanges
- Signed installers for Windows, macOS (notarized), Linux (AppImage, deb, rpm)
