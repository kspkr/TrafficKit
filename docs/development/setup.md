# Development

## Prerequisites

| Tool | Version | Needed for |
|---|---|---|
| Go | 1.25+ | engine, CLI, Go tests |
| Node | 20+ | UI, dev runner, UI tests |
| Rust + Tauri prerequisites | stable | desktop app only ([Tauri guide](https://v2.tauri.app/start/prerequisites/)) |

You can work on the engine and UI without Rust: `npm run dev` runs the UI in
your browser against a real engine.

## Running

```sh
npm install
npm run dev
```

`scripts/dev.mjs` builds the engine into `.dev/`, runs it with
`--api-port 47821 --dev-state .dev/engine.json`, and starts Vite. The Vite dev
server proxies `/v1/*` to the engine and adds the API token from the state
file, so the token never reaches the browser. Ctrl+C stops both.

Change the API port with `TK_DEV_API_PORT`. The proxy uses the normal config
(port 8877 unless you've changed it).

### Desktop app

```sh
cd apps/desktop
npm run tauri:icons   # once: generates src-tauri/icons from public/icon.svg
npm run tauri:dev
```

`tauri:dev` builds the engine as a sidecar
(`src-tauri/binaries/traffickit-<target-triple>`), then runs `tauri dev`. The
shell spawns the sidecar, reads its ready line and passes `{apiUrl, token}`
to the webview via the `engine_info` command.

Verified on Windows 11 (Rust 1.98, VS 2022 C++ tools, WebView2): the app
starts the engine, captures traffic, and the engine exits when the window
closes. macOS and Linux builds haven't been run yet.

## Tests

```sh
npm test            # everything
npm run test:go     # go test ./...
npm run test:ui     # vitest in apps/desktop
go test -race ./... # needs cgo
```

What's covered:

- `internal/proxy`: forwarding, hop-by-hop header removal, body capture
  limits, streaming flush, upstream failures, loop refusal, CONNECT tunnels,
  protocol upgrades, shutdown of hijacked connections, TLS error
  classification. All against real sockets.
- `internal/api`: token, Host and Origin checks, CORS preflight, input
  validation, paging, body endpoint headers, the event stream.
- `internal/engine`: proxy start/stop/restart, port conflicts, failed moves.
- `tests/integration`: builds the binary, runs `traffickit serve` the way the
  shell does, captures traffic through it, checks exit-on-stdin-close.
- UI: NDJSON parsing across chunk boundaries, engine client errors, store
  revision handling, filter/sort, body detection, table virtualization, and
  that captured values render as text.

## Code layout

See [architecture/overview.md](../architecture/overview.md#repository-layout).

## Building a release

```sh
cd apps/desktop
npm run tauri:build
```

This builds the engine sidecar, the UI and the shell, and writes installers to
`apps/desktop/src-tauri/target/release/bundle/` (on Windows: an `.msi` and an
NSIS `-setup.exe`). Set `TRAFFICKIT_VERSION` to stamp the engine version.

Cross-platform installers and signing are phase 5 work. The plan:

- **Windows**: MSI/NSIS via Tauri, Authenticode signing with the certificate
  in CI secrets.
- **macOS**: universal `.dmg`, Developer ID signing and notarization
  (`APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_PASSWORD` in CI). The sidecar must be
  signed with the same identity.
- **Linux**: AppImage, `.deb` and `.rpm`.

Builds will run on native runners per platform (the sidecar is pure Go with
`CGO_ENABLED=0`, so it cross-compiles, but notarization needs macOS).
