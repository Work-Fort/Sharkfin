// SPDX-License-Identifier: Apache-2.0
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// --- Identity ---

// Register registers the calling identity as a service bot.
func (c *RESTClient) Register(ctx context.Context) error {
	_, err := c.transport.do(ctx, http.MethodPost, "/api/v1/auth/register", nil, nil)
	return err
}

// --- Channels ---

// Channels returns all channels visible to the current user.
// The REST endpoint shape matches the shared Channel type's JSON tags
// ({name, public, member}); the server also includes an {id} field
// which the client ignores.
func (c *RESTClient) Channels(ctx context.Context) ([]Channel, error) {
	var out []Channel
	if _, err := c.transport.do(ctx, http.MethodGet, "/api/v1/channels", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateChannel creates a new channel.
func (c *RESTClient) CreateChannel(ctx context.Context, name string, public bool) error {
	body := map[string]any{"name": name, "public": public}
	_, err := c.transport.do(ctx, http.MethodPost, "/api/v1/channels", body, nil)
	return err
}

// JoinChannel joins a public channel by name.
func (c *RESTClient) JoinChannel(ctx context.Context, channel string) error {
	path := "/api/v1/channels/" + url.PathEscape(channel) + "/join"
	_, err := c.transport.do(ctx, http.MethodPost, path, nil, nil)
	return err
}

// --- Messages ---

// SendMessage sends a message to a channel. Returns the message ID
// assigned by the server.
//
// SendOpts.Metadata is a JSON-encoded string matching the WS path's
// type. The REST endpoint expects a JSON object, not a string, so the
// metadata is parsed into a map before sending. Invalid JSON returns
// an error without contacting the server.
func (c *RESTClient) SendMessage(ctx context.Context, channel, body string, opts *SendOpts) (int64, error) {
	reqBody := map[string]any{"body": body}
	if opts != nil {
		if opts.ThreadID != nil {
			reqBody["thread_id"] = *opts.ThreadID
		}
		if opts.Metadata != nil {
			var m map[string]any
			if err := json.Unmarshal([]byte(*opts.Metadata), &m); err != nil {
				return 0, fmt.Errorf("client: metadata is not valid JSON: %w", err)
			}
			reqBody["metadata"] = m
		}
	}
	var out struct {
		ID int64 `json:"id"`
	}
	path := "/api/v1/channels/" + url.PathEscape(channel) + "/messages"
	if _, err := c.transport.do(ctx, http.MethodPost, path, reqBody, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

// ListMessages retrieves messages from a channel, subject to
// optional before/after/limit pagination.
func (c *RESTClient) ListMessages(ctx context.Context, channel string, opts *HistoryOpts) ([]Message, error) {
	q := url.Values{}
	if opts != nil {
		if opts.Before != nil {
			q.Set("before", strconv.FormatInt(*opts.Before, 10))
		}
		if opts.After != nil {
			q.Set("after", strconv.FormatInt(*opts.After, 10))
		}
		if opts.Limit != nil {
			q.Set("limit", strconv.Itoa(*opts.Limit))
		}
	}
	path := "/api/v1/channels/" + url.PathEscape(channel) + "/messages"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out []Message
	if _, err := c.transport.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// --- Webhooks ---

// RegisterWebhook registers a webhook URL for the calling identity.
// Returns the webhook ID.
func (c *RESTClient) RegisterWebhook(ctx context.Context, hookURL string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if _, err := c.transport.do(ctx, http.MethodPost, "/api/v1/webhooks", map[string]string{"url": hookURL}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// ListWebhooks returns all active webhooks for the calling identity.
func (c *RESTClient) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	var out []Webhook
	if _, err := c.transport.do(ctx, http.MethodGet, "/api/v1/webhooks", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UnregisterWebhook removes a registered webhook by ID.
func (c *RESTClient) UnregisterWebhook(ctx context.Context, id string) error {
	path := "/api/v1/webhooks/" + url.PathEscape(id)
	_, err := c.transport.do(ctx, http.MethodDelete, path, nil, nil)
	return err
}
