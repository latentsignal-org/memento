# Memento Decisions

Durable decisions that future agents and maintainers should preserve. Keep this file
sparse: record choices that affect product behavior, data safety, architecture, public
interfaces, or long-term maintenance. Do not record routine implementation details,
temporary plans, TODOs, test notes, or decisions already obvious from code.

## 2026-07-06 — Browser avatar traffic stays local

The browser must not request Gravatar or any external avatar host directly. Avatar image URLs use Memento's local
`/api/avatar/{sha256}` endpoint, where the hash is the SHA256 hex digest of the trimmed, lowercased email and query
parameters carry only bucketed size plus initials.

The backend may fetch Gravatar with `d=404`, but only when the requested hash matches a known local person email or the
configured owner email. Results are cached in SQLite in `memento_avatar`, including negative `notfound` rows. Transient
network/upstream errors are not cached. When no cached photo is available, the server returns a deterministic local SVG
initials avatar. Bulk avatar refresh is explicitly opt-in via `memento refresh --avatars`; plain `memento refresh` must
not contact avatar providers.

## 2026-06-16 — Generated markdown must not trigger arbitrary browser egress

LLM-generated markdown is treated as untrusted content because it can be influenced by
ingested email/newsletter text. Markdown images are not a supported product feature and
must be stripped at render sinks. Served HTML documents must also carry a restrictive
Content-Security-Policy that limits `img-src` and `connect-src` to same-origin browser
traffic, with only the explicit product exceptions needed for data URLs and
Google-hosted Material Symbols font assets. The former Gravatar image exception was
removed by the 2026-07-06 local avatar proxy decision.

The same untrusted-content rule forbids rendering archive-derived values through
`dangerouslySetInnerHTML`. Display names and addresses come from email participant data
an attacker controls, so generated narratives are passed to the UI as structured data and
composed as React-escaped JSX, never as pre-built HTML strings.

## 2026-06-13 — Single-binary distribution: Go serves the static UI

The public repository ships a single Go binary that embeds the statically
exported Next.js frontend (`output: "export"` staged into
`backend/internal/webui/dist` via `pnpm stage:webui`). There is no Node.js
runtime dependency and no Next.js server: the browser calls the Go API
directly on one localhost origin, and the former Next.js proxy/server-action
layer is gone. Dynamic `[slug]` routes are exported with a placeholder param
("_"); the Go server rewrites real-slug URLs onto those shells and clients
read the slug from `window.location`. Browser-facing agent SSE endpoints live
in `backend/internal/server/browser_api.go`; the token-gated
`/api/internal/*` routes remain for tests and tooling. `next dev` remains
supported for frontend iteration via an /api rewrite proxy. End-user entry
point is `memento app`.

## 2026-06-12 PDT — Reasoning-model controls are provider-gated

`MEMENTO_MODEL_REASONING_EFFORT` is a public runtime knob, but it must not be sent
blindly to every OpenAI-compatible gateway. OpenAI-hosted models at `api.openai.com`
use the Responses API and `reasoning.effort`; generic OpenAI-compatible gateways stay
on Chat Completions and ignore the knob. DeepSeek V4 thinking controls are model-name
gated and use DeepSeek's Chat Completions fields (`thinking` and `reasoning_effort`).

## 2026-06-04 17:45 PDT — Demo mode is isolated from the real archive

Demo data must live in a separate local SQLite file, defaulting to
`data/memento-demo.db`. Demo mode must not seed rows into the user's real msgvault
archive and must not introduce a demo-specific DB env var. The simple demo entrypoint is
`memento serve --demo`, which prepares or uses the demo DB and starts the API against it.
Demo commands must refuse to overwrite an existing DB unless it is clearly marked as a
Memento demo DB. This supersedes the original `./memento demo` command name.

## 2026-06-04 17:45 PDT — Running backend DB handles are not hot-swapped

The Go server owns one SQLite DB handle and one msgvault reader for the lifetime of the
process. If setup changes the archive DB path for future runs, the UI must tell the user
to restart the backend instead of swapping DB handles in-process.

