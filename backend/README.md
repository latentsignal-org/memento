# Memento Backend

The backend is the Go side of Memento. It owns the CLI, local HTTP API,
SQLite migrations, materialized rollups, deterministic extraction, and durable
agent runner. It treats msgvault-owned archive tables as read-only and writes
only Memento-owned `memento_*` tables.

The release binary embeds the statically exported frontend from
`backend/internal/webui/dist`, so `memento app` and `memento serve` host both
the UI and API from one process on `127.0.0.1`.

## Useful Commands

From the repository root:

```bash
pnpm build:backend
./memento app --demo
./memento doctor
./memento refresh
```

From `backend/` without a prebuilt binary:

```bash
go run ./cmd/memento stats
go run ./cmd/memento inspect-schema
go run ./cmd/memento migrate
go run ./cmd/memento serve --port 8787
go test ./...
```

Use `MEMENTO_MSGVAULT_DB=/path/to/msgvault.db` or `--db PATH` to override the
auto-detected msgvault database.

## Package Map

- `cmd/memento`: CLI entrypoints.
- `internal/server`: HTTP routes, local request guards, jobs, SSE, debug tools.
- `internal/webui`: embedded static frontend handler and dynamic slug rewrites.
- `internal/store`: `memento_*` migrations and persistence helpers.
- `internal/msgvault`: read-only adapter for msgvault archive tables.
- `internal/refresh`: materialized report rebuilds.
- `internal/agentrunner`: durable provider-neutral agent loop.
- `internal/person`, `internal/people`, `internal/project`,
  `internal/concept`, `internal/newsletter`, `internal/social`: domain logic.
