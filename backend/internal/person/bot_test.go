package person

import (
	"context"
	"testing"
)

func TestBotPersonIDs(t *testing.T) {
	db := newMergeTestDB(t)
	ctx := context.Background()

	// Real humans — must NOT be flagged.
	alice := seedPerson(t, db, "Alice Smith", "alice@example.com")
	seedEmail(t, db, "alice@example.com", alice, false)
	bob := seedPerson(t, db, "Bob Jones", "bob.jones@work.com")
	seedEmail(t, db, "bob.jones@work.com", bob, false)
	// A human with one role-ish alias but also a personal address — not all
	// emails match S1, so NOT a bot.
	carol := seedPerson(t, db, "Carol Lee", "carol@personal.com")
	seedEmail(t, db, "carol@personal.com", carol, false)
	seedEmail(t, db, "support@carolco.com", carol, false)

	// S1 — every email is a no-reply / role local-part.
	noreply := seedPerson(t, db, "Acme Updates", "no-reply@acme.com")
	seedEmail(t, db, "no-reply@acme.com", noreply, false)
	seedEmail(t, db, "notifications@acme.com", noreply, false)

	// S2 — generic canonical name, even though the address looks human.
	github := seedPerson(t, db, "GitHub", "ben@github.com")
	seedEmail(t, db, "ben@github.com", github, false)

	// NL — author-name newsletter sender the regex can't catch; flagged via
	// the newsletter-source join.
	lenny := seedPerson(t, db, "Lenny Rachitsky", "lenny@substack.com")
	seedEmail(t, db, "lenny@substack.com", lenny, false)
	if _, err := db.Exec(`INSERT INTO memento_newsletter_source
		(sender_email, display_name, domain, slug) VALUES (?, '', 'substack.com', 'lenny')`,
		"lenny@substack.com"); err != nil {
		t.Fatalf("seed newsletter source: %v", err)
	}

	got, err := BotPersonIDs(ctx, db)
	if err != nil {
		t.Fatalf("BotPersonIDs: %v", err)
	}

	wantBots := map[int64]string{
		noreply: botReasonNoReply,
		github:  botReasonGenericName,
		lenny:   botReasonNewsletter,
	}
	for id, want := range wantBots {
		if got[id] != want {
			t.Errorf("person %d: reason = %q, want %q", id, got[id], want)
		}
	}
	for _, human := range []int64{alice, bob, carol} {
		if reason, ok := got[human]; ok {
			t.Errorf("human person %d wrongly flagged as bot (%q)", human, reason)
		}
	}
	if len(got) != len(wantBots) {
		t.Errorf("got %d bots, want %d: %v", len(got), len(wantBots), got)
	}
}

func TestBotReasonPrecedence(t *testing.T) {
	db := newMergeTestDB(t)
	ctx := context.Background()

	// Person hits all three signals; newsletter wins.
	p := seedPerson(t, db, "Newsletter", "no-reply@news.com")
	seedEmail(t, db, "no-reply@news.com", p, false)
	if _, err := db.Exec(`INSERT INTO memento_newsletter_source
		(sender_email, display_name, domain, slug) VALUES (?, '', 'news.com', 'n')`,
		"no-reply@news.com"); err != nil {
		t.Fatalf("seed newsletter source: %v", err)
	}

	got, err := BotPersonIDs(ctx, db)
	if err != nil {
		t.Fatalf("BotPersonIDs: %v", err)
	}
	if got[p] != botReasonNewsletter {
		t.Errorf("reason = %q, want %q (newsletter beats S1/S2)", got[p], botReasonNewsletter)
	}
}
