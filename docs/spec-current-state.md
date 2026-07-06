# Memento Current-State Spec

Document status: authoritative implementation handoff
Last updated: May 30, 2026
Audience: another agent or engineer picking up active development

## 1. What Memento is right now

Memento is a local-first knowledge layer over a `msgvault` archive. It is not an email client and does not rebuild a
chronological inbox. It turns long-term email history into four navigable dimensions plus a dashboard:

1. People: relationship wikis backed by resolved identities and message history.
2. Projects: bounded narratives over user-confirmed message bundles.
3. Newsletters: detected broadcast sources with generated coverage summaries.
4. Concepts: opt-in evergreen topic pages built from curated evidence bundles.
5. Dashboard: an executive synthesis layer plus a freeform Memento router agent.

The current product is a hybrid local app:

- **Go** owns msgvault access, SQLite persistence, deterministic extraction, rollup refresh, CRUD, the durable agent
  runtime, and public/internal HTTP APIs.
- **Next.js** owns the UI and dashboard composition. It is statically exported and embedded into the Go binary; the
  browser calls Go directly (no proxy layer).

Agent loop architecture, prompts, and tools: [`docs/agent-runtime.md`](agent-runtime.md).

For a cross-dimension guide to deterministic extraction, including inputs, outputs, tables, refresh triggers, and LLM
boundaries, see [`docs/deterministic-extraction.md`](deterministic-extraction.md).

The product promise is still source-attributed living documents, but the current implementation is uneven across
dimensions:

- People: deterministic rollup + AI enrichment are both implemented.
- Projects: draft curation and AI narrative compile are implemented.
- Newsletters: detection and summary generation are implemented, but newsletter detail is still simpler than
  projects/concepts.
- Concepts: opt-in draft curation and AI compile are implemented.
- Dashboard: implemented as a frontend-composed overview plus a chat router agent.

## 2. Product problem and positioning

### The problem

Adults accumulate years of email — often 100K–500K messages — with real signal about relationships, projects, decisions,
money, subscriptions, and evolving interests. Conventional email tools treat the archive as a chronological stream.
Search is keyword-based; AI features focus on recent inbox triage. Long-term archives are effectively write-only.

That leaves relationship history, multi-year projects, newsletter knowledge, and durable topics without a consolidated,
source-attributed view.

### The approach

A **local-first** system that reads a `msgvault` archive and maintains **source-attributed living documents** across
four user-facing dimensions plus a dashboard router. Memento does not replace the inbox or rebuild a chronological mail
client.

### Positioning

The product claim is **dimensional living documents from email**, not generic “GraphRAG on mail.” Documents should cite
real `message_id` values, support user curation before expensive generation, and improve over time as the archive grows.

### Architectural commitments (product level)

- **Local-first:** data stays on the user's machine; API binds to `127.0.0.1`.
- **Source-attributable:** factual claims trace to source messages via inline citations and stored ID lists.
- **Incremental (target):** new mail should update documents without full regeneration; not fully implemented for all
  dimensions yet.
- **User-curatable (target):** user edits and notes must survive updates; partial today (notes, bundle curation;
  section-level versioning still incomplete).
- **Hybrid retrieval:** FTS, vector search, rollups, and deterministic extraction — not a separate graph database
  product.
- **Opt-in expensive work:** projects and concepts require user-confirmed bundles before compile; concepts are
  explicitly opt-in.

### Explicitly out of scope

Unless this spec is updated:

- Email client features (send, reply, inbox triage as primary UX).
- Real-time inbox tooling; Memento operates on archives with periodic refresh.
- Cloud-synced multi-device V1.
- General-purpose PKM unrelated to the mail archive.
- Mobile-native app (current UI is desktop web).

## 3. Dimension product specification

Each dimension has its own pipeline, document shape, and update rules. Shared infrastructure: msgvault read path, SQLite
`memento_*` tables, rollups, Go agents where noted.

### People (relationship wikis)

**Unit:** one page per meaningful human contact.

**Meaningful contact bar (product intent):** bidirectional human interaction, sufficient volume, exclusion of
noreply/system/list-only identities. Enforced in `backend/internal/people` candidate classification and person
resolution — not a separate graph.

