# Deterministic Extraction

Document status: implementation guide for humans and agents
Last updated: May 25, 2026

## Purpose

Memento turns a local `msgvault` archive into source-attributed dimension pages. This document defines what the backend
derives without LLM judgment, what tables it reads and writes, and where deterministic extraction ends and generation
begins.

Use this guide when changing backend extraction, adding a dimension field, debugging stale UI data, or deciding whether
a new behavior belongs in Go extraction, a Go agent, or a generated narrative section.

## Core Rules

- `msgvault` owns raw archive data. Memento treats `messages`, `participants`, `message_recipients`, `message_bodies`,
  `labels`, `message_labels`, `sources`, and related archive tables as read-only.
- Memento writes only `memento_*` tables.
- Deterministic extraction runs before LLM generation. Generated text should consume deterministic bundles and write
  cited sections, not rediscover the archive shape from scratch.
- People and Newsletters may be discovered automatically from archive patterns.
- Projects and Concepts require user declaration or confirmation before expensive generation work.
- Every downstream generated claim must be able to point back to source message IDs.
- Index pages should read materialized rollup tables, not recompute heavy archive joins on the request path.

## Pipeline Summary

| Dimension   | Deterministic inputs                                                                                | Deterministic outputs                                                         | Primary tables written                                                                            | LLM boundary                         |
|-------------|-----------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------|--------------------------------------|
| People      | `participants`, `messages`, `message_recipients`, `sources`, existing locked person mappings        | canonical people, email aliases, meaningful-contact candidates, people rollup | `memento_person`, `memento_person_email`, `memento_people_candidates`, `memento_people_report`    | person facets and narrative sections |
| Projects    | user-confirmed project rows, attached message IDs, labels/search/thread additions, people mappings  | project metadata, members, message bundle, timeline, project rollup           | `memento_project`, `memento_project_member`, `memento_project_message`, `memento_projects_report` | project narrative compile            |
| Newsletters | senders, message bodies, source account emails, people candidates                                   | newsletter sources, recent-subject rollup                                     | `memento_newsletter_source`, `memento_newsletters_report`                                         | newsletter narrative generation      |
| Concepts    | user-declared concept rows, seed terms, attached/search/thread messages, newsletter/person mappings | concept metadata, evidence bundle, source map, concept rollup                 | `memento_concept`, `memento_concept_message`, `memento_concepts_report`                           | concept narrative compile            |

The dashboard is not a separate extraction dimension. It composes existing rollups and uses an agent to route user
questions or create drafts.

## Operational Entrypoints

Standard setup:

```bash
./memento init
```

`init` runs migrations, owner config, person resolution, people candidates, newsletter detection, rollups, and env
seeding.

Individual deterministic commands:

```bash
./memento person-resolve --persist
./memento person-repair-nondeterministic --dry-run
./memento person-repair-nondeterministic --apply
./memento person-merge-suggest --sort combined
./memento people-candidates --limit 200 --persist
./memento newsletter-detect --persist
./memento refresh
```

Related API/job triggers:

- `POST /api/people/refresh` runs person resolution, candidate classification, and people rollup refresh.
- `POST /api/newsletters/detect` runs newsletter detection and newsletter rollup refresh.
- Draft commit flows create Projects or Concepts and attach selected messages.
- Project, Concept, and Newsletter generation completion refreshes the relevant rollup after narrative writes.

## Shared Read Model

The Go backend opens the msgvault SQLite file through `backend/internal/msgvault.Reader`. The reader exposes typed
helper methods and intentionally keeps archive access behind a small adapter so msgvault schema details do not leak
through the app.

Account-owned email addresses come from `sources.identifier`. Direction detection should not use `messages.is_from_me`;
that field is unreliable for Memento. Instead, outbound/account-authored messages are detected by joining
`messages.sender_id -> participants.email_address -> sources.identifier`.

Materialized rollups live in:

- `memento_people_report`
- `memento_projects_report`
- `memento_newsletters_report`
- `memento_concepts_report`
- `memento_report_meta`

Rollups are rebuilt by `backend/internal/refresh`. Each refresh deletes and inserts rows inside a transaction so readers
do not see a half-built report.

## People Extraction

People has the richest automatic deterministic extraction. It is a two-stage pipeline:

1. Resolve raw email participants into stable canonical people.
2. Classify those people into meaningful relationship candidates.

### Inputs

The resolver reads:

- `participants.email_address`
- `participants.display_name`
- `participants.domain`
- sender counts from `messages`
- recipient counts from `message_recipients`
- locked/manual rows from `memento_person_email`

The candidate classifier reads:

- canonical person mappings from `memento_person` and `memento_person_email`
- message involvement from `messages` and `message_recipients`
- account-owned addresses from `sources.identifier`

### Person Resolution Algorithm

