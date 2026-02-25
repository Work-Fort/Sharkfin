// SPDX-License-Identifier: GPL-2.0-only
package protocol

import (
	"encoding/json"
	"fmt"
)

// Standard JSON-RPC 2.0 error codes.
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// RequestID can be a string or an integer per JSON-RPC 2.0.
type RequestID struct {
	IsString    bool
	StringValue string
	IntValue    int64
}

func (id *RequestID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		id.IsString = true
		id.StringValue = s
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		id.IsString = false
		id.IntValue = n
		return nil
	}
	return fmt.Errorf("request id must be string or integer, got %s", string(data))
}

func (id RequestID) MarshalJSON() ([]byte, error) {
	if id.IsString {
		return json.Marshal(id.StringValue)
	}
	return json.Marshal(id.IntValue)
}

// Request is a JSON-RPC 2.0 request. ID is nil for notifications.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      *RequestID      `json:"id,omitempty"`
}

// IsNotification returns true if the request has no ID (a notification).
func (r *Request) IsNotification() bool {
	return r.ID == nil
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	ID      *RequestID      `json:"id,omitempty"`
}

// Error is a JSON-RPC 2.0 error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// NewResponse creates a successful JSON-RPC 2.0 response.
func NewResponse(id *RequestID, result json.RawMessage) Response {
	return Response{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
}

// NewErrorResponse creates an error JSON-RPC 2.0 response.
func NewErrorResponse(id *RequestID, code int, message string) Response {
	return Response{
		JSONRPC: "2.0",
		Error: &Error{
			Code:    code,
			Message: message,
		},
		ID: id,
	}
}
