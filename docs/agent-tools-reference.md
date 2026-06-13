# Agent Tools Reference

Document status: current implementation reference
Last updated: June 2026
Audience: engineers and agents changing tool schemas, prompts, or debug flows

This is the current tool reference for the Go agent runner. It is based on
`backend/internal/server/agent_tool_registry.go`,
`backend/internal/server/agent_tool_schemas.go`, and
`backend/internal/server/agent_tool_direct.go`.

For prompt workflows, see [`agent-loop-and-prompts.md`](agent-loop-and-prompts.md).
For provider behavior and run lifecycle, see [`agent-runtime.md`](agent-runtime.md).

## Tool Kinds and Dispatch

| Kind            | Behavior                                                                                                          |
|-----------------|-------------------------------------------------------------------------------------------------------------------|
| `read_only`     | Can run in parallel within a model-emitted batch. No lock key.                                                    |
| `mutating`      | Serialized by lock key so writes for the same entity do not interleave.                                           |
| `human_waiting` | Creates a durable decision, sets run status to `waiting_for_user`, and blocks until the UI resolves or times out. |

Debug invocability:

- `/debug/tools` exposes read-only tools only.
- `context_status` is excluded from debug invocation because it needs a live run.
- Mutating and human-waiting tools are rejected server-side.
- Bound tools expose explicit ids in the debug schema because normal run
  metadata is not present in the tool console.

Entity binding:

- `get_bundle_index` injects project/concept kind and id from run metadata.
- `get_project_bundle`, `write_section`, `get_concept_bundle`,
  `write_concept_section`, person tools, `get_group`, and `get_cluster` inject
  entity ids from run metadata.
- `get_person_summary` and `get_person_network` use the bound `person_id` only
  when the model did not pass one.

## Availability Matrix

Legend: C = collector, P = project compile, K = concept compile,
E = person enrich, D = dashboard.

| Tool                          | Kind                   | C | P | K | E | D |
|-------------------------------|------------------------|--:|--:|--:|--:|--:|
| `fts_search`                  | read                   | x | x | x |   | x |
| `vector_search`               | read                   | x | x | x |   | x |
| `get_message`                 | read                   | x | x | x | x |   |
| `get_message_batch`           | read                   | x | x | x | x | x |
| `get_thread`                  | read                   | x |   |   |   |   |
| `summarize_thread`            | read                   | x | x | x |   | x |
| `find_people`                 | read                   | x |   |   |   |   |
| `get_person_summary`          | read                   | x | x | x |   | x |
| `propose_bundle`              | write                  | x |   |   |   |   |
| `propose_backfill`            | human                  | x |   |   |   |   |
| `detect_gaps`                 | read                   | x | x | x |   | x |
| `detect_gaps_with_results`    | read                   | x | x | x |   | x |
| `context_status`              | read                   | x | x | x | x | x |
| `find_missing_collaborators`  | read                   | x |   |   |   |   |
| `get_bundle_index`            | read, bound            |   | x | x |   |   |
| `get_project_bundle`          | read, bound            |   | x |   |   |   |
| `write_section`               | write, bound           |   | x |   |   |   |
| `get_concept_bundle`          | read, bound            |   |   | x |   |   |
| `cluster_messages_by_subject` | read                   |   |   | x |   |   |
| `write_concept_section`       | write, bound           |   |   | x |   |   |
| `list_person_messages`        | read, bound            |   |   |   | x |   |
| `fts_search_scoped`           | read, bound            |   |   |   | x |   |
| `write_facet`                 | write, bound           |   |   |   | x |   |
| `write_person_attribute`      | write, bound           |   |   |   | x |   |
| `record_no_person_attributes` | read                   |   |   |   | x |   |
| `write_person_section`        | write, bound           |   |   |   | x |   |
| `get_person_network`          | read, bound if missing |   | x |   | x | x |
| `get_group`                   | read, bound            |   |   |   | x |   |
| `get_cluster`                 | read, bound            |   |   |   | x |   |
| `find_bridges_between`        | read                   |   | x |   |   | x |
| `search_persons`              | read                   |   |   |   |   | x |
| `search_projects`             | read                   |   |   |   |   | x |
| `search_concepts`             | read                   |   |   |   |   | x |
| `get_project_summary`         | read                   |   |   |   |   | x |
| `get_concept_summary`         | read                   |   |   |   |   | x |
| `create_project_draft`        | write                  |   |   |   |   | x |
| `create_concept_draft`        | write                  |   |   |   |   | x |

