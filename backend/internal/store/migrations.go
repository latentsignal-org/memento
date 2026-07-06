package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite"
)

type Migration struct {
	Version int
	Name    string
	SQL     string
}

var migrations = []Migration{
	{
		Version: 1,
		Name:    "create_memento_people_candidates",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS memento_people_candidates (
	contact_id INTEGER PRIMARY KEY,
	email_address TEXT NOT NULL,
	display_name TEXT NOT NULL,
	domain TEXT NOT NULL DEFAULT '',
	total_messages INTEGER NOT NULL,
	from_contact_count INTEGER NOT NULL,
	to_contact_count INTEGER NOT NULL,
	first_message_at DATETIME,
	last_message_at DATETIME,
	bidirectional_score REAL NOT NULL,
	classification TEXT NOT NULL,
	exclusion_reason TEXT NOT NULL DEFAULT '',
	sample_message_ids TEXT NOT NULL DEFAULT '[]',
	generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_memento_people_candidates_classification
	ON memento_people_candidates(classification, total_messages DESC);
`,
	},
	{
		Version: 2,
		Name:    "create_memento_person",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_person (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	canonical_name TEXT NOT NULL,
	primary_email TEXT NOT NULL,
	note TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS memento_person_email (
	email_address TEXT PRIMARY KEY,
	person_id INTEGER NOT NULL REFERENCES memento_person(id) ON DELETE CASCADE,
	display_name TEXT NOT NULL DEFAULT '',
	link_source TEXT NOT NULL,
	confidence REAL NOT NULL,
	locked INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_memento_person_email_person
	ON memento_person_email(person_id);
CREATE INDEX IF NOT EXISTS idx_memento_person_email_link_source
	ON memento_person_email(link_source);
`,
	},
	{
		Version: 3,
		Name:    "rekey_people_candidates_on_person",
		SQL: `
DROP TABLE IF EXISTS memento_people_candidates;

CREATE TABLE memento_people_candidates (
	person_id INTEGER PRIMARY KEY REFERENCES memento_person(id) ON DELETE CASCADE,
	canonical_name TEXT NOT NULL,
	primary_email TEXT NOT NULL,
	domain TEXT NOT NULL DEFAULT '',
	email_count INTEGER NOT NULL,
	total_messages INTEGER NOT NULL,
	from_contact_count INTEGER NOT NULL,
	to_contact_count INTEGER NOT NULL,
	first_message_at DATETIME,
	last_message_at DATETIME,
	bidirectional_score REAL NOT NULL,
	classification TEXT NOT NULL,
	exclusion_reason TEXT NOT NULL DEFAULT '',
	sample_message_ids TEXT NOT NULL DEFAULT '[]',
	generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_memento_people_candidates_classification
	ON memento_people_candidates(classification, total_messages DESC);
`,
	},
	{
		Version: 4,
		Name:    "create_memento_project",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_project (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  slug TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  aliases TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'active',
  started_at DATE,
  note TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS memento_project_member (
  project_id INTEGER NOT NULL REFERENCES memento_project(id) ON DELETE CASCADE,
  person_id INTEGER NOT NULL REFERENCES memento_person(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (project_id, person_id)
);

CREATE TABLE IF NOT EXISTS memento_project_message (
  project_id INTEGER NOT NULL REFERENCES memento_project(id) ON DELETE CASCADE,
  message_id INTEGER NOT NULL,
  included_by TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (project_id, message_id)
);

CREATE TABLE IF NOT EXISTS memento_project_narrative (
  project_id INTEGER NOT NULL REFERENCES memento_project(id) ON DELETE CASCADE,
  section TEXT NOT NULL,
  content TEXT NOT NULL,
  source_message_ids TEXT NOT NULL DEFAULT '[]',
  generated_at DATETIME,
  edited_by TEXT NOT NULL DEFAULT 'llm',
  PRIMARY KEY (project_id, section)
);
`,
	},
	{
		Version: 5,
		Name:    "create_memento_newsletter",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_newsletter_source (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  sender_email TEXT UNIQUE NOT NULL,
  display_name TEXT NOT NULL,
  domain TEXT NOT NULL,
  slug TEXT UNIQUE NOT NULL,
  first_seen DATETIME,
  last_seen DATETIME,
  message_count INTEGER NOT NULL DEFAULT 0,
  unsubscribe_count INTEGER NOT NULL DEFAULT 0,
  classification_reason TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS memento_newsletter_narrative (
  source_id INTEGER NOT NULL REFERENCES memento_newsletter_source(id) ON DELETE CASCADE,
  section TEXT NOT NULL,
  content TEXT NOT NULL,
  source_message_ids TEXT NOT NULL DEFAULT '[]',
  generated_at DATETIME,
  edited_by TEXT NOT NULL DEFAULT 'llm',
  PRIMARY KEY (source_id, section)
);

CREATE INDEX IF NOT EXISTS idx_memento_newsletter_source_count
  ON memento_newsletter_source(message_count DESC);
`,
	},
	{
		Version: 6,
		Name:    "create_memento_concept",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_concept (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  slug TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  scope_description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  seed_keywords TEXT NOT NULL DEFAULT '[]',
  note TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS memento_concept_message (
  concept_id INTEGER NOT NULL REFERENCES memento_concept(id) ON DELETE CASCADE,
  message_id INTEGER NOT NULL,
  added_by TEXT NOT NULL,
  query_term TEXT NOT NULL DEFAULT '',
  relevance_score REAL NOT NULL DEFAULT 1.0,
  note TEXT NOT NULL DEFAULT '',
  added_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (concept_id, message_id)
);

CREATE TABLE IF NOT EXISTS memento_concept_narrative (
  concept_id INTEGER NOT NULL REFERENCES memento_concept(id) ON DELETE CASCADE,
  section TEXT NOT NULL,
  content TEXT NOT NULL,
  source_message_ids TEXT NOT NULL DEFAULT '[]',
  generated_at DATETIME,
  edited_by TEXT NOT NULL DEFAULT 'llm',
  PRIMARY KEY (concept_id, section)
);

CREATE INDEX IF NOT EXISTS idx_memento_concept_message_concept
  ON memento_concept_message(concept_id);
`,
	},
	{
		Version: 7,
		Name:    "create_memento_config",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_config (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL DEFAULT 0
);
`,
	},
	{
		Version: 8,
		Name:    "create_rollup_report_tables",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_people_report (
  person_id INTEGER PRIMARY KEY REFERENCES memento_person(id) ON DELETE CASCADE,
  canonical_name TEXT NOT NULL,
  primary_email TEXT NOT NULL,
  domain TEXT NOT NULL DEFAULT '',
  email_count INTEGER NOT NULL,
  total_messages INTEGER NOT NULL,
  from_contact_count INTEGER NOT NULL,
  to_contact_count INTEGER NOT NULL,
  bidirectional_score REAL NOT NULL,
  classification TEXT NOT NULL,
  first_message_at DATETIME,
  last_message_at DATETIME,
  slug TEXT NOT NULL,
  aliases_json TEXT NOT NULL DEFAULT '[]',
  timeline_json TEXT NOT NULL DEFAULT '[]',
  top_correspondents_json TEXT NOT NULL DEFAULT '[]',
  generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_memento_people_report_listing
  ON memento_people_report(classification, total_messages DESC, last_message_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memento_people_report_slug
  ON memento_people_report(slug);

CREATE TABLE IF NOT EXISTS memento_newsletters_report (
  source_id INTEGER PRIMARY KEY REFERENCES memento_newsletter_source(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  display_name TEXT NOT NULL,
  sender_email TEXT NOT NULL,
  domain TEXT NOT NULL,
  message_count INTEGER NOT NULL,
  unsubscribe_count INTEGER NOT NULL,
  first_seen DATETIME,
  last_seen DATETIME,
  classification_reason TEXT NOT NULL DEFAULT '',
  recent_subjects_json TEXT NOT NULL DEFAULT '[]',
  generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memento_newsletters_report_slug
  ON memento_newsletters_report(slug);
CREATE INDEX IF NOT EXISTS idx_memento_newsletters_report_count
  ON memento_newsletters_report(message_count DESC);

CREATE TABLE IF NOT EXISTS memento_projects_report (
  project_id INTEGER PRIMARY KEY REFERENCES memento_project(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at DATE,
  summary_json TEXT NOT NULL DEFAULT '{}',
  members_json TEXT NOT NULL DEFAULT '[]',
  generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memento_projects_report_slug
  ON memento_projects_report(slug);

CREATE TABLE IF NOT EXISTS memento_concepts_report (
  concept_id INTEGER PRIMARY KEY REFERENCES memento_concept(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL,
  scope_description TEXT NOT NULL DEFAULT '',
  message_count INTEGER NOT NULL DEFAULT 0,
  payload_json TEXT NOT NULL DEFAULT '{}',
  generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memento_concepts_report_slug
  ON memento_concepts_report(slug);

CREATE TABLE IF NOT EXISTS memento_report_meta (
  dimension TEXT PRIMARY KEY,
  generated_at DATETIME NOT NULL,
  row_count INTEGER NOT NULL DEFAULT 0
);
`,
	},
	{
		Version: 9,
		Name:    "create_memento_draft",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_draft (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  name_hint TEXT NOT NULL DEFAULT '',
  transcript_json TEXT NOT NULL DEFAULT '[]',
  entities_json TEXT NOT NULL DEFAULT '{}',
  interaction_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'collecting',
  committed_entity_id INTEGER,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_memento_draft_status
  ON memento_draft(kind, status, updated_at DESC);
`,
	},
	{
		Version: 10,
		Name:    "create_notes_and_person_agent_tables",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_note (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  dimension TEXT NOT NULL,
  entity_id INTEGER NOT NULL,
  content TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_memento_note_lookup
  ON memento_note(dimension, entity_id);

CREATE TABLE IF NOT EXISTS memento_person_facet (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  person_id INTEGER NOT NULL REFERENCES memento_person(id) ON DELETE CASCADE,
  facet_type TEXT NOT NULL,
  content TEXT NOT NULL,
  source_message_ids TEXT NOT NULL DEFAULT '[]',
  confidence REAL NOT NULL DEFAULT 1.0,
  generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  edited_by TEXT NOT NULL DEFAULT 'llm'
);
CREATE INDEX IF NOT EXISTS idx_memento_person_facet_person
  ON memento_person_facet(person_id, facet_type);

CREATE TABLE IF NOT EXISTS memento_person_narrative (
  person_id INTEGER NOT NULL REFERENCES memento_person(id) ON DELETE CASCADE,
  section TEXT NOT NULL,
  content TEXT NOT NULL,
  source_message_ids TEXT NOT NULL DEFAULT '[]',
  generated_at DATETIME,
  edited_by TEXT NOT NULL DEFAULT 'llm',
  PRIMARY KEY (person_id, section)
);
`,
	},
	{
		Version: 11,
		Name:    "newsletter_dismissed_at",
		SQL: `
ALTER TABLE memento_newsletter_source ADD COLUMN dismissed_at DATETIME;
`,
	},
	{
		Version: 12,
		Name:    "dimension_dismissed_at",
		SQL: `
ALTER TABLE memento_person ADD COLUMN dismissed_at DATETIME;
ALTER TABLE memento_project ADD COLUMN dismissed_at DATETIME;
ALTER TABLE memento_concept ADD COLUMN dismissed_at DATETIME;
`,
	},
	{
		Version: 13,
		Name:    "normalize_person_canonical_name_last_first",
		// Fix existing "Last, First" canonical names → "First Last".
		// Only touches rows with exactly one comma and a non-empty first part.
		// Run `./memento refresh` after migrating to propagate to report tables.
		SQL: `
UPDATE memento_person
SET
  canonical_name = TRIM(SUBSTR(canonical_name, INSTR(canonical_name, ',') + 1))
                   || ' ' ||
                   TRIM(SUBSTR(canonical_name, 1, INSTR(canonical_name, ',') - 1)),
  updated_at = CURRENT_TIMESTAMP
WHERE
  INSTR(canonical_name, ',') > 0
  AND INSTR(SUBSTR(canonical_name, INSTR(canonical_name, ',') + 1), ',') = 0
  AND TRIM(SUBSTR(canonical_name, INSTR(canonical_name, ',') + 1)) != '';
`,
	},
	{
		Version: 14,
		Name:    "create_agent_logging_tables",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_agent_session (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  interaction_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_memento_agent_session_lookup
  ON memento_agent_session(session_type, entity_id);

CREATE TABLE IF NOT EXISTS memento_agent_loop (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id INTEGER NOT NULL REFERENCES memento_agent_session(id) ON DELETE CASCADE,
  step_index INTEGER NOT NULL,
  input_type TEXT NOT NULL,
  input_content TEXT NOT NULL,
  assistant_text TEXT NOT NULL DEFAULT '',
  reasoning_text TEXT NOT NULL DEFAULT '',
  tool_calls_json TEXT NOT NULL DEFAULT '[]',
  tool_results_json TEXT NOT NULL DEFAULT '[]',
  duration_ms INTEGER,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_memento_agent_loop_session
  ON memento_agent_loop(session_id, step_index);
`,
	},
	{
		Version: 15,
		Name:    "draft_provenance_and_revisions",
		SQL: `
ALTER TABLE memento_project ADD COLUMN origin_draft_id INTEGER REFERENCES memento_draft(id);
ALTER TABLE memento_concept ADD COLUMN origin_draft_id INTEGER REFERENCES memento_draft(id);

CREATE TABLE IF NOT EXISTS memento_draft_revision (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  draft_id INTEGER NOT NULL REFERENCES memento_draft(id) ON DELETE CASCADE,
  revision_kind TEXT NOT NULL DEFAULT 'entities_update',
  transcript_json TEXT NOT NULL DEFAULT '[]',
  entities_json TEXT NOT NULL DEFAULT '{}',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_memento_draft_revision_lookup
  ON memento_draft_revision(draft_id, id);
`,
	},
	{
		Version: 16,
		Name:    "social_communication_graph",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_social_edge (
  person_a_id        INTEGER NOT NULL REFERENCES memento_person(id) ON DELETE CASCADE,
  person_b_id        INTEGER NOT NULL REFERENCES memento_person(id) ON DELETE CASCADE,
  direct_count       INTEGER NOT NULL DEFAULT 0,
  to_count           INTEGER NOT NULL DEFAULT 0,
  cc_count           INTEGER NOT NULL DEFAULT 0,
  bcc_count          INTEGER NOT NULL DEFAULT 0,
  co_recipient_count INTEGER NOT NULL DEFAULT 0,
  a_to_b_count       INTEGER NOT NULL DEFAULT 0,
  b_to_a_count       INTEGER NOT NULL DEFAULT 0,
  thread_count       INTEGER NOT NULL DEFAULT 0,
  first_ts           DATETIME,
  last_ts            DATETIME,
  weight             REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (person_a_id, person_b_id)
);

CREATE INDEX IF NOT EXISTS idx_memento_social_edge_a
  ON memento_social_edge(person_a_id, weight DESC, last_ts DESC);
CREATE INDEX IF NOT EXISTS idx_memento_social_edge_b
  ON memento_social_edge(person_b_id, weight DESC, last_ts DESC);

CREATE TABLE IF NOT EXISTS memento_social_metric (
  person_id           INTEGER PRIMARY KEY REFERENCES memento_person(id) ON DELETE CASCADE,
  degree              INTEGER NOT NULL DEFAULT 0,
  weighted_degree     REAL    NOT NULL DEFAULT 0,
  direct_degree       INTEGER NOT NULL DEFAULT 0,
  co_recipient_degree INTEGER NOT NULL DEFAULT 0,
  cluster_id          INTEGER,
  dormancy_days       INTEGER,
  structural_role     TEXT    NOT NULL DEFAULT 'peripheral',
  computed_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_memento_social_metric_cluster
  ON memento_social_metric(cluster_id, weighted_degree DESC);

CREATE TABLE IF NOT EXISTS memento_social_cluster (
  cluster_id          INTEGER PRIMARY KEY,
  size                INTEGER NOT NULL,
  density             REAL    NOT NULL DEFAULT 0,
  top_member_ids_json TEXT    NOT NULL DEFAULT '[]',
  label               TEXT    NOT NULL DEFAULT '',
  label_source        TEXT    NOT NULL DEFAULT 'none',
  computed_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`,
	},
	{
		Version: 17,
		Name:    "agent_decisions",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_agent_decision (
  id TEXT PRIMARY KEY,
  decision_type TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  payload_json TEXT NOT NULL DEFAULT '{}',
  result_json TEXT NOT NULL DEFAULT '{}',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  resolved_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_memento_agent_decision_lookup
  ON memento_agent_decision(entity_type, entity_id, status, created_at DESC);
`,
	},
	{
		Version: 18,
		Name:    "agentrunner_durability",
		SQL: `
ALTER TABLE memento_agent_session ADD COLUMN provider TEXT NOT NULL DEFAULT 'gemini';
ALTER TABLE memento_agent_session ADD COLUMN model TEXT NOT NULL DEFAULT 'gemini-3.5-flash';
ALTER TABLE memento_agent_session ADD COLUMN run_input_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE memento_agent_session ADD COLUMN request_metadata_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE memento_agent_session ADD COLUMN provider_state_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE memento_agent_session ADD COLUMN error_message TEXT NOT NULL DEFAULT '';
ALTER TABLE memento_agent_session ADD COLUMN heartbeat_at DATETIME;
ALTER TABLE memento_agent_session ADD COLUMN started_at DATETIME;
ALTER TABLE memento_agent_session ADD COLUMN finished_at DATETIME;

CREATE INDEX IF NOT EXISTS idx_memento_agent_session_status
  ON memento_agent_session(status, updated_at DESC);

CREATE TABLE IF NOT EXISTS memento_agent_event (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id INTEGER NOT NULL REFERENCES memento_agent_session(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memento_agent_event_seq
  ON memento_agent_event(session_id, seq);
CREATE INDEX IF NOT EXISTS idx_memento_agent_event_session_id
  ON memento_agent_event(session_id, id);
`,
	},
	{
		Version: 19,
		Name:    "agent_usage_accounting",
		SQL: `
ALTER TABLE memento_agent_session ADD COLUMN total_estimated_input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_agent_session ADD COLUMN total_estimated_output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_agent_session ADD COLUMN total_estimated_tool_result_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_agent_session ADD COLUMN total_model_input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_agent_session ADD COLUMN total_model_output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_agent_session ADD COLUMN total_model_tokens INTEGER NOT NULL DEFAULT 0;

ALTER TABLE memento_agent_loop ADD COLUMN estimated_input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_agent_loop ADD COLUMN estimated_output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_agent_loop ADD COLUMN estimated_tool_result_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_agent_loop ADD COLUMN model_input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_agent_loop ADD COLUMN model_output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_agent_loop ADD COLUMN model_total_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_agent_loop ADD COLUMN usage_json TEXT NOT NULL DEFAULT '{}';
`,
	},
	{
		Version: 20,
		Name:    "social_groups",
		// Community-detected "groups" — distinct from memento_social_cluster,
		// which stays as the raw connected-component diagnostic table. Groups are
		// produced by Louvain over a strict, bot-free edge subset and carry an
		// actionability flag so the UI/agents only act on small, coherent groups.
		SQL: `
CREATE TABLE IF NOT EXISTS memento_social_group (
  group_id            INTEGER PRIMARY KEY,
  size                INTEGER NOT NULL,
  density             REAL NOT NULL,
  label               TEXT NOT NULL DEFAULT '',
  label_source        TEXT NOT NULL DEFAULT 'none',
  is_actionable       INTEGER NOT NULL DEFAULT 0,
  suppression_reason  TEXT NOT NULL DEFAULT '',
  top_member_ids_json TEXT NOT NULL DEFAULT '[]',
  computed_at         TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS memento_social_group_member (
  group_id   INTEGER NOT NULL REFERENCES memento_social_group(group_id) ON DELETE CASCADE,
  person_id  INTEGER NOT NULL,
  PRIMARY KEY (group_id, person_id)
);
CREATE INDEX IF NOT EXISTS idx_memento_social_group_member_person
  ON memento_social_group_member(person_id);
`,
	},
	{
		Version: 21,
		Name:    "classification_overrides",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_classification_override (
  person_id               INTEGER PRIMARY KEY REFERENCES memento_person(id) ON DELETE CASCADE,
  classification_override TEXT NOT NULL,
  updated_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`,
	},
	{
		Version: 22,
		Name:    "social_group_lifecycle",
		// User-facing lifecycle on top of the auto-detected group: promotion
		// (saved_at), soft-delete (dismissed_at), rename (display_name), free-text
		// note, and cached top-thread/cadence snapshots so the card renders in
		// O(1) without re-walking msgvault on every page load. Per-member
		// excluded_at lets the user trim wrong attributions and survives Refresh.
		SQL: `
ALTER TABLE memento_social_group ADD COLUMN saved_at        DATETIME;
ALTER TABLE memento_social_group ADD COLUMN dismissed_at    DATETIME;
ALTER TABLE memento_social_group ADD COLUMN display_name    TEXT NOT NULL DEFAULT '';
ALTER TABLE memento_social_group ADD COLUMN note            TEXT NOT NULL DEFAULT '';
ALTER TABLE memento_social_group ADD COLUMN top_threads_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE memento_social_group ADD COLUMN cadence_json     TEXT NOT NULL DEFAULT '[]';

ALTER TABLE memento_social_group_member ADD COLUMN excluded_at  DATETIME;
ALTER TABLE memento_social_group_member ADD COLUMN added_by_user INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		Version: 23,
		Name:    "social_group_activity_stats",
		// All-time co-thread message count and the unix-seconds timestamp of the
		// most recent co-thread message. The cadence sparkline only covers the
		// trailing 12 months, so the card needs these separately to show an
		// honest "N messages · last activity X ago" summary for dormant groups.
		SQL: `
ALTER TABLE memento_social_group ADD COLUMN message_count   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memento_social_group ADD COLUMN last_activity_ts INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		Version: 24,
		Name:    "person_structured_attributes",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_person_attribute (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  person_id INTEGER NOT NULL REFERENCES memento_person(id) ON DELETE CASCADE,
  category TEXT NOT NULL,
  label TEXT NOT NULL,
  value TEXT NOT NULL,
  date_value TEXT,
  source_message_ids TEXT NOT NULL DEFAULT '[]',
  confidence REAL NOT NULL DEFAULT 1.0,
  generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  edited_by TEXT NOT NULL DEFAULT 'llm'
);
CREATE INDEX IF NOT EXISTS idx_memento_person_attribute_person
  ON memento_person_attribute(person_id, category, label);
`,
	},
	{
		Version: 25,
		Name:    "agent_tool_call_traces",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_agent_tool_call (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id INTEGER NOT NULL REFERENCES memento_agent_session(id) ON DELETE CASCADE,
  step_index INTEGER NOT NULL,
  call_index INTEGER NOT NULL,
  call_id TEXT NOT NULL DEFAULT '',
  tool_name TEXT NOT NULL,
  tool_kind TEXT NOT NULL DEFAULT '',
  lock_key TEXT NOT NULL DEFAULT '',
  args_json TEXT NOT NULL DEFAULT '{}',
  result_json TEXT NOT NULL DEFAULT '{}',
  error_message TEXT NOT NULL DEFAULT '',
  queued_at DATETIME,
  started_at DATETIME,
  finished_at DATETIME,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  queue_wait_ms INTEGER NOT NULL DEFAULT 0,
  lock_wait_ms INTEGER NOT NULL DEFAULT 0,
  parallel_limit INTEGER NOT NULL DEFAULT 0,
  batch_size INTEGER NOT NULL DEFAULT 0,
  args_bytes INTEGER NOT NULL DEFAULT 0,
  result_bytes INTEGER NOT NULL DEFAULT 0,
  estimated_result_tokens INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(session_id, step_index, call_index)
);

CREATE INDEX IF NOT EXISTS idx_memento_agent_tool_call_session
  ON memento_agent_tool_call(session_id, step_index, call_index);
CREATE INDEX IF NOT EXISTS idx_memento_agent_tool_call_name
  ON memento_agent_tool_call(tool_name, finished_at DESC);
`,
	},
	{
		Version: 26,
		Name:    "create_memento_ask_sessions",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_ask_session (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  pinned INTEGER NOT NULL DEFAULT 0,
  archived_at DATETIME,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_memento_ask_session_listing
  ON memento_ask_session(archived_at, pinned DESC, updated_at DESC);

CREATE TABLE IF NOT EXISTS memento_ask_turn (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ask_session_id INTEGER NOT NULL REFERENCES memento_ask_session(id) ON DELETE CASCADE,
  run_id INTEGER REFERENCES memento_agent_session(id) ON DELETE SET NULL,
  turn_index INTEGER NOT NULL,
  user_message TEXT NOT NULL,
  assistant_answer TEXT NOT NULL DEFAULT '',
  answer_summary TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'running',
  cited_message_ids_json TEXT NOT NULL DEFAULT '[]',
  tool_summary_json TEXT NOT NULL DEFAULT '{}',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(ask_session_id, turn_index)
);

CREATE TABLE IF NOT EXISTS memento_ask_context_ref (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ask_turn_id INTEGER NOT NULL REFERENCES memento_ask_turn(id) ON DELETE CASCADE,
  ref_kind TEXT NOT NULL,
  ref_id TEXT NOT NULL,
  label TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_memento_ask_context_ref_turn
  ON memento_ask_context_ref(ask_turn_id);
`,
	},
	{
		Version: 27,
		Name:    "create_memento_avatar",
		SQL: `
CREATE TABLE IF NOT EXISTS memento_avatar (
  email_hash TEXT PRIMARY KEY,
  status TEXT NOT NULL CHECK (status IN ('found', 'notfound')),
  image BLOB,
  mime_type TEXT,
  byte_size INTEGER,
  upstream_etag TEXT,
  fetched_at TEXT NOT NULL DEFAULT (datetime('now')),
  CHECK (
    (status = 'found' AND image IS NOT NULL AND mime_type IS NOT NULL AND byte_size > 0)
    OR
    (status = 'notfound' AND image IS NULL AND mime_type IS NULL AND byte_size IS NULL)
  )
);
`,
	},
}

func Open(path string) (*sql.DB, error) {
	// _txlock=immediate makes BeginTx take SQLite's write lock upfront, so a
	// deferred read-then-write transaction can't be invalidated by an external
	// writer committing in between (which surfaces as SQLITE_BUSY_SNAPSHOT 517
	// when the msgvault CLI subprocess writes to the same DB file).
	// busy_timeout(10000) gives those external writers room to finish.
	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB) ([]Migration, error) {
	if err := assertMementoOnlyMigrations(); err != nil {
		return nil, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS memento_schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return nil, err
	}

	var applied []Migration
	for _, migration := range migrations {
		var exists int
		err := tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM memento_schema_migrations WHERE version = ?",
			migration.Version,
		).Scan(&exists)
		if err != nil {
			return nil, err
		}
		if exists != 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			return nil, fmt.Errorf("migration %d %s: %w", migration.Version, migration.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO memento_schema_migrations(version, name) VALUES (?, ?)",
			migration.Version,
			migration.Name,
		); err != nil {
			return nil, err
		}
		applied = append(applied, migration)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return applied, nil
}

// assertMementoOnlyMigrations refuses to run any migration that mutates a
// msgvault-owned table. Memento may only own tables with the `memento_` prefix.
func assertMementoOnlyMigrations() error {
	forbiddenTables := []string{
		"messages",
		"participants",
		"message_recipients",
		"conversations",
		"labels",
		"attachments",
		"sources",
		"threads",
	}
	for _, migration := range migrations {
		upper := strings.ToUpper(migration.SQL)
		for _, table := range forbiddenTables {
			for _, verb := range []string{"CREATE TABLE", "ALTER TABLE", "DROP TABLE", "INSERT INTO", "UPDATE", "DELETE FROM"} {
				needle := verb + " " + strings.ToUpper(table)
				if strings.Contains(upper, needle) {
					return fmt.Errorf("migration %d %s touches non-Memento table %q", migration.Version, migration.Name, table)
				}
				needleQuoted := verb + ` "` + strings.ToUpper(table) + `"`
				if strings.Contains(upper, needleQuoted) {
					return fmt.Errorf("migration %d %s touches non-Memento table %q", migration.Version, migration.Name, table)
				}
			}
		}
	}
	return nil
}