## 2026-06-04 17:45 PDT — Demo setup must not require external model or embedding services

The demo experience should work without a real LLM key, Ollama, or a msgvault vector
index. Demo data should be richer than the E2E fixture and should include synthetic,
fake-domain content sufficient for People, Projects, Newsletters, Concepts, and Ask
Memento flows.

## 2026-06-04 17:45 PDT — Pause after each implementation phase

After each spec phase, stop and summarize exactly what changed, how to test it, and a
suggested commit message. The user tests and commits manually before the next phase
starts.

## 2026-06-04 17:45 PDT — Setup status failures fail open with visible remediation (superseded terminology)

If Next.js cannot reach the Go backend while deciding whether to redirect to first-run
onboarding, it should not hide the real error behind an onboarding redirect. Let the
normal route load and show a clear backend-offline/onboarding-status error with likely
causes and commands to fix it. The route terminology is superseded by the 2026-06-05
22:17 PDT decision: use `/onboard`, not `/setup`.

## 2026-06-05 13:31 PDT — Setup demo is a mode of the demo command (superseded)

This user-facing command model is no longer active. Superseded by the 2026-06-05 22:17
PDT decision below. Do not keep obsolete aliases after implementing the replacement
commands. Use `memento serve --demo` for the populated try-it demo and
`memento onboard --demo` for onboarding test data.

## 2026-06-05 16:53 PDT — Vector search is first-run setup readiness (superseded)

First-run setup treats msgvault vector search/embeddings as a first-class readiness check,
not optional polish. The onboarding flow may still show keyword-search status on the
archive step, but the environment preflight should make missing embeddings visible and
block continuation when vector search is not ready. Demo/onboarding-test archives must
present a ready semantic-retrieval path without requiring an external embedding service.

Superseded by the 2026-06-05 17:05 PDT decision below.

## 2026-06-05 17:05 PDT — Vector readiness belongs to archive setup

First-run onboarding shows vector search with the Email archive checks, alongside archive
presence and keyword search, because embeddings are a property of the selected msgvault
archive rather than the general development environment. Preflight stays focused on
tools, project root, and `.env`. Internal frontend/backend bridge wiring is not a
user-facing readiness check and should not be shown in onboarding UI. Demo/onboarding-test
archives must not claim semantic retrieval is ready unless real vector data or a real
msgvault vector probe supports that claim; until a real demo vector path exists, missing
vector readiness is visible remediation guidance rather than an onboarding build blocker.

## 2026-06-05 22:17 PDT — Use onboarding terminology and remove obsolete aliases

The product-facing first-run flow is "onboarding", not "setup". Use `/onboard` as the
only browser route for that flow; do not keep `/setup` as a compatibility route. Use
`memento serve` as the normal entrypoint, routing to onboarding or dashboard based on DB
state. Use `memento serve --demo` for the populated try-it demo and
`memento onboard --demo` for developer/manual testing of onboarding against a synthetic
uninitialized archive. Do not keep obsolete commands or aliases such as `memento demo` or
`memento demo --setup` once the replacement commands are implemented. Demo entrypoints
must not mutate the user's real archive, and only `memento reset` may explicitly reset
Memento-owned tables.

## 2026-06-11 13:51 PDT — Ask Sessions are product artifacts, not debug runs

Ask Memento conversations are saved as a top-level "Sessions" dimension, persisted in
`memento_ask_session`, `memento_ask_turn`, and `memento_ask_context_ref`. Raw
`memento_agent_*` tables remain runtime/debug infrastructure and may be purged without
deleting saved Ask answers; Ask turns link to raw runs only through nullable `run_id`.
The user-facing session API is the public Go route family `/api/sessions*`, proxied by
Next.js, rather than internal-only ask-session routes.

## 2026-06-11 15:34 PDT — Session promotion enters the draft workflow

Promoting an Ask Session creates a Project or Concept draft seeded with the saved
transcript, then routes the user into the existing collector/curation flow. It does not
directly create a durable Project or Concept from an ad hoc answer, because the existing
draft workflow is where users review entity names, source messages, and bundle contents
before commit.