Registered but not offered to current model prompts:

- `get_notes`: read-only person notes lookup; notes are already included in
  `get_person_summary` and person-enrich bootstrap.
- `add_project_messages`, `add_concept_messages`: accept-path tools for
  backfill decisions.
- `refresh-projects-rollup`, `refresh-concepts-rollup`, `refresh-people-rollup`:
  internal maintenance tools.

## Search and Retrieval Tools

### `fts_search`

Agents: collector, project, concept, dashboard

Parameters:

- `query` string, required
- `limit` number, optional

Searches msgvault with keyword/operator syntax. Despite the tool name, the
implementation asks msgvault for `hybrid` first when possible, then falls back to
`fts`. Queries that require FTS mode, such as operator-heavy queries, go straight
to FTS. If `MEMENTO_MSGVAULT_API_URL` is set, Memento uses msgvault's HTTP API;
otherwise it shells out to `msgvault search`.

Use this for exact terms, email addresses, subjects, labels, and date operators.
Do not use it for broad semantic recall when `vector_search` is available.

### `vector_search`

Agents: collector, project, concept, dashboard

Parameters:

- `query` string, required
- `limit` number, optional

Runs msgvault vector search for semantic similarity. It uses the msgvault HTTP
API when configured and otherwise shells out to `msgvault search --mode vector`.
The query should be natural language; search operators are not meaningful here.

### `get_message`

Agents: collector, project, concept, person

Parameters:

- `message_id` number, required

Fetches one message's deterministic detail by id. Use this to confirm metadata
or inspect a single candidate. For batches and bounded body text, prefer
`get_message_batch`.

### `get_message_batch`

Agents: all five

Parameters:

- `message_ids` number array, required; max 25
- `include_body` boolean, optional
- `body_char_limit` number, optional; default 1200, max 4000
- `include_headers` boolean, optional

Fetches selected messages in requested order. This is the main compact evidence
tool for generation. Keep `body_char_limit` small unless the prompt explicitly
needs more detail.

### `get_thread`

Agents: collector

Parameters:

- `thread_id` number, required

Returns thread-level context for a conversation. The collector uses it when a
search hit is part of a back-and-forth that may belong in a bundle.

### `summarize_thread`

Agents: collector, project, concept, dashboard

Parameters:

- `thread_id` number, required
- `max_messages` number, optional; default 12, max 30

Returns a compact deterministic thread digest: participants, date span,
representative snippets, estimated token cost, and next-step guidance. Prefer it
before fetching many full message bodies.

## Bundle and Analysis Tools

### `get_bundle_index`

Agents: project, concept

Model-visible parameters:

- `nonce` string, required
- `kind` enum `project | concept`, optional outside bound runs

Run metadata injects the actual project or concept id. The result is a compact,
body-free index of every attached message: ids, dates, sender, subject, snippet,
direction, thread, and estimated token count.

### `get_project_bundle` / `get_concept_bundle`

Agents: project / concept

Parameters:

- `nonce` string, required
- `detail` enum `full | index`, optional; default full

Heavy fallback for reading all attached messages. `detail="index"` skips body
text. Prompts should prefer `get_bundle_index`, `get_message_batch`, and
`summarize_thread` before these tools.

### `cluster_messages_by_subject`

Agents: concept

Parameters:

- `message_ids` number array, required
- `k` number, required

Deterministically clusters concept bundle messages by subject/body terms and
returns cluster labels, message ids, and top terms. The concept prompt uses
these as candidate themes but may override weak labels.

### `detect_gaps`

Agents: collector, project, concept, dashboard

Parameters:

