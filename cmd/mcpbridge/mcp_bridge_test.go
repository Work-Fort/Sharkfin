// SPDX-License-Identifier: AGPL-3.0-or-later
package mcpbridge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadResponseBody_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)

	messages, err := readResponseBody(resp)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":{}}`, string(messages[0]))
}

func TestReadResponseBody_202(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)

	messages, err := readResponseBody(resp)
	require.NoError(t, err)
	require.Nil(t, messages)
}

func TestReadResponseBody_SSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)

	messages, err := readResponseBody(resp)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":{}}`, string(messages[0]))
}

func TestReadResponseBody_SSEMultipleMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(
			"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"progress\"}\n\n" +
				"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n",
		))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)

	messages, err := readResponseBody(resp)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Contains(t, string(messages[0]), "progress")
	require.Contains(t, string(messages[1]), "result")
}
