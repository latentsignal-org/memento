# Agent Context Management

Document status: technical reference (problem analysis + mitigation catalog)  
Last updated: May 30, 2026  
Audience: engineers tuning agent cost, reliability, and context pressure

Companion docs:

- [`agent-runtime.md`](agent-runtime.md) — loop architecture, tools, prompts
- [`agent-tool-improvement-log.md`](agent-tool-improvement-log.md) — empirical findings from `memento_agent_loop`
  telemetry

---

## 1. Problem summary

Memento's five Go agent runs accumulate context monotonically:

- **Gemini:** every tool result and turn is retained server-side via `interaction_id`.
- **OpenAI-compatible:** the runner replays a growing local transcript each step.

There is still **no trimming or summarization** of completed steps mid-run. Social-graph tools, vector search, gap
detection, and backfill flows increased per-agent tool surface area without removing older heavy tools.

On projects with 30+ attached messages, compile agents can spend most of the context window on tool-result payloads
before writing sections. Large bundles or repeated searches can push runs toward provider errors, hallucinated message
IDs, or incomplete output.

`MEMENTO_AGENT_STEP_LIMIT` (default 20) caps **steps**, not **tokens**.

---

## 2. Where context pressure comes from

### Bundle tools (largest risk)

`get_project_bundle` / `get_concept_bundle` with `detail="full"` can inject full message bodies. Per-message payload is
dominated by `body_text` (truncated per message, bundle-level cap in Go):

```go
const maxChars = 150000 * 4 // ~600K characters in project/concept bundle builders
```

Rough scale for a 50-message project:

| Mode                     | Approx. tool-result tokens |
|--------------------------|----------------------------|
| Index / snippet only     | ~3K                        |
| Full bodies              | ~25K–60K+                  |
| Worst case at bundle cap | can approach model limits  |

**Mitigation in production:** compile prompts require `get_bundle_index` first; `get_project_bundle` /
`get_concept_bundle` accept `detail="index"` to skip bodies.

### Search tools

`fts_search` / `vector_search` return up to 50 hits with subject, snippet, sender, dates (~3K tokens at limit).
Collector prompts ask for 3–5 searches → **9–15K tokens** before `get_message` calls.

### Person / summary tools

| Tool                                                       | Typical size   |
|------------------------------------------------------------|----------------|
| `list_person_messages` (full, limit 100)                   | ~4K–8K tokens  |
| `list_person_messages` (`fields="compact"`, limit 50)      | ~1K–2K tokens  |
| `get_person_summary` (full)                                | ~8K–20K tokens |
| `get_person_summary` (`brief=true`, default in prompts)    | ~500 chars     |
| `get_project_summary` / `get_concept_summary` with `brief` | metadata only  |

### Social graph tools

Usually ~1K–4K tokens each (`get_person_network`, `get_cluster`, `find_bridges_between`, `find_missing_collaborators`).

### Gap tools

`detect_gaps` alone is small (~1–3K). Follow-up searches it triggers are not. `detect_gaps_with_results` bundles gap
records plus lightweight search hits in one call.

### Write tools

Negligible (~50 chars): `write_section`, `write_facet`, `write_person_section`, `write_concept_section`.

---

## 3. Typical accumulation by agent

Estimates are tool-result tokens only; model reasoning/output adds more.

### Project compile (40-message bundle)

| Phase                | Tools                                      | Est. tokens |
|----------------------|--------------------------------------------|-------------|
| Index + budget check | `get_bundle_index`, `context_status`       | ~3K–8K      |
| Targeted reads       | `get_message_batch`, `summarize_thread`    | ~5K–20K     |
| Gaps                 | `detect_gaps_with_results`                 | ~2K–8K      |
| Person/graph         | `get_person_summary`, `get_person_network` | ~3K–15K     |
| Writes               | four `write_section` calls                 | negligible  |

**Before compact tools:** often **50K–80K+** when starting with full bundle.  
**After compact-first prompts:** target **15K–25K** (see improvement log for measured runs).

### Collector

Multiple searches + messages + gap/backfill paths → **27K–75K+** on heavy curation turns.

### Person enrich

Profile + messages + scoped search + facets → **23K–45K+** without compact-first discipline.

### Dashboard (multi-turn)

Per turn: **3K–15K** tool results. Over many turns, Gemini `interaction_id` chains full history → **30K–100K+**
unbounded growth. **No pruning strategy is implemented yet.**

---

## 4. Known structural problems

1. **Monotonic context** — nothing drops old tool results from the active interaction/transcript mid-run.
2. **Bundle firehose** — still possible if the model ignores prompts and calls `get_*_bundle(detail="full")` early.
3. **`propose_backfill` blocking** — human-waiting tool holds the run open until decision or timeout (
   `MEMENTO_AGENT_DECISION_TIMEOUT_MS`, default 90s). Multiple proposals extend wall time; context stays pinned.
