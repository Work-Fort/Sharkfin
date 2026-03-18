// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestUIHealthReturnsManifest(t *testing.T) {
	mux := http.NewServeMux()
	registerUIRoutes(mux, "", nil)

	req := httptest.NewRequest("GET", "/ui/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var health uiHealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&health); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if health.Name != "sharkfin" {
		t.Fatalf("expected name=sharkfin, got %s", health.Name)
	}
	if health.Label != "Chat" {
		t.Fatalf("expected label=Chat, got %s", health.Label)
	}
	if health.Route != "/chat" {
		t.Fatalf("expected route=/chat, got %s", health.Route)
	}
	if len(health.WSPaths) != 2 || health.WSPaths[0] != "/ws" || health.WSPaths[1] != "/presence" {
		t.Fatalf("expected ws_paths=[/ws, /presence], got %v", health.WSPaths)
	}
	if health.NotificationPath != "/notifications/subscribe" {
		t.Fatalf("expected notification_path=/notifications/subscribe, got %s", health.NotificationPath)
	}
}

func TestUIStaticFileServing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.js"), []byte("console.log('hi')"), 0644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerUIRoutes(mux, dir, nil)

	req := httptest.NewRequest("GET", "/ui/test.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "console.log('hi')" {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestUINoStaticWhenDirEmpty(t *testing.T) {
	mux := http.NewServeMux()
	registerUIRoutes(mux, "", nil)

	req := httptest.NewRequest("GET", "/ui/test.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 when uiDir is empty, got %d", rec.Code)
	}
}

func TestUIEmbeddedFS(t *testing.T) {
	fsys := fstest.MapFS{
		"dist/test.js": &fstest.MapFile{Data: []byte("embedded")},
	}

	mux := http.NewServeMux()
	registerUIRoutes(mux, "", fsys)

	req := httptest.NewRequest("GET", "/ui/test.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "embedded" {
		t.Fatalf("unexpected body: %s", body)
	}
}
