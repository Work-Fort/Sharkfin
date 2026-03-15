// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUIHealthReturns200(t *testing.T) {
	mux := http.NewServeMux()
	registerUIRoutes(mux, "")

	req := httptest.NewRequest("GET", "/ui/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestUIStaticFileServing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.js"), []byte("console.log('hi')"), 0644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerUIRoutes(mux, dir)

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
	registerUIRoutes(mux, "")

	req := httptest.NewRequest("GET", "/ui/test.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 when uiDir is empty, got %d", rec.Code)
	}
}