4. **Redundant fetches** — same messages can appear via bundle index, search hits, and `get_message_batch`; dashboard
   may call search then full summary for the same entity.
5. **No incremental compile/enrich** — re-runs reload evidence from scratch; person enrich clears prior LLM output each
   time.
6. **Output detail modes ≠ input budget** — prompts scale *writing* length by bundle size (concise/standard/deep) but do
   not cap *input* volume; deep mode on 121+ messages can still load huge indexes.

---

## 5. Mitigation catalog (original proposals → status)

| #  | Proposal                                              | Status         | Notes                                                                                                                        |
|----|-------------------------------------------------------|----------------|------------------------------------------------------------------------------------------------------------------------------|
| 1  | `get_bundle_index`                                    | **Shipped**    | Required first step in project/concept prompts                                                                               |
| 2  | `get_message_batch`                                   | **Shipped**    | `include_body`, `body_char_limit` (default 1200, max 4000)                                                                   |
| 3  | `summarize_thread`                                    | **Shipped**    | Deterministic digest                                                                                                         |
| 4  | `detect_gaps_with_results`                            | **Shipped**    | `min_severity`, `max_gaps` on schema                                                                                         |
| 5  | `context_status`                                      | **Shipped**    | Reads persisted run usage; `budget_level` + guidance; limit via `MEMENTO_AGENT_CONTEXT_LIMIT_TOKENS` (default 128K estimate) |
| 6  | Bundle `detail` param (`full` / `index`)              | **Shipped**    | On `get_project_bundle`, `get_concept_bundle`                                                                                |
| 7  | Search `detail` (`full` / `compact`)                  | **Open**       | Not on `fts_search` / `vector_search` schemas yet                                                                            |
| 8  | `list_person_messages` `fields` + lower default limit | **Partial**    | `fields=compact` in schema + person prompt (`limit=50`); default in handler may still differ                                 |
| 9  | Summary `brief` param                                 | **Shipped**    | `get_person_summary`, project/concept summaries                                                                              |
| 10 | Merge `get_person` + `get_notes`                      | **Superseded** | Person agent uses `get_person_summary` (includes authoritative notes)                                                        |
| 11 | `detect_gaps` filters                                 | **Shipped**    | `min_severity`, `max_gaps`                                                                                                   |
| 12 | `get_person_network` `depth`                          | **Open**       | No `self` / `immediate` enum yet                                                                                             |

---

## 6. Open non-tool work

| Item                                           | Status           | Rationale                                                                                                             |
|------------------------------------------------|------------------|-----------------------------------------------------------------------------------------------------------------------|
| Per-agent step limits                          | **Open**         | Suggested: dashboard 20, collector 15, project 12, concept 10, person 12 — still global `MEMENTO_AGENT_STEP_LIMIT=20` |
| Tighter bundle body cap                        | **Open**         | Original suggestion: 800 chars/msg, 120K total bundle cap vs current 150K×4                                           |
| Dashboard chat context pruning                 | **Open**         | Summarize older turns, start fresh interaction with summary preamble                                                  |
| Per-tool result size telemetry                 | **Partial**      | Usage in `memento_agent_loop` + run totals; not yet per-call rows easy to query                                       |
| Person-enrich bootstrap context                | **Open**         | Pre-load compact identity/stats before first model step (see improvement log)                                         |
| Search result modes (`ids` / `brief` / `full`) | **Open**         | Improvement log item #6                                                                                               |
| TOON / alternate tool-result encoding          | **Experimental** | Improvement log item #12 — flag-gated only                                                                            |

---

## 7. Prompt and operator guidance

All five agents include `context_status` in their tool allowlists. Prompts instruct:

- Call `context_status` before broad expansion.
- At `budget_level` `watch` / `low` / `critical`, prefer `get_bundle_index`, small `get_message_batch`, and
  `summarize_thread`; avoid full bundles.
- Use `get_person_summary` with default compact/brief behavior unless full narrative is needed.

Operators can tune:

- `MEMENTO_AGENT_CONTEXT_LIMIT_TOKENS` — denominator for budget levels
- `MEMENTO_AGENT_DECISION_TIMEOUT_MS` — backfill wait (default 90s)
- `MEMENTO_AGENT_STEP_LIMIT`

---

## 8. Measuring improvement

Use [`agent-tool-improvement-log.md`](agent-tool-improvement-log.md) SQL patterns against:

- `memento_agent_session` — totals per run
- `memento_agent_loop` — per-step tool calls and result JSON
- First-tool frequency (expect `get_bundle_index` + `context_status` on new compile runs)

Regression signal: project compile runs that still start with `get_project_bundle` before index/budget check.
