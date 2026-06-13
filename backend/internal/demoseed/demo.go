package demoseed

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed fixtures/corpus.json
var demoCorpusJSON []byte

//go:embed fixtures/baked.json
var demoBakedJSON []byte

//go:embed fixtures/relationships.json
var demoRelationshipsJSON []byte

//go:embed fixtures/duplicates.json
var demoDuplicatesJSON []byte

type demoCorpus struct {
	Owner struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"owner"`
	Participants []demoParticipant `json:"participants"`
	Messages     []demoMessage     `json:"messages"`
}

type demoParticipant struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type demoMessage struct {
	ID           int64   `json:"id"`
	Conversation int64   `json:"conversation"`
	SentAt       string  `json:"sent_at"`
	Sender       int64   `json:"sender"`
	To           []int64 `json:"to"`
	Subject      string  `json:"subject"`
	Body         string  `json:"body"`
}

type demoBaked struct {
	People      []demoPerson     `json:"people"`
	Projects    []demoProject    `json:"projects"`
	Concepts    []demoConcept    `json:"concepts"`
	Newsletters []demoNewsletter `json:"newsletters"`
}

type demoPerson struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Email   string   `json:"email"`
	Aliases []string `json:"aliases"`
	Note    string   `json:"note"`
	Summary string   `json:"summary"`
	Facet   string   `json:"facet"`
}

type demoProject struct {
	ID         int64           `json:"id"`
	Slug       string          `json:"slug"`
	Name       string          `json:"name"`
	StartedAt  string          `json:"started_at"`
	MessageIDs []int64         `json:"message_ids"`
	Members    [][]interface{} `json:"members"`
	Summary    string          `json:"summary"`
}

type demoConcept struct {
	ID         int64    `json:"id"`
	Slug       string   `json:"slug"`
	Name       string   `json:"name"`
	Scope      string   `json:"scope"`
	Keywords   []string `json:"keywords"`
	MessageIDs []int64  `json:"message_ids"`
	Summary    string   `json:"summary"`
}

type demoNewsletter struct {
	Email    string            `json:"email"`
	Sections map[string]string `json:"sections"`
}

func loadDemoFixtures() (demoCorpus, demoBaked, error) {
	var corpus demoCorpus
	if err := json.Unmarshal(demoCorpusJSON, &corpus); err != nil {
		return corpus, demoBaked{}, fmt.Errorf("decode demo corpus: %w", err)
	}
	var baked demoBaked
	if err := json.Unmarshal(demoBakedJSON, &baked); err != nil {
		return corpus, baked, fmt.Errorf("decode baked demo content: %w", err)
	}
	var relationships struct {
		Messages []demoMessage `json:"messages"`
	}
	if err := json.Unmarshal(demoRelationshipsJSON, &relationships); err != nil {
		return corpus, baked, fmt.Errorf("decode demo relationship corpus: %w", err)
	}
	corpus.Messages = append(corpus.Messages, relationships.Messages...)
	var duplicates struct {
		Participants []demoParticipant `json:"participants"`
		Messages     []demoMessage     `json:"messages"`
	}
	if err := json.Unmarshal(demoDuplicatesJSON, &duplicates); err != nil {
		return corpus, baked, fmt.Errorf("decode demo duplicate corpus: %w", err)
	}
	corpus.Participants = append(corpus.Participants, duplicates.Participants...)
	corpus.Messages = append(corpus.Messages, duplicates.Messages...)
	return corpus, baked, nil
}

// SeedDemo inserts the authored raw corpus, fixed entity IDs, and baked narrative
// content. Deterministic classifications, newsletter sources, rollups, and the
// social graph are intentionally left to the real pipeline.
func SeedDemo(ctx context.Context, db *sql.DB) error {
	corpus, baked, err := loadDemoFixtures()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO sources (id, source_type, identifier) VALUES (1, 'email', ?)`, corpus.Owner.Email); err != nil {
		return err
	}
	for _, p := range corpus.Participants {
		domain := ""
		if parts := strings.SplitN(p.Email, "@", 2); len(parts) == 2 {
			domain = parts[1]
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO participants (id, email_address, display_name, domain) VALUES (?, ?, ?, ?)`, p.ID, p.Email, p.Name, domain); err != nil {
			return fmt.Errorf("insert participant %d: %w", p.ID, err)
		}
	}
	seenConversations := map[int64]bool{}
	recipientID := int64(1)
	for _, m := range corpus.Messages {
		if !seenConversations[m.Conversation] {
			if _, err := tx.ExecContext(ctx, `INSERT INTO conversations (id, source_id, source_conversation_id, subject) VALUES (?, 1, ?, ?)`, m.Conversation, fmt.Sprintf("demo-thread-%d", m.Conversation), m.Subject); err != nil {
				return err
			}
			seenConversations[m.Conversation] = true
		}
		fromMe := 0
		if m.Sender == 1 {
			fromMe = 1
		}
		snippet := m.Body
		if len(snippet) > 180 {
			snippet = snippet[:180]
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO messages (id, conversation_id, source_id, source_message_id, message_type, sent_at, sender_id, is_from_me, subject, snippet)
			VALUES (?, ?, 1, ?, 'email', ?, ?, ?, ?, ?)`,
			m.ID, m.Conversation, fmt.Sprintf("demo-message-%d", m.ID), m.SentAt, m.Sender, fromMe, m.Subject, snippet); err != nil {
			return fmt.Errorf("insert message %d: %w", m.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO message_bodies (message_id, body_text) VALUES (?, ?)`, m.ID, m.Body); err != nil {
			return err
		}
		for _, recipient := range m.To {
			if _, err := tx.ExecContext(ctx, `INSERT INTO message_recipients (id, message_id, participant_id, recipient_type) VALUES (?, ?, ?, 'to')`, recipientID, m.ID, recipient); err != nil {
				return err
			}
			recipientID++
		}
	}

	for _, item := range []struct{ key, value string }{
		{"owner_name", corpus.Owner.Name},
		{"owner_email", corpus.Owner.Email},
		{"demo_mode", "true"},
		{"onboarding_status", "complete"},
		{"onboarding_completed_at", "2026-06-05T00:00:00Z"},
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO memento_config (key, value, updated_at) VALUES (?, ?, 0)`, item.key, item.value); err != nil {
			return err
		}
	}

	participantByEmail := map[string]demoParticipant{}
	for _, p := range corpus.Participants {
		participantByEmail[p.Email] = p
	}
	for _, p := range baked.People {
		if _, err := tx.ExecContext(ctx, `INSERT INTO memento_person (id, canonical_name, primary_email, note) VALUES (?, ?, ?, ?)`, p.ID, p.Name, p.Email, p.Note); err != nil {
			return err
		}
		emails := append([]string{p.Email}, p.Aliases...)
		for _, email := range emails {
			displayName := p.Name
			if participant, ok := participantByEmail[email]; ok && participant.Name != "" {
				displayName = participant.Name
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO memento_person_email (email_address, person_id, display_name, link_source, confidence, locked)
				VALUES (?, ?, ?, 'demo', 1.0, 1)`, email, p.ID, displayName); err != nil {
				return err
			}
		}
		sourceIDs := sourceIDsFromText(p.Summary)
		for section, content := range map[string]string{
			"summary":          p.Summary,
			"relationship_arc": fmt.Sprintf("The relationship developed through recurring, bidirectional planning and review. %s", citationSuffix(sourceIDs)),
			"current_status":   fmt.Sprintf("The collaboration remains active in the most recent demo correspondence. %s", citationSuffix(sourceIDs)),
		} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO memento_person_narrative (person_id, section, content, source_message_ids, generated_at, edited_by) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, 'llm')`, p.ID, section, content, mustJSON(sourceIDs)); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO memento_person_facet (person_id, facet_type, content, source_message_ids, confidence, generated_at, edited_by) VALUES (?, 'collaboration', ?, ?, 0.95, CURRENT_TIMESTAMP, 'llm')`, p.ID, p.Facet, mustJSON(sourceIDs)); err != nil {
			return err
		}
	}

	for _, p := range baked.Projects {
		if _, err := tx.ExecContext(ctx, `INSERT INTO memento_project (id, slug, name, status, started_at, note) VALUES (?, ?, ?, 'active', ?, 'Synthetic demo project.')`, p.ID, p.Slug, p.Name, p.StartedAt); err != nil {
			return err
		}
		for _, messageID := range p.MessageIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO memento_project_message (project_id, message_id, included_by) VALUES (?, ?, 'demo')`, p.ID, messageID); err != nil {
				return err
			}
		}
		for _, member := range p.Members {
			personID, ok := member[0].(float64)
			if !ok {
				return fmt.Errorf("invalid project member person ID for %s", p.Slug)
			}
			role, _ := member[1].(string)
			if _, err := tx.ExecContext(ctx, `INSERT INTO memento_project_member (project_id, person_id, role) VALUES (?, ?, ?)`, p.ID, int64(personID), role); err != nil {
				return err
			}
		}
		for section, content := range map[string]string{
			"summary":               p.Summary,
			"current_understanding": fmt.Sprintf("The latest messages leave this project in a documented, reviewable state. %s", citationSuffix(p.MessageIDs)),
		} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO memento_project_narrative (project_id, section, content, source_message_ids, generated_at, edited_by) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, 'llm')`, p.ID, section, content, mustJSON(p.MessageIDs)); err != nil {
				return err
			}
		}
	}

	for _, c := range baked.Concepts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO memento_concept (id, slug, name, scope_description, status, seed_keywords, note) VALUES (?, ?, ?, ?, 'active', ?, 'Synthetic demo concept.')`, c.ID, c.Slug, c.Name, c.Scope, mustJSON(c.Keywords)); err != nil {
			return err
		}
		for _, messageID := range c.MessageIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO memento_concept_message (concept_id, message_id, added_by, query_term, relevance_score) VALUES (?, ?, 'demo', ?, 1.0)`, c.ID, messageID, c.Name); err != nil {
				return err
			}
		}
		for section, content := range map[string]string{
			"scope_summary":          c.Summary,
			"evolving_understanding": fmt.Sprintf("The demo archive shows this concept recurring across people and projects over time. %s", citationSuffix(c.MessageIDs)),
		} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO memento_concept_narrative (concept_id, section, content, source_message_ids, generated_at, edited_by) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, 'llm')`, c.ID, section, content, mustJSON(c.MessageIDs)); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// SeedDemoNewsletterNarratives attaches baked newsletter content after the
