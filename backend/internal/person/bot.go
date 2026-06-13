package person

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
)

// Bot-person detection: deterministic identification of non-human identities
// (newsletters, no-reply / notification senders, generic role mailboxes,
// brand-as-person collisions). These pollute every downstream feature that
// joins through memento_person — the People directory, the candidate
// classifier, the social graph, clustering, and agent context — so we detect
// them once here and let callers mark them `excluded`.
//
// The signal is the union of three deterministic tests, with zero false
// positives observed on the reference archive:
//
//	S1 — every email address for the person has a local-part matching the
//	     no-reply / role / generic regex below.
//	S2 — the canonical name is an exact (case-insensitive) match for a generic
//	     role word ("Admin", "Support", "GitHub", "No Reply", …).
//	NL — the person is the resolver for a detected newsletter source. This
//	     catches author-first-name senders the regex cannot (lenny@substack.com,
//	     tyler@ui.dev) because the newsletter detector already proved they are
//	     broadcast-only.
//
// All three are pure SQL + regex; no LLM involvement.

// botLocalPartPattern matches no-reply / role / generic mailbox local-parts,
// optionally followed by a `+tag`. Anchored to the whole local-part so we do
// not flag real humans like `alexreports@…`. Intentionally broader than the
// classifier regex in internal/people/candidates.go — this is the canonical
// bot test reused across modules.
var botLocalPartPattern = regexp.MustCompile(`(?i)^(no[-_.]?reply|donotreply|notifications?|alerts?|security[-_.]?alerts?|admin|info|hello|hi|support|contact|help|billing|accounts?|team|automated|mailer|bounce|news|newsletter|marketing|updates?|digest|robot|bot|system|webmaster|postmaster|reply|notify|service|services|email|mail|messages?|inbox|customercare|customerservice|orders?|receipts?|invoice|hr|jobs|careers|recruiting|talent)(\+.*)?$`)

// genericPersonNames is the S2 blocklist: canonical names that are never a real
// person. The brand entries are pragmatic (the newsletter signal catches most
// brand senders going forward); the role words are principled.
var genericPersonNames = map[string]bool{
	"admin": true, "administrator": true, "info": true, "hello": true,
	"hi": true, "support": true, "team": true, "notifications": true,
	"notification": true, "security alert": true, "security alerts": true,
	"subscribed": true, "push": true, "comment": true, "updates": true,
	"update": true, "newsletter": true, "digest": true,
	"no reply": true, "do not reply": true, "noreply": true,
	"alerts": true, "alert": true, "news": true, "marketing": true,
	// brand-as-person collisions seen in real data
	"google": true, "google in": true, "adobe": true, "github": true,
	"amazon web services": true, "pwsupport": true,
}

// botReason labels, ordered by descending authority (the first match wins).
const (
	botReasonNewsletter  = "newsletter source"
	botReasonNoReply     = "no-reply / role address"
	botReasonGenericName = "generic role name"
)

func localPart(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if at := strings.Index(email, "@"); at > 0 {
		return email[:at]
	}
	return email
}

// BotPersonIDs returns every bot person mapped to a human-readable reason.
// Batch primitive — one pass over memento_person_email, memento_person, and
// memento_newsletter_source. Reasons are assigned by authority: a newsletter
// source beats a no-reply local-part beats a generic name.
func BotPersonIDs(ctx context.Context, db *sql.DB) (map[int64]string, error) {
	reasons := map[int64]string{}
	set := func(id int64, reason string) {
		if _, ok := reasons[id]; !ok {
			reasons[id] = reason
		}
	}

	// NL — newsletter-source senders resolved to a person (highest authority).
	nlRows, err := db.QueryContext(ctx, `
		SELECT DISTINCT pe.person_id
		FROM memento_newsletter_source ns
		JOIN memento_person_email pe ON pe.email_address = lower(ns.sender_email)
	`)
	if err != nil {
		return nil, err
	}
	for nlRows.Next() {
		var id int64
		if err := nlRows.Scan(&id); err != nil {
			nlRows.Close()
			return nil, err
		}
		set(id, botReasonNewsletter)
	}
	if err := nlRows.Close(); err != nil {
		return nil, err
	}

	// S1 — every email's local-part matches the bot regex. Gather all emails
	// per person, then require the whole set to match.
	emailRows, err := db.QueryContext(ctx, `
		SELECT person_id, email_address FROM memento_person_email ORDER BY person_id
	`)
	if err != nil {
		return nil, err
	}
	emailsByPerson := map[int64][]string{}
	for emailRows.Next() {
		var id int64
		var email string
		if err := emailRows.Scan(&id, &email); err != nil {
			emailRows.Close()
			return nil, err
		}
		emailsByPerson[id] = append(emailsByPerson[id], email)
	}
	if err := emailRows.Close(); err != nil {
		return nil, err
	}
	for id, emails := range emailsByPerson {
		if len(emails) == 0 {
			continue
		}
		allBot := true
		for _, e := range emails {
			if !botLocalPartPattern.MatchString(localPart(e)) {
				allBot = false
				break
			}
		}
		if allBot {
			set(id, botReasonNoReply)
		}
	}

	// S2 — generic canonical name.
	nameRows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(canonical_name, '') FROM memento_person
	`)
	if err != nil {
		return nil, err
	}
	for nameRows.Next() {
		var id int64
		var name string
		if err := nameRows.Scan(&id, &name); err != nil {
			nameRows.Close()
			return nil, err
		}
		if genericPersonNames[strings.ToLower(strings.TrimSpace(name))] {
			set(id, botReasonGenericName)
		}
	}
	if err := nameRows.Close(); err != nil {
		return nil, err
	}

	return reasons, nil
}

// IsBotPerson reports whether one person is a bot, with the reason. Convenience
// wrapper for the per-person path (e.g. resolution-time hardening); for bulk
// classification use BotPersonIDs.
func IsBotPerson(ctx context.Context, db *sql.DB, personID int64) (bool, string, error) {
	reasons, err := BotPersonIDs(ctx, db)
	if err != nil {
		return false, "", err
	}
	reason, ok := reasons[personID]
	return ok, reason, nil
}