**Target page content:**

- Identity header: name, primary email, aliases
- Relationship narrative: summary, arc, current status (agent-generated, citation-backed)
- Structured facets: interests, life events, topics, relationship signals, facts
- Timeline of significant exchanges (deterministic rollup)
- User notes (authoritative for the person agent)
- Social context: correspondents, groups/clusters where implemented

**Update triggers:** person enrich agent, rollup refresh, new mail reflected on next refresh/enrich.

**Current gaps vs full vision:** re-engagement CTAs are UI stubs; incremental narrative merge without full re-enrich is
not implemented; open-thread extraction is not a dedicated section.

### Projects (bounded narratives)

**Unit:** one narrative per user-confirmed life/work project with a beginning and end (possibly fuzzy).

**Discovery:** two-step — collector agent proposes a message/people bundle; user curates and commits via
`memento_draft`. User may also attach messages manually through existing project APIs.

**Lifecycle (product intent):** proposed → active → dormant → completed → archived. Email patterns may suggest
transitions; user confirmation is preferred for expensive state changes. Current UI exposes status markers but not a
full state machine.

**Target document sections (shipped compile output):**

- `summary` — executive overview
- `phases` — chronological JSON array with citations
- `friction_points` — material disruptions only
- `current_understanding` — status anchored to latest evidence

**Also on detail pages when present:** members, decisions, expenses, attachments, message timeline.

**Quality bar:** every prose claim cites `[msg:<id>]`. Inferred claims must be marked Likely/Possible. User-edited
sections should be preserved on recompile (versioning not fully built).

**Computational cost:** high — project compile agent.

### Newsletters (broadcast synthesis)

**Unit:** detected newsletter **sources** with generated coverage pages.

**Product vision (partially built):** newsletters are one-to-many; the relevant unit is often an *item* within an
edition, not the whole MIME message. Longer term: per-item extraction, action vs. knowledge split, cross-source topic
rollups, and deduplicated “same story from four senders” views.

**Shipped today:**

- Source detection and index from rollups
- Per-source detail with coverage summary, recurring themes, notable items, message timeline
- Go-side provider-neutral one-shot generation (`POST /api/newsletters/{slug}/generate`)

**Not shipped:** action queue with expiry, per-topic rollup pages across newsletters, template-learning item parser,
cross-newsletter deduplication pipeline.

### Concepts (evergreen topics)

**Unit:** one page per **user-declared** topic.

**Opt-in rule:** the system does not auto-create concept pages. User starts a draft, curates evidence, commits — same
pattern as projects.

**Target document sections (shipped compile output):**

- `scope_summary`
- `distilled_insights` — thematic JSON array, not chronological phases
- `evolving_understanding`

**Quality bar:** relevance over recall; cap noise; cite sources; scope description from user at creation time.

### Cross-dimension relationships

Documents should cross-link where evidence supports it (people on projects, concepts touching multiple sources). Today:
members on projects, source maps on concepts, dashboard router can navigate between entities. Automatic bidirectional
wiki links across all dimensions are not fully maintained.

## 4. Guardrails

Non-negotiable without explicit spec update:

1. **Local-first default:** no accidental broad egress; LLM calls are user-configured (e.g. `MEMENTO_MODEL_API_KEY` in
   local `.env`).
2. **No unsourced factual claims** in generated documents — real `message_id` citations required.
3. **User edits and notes are authoritative** — agents must not override user notes with guesses; preserved edits across
   regenerations is a continuing requirement.
4. **Projects and concepts are user-confirmed** before expensive compile; concepts remain opt-in.
5. **Meaningful-contact thresholds enforced in code** — system/newsletter identities must not receive People pages.
6. **Schema changes are explicit** — `memento_*` migrations only; msgvault tables stay read-only from Memento.
7. **No scope creep** — see out-of-scope list in §2 and [`AGENTS.md`](../AGENTS.md).

## 5. Glossary