The resolver is in `backend/internal/person`.

It skips locked email rows, then creates candidate cluster members from every participant with a non-empty email
address.

Email normalization:

- lowercases and trims email addresses
- selectively strips plus-tags only for domains known to support plus addressing
- strips dots only for Gmail and Googlemail addresses
- avoids stripping generated/system-looking local parts to prevent large false clusters

Name normalization:

- lowercases display names
- collapses whitespace
- strips trailing forwarder parentheticals when they look like forwarding markers, for example
  `Jane Smith (via Google Photos)`
- tokenizes display names for advisory merge suggestions

Automatic merge pass:

1. Normalized-email merge: participants with the same provider-normalized email enter one cluster.

Display-name equality, forwarder parentheticals, Jaro-Winkler similarity, token overlap, and graph signatures are
advisory only. They are emitted as person-person merge suggestions and never silently link identities. The legacy
`person-resolve --fuzzy` flag is a deprecated no-op; fuzzy thresholds now affect advisory suggestion generation only.

### Person Persistence

Resolved clusters are persisted into:

- `memento_person`
- `memento_person_email`
- `memento_merge_suggestion`

Persistence preserves stable `person_id` values:

- If any current cluster email already maps to a person, that person ID is reused.
- Locked rows win conflicts.
- Majority overlap wins when multiple existing IDs are present.
- Lowest ID breaks deterministic ties.
- Non-locked email rows that disappeared from msgvault are swept.
- Persons with no remaining email rows are removed.

Each email row records the link source:

- `plus_tag`
- `exact_name`
- `forwarder_unwrap`
- `jaro_winkler`
- `jaccard`
- `manual`
- `singleton`

New automatic rows should be `plus_tag` or `singleton`; the name and fuzzy sources are retained for legacy rows, repair
queries, and historical reporting.

### Legacy Repair

`person-repair-nondeterministic` is an explicit operator command for repairing old resolver-created over-merges. It is
not part of normal resolver persistence.

The command scans `memento_person_email` and targets only unlocked rows whose source is `exact_name`,
`forwarder_unwrap`, `jaro_winkler`, or `jaccard`. It preserves locked rows, manual rows, `signature_merge` rows, and
deterministic normalized-email groups, including plus-tag and Gmail/Googlemail dot equivalents. For each person it keeps
the group containing the person's primary email; if there is no primary-email group, it falls back to a locked/manual or
signature anchor, then the largest/oldest deterministic group.

`--dry-run` reports persons scanned, persons affected, split counts by prior source, and the emails that would move.
`--apply` splits unsafe non-equivalent emails into new locked/manual person rows with a repair note.

### Merge Suggestions

Advisory duplicate-person suggestions are persisted in `memento_merge_suggestion`. The queue stores person-person pairs
only; it does not contain legacy past-merge review rows.

Suggestion sources include exact display-name matches, forwarder parenthetical matches, Jaro-Winkler spelling
similarity, shared name-word overlap, and graph signatures. Re-running resolution or refresh upserts pending rows,
dedupes by person pair, and preserves accepted/rejected decisions so rejected pairs do not resurface.

Review entry points:

- `GET /api/people/merge-suggestions?sort=combined|name_similarity|token_overlap|signature`
- `POST /api/people/merge-decision` with one `{id, decision}` per request
- `./memento person-merge-suggest`

### Candidate Aggregation

The candidate query builds one row per canonical person. It first maps participants to persons via
`memento_person_email`, then computes involvement:

- `from_contact`: messages where the person is the sender and the sender is not an account-owned participant
- `to_contact`: messages authored by an account-owned participant where the person is a `to`, `cc`, `bcc`, or `mention`
  recipient

It then computes:

- total distinct messages
- messages from the contact
- messages to the contact
- first message date
- last message date
- email alias count
- primary email domain
- recent sample message IDs

The bidirectional score is:

```text
min(from_contact_count, to_contact_count) / max(from_contact_count, to_contact_count)
```

If both counts are zero, the score is zero.

### Candidate Classification

Candidate classification is deterministic and rule-based.

Rows are excluded when they look like:

- missing email addresses
- no-reply/system addresses
- newsletter or broadcast domains
- broadcast display names
- generic role mailboxes
- plus-tagged broadcast/transactional addresses
- unidirectional senders, when outbound/account-authored mail exists

Remaining rows are classified as:

- `candidate`: at least 10 total messages and bidirectional score at least 0.10
- `candidate_inbound_only`: at least 10 total messages, but the archive has no detectable outbound messages
- `weak_signal`: below the meaningful-contact threshold
- `excluded`: filtered by one of the exclusion rules

The classifier persists a snapshot into `memento_people_candidates`.

### People Rollup

`RefreshPeopleReport` rebuilds `memento_people_report` from `memento_people_candidates`.

