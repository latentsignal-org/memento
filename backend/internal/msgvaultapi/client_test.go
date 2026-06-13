package msgvaultapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSearchParsesFTSAndAuth(t *testing.T) {
	var gotAuth string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"skilling","total":2,"messages":[{"id":101,"subject":"A"},{"id":102,"subject":"B"}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	ids, err := client.SearchIDs(context.Background(), "skilling", "fts", 2)
	if err != nil {
		t.Fatalf("SearchIDs: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q, want Bearer secret", gotAuth)
	}
	if gotQuery != "mode=fts&page_size=2&q=skilling" {
		t.Fatalf("query = %q", gotQuery)
	}
	if len(ids) != 2 || ids[0] != 101 || ids[1] != 102 {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestClientSearchParsesHybridResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"energy trading","mode":"hybrid","returned":1,"pool_saturated":true,"results":[{"id":201,"score":{"rrf":0.03}}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "Bearer full-token")
	resp, err := client.Search(context.Background(), "energy trading", "hybrid", 5, true)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	items := resp.Items()
	if len(items) != 1 || items[0].ID != 201 {
		t.Fatalf("items = %#v", items)
	}
	if !resp.PoolSaturated {
		t.Fatal("PoolSaturated = false, want true")
	}
}

func TestClientMessageBodyFallbacks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":301,"body":"plain body"}`))
	}))
	defer server.Close()

	client := New(server.URL, "")
	msg, err := client.Message(context.Background(), 301)
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if msg.TextBody() != "plain body" {
		t.Fatalf("TextBody = %q", msg.TextBody())
	}
}

func TestClientReturnsHTTPErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad token", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := New(server.URL, "bad")
	if _, err := client.SearchIDs(context.Background(), "x", "fts", 1); err == nil {
		t.Fatal("SearchIDs error = nil, want error")
	}
}

func TestRequiresFTSMode(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{name: "plain natural language", query: "internal audit Enron accounting", want: false},
		{name: "email address", query: "steven.kean@enron.com 1999 OR 2000", want: true},
		{name: "quoted phrase", query: `"internal audit" Enron accounting`, want: true},
		{name: "boolean operator", query: "audit OR accounting", want: true},
		{name: "hyphenated token", query: "off-balance-sheet", want: true},
		{name: "gmail operator", query: "from:alice@example.com meeting", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequiresFTSMode(tt.query); got != tt.want {
				t.Fatalf("RequiresFTSMode(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}