- **Archive:** the user's mail corpus in msgvault (read-only to Memento).
- **Living document:** a dimension page maintained from mail evidence and optional agent passes.
- **Dimension:** People, Projects, Newsletters, or Concepts (plus Dashboard as synthesis/router).
- **Meaningful contact:** a person meeting the human-interaction bar for a People page.
- **Rollup / materialized report:** precomputed `memento_*_report` table used for fast index reads.
- **Bundle / draft:** curated message and people set in `memento_draft` before project/concept commit.
- **Compile / enrich:** agent runs that write project/concept narratives or person facets/sections.
- **Item (newsletter vision):** a discrete story/section inside one newsletter edition — not yet first-class in storage.
- **Action item (newsletter vision):** time-sensitive task extracted from newsletter content — not yet implemented as a
  queue.

## 6. Verified current runtime state

Typical setup:

- **Release / single binary:** `memento app` (or `memento serve`) runs one Go
  process on `127.0.0.1:8787` that serves both the embedded web UI and the API.
  No Node.js runtime.
- **Frontend development only:** `pnpm run dev` runs `next dev` on
  `127.0.0.1:3000` and proxies `/api/*` to the Go backend on `:8787`.
- `/` redirects to `/home`.

Before substantial changes, verify:

```bash
cd backend && go test ./...
pnpm run build
```

## 7. Stack and versions

Frontend:

- Next.js `16.2.6`
- React `19.2.4`
- TypeScript `5`
- Tailwind CSS `4`
- `lucide-react`
- small custom UI layer plus generated shadcn/base-ui primitives

Backend:

- Go module in `backend/`
- SQLite via `modernc.org/sqlite`
- `msgvault` as the archive substrate

Runtime model assumptions:

- Default Gemini model: `gemini-3.5-flash`
- Agent runtime uses the Go agent runner in `backend/internal/agentrunner`
- Newsletter summary generation and social cluster one-shot helpers happen in Go

## 8. Top-level repo map

Repo root:

- `src/`: Next.js app, statically exported (`output: "export"`) and embedded
  into the Go binary. The browser calls the Go API directly via relative
  `/api/*` paths — there are no Next.js API/proxy routes.
- `backend/`: Go backend, including `internal/webui` which embeds and serves the
  exported UI.
- `docs/`: handoff and reference docs (`agent-runtime.md`, `deterministic-extraction.md`, …)
- `AGENTS.md`: product rules and doc index (start here)
- `docs/spec-current-state.md`: this file
- `docs/agent-runtime.md`: Go agent loop reference

Important frontend directories:

- `src/app/`: app router pages; there are no Next.js API routes
- `src/components/`: UI components and agent UX widgets
- `src/lib/`: API client, citation rendering, masking, data helpers

Important backend directories:

- `backend/cmd/memento/`: CLI entrypoints
- `backend/internal/config/`: env loading and msgvault DB path resolution
- `backend/internal/msgvault/`: read-only msgvault adapter
- `backend/internal/store/`: migrations and store helpers
- `backend/internal/person/`: participant clustering and canonical person resolution
- `backend/internal/people/`: people candidate report generation
- `backend/internal/project/`: project CRUD and report building
- `backend/internal/newsletter/`: newsletter source detection and summary generation
- `backend/internal/concept/`: concept CRUD and report building
- `backend/internal/refresh/`: materialized rollup rebuilds
- `backend/internal/server/`: public API, internal agent tool API, jobs, notes, drafts

## 9. Startup and local commands

Release-style startup:

```bash
./memento app --demo   # populated synthetic archive, safest first run
./memento app          # real msgvault archive; routes to /onboard if needed
```

Source development startup:

```bash
pnpm build:backend
./memento serve --demo # terminal 1: Go API on :8787
pnpm run dev           # terminal 2: Next.js dev server on :3000
```

Without the built binary:

```bash
cd backend
go run ./cmd/memento init
go run ./cmd/memento serve --port 8787
go run ./cmd/memento refresh
go test ./...
```

Useful frontend commands:

```bash
pnpm run dev
pnpm run build
```

Important CLI commands currently supported by `./memento`:

