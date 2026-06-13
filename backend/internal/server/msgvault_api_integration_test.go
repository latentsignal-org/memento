package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunMsgvaultSearchPrefersConfiguredAPI(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("q"); got != "skilling" {
			t.Fatalf("q = %q", got)
		}
		if got := r.URL.Query().Get("mode"); got != "hybrid" {
			t.Fatalf("mode = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"skilling","mode":"hybrid","results":[{"id":42,"subject":"S","snippet":"N","sent_at":"2001-01-02T03:04:05Z"}]}`))
	}))
	defer api.Close()

	t.Setenv("MEMENTO_MSGVAULT_API_URL", api.URL)
	t.Setenv("MEMENTO_MSGVAULT_API_KEY", "token")

	results, err := runMsgvaultSearch(context.Background(), "skilling", "hybrid", 5)
	if err != nil {
		t.Fatalf("runMsgvaultSearch: %v", err)
	}
	if len(results) != 1 || results[0].ID != 42 || results[0].Subject != "S" {
		t.Fatalf("results = %#v", results)
	}
}

func TestRunMsgvaultSearchSkipsHybridForFTSQuerySyntax(t *testing.T) {
	var modes []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modes = append(modes, r.URL.Query().Get("mode"))
		if got := r.URL.Query().Get("q"); got != "steven.kean@enron.com 1999 OR 2000" {
			t.Fatalf("q = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"steven.kean@enron.com 1999 OR 2000","total":1,"messages":[{"id":43,"subject":"FTS"}]}`))
	}))
	defer api.Close()

	t.Setenv("MEMENTO_MSGVAULT_API_URL", api.URL)
	t.Setenv("MEMENTO_MSGVAULT_API_KEY", "token")

	results, err := runMsgvaultSearch(context.Background(), "steven.kean@enron.com 1999 OR 2000", "hybrid", 5)
	if err != nil {
		t.Fatalf("runMsgvaultSearch: %v", err)
	}
	if len(modes) != 1 || modes[0] != "fts" {
		t.Fatalf("modes = %#v, want only fts", modes)
	}
	if len(results) != 1 || results[0].ID != 43 {
		t.Fatalf("results = %#v", results)
	}
}

func TestRunMsgvaultSearchCLISkipsHybridForFTSQuerySyntax(t *testing.T) {
	var modes []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modes = append(modes, r.URL.Query().Get("mode"))
		if got := r.URL.Query().Get("q"); got != `"internal audit" OR "off-balance-sheet" OR "SPE" Enron accounting` {
			t.Fatalf("q = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"accounting","total":1,"messages":[{"id":44,"subject":"FTS"}]}`))
	}))
	defer api.Close()

	t.Setenv("MEMENTO_MSGVAULT_API_URL", api.URL)
	t.Setenv("MEMENTO_MSGVAULT_API_KEY", "token")

	ids, err := runMsgvaultSearchCLI(context.Background(), `"internal audit" OR "off-balance-sheet" OR "SPE" Enron accounting`, 5)
	if err != nil {
		t.Fatalf("runMsgvaultSearchCLI: %v", err)
	}
	if len(modes) != 1 || modes[0] != "fts" {
		t.Fatalf("modes = %#v, want only fts", modes)
	}
	if len(ids) != 1 || ids[0] != 44 {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestLoadMessageBodyTextPrefersConfiguredAPI(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messages/42" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"body":"api body","source_message_id":"src-42","source_conversation_id":"conv-42"}`))
	}))
	defer api.Close()

	t.Setenv("MEMENTO_MSGVAULT_API_URL", api.URL)
	t.Setenv("MEMENTO_MSGVAULT_API_KEY", "")

	body, sourceMessageID, sourceConversationID, err := loadMessageBodyText(context.Background(), 42)
	if err != nil {
		t.Fatalf("loadMessageBodyText: %v", err)
	}
	if body != "api body" || sourceMessageID != "src-42" || sourceConversationID != "conv-42" {
		t.Fatalf("body/source = %q/%q/%q", body, sourceMessageID, sourceConversationID)
	}
}
