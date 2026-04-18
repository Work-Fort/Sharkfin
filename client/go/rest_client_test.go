// SPDX-License-Identifier: Apache-2.0
package client

import (
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