- `init`
- `reset`
- `stats`
- `inspect-schema`
- `migrate`
- `people-candidates`
- `people-pages`
- `person-resolve`
- `person-show`
- `person-link`
- `person-split`
- `person-merge`
- `person-merge-suggest`
- `person-repair-nondeterministic`
- `project`
- `project-pages`
- `newsletter-detect`
- `newsletter-generate`
- `newsletter-pages`
- `concept`
- `concept-pages`
- `refresh`
- `serve`

## 10. Environment and config

Environment loading:

- The Go backend calls `config.LoadDotEnv()` and searches upward for `.env` or `.dev.vars`.
- The frontend also reads the same repo-root `.env` via Next.js.

Current important env vars:

- `MEMENTO_MODEL_API_KEY`
- `MEMENTO_MODEL_BASE_URL`
- `MEMENTO_AGENT_STEP_LIMIT` default `20`
- `MEMENTO_AGENT_MODEL` default `gemini-3.5-flash`
- `MEMENTO_<NAME>_MODEL` per-agent overrides, e.g. `MEMENTO_PERSON_MODEL`
- `MEMENTO_AGENT_VERBOSE_LOGS=1` enables extra provider request logs and raw SSE lines
- `MEMENTO_AGENT_SIMULATION=1` enables simulated agent SSE runs (no LLM token usage) for project/concept/person
  generation routes
- `MEMENTO_AGENT_SIMULATION_DELAY_MS` optional pacing for simulated events (default `220`)
- `NEXT_PUBLIC_MEMENTO_AGENT_SIMULATION=1` shows "Simulate" buttons in Project/Concept/Person generation UI
- Per-request override: append `?sim=1` to generation endpoints (or the project page URL, which now drives project
  generation calls in sim mode)
- `MEMENTO_MSGVAULT_DB` optional direct DB path override
- `MEMENTO_MSGVAULT_HOME` optional msgvault home directory override
- `MEMENTO_MSGVAULT_API_URL` optional `msgvault serve` URL

Development/internal-only env vars:

- `MEMENTO_BACKEND_URL` targets the `next dev` `/api/*` proxy and defaults to
  `http://127.0.0.1:8787`.
- `MEMENTO_INTERNAL_TOKEN` gates `/api/internal/*` tooling endpoints. It is not
  part of the normal browser UI contract.

What `memento init` does now:

1. Migrates all `memento_*` tables.
2. Prompts for owner name and owner email and stores them in `memento_config`.
3. Prompts for model provider, model name, optional base URL, and optional API key, using existing `.env` values as
   defaults when present.
4. Stores `MEMENTO_MODEL_PROVIDER`, `MEMENTO_AGENT_MODEL`, `MEMENTO_MODEL_BASE_URL`, and optionally
   `MEMENTO_MODEL_API_KEY` in repo-root `.env`.
5. Seeds optional internal/development defaults such as `MEMENTO_INTERNAL_TOKEN`,
   `MEMENTO_BACKEND_URL`, and `MEMENTO_AGENT_STEP_LIMIT` when needed.
6. Resolves deterministic person clusters and persists advisory merge suggestions.
7. Builds people candidates.
8. Detects newsletter sources.
9. Refreshes all materialized rollup tables.
10. Builds the `./memento` binary when possible.

Msgvault path resolution order:

1. `MEMENTO_MSGVAULT_DB`
2. `MEMENTO_MSGVAULT_HOME/msgvault.db`
3. `~/.msgvault/config.toml` `[data].data_dir/msgvault.db`
4. fallback `~/.msgvault/msgvault.db`

## 11. Data ownership model

Hard rule:

- msgvault tables are read-only from Memento.
- Memento may only mutate tables with the `memento_` prefix.

This is enforced in code by `assertMementoOnlyMigrations` in `backend/internal/store/migrations.go`.

The system currently uses SQLite as the single writable persistence layer. There is no external queue, separate job
database, or graph store in the implementation.

## 12. Current schema summary

The current migration version is 27.

### 8.1 Core Memento tables

People and identity:

- `memento_people_candidates`
- `memento_merge_suggestion`
- `memento_person`
- `memento_person_email`

Projects:

- `memento_project`
- `memento_project_member`
- `memento_project_message`
- `memento_project_narrative`

Newsletters:

- `memento_newsletter_source`
- `memento_newsletter_narrative`

