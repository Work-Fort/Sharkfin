// SPDX-License-Identifier: Apache-2.0
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// restTransport is the shared HTTP transport used by both the WS-backed
// *Client and the REST-only *RESTClient. It owns the base URL, HTTP
// client, and auth credentials, and performs authenticated JSON
// request/response round trips.
type restTransport struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

// do performs an authenticated JSON request. If reqBody is non-nil it
// is marshaled as JSON. If out is non-nil and the response has a body,
// the body is decoded into out. Returns the HTTP status code and any
// error. On 4xx/5xx, returns a *ServerError that wraps a sentinel
// (ErrNotFound, ErrConflict, ErrBadRequest, ErrUnauthorized) when the
// status maps to one.
func (t *restTransport) do(ctx context.Context, method, path string, reqBody, out any) (int, error) {
	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return 0, fmt.Errorf("client: marshal: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, t.baseURL+path, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("client: new request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t.apiKey != "" {
		req.Header.Set("Authorization", "ApiKey-v1 "+t.apiKey)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("client: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(b))
		serr := &ServerError{Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, msg), Status: resp.StatusCode}
		switch resp.StatusCode {
		case http.StatusBadRequest:
			serr.wrapped = ErrBadRequest
		case http.StatusUnauthorized:
			serr.wrapped = ErrUnauthorized
		case http.StatusNotFound:
			serr.wrapped = ErrNotFound
		case http.StatusConflict:
			serr.wrapped = ErrConflict
		}
		return resp.StatusCode, serr
	}

	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("client: decode response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// deriveBaseURL converts a ws(s):// URL into the corresponding
// http(s):// base, trimming any trailing "/ws". Exported indirectly
// through the constructors.
func deriveBaseURL(wsURL string) string {
	base := strings.TrimSuffix(wsURL, "/ws")
	base = strings.TrimSuffix(base, "/")
	switch {
	case strings.HasPrefix(base, "ws://"):
		return "http://" + base[len("ws://"):]
	case strings.HasPrefix(base, "wss://"):
		return "https://" + base[len("wss://"):]
	}
	return base
}
