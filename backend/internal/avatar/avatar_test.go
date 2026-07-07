package avatar

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"memento/backend/internal/store"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestHashEmailNormalizes(t *testing.T) {
	if HashEmail(" Alice@Example.COM ") != HashEmail("alice@example.com") {
		t.Fatal("HashEmail did not normalize case and whitespace")
	}
}

func TestFallbackSVGDeterministicAndEscaped(t *testing.T) {
	hashA := strings.Repeat("0", 64)
	hashB := "ff" + strings.Repeat("0", 62)
	a1 := string(FallbackSVG(hashA, "<a", 64))
	a2 := string(FallbackSVG(hashA, "<a", 64))
	b := string(FallbackSVG(hashB, "<a", 64))
	if a1 != a2 {
		t.Fatal("FallbackSVG should be deterministic")
	}
	if a1 == b {
		t.Fatal("different hashes should select different palette entries")
	}
	if strings.Contains(a1, "<A") || !strings.Contains(a1, "&lt;A") {
		t.Fatalf("initials were not XML-escaped: %s", a1)
	}
	if !strings.Contains(string(FallbackSVG(hashA, "AB", 1)), `width="24"`) {
		t.Fatal("small size was not clamped")
	}
	if !strings.Contains(string(FallbackSVG(hashA, "AB", 900)), `width="512"`) {
		t.Fatal("large size was not clamped")
	}
	if strings.Contains(string(FallbackSVG(hashA, "AB", 64)), `rx=`) {
		t.Fatal("fallback avatar should paint edge-to-edge and let the UI frame provide rounding")
	}
}

func TestFetcher(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("d") != "404" || r.URL.Query().Get("s") != "256" || r.URL.Query().Get("r") != "g" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("ETag", `"abc"`)
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
		}))
		defer server.Close()
		got, err := (&Fetcher{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), strings.Repeat("a", 64))
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusFound || got.MimeType != "image/png" || got.ByteSize != int64(len(got.Image)) || got.UpstreamETag != `"abc"` {
			t.Fatalf("unexpected result: %+v", got)
		}
	})
	t.Run("notfound", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		defer server.Close()
		got, err := (&Fetcher{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), strings.Repeat("a", 64))
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusNotFound {
			t.Fatalf("status = %q, want notfound", got.Status)
		}
	})
	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusInternalServerError)
		}))
		defer server.Close()
		_, err := (&Fetcher{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), strings.Repeat("a", 64))
		if !errors.Is(err, ErrTransient) {
			t.Fatalf("err = %v, want ErrTransient", err)
		}
	})
	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		url := server.URL
		server.Close()
		_, err := (&Fetcher{BaseURL: url, Client: server.Client()}).Fetch(context.Background(), strings.Repeat("a", 64))
		if !errors.Is(err, ErrTransient) {
			t.Fatalf("err = %v, want ErrTransient", err)
		}
	})
	t.Run("body limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(make([]byte, maxAvatarBytes+1))
		}))
		defer server.Close()
		_, err := (&Fetcher{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), strings.Repeat("a", 64))
		if !errors.Is(err, ErrTransient) {
			t.Fatalf("err = %v, want ErrTransient", err)
		}
	})
	t.Run("empty image body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
		}))
		defer server.Close()
		_, err := (&Fetcher{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), strings.Repeat("a", 64))
		if !errors.Is(err, ErrTransient) {
			t.Fatalf("err = %v, want ErrTransient", err)
		}
	})
}

func TestStoreRoundTripAndKnownHash(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	hash := HashEmail("found@example.com")
	if err := Put(ctx, db, Row{
		EmailHash:    hash,
		Status:       StatusFound,
		Image:        []byte("image"),
		MimeType:     "image/png",
		UpstreamETag: `"etag"`,
	}); err != nil {
		t.Fatal(err)
	}
	row, ok, err := Get(ctx, db, hash)
	if err != nil || !ok {
		t.Fatalf("Get found = ok %v, err %v", ok, err)
	}
	if row.Status != StatusFound || string(row.Image) != "image" || row.MimeType != "image/png" || row.ByteSize != 5 || row.UpstreamETag != `"etag"` {
		t.Fatalf("unexpected found row: %+v", row)
	}
	if err := Put(ctx, db, Row{EmailHash: hash, Status: StatusNotFound}); err != nil {
		t.Fatal(err)
	}
	row, ok, err = Get(ctx, db, hash)
	if err != nil || !ok || row.Status != StatusNotFound || row.Image != nil || row.MimeType != "" || row.ByteSize != 0 {
		t.Fatalf("unexpected notfound row: row=%+v ok=%v err=%v", row, ok, err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email) VALUES (1, 'Primary Person', 'primary@example.com');
		INSERT INTO memento_person_email (email_address, person_id, link_source, confidence) VALUES ('alias@example.com', 1, 'test', 1);
		INSERT INTO memento_config (key, value, updated_at) VALUES ('owner_email', 'owner@example.com', 0);
	`); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"alias@example.com", "primary@example.com", "owner@example.com"} {
		known, err := KnownHash(ctx, db, HashEmail(email))
		if err != nil {
			t.Fatal(err)
		}
		if !known {
			t.Fatalf("%s was not known", email)
		}
	}
	known, err := KnownHash(ctx, db, HashEmail("unknown@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if known {
		t.Fatal("unknown hash was accepted")
	}
}

func TestServiceCachesKnownMissAndSkipsUnknown(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email) VALUES (1, 'Known Person', 'known@example.com');
	`); err != nil {
		t.Fatal(err)
	}
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.NotFound(w, r)
	}))
	defer server.Close()
	svc := NewService(db, &Fetcher{BaseURL: server.URL, Client: server.Client()})
	if _, err := svc.Image(ctx, HashEmail("known@example.com"), "KP", 64); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	row, ok, err := Get(ctx, db, HashEmail("known@example.com"))
	if err != nil || !ok || row.Status != StatusNotFound {
		t.Fatalf("notfound not cached: row=%+v ok=%v err=%v", row, ok, err)
	}
	if _, err := svc.Image(ctx, HashEmail("unknown@example.com"), "U", 64); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("unknown hash triggered outbound fetch")
	}
}