// deterministic detector has assigned source IDs.
func SeedDemoNewsletterNarratives(ctx context.Context, db *sql.DB) error {
	_, baked, err := loadDemoFixtures()
	if err != nil {
		return err
	}
	for _, newsletter := range baked.Newsletters {
		var sourceID int64
		if err := db.QueryRowContext(ctx, `SELECT id FROM memento_newsletter_source WHERE sender_email = ?`, newsletter.Email).Scan(&sourceID); err != nil {
			return fmt.Errorf("find detected demo newsletter %s: %w", newsletter.Email, err)
		}
		for section, content := range newsletter.Sections {
			sourceIDs := sourceIDsFromText(content)
			if _, err := db.ExecContext(ctx, `INSERT INTO memento_newsletter_narrative (source_id, section, content, source_message_ids, generated_at, edited_by) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, 'llm')`, sourceID, section, content, mustJSON(sourceIDs)); err != nil {
				return err
			}
		}
	}
	return nil
}

func sourceIDsFromText(text string) []int64 {
	var ids []int64
	original := text
	for {
		start := strings.Index(text, "[msg:")
		if start < 0 {
			break
		}
		text = text[start+5:]
		end := strings.IndexByte(text, ']')
		if end < 0 {
			break
		}
		var id int64
		if _, err := fmt.Sscan(text[:end], &id); err == nil {
			ids = append(ids, id)
		}
		text = text[end+1:]
	}
	if len(ids) == 0 {
		var value any
		if json.Unmarshal([]byte(original), &value) == nil {
			collectSourceIDs(value, &ids)
		}
	}
	return ids
}

func collectSourceIDs(value any, ids *[]int64) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "source_message_ids" {
				if values, ok := child.([]any); ok {
					for _, raw := range values {
						if id, ok := raw.(float64); ok {
							*ids = append(*ids, int64(id))
						}
					}
					continue
				}
			}
			collectSourceIDs(child, ids)
		}
	case []any:
		for _, child := range typed {
			collectSourceIDs(child, ids)
		}
	}
}

func citationSuffix(ids []int64) string {
	var out strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&out, "[msg:%d]", id)
	}
	return out.String()
}
