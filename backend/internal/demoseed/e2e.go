package demoseed

import (
	"context"
	"database/sql"
	"encoding/json"
)

func CreateMsgvaultTables(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE sources (
  id INTEGER PRIMARY KEY,
  source_type TEXT NOT NULL,
  identifier TEXT NOT NULL
);

CREATE TABLE participants (
  id INTEGER PRIMARY KEY,
  email_address TEXT,
  display_name TEXT,
  domain TEXT,
  canonical_id TEXT
);

CREATE TABLE conversations (
  id INTEGER PRIMARY KEY,
  source_id INTEGER,
  source_conversation_id TEXT,
  subject TEXT
);

CREATE TABLE messages (
  id INTEGER PRIMARY KEY,
  conversation_id INTEGER,
  source_id INTEGER,
  source_message_id TEXT,
  message_type TEXT NOT NULL DEFAULT 'email',
  sent_at DATETIME,
  sender_id INTEGER,
  is_from_me BOOLEAN,
  subject TEXT,
  snippet TEXT
);

CREATE TABLE message_bodies (
  message_id INTEGER PRIMARY KEY,
  body_text TEXT
);

CREATE TABLE message_recipients (
  id INTEGER PRIMARY KEY,
  message_id INTEGER NOT NULL,
  participant_id INTEGER NOT NULL,
  recipient_type TEXT NOT NULL,
  display_name TEXT
);

CREATE TABLE labels (
  id INTEGER PRIMARY KEY,
  name TEXT
);

CREATE TABLE message_labels (
  message_id INTEGER,
  label_id INTEGER
);

CREATE TABLE attachments (
  id INTEGER PRIMARY KEY,
  message_id INTEGER,
  filename TEXT
);
`)
	return err
}

func SeedE2E(ctx context.Context, db *sql.DB) error {
	aliases := mustJSON([]map[string]any{
		{
			"email_address": "casey@example.com",
			"display_name":  "Casey Rivera",
			"link_source":   "resolve",
			"locked":        false,
		},
	})
	timeline := mustJSON([]map[string]any{
		{
			"date":       "2026-05-20T10:00:00Z",
			"subject":    "Memento review notes",
			"snippet":    "The dashboard and people directory are ready for review.",
			"message_id": 1002,
			"direction":  "from_contact",
			"via_email":  "casey@example.com",
		},
		{
			"date":       "2026-05-18T09:30:00Z",
			"subject":    "Re: Fixture project kickoff",
			"snippet":    "Thanks Casey, I will wire the local-first test harness.",
			"message_id": 1001,
			"direction":  "to_contact",
			"via_email":  "casey@example.com",
		},
	})
	correspondents := mustJSON([]map[string]any{})
	projectPhases := mustJSON([]map[string]any{
		{
			"title":              "Harness foundation",
			"date_range":         "May 2026",
			"content":            "The team agreed to start with a disposable fixture database and two smoke pages. [msg:1001]",
			"source_message_ids": []int64{1001},
		},
		{
			"title":              "Review loop",
			"date_range":         "May 2026",
			"content":            "Casey reviewed the dashboard and people workflows before expanding to projects. [msg:1002]",
			"source_message_ids": []int64{1002},
		},
	})
	projectFriction := mustJSON([]map[string]any{
		{
			"text":               "The team deferred broader dimension coverage until the project generation path could be reviewed. [msg:1003]",
			"source_message_ids": []int64{1003},
		},
	})
	projectSummary := mustJSON(map[string]any{
		"project_id":      1,
		"slug":            "fixture-launch",
		"name":            "Fixture Launch",
		"aliases":         []string{"E2E harness"},
		"status":          "active",
		"started_at":      "2026-05-18",
		"message_count":   3,
		"summary_excerpt": "Fixture Launch coordinates the E2E harness rollout across dashboard, people, and project workflows. [msg:1001][msg:1002]",
	})
	newsletterSubjects := mustJSON([]string{
		"Local-first digest: fixture coverage",
		"Local-first digest: simulated agents",
	})
	newsletterThemes := mustJSON([]map[string]any{
		{
			"theme":              "Testing local-first product workflows",
			"source_message_ids": []int64{1004, 1005},
		},
	})
	newsletterNotableRecent := mustJSON([]map[string]any{
		{
			"headline":           "Digest highlights simulated generation checks",
			"date":               "2026-05-23",
			"source_message_ids": []int64{1005},
		},
	})
	conceptInsights := mustJSON([]map[string]any{
		{
			"title":              "Disposable fixtures keep E2E runs isolated",
			"content":            "The test harness uses seeded local data so browser checks do not touch the real archive. [msg:1002][msg:1004]",
			"source_message_ids": []int64{1002, 1004},
		},
	})
	conceptIndexPayload := mustJSON(map[string]any{
		"slug":              "local-first-memory",
		"name":              "Local-first Memory",
		"scope_description": "How Memento turns a local email archive into source-attributed dimensional memory.",
		"status":            "active",
		"message_count":     2,
		"has_narrative":     true,
	})

	_, err := db.ExecContext(ctx, `
INSERT INTO sources (id, source_type, identifier)
VALUES (1, 'email', 'owner@example.com');

INSERT INTO participants (id, email_address, display_name, domain)
VALUES
  (1, 'owner@example.com', 'Ann Owner', 'example.com'),
  (2, 'casey@example.com', 'Casey Rivera', 'example.com'),
  (3, 'digest@fixture.dev', 'Fixture Digest', 'fixture.dev');

INSERT INTO conversations (id, source_id, source_conversation_id, subject)
VALUES (900, 1, 'fixture-conversation-900', 'Fixture project kickoff');

INSERT INTO messages (id, conversation_id, source_id, source_message_id, message_type, sent_at, sender_id, is_from_me, subject, snippet)
VALUES
  (1001, 900, 1, 'fixture-message-1001', 'email', '2026-05-18T09:30:00Z', 1, 1, 'Re: Fixture project kickoff', 'Thanks Casey, I will wire the local-first test harness.'),
  (1002, 900, 1, 'fixture-message-1002', 'email', '2026-05-20T10:00:00Z', 2, 0, 'Memento review notes', 'The dashboard and people directory are ready for review.'),
  (1003, 900, 1, 'fixture-message-1003', 'email', '2026-05-21T14:15:00Z', 1, 1, 'Project coverage next step', 'Next we will cover project browsing and simulated generation.'),
  (1004, 900, 1, 'fixture-message-1004', 'email', '2026-05-22T08:00:00Z', 3, 0, 'Local-first digest: fixture coverage', 'This digest covers local-first memory, fixture databases, and cited UI checks.'),
  (1005, 900, 1, 'fixture-message-1005', 'email', '2026-05-23T08:00:00Z', 3, 0, 'Local-first digest: simulated agents', 'This issue highlights simulated generation flows for concepts and newsletters.');

INSERT INTO message_bodies (message_id, body_text)
VALUES
  (1001, 'Thanks Casey, I will wire the local-first test harness.'),
  (1002, 'The dashboard and people directory are ready for review.'),
  (1003, 'Next we will cover project browsing and simulated generation before expanding to concepts and newsletters.'),
  (1004, 'This digest covers local-first memory, fixture databases, source citations, and browser checks. Unsubscribe from Fixture Digest.'),
  (1005, 'This issue highlights simulated generation flows for concepts and newsletters in the E2E harness. Unsubscribe from Fixture Digest.');

INSERT INTO message_recipients (id, message_id, participant_id, recipient_type, display_name)
VALUES
  (1, 1001, 2, 'to', 'Casey Rivera'),
  (2, 1002, 1, 'to', 'Ann Owner'),
  (3, 1003, 2, 'to', 'Casey Rivera'),
  (4, 1004, 1, 'to', 'Ann Owner'),
  (5, 1005, 1, 'to', 'Ann Owner');

INSERT INTO memento_config (key, value, updated_at)
VALUES
  ('owner_name', 'Ann Owner', 0),
  ('owner_email', 'owner@example.com', 0),
  ('onboarding_status', 'complete', 0),
  ('onboarding_completed_at', '2026-06-05T00:00:00Z', 0);

INSERT INTO memento_person (id, canonical_name, primary_email, note)
VALUES (1, 'Casey Rivera', 'casey@example.com', 'Design partner for the E2E fixture.');

INSERT INTO memento_person_email (email_address, person_id, display_name, link_source, confidence, locked)
VALUES ('casey@example.com', 1, 'Casey Rivera', 'resolve', 1.0, 0);

INSERT INTO memento_people_candidates (
  person_id, canonical_name, primary_email, domain, email_count,
  total_messages, from_contact_count, to_contact_count, bidirectional_score,
  classification, first_message_at, last_message_at, sample_message_ids
)
VALUES (
  1, 'Casey Rivera', 'casey@example.com', 'example.com', 1,
  2, 1, 1, 1.0,
  'candidate', '2026-05-18T09:30:00Z', '2026-05-20T10:00:00Z', '[1001,1002]'
);

INSERT INTO memento_person_narrative (person_id, section, content, source_message_ids, generated_at, edited_by)
VALUES
  (1, 'summary', 'Casey has been the main design partner for the Memento fixture project. [msg:1002]', '[1002]', '2026-05-20T10:00:00Z', 'llm'),
  (1, 'relationship_arc', 'The relationship starts with implementation planning and moves into review feedback. [msg:1001][msg:1002]', '[1001,1002]', '2026-05-20T10:00:00Z', 'llm'),
  (1, 'current_status', 'Current collaboration is active around dashboard and people page validation. [msg:1002]', '[1002]', '2026-05-20T10:00:00Z', 'llm');

INSERT INTO memento_person_facet (person_id, facet_type, content, source_message_ids, confidence, generated_at, edited_by)
VALUES (1, 'collaboration', 'Reviews product and design details for the local-first memory layer.', '[1002]', 0.95, '2026-05-20T10:00:00Z', 'llm');

INSERT INTO memento_person_attribute (person_id, category, label, value, source_message_ids, confidence, generated_at, edited_by)
VALUES (1, 'role', 'Fixture role', 'Design reviewer', '[1002]', 0.95, '2026-05-20T10:00:00Z', 'llm');

INSERT INTO memento_social_metric (person_id, degree, weighted_degree, direct_degree, co_recipient_degree, structural_role, dormancy_days)
VALUES (1, 0, 0, 0, 0, 'peripheral', 14);

INSERT INTO memento_project (id, slug, name, aliases, status, started_at, note)
VALUES (1, 'fixture-launch', 'Fixture Launch', '["E2E harness"]', 'active', '2026-05-18', 'Seeded project for Phase 2 Playwright coverage.');

INSERT INTO memento_project_member (project_id, person_id, role)
VALUES (1, 1, 'Design reviewer');

INSERT INTO memento_project_message (project_id, message_id, included_by)
VALUES
  (1, 1001, 'fixture'),
  (1, 1002, 'fixture'),
  (1, 1003, 'fixture');

INSERT INTO memento_newsletter_source (
  id, sender_email, display_name, domain, slug, first_seen, last_seen,
  message_count, unsubscribe_count, classification_reason
)
VALUES (
  1, 'digest@fixture.dev', 'Fixture Digest', 'fixture.dev', 'fixture-digest',
  '2026-05-22T08:00:00Z', '2026-05-23T08:00:00Z',
  2, 2, 'newsletter-like sender address'
);

INSERT INTO memento_concept (
  id, slug, name, scope_description, status, seed_keywords, note
)
VALUES (
  1, 'local-first-memory', 'Local-first Memory',
  'How Memento turns a local email archive into source-attributed dimensional memory.',
  'active', '["local-first","citations","fixtures"]',
  'Seeded concept for Phase 3 Playwright coverage.'
);

INSERT INTO memento_concept_message (concept_id, message_id, added_by, query_term, relevance_score)
VALUES
  (1, 1002, 'fixture', 'local-first memory', 0.92),
  (1, 1004, 'fixture', 'local-first memory', 0.98);

INSERT INTO memento_report_meta (dimension, generated_at, row_count)
VALUES
  ('people', '2026-05-20T10:00:00Z', 1),
  ('projects', '2026-05-21T14:15:00Z', 1),
  ('newsletters', '2026-05-23T08:00:00Z', 1),
  ('concepts', '2026-05-23T08:00:00Z', 1);
`)
	if err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO memento_people_report (
  person_id, canonical_name, primary_email, domain, email_count,
  total_messages, from_contact_count, to_contact_count, bidirectional_score,
  classification, first_message_at, last_message_at, slug,
  aliases_json, timeline_json, top_correspondents_json
)
VALUES (?, 'Casey Rivera', 'casey@example.com', 'example.com', 1, 2, 1, 1, 1.0,
  'candidate', '2026-05-18T09:30:00Z', '2026-05-20T10:00:00Z', 'casey-rivera',
  ?, ?, ?);
`, int64(1), aliases, timeline, correspondents); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO memento_project_narrative (project_id, section, content, source_message_ids, generated_at, edited_by)
VALUES
  (1, 'summary', 'Fixture Launch coordinates the E2E harness rollout across dashboard, people, and project workflows. [msg:1001][msg:1002]', '[1001,1002]', '2026-05-21T14:15:00Z', 'llm'),
  (1, 'phases', ?, '[1001,1002]', '2026-05-21T14:15:00Z', 'llm'),
  (1, 'friction_points', ?, '[1003]', '2026-05-21T14:15:00Z', 'llm'),
  (1, 'current_understanding', 'The project is ready to validate one simulated generation workflow before broader E2E expansion. [msg:1003]', '[1003]', '2026-05-21T14:15:00Z', 'llm');
