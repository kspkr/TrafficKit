# Contributing

Thanks for looking. A few things that make reviews quick:

- **Talk first about big changes.** Open an issue describing the problem
  before sending a large PR, especially anything touching the engine API or
  the security model.
- **Keep the layers separate.** Network code lives in Go under `internal/`.
  The UI never opens sockets; it goes through the engine API
  (`docs/architecture/ipc.md`). If you need something new from the engine,
  add an endpoint and document it.
- **Treat captured data as hostile.** Render it as text, cap its size, never
  log it. See `docs/security/model.md`.
- **Don't ship half a feature.** If it isn't wired up end to end, it doesn't
  go in the UI.
- **Tests.** Engine changes need Go tests that go through real sockets where
  it matters. UI logic goes in plain modules under `src/lib` with Vitest
  tests. Run `npm test` before pushing.
- **Style.** `gofmt` for Go. For JS, match the existing code: no semicolons,
  single quotes, 2-space indent (`.editorconfig`).

Commit messages: a short imperative summary line, then whatever context a
reviewer needs.

By contributing you agree your work is released under the MIT license.