It includes only `classification = 'candidate'` rows, then adds:

- stable UI slug
- aliases from `memento_person_email`
- recent timeline items
- top shared-thread correspondents
- generated timestamp and report metadata

The People index reads this rollup. People detail pages may read richer data and person-agent output, but their
deterministic base is still the canonical person and candidate pipeline.

### LLM Boundary

Person enrichment is not deterministic extraction. The Go person-enrich agent reads deterministic person summaries,
message lists, notes, and scoped search results, then writes:

- `memento_person_facet`
- `memento_person_narrative`

Those generated rows must carry source message IDs where factual claims are made.

## Project Extraction

Projects are not auto-promoted from raw email clusters. A project exists after the user or a draft commit creates
`memento_project`. Deterministic project extraction is about preserving the confirmed project boundary and building
evidence bundles from attached messages.

### Inputs

Project extraction reads:

- `memento_project`
- `memento_project_member`
- `memento_project_message`
- `memento_person` and `memento_person_email`
- `messages`
- `message_bodies`
- `participants`
- `message_recipients`
- `sources`
- optionally `labels` and `message_labels` when adding messages by label

Messages can be attached by:

- explicit message ID
- msgvault search result
- msgvault label
- thread/conversation ID
- draft commit flow
- agent tool calls that propose and attach a bundle

### Deterministic Behavior

Project CRUD stores the project name, slug, aliases, status, start date, and note.

Project member operations map an email address to `memento_person_email`. If no mapping exists, the backend creates a
locked manual person row so the project member is stable across future resolver runs.

Project message attachment writes message IDs into `memento_project_message` with an `included_by` reason such as
`search:fts`, `search:hybrid`, `label`, `thread`, or an agent/tool source. Message IDs are archive-owned; only the
project-message association is Memento-owned.

### Project Bundle

`GetProjectBundle` deterministically assembles the source bundle for compile and detail views:

- joins attached message IDs to `messages`
- includes sender canonical name when the sender maps to a person
- includes sender email, subject, snippet, and body text
- computes direction as `from_account`, `to_account`, or `other` using `sources.identifier`
- orders messages chronologically
- truncates long message bodies and trims the bundle to a fixed budget

This bundle is the evidence surface for project generation.

### Project Report And Rollup

`BuildProjectReport` assembles the deterministic page shape:

- project metadata
- members
- message count
- date range
- timeline
- existing narrative sections

`RefreshProjectsReport` rebuilds `memento_projects_report` from project summaries. The rollup contains project metadata,
message counts, and a compact summary payload for the index.

### LLM Boundary

Project narrative generation uses the configured model provider through the Go agent runner. It reads the deterministic
project bundle and writes `memento_project_narrative` sections such as:

- `summary`
- `phases`
- `friction_points`
- `current_understanding`

The deterministic backend should not infer project narrative claims by itself. Its job is to maintain the confirmed
boundary and evidence bundle.

## Newsletter Extraction

Newsletters are automatically detected from recurring sender behavior. Their summaries are generated later.

### Inputs

Newsletter detection reads:

- `messages`
- `participants`
- `message_bodies`
- `sources`
- `memento_person_email`
- `memento_people_candidates`

The dependency on people candidates matters: senders classified as meaningful human contacts are excluded from
newsletter detection so a human who forwards newsletter-like content does not become a newsletter source.

### Source Detection Algorithm

`DetectSources` groups messages by sender email after excluding:

- account-owned sender addresses from `sources.identifier`
- human candidate emails from `memento_people_candidates`

For each sender it computes:

- display name
- domain
- message count
- number of messages whose body contains `unsubscribe`
- first seen date
- last seen date

It keeps senders above the minimum message threshold, then classifies a source as newsletter-like when any rule matches:

- known newsletter/broadcast domain
- newsletter-like sender local part, for example `newsletter`, `digest`, or `weekly`
- newsletter-like display name
- unsubscribe links in enough message bodies
- recurring sender with many unsubscribe links

The source slug is generated from the display name with deterministic de-duplication.

### Source Persistence

Detected sources are persisted into `memento_newsletter_source`.

Persistence behaves like a snapshot:

- detected sources are upserted by `sender_email`
- stale newsletter rows no longer detected are deleted
- narrative rows for deleted sources cascade through foreign keys

### Newsletter Rollup

`RefreshNewslettersReport` rebuilds `memento_newsletters_report` from `memento_newsletter_source`.

It adds:

- source metadata
- message count
- unsubscribe count
- first and last seen dates
- classification reason
- recent subjects from the latest messages

The Newsletters index reads this rollup.

### LLM Boundary

Newsletter summary generation uses a Go-side provider-neutral one-shot LLM call. It reads recent newsletter messages and
writes `memento_newsletter_narrative` sections:

