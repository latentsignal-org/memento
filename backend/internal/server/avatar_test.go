package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"memento/backend/internal/avatar"
)

func TestHandleGetAvatarInvalidHash(t *testing.T) {
	srv := newSetupTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/avatar/not-a-hash", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetAvatarCachedFoundAndETag(t *testing.T) {
	srv := newSetupTestServer(t)
	hash := avatar.HashEmail("cached@example.com")
	if err := avatar.Put(context.Background(), srv.db, avatar.Row{
		EmailHash: hash,
		Status:    avatar.StatusFound,
		Image:     []byte("png-bytes"),
		MimeType:  "image/png",
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/avatar/"+hash+"?s=64&i=CE", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" || rec.Body.String() != "png-bytes" {
		t.Fatalf("unexpected response: type=%q body=%q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "private, no-cache" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing cache/nosniff headers: %v", rec.Header())
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/avatar/"+hash+"?s=64&i=CE", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
}

func TestHandleGetAvatarCachedNotFoundServesSVG(t *testing.T) {
	srv := newSetupTestServer(t)
	hash := avatar.HashEmail("missing@example.com")
	if err := avatar.Put(context.Background(), srv.db, avatar.Row{EmailHash: hash, Status: avatar.StatusNotFound}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/avatar/"+hash+"?s=64&i=ME", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/svg+xml; charset=utf-8" || !strings.Contains(rec.Body.String(), ">ME<") {
		t.Fatalf("unexpected SVG response: type=%q body=%q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestHandleGetAvatarKnownMissFetchesAndPersists(t *testing.T) {
	srv := newSetupTestServer(t)
	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `INSERT INTO memento_person (id, canonical_name, primary_email) VALUES (1, 'Known Person', 'known@example.com')`); err != nil {
		t.Fatal(err)
	}
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpg"))
	}))
	defer upstream.Close()
	srv.avatars = avatar.NewService(srv.db, &avatar.Fetcher{BaseURL: upstream.URL, Client: upstream.Client()})
	hash := avatar.HashEmail("known@example.com")
	req := httptest.NewRequest(http.MethodGet, "/api/avatar/"+hash+"?s=64&i=KP", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "jpg" || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("unexpected response: status=%d type=%q body=%q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	row, ok, err := avatar.Get(ctx, srv.db, hash)
	if err != nil || !ok || row.Status != avatar.StatusFound {
		t.Fatalf("row not persisted: row=%+v ok=%v err=%v", row, ok, err)
	}
}

func TestHandleGetAvatarUnknownMissDoesNotFetch(t *testing.T) {
	srv := newSetupTestServer(t)
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	srv.avatars = avatar.NewService(srv.db, &avatar.Fetcher{BaseURL: upstream.URL, Client: upstream.Client()})
	hash := avatar.HashEmail("unknown@example.com")
	req := httptest.NewRequest(http.MethodGet, "/api/avatar/"+hash+"?s=64&i=UE", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), ">UE<") {
		t.Fatalf("unexpected response: status=%d body=%q", rec.Code, rec.Body.String())
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("unknown hash triggered upstream fetch")
	}
	if _, ok, err := avatar.Get(context.Background(), srv.db, hash); err != nil || ok {
		t.Fatalf("unknown hash wrote cache row: ok=%v err=%v", ok, err)
	}
}

func TestHandleGetAvatarTransientErrorDoesNotCache(t *testing.T) {
	srv := newSetupTestServer(t)
	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `INSERT INTO memento_person (id, canonical_name, primary_email) VALUES (1, 'Known Person', 'known@example.com')`); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "later", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	srv.avatars = avatar.NewService(srv.db, &avatar.Fetcher{BaseURL: upstream.URL, Client: upstream.Client()})
	hash := avatar.HashEmail("known@example.com")
	req := httptest.NewRequest(http.MethodGet, "/api/avatar/"+hash+"?s=64&i=KP", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), ">KP<") {
		t.Fatalf("unexpected response: status=%d body=%q", rec.Code, rec.Body.String())
	}
	if _, ok, err := avatar.Get(ctx, srv.db, hash); err != nil || ok {
		t.Fatalf("transient error wrote cache row: ok=%v err=%v", ok, err)
	}
}

func TestHandleGetAvatarConcurrentRequestsSingleFetch(t *testing.T) {
	srv := newSetupTestServer(t)
	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `INSERT INTO memento_person (id, canonical_name, primary_email) VALUES (1, 'Known Person', 'known@example.com')`); err != nil {
		t.Fatal(err)
	}
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	defer upstream.Close()
	srv.avatars = avatar.NewService(srv.db, &avatar.Fetcher{BaseURL: upstream.URL, Client: upstream.Client()})
	hash := avatar.HashEmail("known@example.com")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/avatar/"+hash+"?s=64&i=KP", nil)
			rec := httptest.NewRecorder()
			srv.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