Concepts:

- `memento_concept`
- `memento_concept_message`
- `memento_concept_narrative`

Config:

- `memento_config`

### 8.2 Rollup/materialized report tables

These are the current fast-read path for dimension indexes:

- `memento_people_report`
- `memento_newsletters_report`
- `memento_projects_report`
- `memento_concepts_report`
- `memento_report_meta`

These are rebuilt by `backend/internal/refresh/refresh.go`.

### 8.3 Drafts, notes, and person-agent output

- `memento_draft`
- `memento_note`
- `memento_person_attribute`
- `memento_person_facet`
- `memento_person_narrative`

`memento_draft` is shared by both project and concept draft flows.

## 13. Rollup architecture

This is a major implementation fact that older docs do not capture well.

Dimension index pages are now intended to read from rollup tables rather than recomputing heavy aggregations at request
time.

Current rollup builder package:

- `backend/internal/refresh/refresh.go`

Key functions:

- `RefreshPeopleReport`
- `RefreshNewslettersReport`
- `RefreshProjectsReport`
- `RefreshConceptsReport`
- `RefreshAll`

Refresh happens in these places:

- `memento init`
- `memento refresh`
- people refresh job
- newsletter detect job
- draft commit flow
- project compile completion
- concept compile completion
- newsletter summary generation completion

Read-path split:

- People index reads `memento_people_report`
- Projects index reads `memento_projects_report`
- Newsletters index reads `memento_newsletters_report`
- Concepts index reads `memento_concepts_report`
- Detail pages still call richer report builders in the domain packages

## 14. Backend HTTP API

The Go API binds to `127.0.0.1` only. Beyond the loopback bind, a `guardLocal`
middleware (`backend/internal/server/server.go`) rejects requests with a
non-loopback `Host` header (DNS-rebinding defense) and state-changing requests
with a foreign `Origin` (CSRF defense).

The CORS allowlist only matters for the `pnpm run dev` frontend on `:3000`:

- `http://localhost:3000`
- `http://127.0.0.1:3000`

In the single-binary deployment the browser and API share one origin, so CORS
does not apply.

### 10.1 Public Go endpoints

General:

- `GET /api/health`
- `GET /api/config`
- `POST /api/config`

Dimensions:

- `GET /api/people`
- `GET /api/people/{slug}`
- `GET /api/people/merge-suggestions`
- `POST /api/people/merge-decision`
- `GET /api/projects`
- `GET /api/projects/{slug}`
- `GET /api/newsletters`
- `GET /api/newsletters/{slug}`
- `GET /api/concepts`
- `GET /api/concepts/{slug}`

Notes:

- `GET /api/notes`
- `POST /api/notes`
- `PATCH /api/notes`
- `DELETE /api/notes`

Search:

- `GET /api/search`

Jobs and refresh:

- `POST /api/people/refresh`
- `POST /api/newsletters/detect`
- `POST /api/newsletters/{slug}/generate`
- `GET /api/jobs/{id}`

Drafts:

- `POST /api/drafts`
- `GET /api/drafts/{id}`
- `PATCH /api/drafts/{id}/entities`
- `POST /api/drafts/{id}/commit`
- `POST /api/drafts/{id}/abandon`

Important note:

- Project generate, Concept generate, Person enrich, and Ask Memento are
  browser-facing Go routes registered in `backend/internal/server/browser_api.go`.
  The browser calls them directly; they start durable Go agent runs and stream
  SSE events back.

### 10.2 Token-gated internal Go endpoints

These are for Go agent-runner internals, tooling, and tests. They require
`X-Internal-Token`. The browser no longer uses them — it calls the
browser-facing routes above directly.

Core and collector tools:

- `POST /api/internal/agent-tools/ping`
- `POST /api/internal/agent-tools/fts-search`
- `POST /api/internal/agent-tools/get-message`
- `POST /api/internal/agent-tools/find-people`
- `POST /api/internal/agent-tools/get-thread`

Gap detection (shared across agents):

- `POST /api/internal/agent-tools/detect-gaps`
- `POST /api/internal/agent-tools/add-project-messages`
- `POST /api/internal/agent-tools/add-concept-messages`

