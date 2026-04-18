// SPDX-License-Identifier: Apache-2.0
package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewRESTClient_NoDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	if c == nil {
		t.Fatal("NewRESTClient returned nil")
	}
	// Constructor must not open a WS connection — nothing to close
	// beyond its own http.Client transport. Calling Close is a no-op
	// but must not panic.
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestRESTClientRegister(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", got)
		}
		w.WriteHeader(http.StatusCreated)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	if err := c.Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !called {
		t.Error("expected POST /api/v1/auth/register to be called")
	}
}
