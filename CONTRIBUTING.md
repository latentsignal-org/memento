# Contributing to Memento

Thanks for your interest. Memento is early (0.x) and the public interface is
still settling, so please open an issue to discuss anything non-trivial
before investing significant effort.

## Development setup

Prerequisites: Go 1.26+, Node 22+, pnpm, and a configured
[msgvault](https://github.com/kenn-io/msgvault) archive (or use demo mode).

```bash
pnpm install
pnpm build:backend     # builds ./memento (UI placeholder only)
./memento serve --demo # Go API + demo archive on http://127.0.0.1:8787
pnpm dev               # Next.js dev server on :3000, proxies /api to :8787
```

Frontend changes hot reload through `pnpm dev`. Backend changes require
rebuilding `./memento` and restarting the Go process.

For a production-style run (static UI embedded in the binary):

```bash
pnpm package           # next build -> stage into Go embed -> go build
./memento app
```

## Code layout

- `backend/` — Go: HTTP API, agent runner, rollups, CLI (`cmd/memento`).
- `src/` — Next.js UI, statically exported and served by the Go backend.
- `docs/` — architecture documentation; start with `docs/spec-current-state.md`.
- `AGENTS.md` — working rules if you develop with AI coding agents.

## Tests

```bash
cd backend && go test ./...
pnpm lint
pnpm build   # the static export build is the frontend smoke test
```

## Pull requests

- Keep PRs focused; one change per PR.
- Backend changes need `go test ./...` passing.
- Describe user-visible behavior changes in the PR description.
- Durable, non-obvious decisions belong in `DECISIONS.md` (sparse, timestamped).