Project agent tools:

- `POST /api/internal/agent-tools/get-project-bundle`
- `POST /api/internal/agent-tools/write-section`
- `POST /api/internal/agent-tools/refresh-projects-rollup`

Concept agent tools:

- `POST /api/internal/agent-tools/get-concept-bundle`
- `POST /api/internal/agent-tools/cluster-messages`
- `POST /api/internal/agent-tools/write-concept-section`
- `POST /api/internal/agent-tools/refresh-concepts-rollup`

Person agent tools:

- `POST /api/internal/agent-tools/get-person`
- `POST /api/internal/agent-tools/list-person-messages`
- `POST /api/internal/agent-tools/get-notes`
- `POST /api/internal/agent-tools/fts-search-scoped`
- `POST /api/internal/agent-tools/reset-person-agent-output`
- `POST /api/internal/agent-tools/write-facet`
- `POST /api/internal/agent-tools/write-person-section`

Dashboard router agent tools:

- `POST /api/internal/agent-tools/search-persons`
- `POST /api/internal/agent-tools/search-projects`
- `POST /api/internal/agent-tools/search-concepts`
- `POST /api/internal/agent-tools/get-person-summary`
- `POST /api/internal/agent-tools/get-project-summary`
- `POST /api/internal/agent-tools/get-concept-summary`

Draft state persistence:

- `PATCH /api/internal/drafts/{id}/state`

## 15. Frontend route map

Pages:

- `/` redirects to `/home`
- `/home`
- `/people`
- `/people/[slug]`
- `/projects`
- `/projects/new`
- `/projects/[slug]`
- `/newsletters`
- `/newsletters/[slug]`
- `/concepts`
- `/concepts/new`
- `/concepts/[slug]`

Static export and dynamic slugs:

- The app is built with `output: "export"`. Every `[slug]` route exports a
  single placeholder shell at `<section>/_` (see `src/lib/static-page.ts`).
- The Go web handler (`backend/internal/webui/webui.go`) serves the placeholder
  shell for any real slug without a 301 redirect; the client reads the actual
  slug from `window.location`.
- The root redirect and the onboarding gate (redirect to `/onboard` when setup
  is incomplete) are enforced by the Go handler, replacing the old Next.js
  middleware.

There are no Next.js API routes. The browser calls Go routes directly:

- `POST /api/agents/memento/turn`
- `POST /api/drafts/{id}/turn`
- `POST /api/projects/{slug}/generate`
- `POST /api/concepts/{slug}/generate`
- `POST /api/people/{slug}/enrich`

## 16. Frontend data-fetching pattern

Frontend API helper:

- `src/lib/api.ts` — relative `/api/*` fetches against the same origin.

Important current behavior:

- Pages are client components (`*PageClient.tsx`) that fetch in `useEffect`
  after mount and render a loading state until data arrives. There is no
  server-side rendering or ISR; the static export ships an empty shell that
  hydrates and fetches.
- After a compile/enrich/mutation, the UI calls `window.location.reload()` to
  refetch (the static export has no `router.refresh()` server roundtrip).

## 17. Agent runtime

The durable agent loop, all five agent specs, system prompts, tool registry, env vars, and human-in-the-loop behavior
are documented in [`docs/agent-runtime.md`](agent-runtime.md).

At a glance:

- Go: `backend/internal/agentrunner` + `backend/internal/server/agent_runs.go` + `agent_prompts.go` +
  `agent_tool_registry.go`
- Browser-facing start+stream routes: `backend/internal/server/browser_api.go` (generate/turn/enrich); the client
  consumes SSE via `src/lib/agent-events.ts`
- Runs persist in `memento_agent_*` tables; SSE replays via `/api/agents/runs/{id}/events`

## 18. Draft flow implementation

The new-project and new-concept flows are implemented now. They are not just mock wireframes.

Shared persistence:

- `memento_draft`

Shared UI pieces:

- `src/components/agent/AgentChat.tsx`
- `src/components/agent/EntityCuration.tsx`
- `src/components/agent/useAgentStream.ts`

Project draft page:

- `src/app/projects/new/page.tsx`

Concept draft page:

