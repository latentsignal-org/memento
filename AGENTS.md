<!-- BEGIN:nextjs-agent-rules -->

# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read
the relevant guide in `node_modules/next/dist/docs/` before writing any code. Heed deprecation notices.
<!-- END:nextjs-agent-rules -->

# Memento Project Guidance

Working rules for agents and engineers. For implementation detail, use the linked docs below — do not duplicate them
here.

## Agent workflow rules

- Do not commit changes. After finishing a feature, bug fix, or other code/documentation change, provide a suggested
  commit message instead. Format it as a title, then a blank line, then a concise description.
- Do not run the full Playwright E2E suite. The user runs E2E tests manually. It is fine to recommend specific
  situations where running `pnpm test:e2e` would be valuable, such as after changing fixture data, Playwright config,
  app routing, or generation flows.
- Do not start backend or frontend development servers (`./memento serve`, `./memento serve --demo`,
  `./memento onboard --demo`, `go run ./cmd/memento serve`, `pnpm run dev`, `next dev`, etc.) unless the user explicitly
  asks in that turn. When server-based verification is useful, provide the exact commands for the user to run and wait
  for their results.
- Maintain [`DECISIONS.md`](DECISIONS.md) as a sparse, timestamped decision log. Add entries only for durable,
  non-obvious decisions that affect product behavior, data safety, architecture, public interfaces, or long-term
  maintenance. Do not use it for routine implementation notes, temporary plans, TODOs, test results, or decisions
  already obvious from code.
- For phased specs, pause after each phase: summarize exactly what changed, list how to test it, provide a suggested
  commit message, and wait for the user to test and commit manually before continuing.
- When the user asks multiple numbered or clearly separated questions, answer them one by one using the same
  numbering/structure. Include concrete references to files, specs, commands, or decisions where relevant instead of
  collapsing the response into one summary paragraph.

## Documentation map

| Read this                                                              | When you need                                                                               |
|------------------------------------------------------------------------|---------------------------------------------------------------------------------------------|
| [`DECISIONS.md`](DECISIONS.md)                                         | Sparse durable decision log for product, safety, architecture, and public-interface choices |
| [`docs/spec-current-state.md`](docs/spec-current-state.md)             | Stack, schema, HTTP API, env vars, dimension pages, rollup architecture                     |
| [`docs/agent-runtime.md`](docs/agent-runtime.md)                       | Agent loop, prompts, tools, providers, SSE durability, per-agent behavior                   |
| [`docs/agent-loop-and-prompts.md`](docs/agent-loop-and-prompts.md)     | Current agent execution flow and prompt workflows                                           |
| [`docs/agent-tools-reference.md`](docs/agent-tools-reference.md)       | Current agent tool availability, schemas, bindings, and debug behavior                      |
| [`docs/agent-context-management.md`](docs/agent-context-management.md) | Context window pressure, compact-tool strategy, open mitigations                            |
| [`docs/deterministic-extraction.md`](docs/deterministic-extraction.md) | What is deterministic vs LLM-driven across dimensions                                       |

## Product direction

Memento is not an email client and should not rebuild chronological inbox views. It is a local-first knowledge layer
over a `msgvault` archive that turns long-term email history into source-attributed living documents.

The core product promise is dimensional memory:

- **People** — relationship wikis for meaningful human contacts.
- **Projects** — bounded narratives for user-confirmed life/work projects.
- **Newsletters** — detected broadcast sources with generated coverage summaries.
- **Concepts** — user-declared evergreen topics, backfilled and maintained over time.
- **Dashboard** — executive overview plus freeform “Ask Memento” chat.

Every generated factual claim must trace back to source messages. User edits to generated documents must be preserved
across updates. Projects and Concepts must be proposed/declared and confirmed by the user before expensive generation
work.

## Current implementation (summary)

Local-first monorepo: a single Go binary serves both the statically exported Next.js UI and the API on
`http://127.0.0.1:8787`; msgvault SQLite is the read-only archive substrate. Run `./memento stats` for the SQLite DB
path. The frontend is a static export (`output: "export"`): no Node at runtime, all data fetching is client-side, and
dynamic `[slug]` routes are exported with a placeholder param that the Go server rewrites (see
`backend/internal/webui`).

- All four dimensions are live; `/` redirects to `/home`.
- Dimension indexes read **materialized rollup tables** (`memento_*_report`), refreshed by the backend.
- **Generative agents** (collector, project/concept compile, person enrich, dashboard router) run in the **Go agent
  runner**; the browser calls same-origin Go SSE endpoints directly (`backend/internal/server/browser_api.go`). See [
  `docs/agent-runtime.md`](docs/agent-runtime.md).
- Newsletter summaries and social label one-shots are separate provider-neutral **Go one-shot** flows, not the agent
  loop.
- Draft curation uses `memento_draft`; person enrichment uses `memento_note`, `memento_person_facet`,
  `memento_person_narrative`.
- The API binds to `127.0.0.1` only. No mock JSON fallbacks for dimension pages.

## Local development

```bash
pnpm package        # next build (static export) -> stage into Go embed -> go build
./memento app       # serve UI + API on :8787 and open the browser
```

For frontend iteration, run both dev processes:

```bash
./memento serve   # Go API + embedded UI on :8787; first-run DBs route to /onboard
pnpm run dev      # Next.js dev on :3000 — keep --webpack (Turbopack panics on this codebase); /api proxied to :8787
```

For a populated try-it demo, use `./memento app --demo`. For onboarding-flow testing
against synthetic raw email data, use `./memento onboard --demo`.

Without the binary: `cd backend && go run ./cmd/memento serve --port 8787`

Full CLI list, env vars, and startup notes: [`docs/spec-current-state.md` §5–§6](docs/spec-current-state.md).

### Graceful empty state

The API returns 200 with empty data (not 500) when Memento tables do not exist yet (e.g. after `reset` before `init`).
The UI handles empty states on all pages.

## msgvault integration

Use `msgvault` as the archive substrate (acquisition, messages, FTS, vector search, sync). The user’s vault is
configured at `~/.msgvault/config.toml` — invoke `msgvault` without overriding that path.

```bash
msgvault stats
msgvault quickstart          # agent onboarding guide
msgvault search "terms" --json
msgvault show-message <id> --json
```

Prefer `--json` for programmatic use. Keep msgvault access behind `backend/internal/msgvault` so schema changes do not
leak through the app.

Do not run deletion commands unless the user explicitly asks and confirms the exact command. Treat `delete-staged`,
account removal, and sync mutations as out of scope.

**Outbound detection:** `messages.is_from_me` is unreliable. The backend infers account-authored messages by joining
`messages.sender_id → participants.email_address` against `sources.identifier`.

## Architecture preferences

- Keep msgvault data read-only from Memento by default; new writes go to `memento_*` tables only.
- Start with deterministic extraction before LLM calls.
- Use source message IDs everywhere from the beginning.
- Prefer rollup/materialized tables for dimension index reads; do not put N+1 archive aggregation back on the request
  path.
- Design for paragraph/sentence-level citation later; a document-level source list is not enough.
- Version generated documents and track section ownership/user edits before implementing incremental LLM updates.
- Avoid graph complexity until there is a clear product reason. The system is SQLite + msgvault + rollups + agent
  workflows, not a graph database product.