- `message_ids` number array, required
- `mode` enum `chronological | thematic | participant`, required
- `min_severity` enum `low | medium | high`, optional
- `max_gaps` number, optional; default 5

Runs deterministic gap analysis over known message ids. Results include gap
kind, severity, anchors, descriptions, and search hints. This does not perform
additional retrieval.

### `detect_gaps_with_results`

Agents: collector, project, concept, dashboard

Parameters are the same as `detect_gaps`.

Runs the same deterministic gap analysis and then executes search hints to
return candidate messages inside each gap result. Use this when the agent needs
ready-made evidence for missing timeline, theme, or participant coverage.

## People and Social Graph Tools

### `find_people`

Agents: collector

Parameters:

- `query` string, required
- `limit` number, optional

Looks up canonical people by name or email fragment. The collector should use
the returned `person_id` for downstream person summaries and bundle people.

### `get_person_summary`

Agents: collector, project, concept, dashboard

Parameters:

- `person_id` number, optional
- `slug` string, optional
- `brief` boolean, optional; default true

Returns compact person context from rollups and Memento memory: profile,
classification, alias summary, authoritative notes, existing memory counts, and
social summary. `brief=false` includes larger generated memory payloads and
should be used sparingly. Person enrich does not expose this tool because its
bootstrap already includes this signal.

### `list_person_messages`

Agents: person

Parameters:

- `limit` number, required by schema; default behavior caps at 50 when omitted in direct code paths, max 200
- `fields` enum `full | compact`, optional

Lists recent messages involving the bound person. Involvement includes messages
sent by the person and messages sent by the account to the person. Compact mode
is preferred for timeline checks.

### `fts_search_scoped`

Agents: person

Parameters:

- `query` string, required
- `limit` number, optional

Runs archive search and filters results to messages involving the bound person.
The person prompt caps use of this tool to narrow, specific missing details.

### `get_person_network`

Agents: project, person, dashboard

Parameters:

- `nonce` string, required
- `person_id` number, optional when bound
- `limit` number, optional

Returns deterministic communication-network context: structural role, degree,
group or cluster context, and weighted neighbors. Graph topology is never
citable evidence for human facts; it only guides what messages to retrieve.

### `get_group` / `get_cluster`

Agents: person

Parameters:

- `nonce` string, required

Fetches the bound person's actionable group or legacy raw cluster. Prefer
`get_group` for current product behavior; `get_cluster` remains for legacy
context.

### `find_bridges_between`

Agents: project, dashboard

Parameters:

- `person_a_id` number, required
- `person_b_id` number, required
- `limit` number, optional

Finds shared contacts that bridge two people in the communication graph. Use it
to steer retrieval, not as source-attributed evidence.

### `find_missing_collaborators`

Agents: collector

Parameters:

- `person_ids` number array, required
- `limit` number, optional
- `min_combined_weight` number, optional

Finds graph-connected people who are connected to at least two current draft
people but absent from the draft. The collector must confirm relevance with
message evidence before proposing a backfill.

### `search_persons` / `search_projects` / `search_concepts`

Agents: dashboard

Parameters:

- `query` string, required

Searches materialized dimension rollups for known Memento entities. Use summary
tools after a match to load details and side-panel context.

### `get_project_summary` / `get_concept_summary`

Agents: dashboard

Parameters:

- `project_id` or `concept_id` number, optional
- `slug` string, optional
- `brief` boolean, optional

Returns entity metadata and, unless brief mode omits it, generated narrative
sections.

## Write Tools

All write tools mutate only Memento-owned `memento_*` tables. They never mutate
msgvault-owned archive tables.

### `propose_bundle`

Agents: collector

Parameters:

- `name` string, required
- `summary_hint` string, optional
- `people` array, optional
    - `person_id` number, required per item
    - `display_name` string, required per item
    - `role` string, optional
    - `evidence_message_ids` number array, optional
- `messages` array, optional
    - `message_id` number, required per item
    - `subject` string, optional
    - `date` string, optional
    - `include_reason` string, optional
    - `agent_confidence` number, optional
- `threads` array, optional
    - `thread_id` number, required per item
    - `subject` string, optional
    - `message_count` number, optional
    - `include_reason` string, optional