- `src/app/concepts/new/page.tsx`

Flow:

1. The page creates a draft if no `draftId` query param exists.
2. The user chats with the collector.
3. The collector stages a bundle.
4. The browser PATCHes curated bundle edits back to Go on every change.
5. Commit converts the draft into a real `memento_project` or `memento_concept`.
6. `refresh.RefreshAll` runs.
7. The app navigates to the new detail page.

Bundle shape:

```json
{
  "name": "Kitchen remodel",
  "summary_hint": "short scope text",
  "people": [
    { "person_id": 42, "display_name": "Alice Chen", "role": "contractor" }
  ],
  "messages": [
    { "message_id": 1234, "subject": "Re: countertop options", "agent_confidence": 0.92 }
  ],
  "threads": [
    { "thread_id": 88, "subject": "Permit application", "message_count": 7 }
  ]
}
```

## 19. Current dimension implementations

### 19.1 Dashboard

Page:

- `src/app/home/page.tsx` (exports `src/app/home/DashboardPageClient.tsx`)
- client UI in `src/components/agent/DashboardClient.tsx`

What it does:

- aggregates People, Projects, and Newsletters from multiple API calls
- computes top metrics, priority cards, recent evidence, domain counts, top newsletter, top people
- renders the Memento router chat hero

Important implementation detail:

- dashboard data composition is frontend-side, not a dedicated backend dashboard endpoint

### 19.2 People

Index:

- `src/app/people/page.tsx`
- `src/app/people/PeopleDirectoryClient.tsx`

Source:

- `GET /api/people?top=200`

Observed behavior:

- directory cards include status, org inference, message counts, last interaction, masked email
- search box in the app header routes into people filtering

Detail:

- `src/app/people/[slug]/page.tsx`
- `src/app/people/[slug]/PersonDetailClient.tsx`

Detail content includes:

- aliases
- archive snapshot
- recent exchanges grouped by year
- AI facets
- structured personal details from `memento_person_attribute`
- AI narrative sections:
    - `summary`
    - `relationship_arc`
    - `current_status`
- top correspondents
- static re-engagement suggestion stub when inactivity is high

Notes:

- notes are stored and retrievable through `memento_note`
- notes are consumed by the person agent, but the current person detail page is more focused on enrichment output than
  note editing UI

### 19.3 Projects

Index:

- `src/app/projects/page.tsx`

Source pattern:

1. `GET /api/projects`
2. then sequential detail hydration with `GET /api/projects/{slug}` for richer cards

Current live state:

- “New project” entry point is live

Detail:

- `src/app/projects/[slug]/page.tsx`
- `src/app/projects/[slug]/ProjectDetailClient.tsx`

Detail content includes:

- status and update marker
- executive summary
- phases
- friction points
- current understanding
- members
- decisions, expenses, attachments if present
- message timeline
- source viewer driven by inline `[msg:NNN]` citations

Compile behavior:

- `ProjectCompileButton` triggers the Next.js project agent route
- UI exposes live tool-call progress

### 19.4 Newsletters

Index:

- `src/app/newsletters/page.tsx`

Source:

- `GET /api/newsletters`

Observed behavior:

- page lists every detected source from the rollup, not a capped top slice

Detail:

- `src/app/newsletters/[slug]/page.tsx`
- `src/app/newsletters/[slug]/NewsletterDetailClient.tsx`

Detail content includes:

- coverage summary
- recurring themes
- notable recent items when present
- message timeline
- source viewer for selected newsletter emails

Generation behavior:

- uses Go endpoint `POST /api/newsletters/{slug}/generate`
- `RefreshButton` component triggers it
- uses the configured model provider; `MEMENTO_MODEL_API_KEY` is required when the provider needs an API key

Important distinction:

- newsletter generation is a Go-side provider-neutral one-shot flow, separate from the durable Go agent runner

### 19.5 Concepts

Index:

- `src/app/concepts/page.tsx`

Source:

- `GET /api/concepts`

Observed behavior:

- concepts are explicitly positioned as opt-in

Detail:

- `src/app/concepts/[slug]/page.tsx`

Detail content includes:

