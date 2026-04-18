// SPDX-License-Identifier: Apache-2.0
package client

import (
	"net/http"
	"time"
)

// RESTClient is a stateless HTTP-only client for the Sharkfin server.
// Unlike *Client it does not open a WebSocket connection and does not
// receive server-pushed events. Consumers that receive events via a
// registered webhook instead of the WS event stream should use this
// type — it has no background goroutines, no reconnection state, and
// no Dial step.
type RESTClient struct {
	transport restTransport
}

// NewRESTClient constructs a REST-only Sharkfin client. baseURL must
// point at the server root (e.g. "http://localhost:16000"), not at a
// sub-path — REST method paths like "/api/v1/channels" are appended
// directly. A WebSocket URL such as "ws://localhost:16000/ws" is also
// accepted: the scheme is rewritten to http(s) and a trailing "/ws"
// is trimmed. Authentication is provided via WithToken or WithAPIKey;
// other Options (WithDialer, WithReconnect) are accepted for
// signature compatibility but are ignored because there is no WS
// connection.
func NewRESTClient(baseURL string, opts ...Option) *RESTClient {
	o := clientOpts{}
	for _, opt := range opts {
		opt(&o)
	}
	return &RESTClient{
		transport: restTransport{
			baseURL:    deriveBaseURL(baseURL),
			httpClient: &http.Client{Timeout: 30 * time.Second},
			token:      o.token,
			apiKey:     o.apiKey,
		},
	}
}

// Close releases any resources held by the client. Currently a no-op
// because the underlying http.Client does not require explicit
// cleanup. Provided so callers can write symmetric setup/teardown
// code.
func (c *RESTClient) Close() error {
	return nil
}