`, projectPhases, projectFriction); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO memento_projects_report (project_id, slug, name, status, started_at, summary_json, members_json)
VALUES (1, 'fixture-launch', 'Fixture Launch', 'active', '2026-05-18', ?, '[]');
`, projectSummary); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO memento_newsletter_narrative (source_id, section, content, source_message_ids, generated_at, edited_by)
VALUES
  (1, 'coverage_summary', 'Fixture Digest tracks local-first memory patterns, source citations, and simulated generation workflows for the E2E harness. [msg:1004][msg:1005]', '[1004,1005]', '2026-05-23T08:00:00Z', 'llm'),
  (1, 'recurring_themes', ?, '[1004,1005]', '2026-05-23T08:00:00Z', 'llm'),
  (1, 'notable_recent', ?, '[1005]', '2026-05-23T08:00:00Z', 'llm');
`, newsletterThemes, newsletterNotableRecent); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO memento_newsletters_report (
  source_id, slug, display_name, sender_email, domain,
  message_count, unsubscribe_count, first_seen, last_seen,
  classification_reason, recent_subjects_json
)
VALUES (
  1, 'fixture-digest', 'Fixture Digest', 'digest@fixture.dev', 'fixture.dev',
  2, 2, '2026-05-22T08:00:00Z', '2026-05-23T08:00:00Z',
  'newsletter-like sender address', ?
);
`, newsletterSubjects); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO memento_concept_narrative (concept_id, section, content, source_message_ids, generated_at, edited_by)
VALUES
  (1, 'scope_summary', 'Local-first Memory covers the way Memento keeps source-attributed knowledge on top of a local archive. [msg:1002][msg:1004]', '[1002,1004]', '2026-05-23T08:00:00Z', 'llm'),
  (1, 'distilled_insights', ?, '[1002,1004]', '2026-05-23T08:00:00Z', 'llm'),
  (1, 'evolving_understanding', 'Coverage has moved from dashboard and people smoke checks toward broader simulated generation workflows. [msg:1002][msg:1004]', '[1002,1004]', '2026-05-23T08:00:00Z', 'llm');
`, conceptInsights); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO memento_concepts_report (
  concept_id, slug, name, status, scope_description, message_count, payload_json
)
VALUES (
  1, 'local-first-memory', 'Local-first Memory', 'active',
  'How Memento turns a local email archive into source-attributed dimensional memory.',
  2, ?
);
`, conceptIndexPayload); err != nil {
		return err
	}

	return nil
}

func mustJSON(v any) string {
	out, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(out)
}