- scope description
- seed keywords
- source count/date range
- scope summary
- distilled insights
- evolving understanding
- source map
- recent mentions

Compile behavior:

- `ConceptCompileButton` triggers the Next.js concept agent route

## 20. Search behavior

Two different search ideas exist in the codebase:

1. Header search in the UI:
    - this is primarily a people-directory search/filter affordance
    - it routes to `/people?search=...`
2. Backend archive search:
    - `GET /api/search?q=...`
    - shells out to `msgvault search`
    - defaults to `hybrid`, falls back to `fts`

Agent search tools use internal endpoints rather than directly calling `/api/search`.

## 21. Notes on citations and source rendering

Projects and concepts depend heavily on source citations embedded in narrative text.

Key frontend citation helpers:

- `src/lib/citation.tsx`

Patterns:

- project and concept narratives render inline `[msg:NNN]` citations as clickable chips
- person detail uses `CitationChip` and `renderCitedText`
- newsletter detail maps message IDs to smaller source chips

The codebase already assumes source IDs are first-class. Any new generation flow should keep using real `message_id`
references.

## 22. Current backend jobs and long-running flows

Go background jobs currently cover:

- people refresh
- newsletter detect
- newsletter generate

Person/project/concept compile and person enrich are **durable Go agent runs** streamed to the browser via the Go SSE
routes in `browser_api.go` — not background jobs in `runners.go`.

`backend/internal/server/runners.go` currently:

- resolves persons into deterministic mailbox-equivalent clusters
- persists deterministic clusters and advisory merge suggestions
- builds people candidates
- refreshes people report
- detects newsletters
- refreshes newsletter report
- generates newsletter narratives

## 23. Current UI and UX characteristics

The current visual system is a pale editorial light theme with dark green accents. It is materially different from a
default Tailwind demo and should be preserved unless intentionally redesigned.

Consistent UI traits:

- masked email addresses by default
- owner identity and refresh controls in the settings popover
- material symbol icons plus `lucide-react`
- large summary cards and citation-driven detail views
- agent actions exposed as explicit buttons with small live progress panels

## 24. Important implementation quirks and gaps

These are the kinds of things another agent could easily miss.

- Several frontend relative-date helpers use hard-coded “now” dates around `2026-05-21` instead of true runtime time.
- Newsletter generation and social label one-shot helpers use the provider-neutral Go one-shot path outside the durable
  agent runner.
- Project index card hydration is sequential per project detail request. This is acceptable at current scale but is not
  ideal if project count grows.
- Person re-engagement CTA buttons are UI stubs only.
- Header search is not a global semantic search UX; it is effectively a people-filter shortcut.
- The old `contact-detail.tsx` sheet-style people detail UI still exists, but the current primary route is
  `/people/[slug]`.
- `memento reset` can intentionally leave the UI in empty-but-200 mode until `init` or refresh runs again.
- The backend uses `db.SetMaxOpenConns(1)`, which simplifies SQLite behavior but means refresh/build flows are
  serialized.

## 25. Guidance for the next agent

Before changing behavior:

1. Read [`AGENTS.md`](../AGENTS.md).
2. Read this file for stack/schema/API context.
3. Read [`agent-runtime.md`](agent-runtime.md) for any agent, prompt, or tool change.
4. Inspect the relevant page, route, and backend package together.
5. Assume msgvault is read-only.
6. Preserve source IDs and citation semantics.

If you are touching:

- dimension index performance: start with `backend/internal/refresh/` and rollup readers
- generative flows: start with `backend/internal/agentrunner`, `agent_runs.go`, `agent_prompts.go`, and
  `agent_tool_registry.go`
- project/concept draft creation: start with `memento_draft`, `drafts.go`, and `/projects/new` or `/concepts/new`
- people detail enrichment: start with `memento_person_facet`, `memento_person_narrative`, and `PersonDetailClient`
- newsletter generation: Go one-shot in `backend/internal/newsletter`, not the agent loop

Safe invariants to preserve:

- local-only architecture
- `memento_*` write ownership
- masked emails in the UI by default
- source attribution via real message IDs
- opt-in concept creation
- user-confirmed project/concept bundling before expensive generation