- `coverage_summary`
- `recurring_themes`
- `notable_recent`

Narrative writes preserve user-edited sections by skipping rows marked `edited_by = 'user'`.

## Concept Extraction

Concepts are user-declared evergreen topics. The backend does not auto-create concept pages from archive clusters.
Deterministic concept extraction maintains the declared topic, its evidence messages, and source map.

### Inputs

Concept extraction reads:

- `memento_concept`
- `memento_concept_message`
- `memento_person` and `memento_person_email`
- `memento_newsletter_source`
- `messages`
- `message_bodies`
- `participants`

Messages can be attached by:

- explicit message ID
- thread/conversation ID
- msgvault hybrid/FTS search over one or more query terms
- draft commit flow
- agent tool calls that propose or refine evidence

### Deterministic Behavior

Concept CRUD stores:

- name
- slug
- scope description
- seed keywords
- status
- note

Message attachment writes to `memento_concept_message` with:

- message ID
- `added_by`
- query term
- relevance score

When the same message is added again, the query term is updated and the higher relevance score is preserved.

### Concept Bundle

`GetConceptBundle` deterministically assembles the evidence bundle:

- joins attached messages to `messages`
- includes body text, subject, snippet, sender name, and sender email
- maps sender to canonical person when possible
- marks whether the sender is a detected newsletter source
- includes newsletter slug and query term
- orders messages chronologically
- truncates long bodies and trims to the same budget strategy as projects

### Source Map And Report

`BuildConceptReport` creates a deterministic page shape:

- concept metadata
- message count
- date range
- source map of top people contributors
- source map of top newsletter contributors
- timeline, capped for display
- existing narrative sections

`BuildConceptIndex` creates the index shape:

- concept slug, name, scope, status
- message count
- whether narrative rows exist

`RefreshConceptsReport` persists this index shape into `memento_concepts_report`.

### LLM Boundary

Concept narrative generation uses the configured model provider through the Go agent runner. It reads deterministic
concept bundles and writes `memento_concept_narrative` sections such as:

- `scope_summary`
- `distilled_insights`
- `evolving_understanding`

The backend extraction layer should not treat a search hit as a final factual claim. It only attaches evidence and
records why the message entered the concept bundle.

## Drafts And User Confirmation

Projects and Concepts use `memento_draft` for curation before commit. The collector agent can search and suggest
bundles, but a committed Project or Concept is the deterministic boundary the backend should trust.

The deterministic part of a draft flow is the commit:

- create the target `memento_project` or `memento_concept`
- attach selected messages
- persist metadata such as slug, name, scope, or seed terms
- refresh the relevant rollup

Do not document drafts as automatic extraction of Projects or Concepts. They are assisted curation workflows.

## Dashboard Read Model

The dashboard composes existing deterministic outputs:

- People rollup
- Projects rollup
- Newsletters rollup
- Concepts rollup

The dashboard router agent can answer questions, search existing dimensions, and create drafts. It does not own
deterministic extraction. Any new dashboard metric should either come from an existing rollup or be added to the
appropriate dimension extraction path first.

## Code Map

Backend packages:

- `backend/internal/msgvault`: read-only archive adapter and shared SQL helpers
- `backend/internal/person`: canonical person resolution and persistence
- `backend/internal/people`: people candidate classification and people page shapes
- `backend/internal/newsletter`: newsletter source detection, pages, and Go-side narrative storage
- `backend/internal/project`: project CRUD, membership, message attachment, bundles, and reports
- `backend/internal/concept`: concept CRUD, message attachment, bundles, source maps, and reports
- `backend/internal/refresh`: materialized rollup rebuilds
- `backend/internal/server`: HTTP routes, jobs, internal agent tool endpoints, draft commits

Frontend and agent boundaries:

- `backend/internal/agentrunner`: durable agent loop
- `backend/internal/server/agent_runs.go`: per-agent run specs
- `backend/internal/server/agent_prompts.go`: system prompts
- `backend/internal/server/agent_tool_registry.go`: tool dispatch
- `backend/internal/server/browser_api.go`: browser-facing start+stream SSE routes; `src/lib/agent-events.ts` consumes
  them

See [`agent-runtime.md`](agent-runtime.md) for the full agent reference.

Current deterministic command entrypoints:

- `backend/cmd/memento/main.go`: CLI command wiring
- `backend/internal/server/runners.go`: server-side background job runners

## Change Checklist

When modifying deterministic extraction:

1. Identify the dimension owner package.
2. Confirm whether the behavior writes only `memento_*` tables.
3. Preserve source message IDs.
4. Keep generated prose out of deterministic extraction.
5. Refresh or extend the relevant rollup.
6. Update this document if inputs, outputs, thresholds, or LLM boundaries change.
7. Add focused Go tests when changing classification, clustering, bundle construction, or persistence stability.