Persists the raw draft bundle JSON in `memento_draft.entities_json`. The draft
UI renders it for user review before conversion to a project or concept.

### `write_section`

Agents: project

Parameters:

- `section` enum `summary | phases | friction_points | current_understanding`, required
- `content` string, required
- `source_message_ids` number array, required and non-empty

Writes one project narrative section. `phases` and `friction_points` content are
JSON arrays serialized as strings. The write is guarded so user-edited sections
are not overwritten; skipped writes do not satisfy required outcomes.

### `write_concept_section`

Agents: concept

Parameters:

- `section` enum `scope_summary | distilled_insights | evolving_understanding`, required
- `content` string, required
- `source_message_ids` number array, required and non-empty

Writes one concept narrative section. `distilled_insights` content is a JSON
array serialized as a string. User-edited sections are protected.

### `write_facet`

Agents: person

Parameters:

- `facet_type` enum `interest | life_event | recurring_topic | relationship_signal | fact`, required
- `content` string, required
- `source_message_ids` number array, required and non-empty
- `confidence` number, required

Inserts a sourced person facet. Person-enrich cleanup later removes superseded
LLM-generated facets after a successful run, using the run bootstrap cutoff.

### `write_person_attribute`

Agents: person

Parameters:

- `category` enum
  `vital_date | preference | interest | relationship_marker | household | work | location | routine | identifier`,
  required
- `label` string, required; one line, 40 chars or fewer
- `value` string, required; one line, 160 chars or fewer
- `date_value` string, optional ISO date
- `source_message_ids` number array, required and non-empty
- `confidence` number, required

Writes a compact right-rail detail for a person. Attributes must be
message-backed; user notes can steer what to look for but do not replace source
message ids.

### `record_no_person_attributes`

Agents: person

Parameters:

- `reason` string, required

Read-only observability tool that satisfies the person attribute-decision
outcome when no structured attributes have strong evidence.

### `write_person_section`

Agents: person

Parameters:

- `section` enum `summary | relationship_arc | current_status`, required
- `content` string, required
- `source_message_ids` number array, required and non-empty

Writes one person narrative section. User-edited sections are protected and
return skipped results. Existing narrative sections in the person bootstrap are
not required outcomes for that run.

### `create_project_draft` / `create_concept_draft`

Agents: dashboard

`create_project_draft` parameters:

- `name` string, required
- `rationale` string, required
- `message_ids` number array, required

`create_concept_draft` parameters:

- `name` string, required
- `scope_description` string, required
- `message_ids` number array, required

Creates a `memento_draft`, seeds a minimal bundle, and returns a URL such as
`/projects/new?draftId=N` or `/concepts/new?draftId=N`.

## Control and Human-Waiting Tools

### `context_status`

Agents: all five

Parameters:

- `nonce` string, required

Reads persisted run usage counters and returns `budget_level`:
`normal`, `watch`, `low`, or `critical`, plus guidance. Prompts use this before
broad expansion.

### `propose_backfill`

Agents: collector

Parameters:

- `rationale` string, required
- `candidate_message_ids` number array, required
- `gap_kind` enum `chronological | thematic | participant | missing_collaborator`, required

Creates a durable `memento_agent_decision` row, emits a `proposed_backfill` SSE
event, sets the run status to `waiting_for_user`, and waits for accept, skip, or
timeout. Accepted decisions call the internal add-message accept path.

## Internal and Maintenance Tools

### `add_project_messages` / `add_concept_messages`

Parameters:

- `project_slug` or `concept_slug` string, required
- `message_ids` number array, required

Internal accept-path tools used after backfill decisions. They are not offered
to model prompts.

### `refresh-projects-rollup` / `refresh-concepts-rollup` / `refresh-people-rollup`

Internal mutating maintenance tools for refreshing materialized report tables.
Current agent `AfterDone` hooks call refresh functions directly where needed.

### `get_notes`

Registered read-only person notes lookup. It is not in current agent tool lists
because notes are already included in person summaries and the person-enrich
bootstrap.
