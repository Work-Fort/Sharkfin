// SPDX-License-Identifier: GPL-2.0-only
package protocol

import (
	"encoding/json"
	"testing"
)

func TestRequestWithStringID(t *testing.T) {
	raw := `{"jsonrpc":"2.0","method":"tools/list","id":"abc-123"}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Method != "tools/list" {
		t.Errorf("method = %q, want %q", req.Method, "tools/list")
	}
	if req.ID == nil || req.ID.StringValue != "abc-123" {
		t.Errorf("id = %v, want string abc-123", req.ID)
	}

	out, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundtrip Request
	if err := json.Unmarshal(out, &roundtrip); err != nil {
		t.Fatalf("roundtrip unmarshal: %v", err)
	}
	if roundtrip.ID.StringValue != "abc-123" {
		t.Errorf("roundtrip id = %v, want abc-123", roundtrip.ID)
	}
}

func TestRequestWithIntID(t *testing.T) {
	raw := `{"jsonrpc":"2.0","method":"initialize","params":{},"id":1}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ID == nil || req.ID.IntValue != 1 || req.ID.IsString {
		t.Errorf("id = %v, want int 1", req.ID)
	}
}

func TestNotificationHasNoID(t *testing.T) {
	raw := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ID != nil {
		t.Errorf("notification should have nil ID, got %v", req.ID)
	}
}

func TestSuccessResponse(t *testing.T) {
	result := json.RawMessage(`{"name":"sharkfin"}`)
	resp := NewResponse(&RequestID{IntValue: 1}, result)

	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed Response
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", parsed.JSONRPC)
	}
	if parsed.Error != nil {
		t.Errorf("error should be nil, got %v", parsed.Error)
	}
	if string(parsed.Result) != `{"name":"sharkfin"}` {
		t.Errorf("result = %s, want {\"name\":\"sharkfin\"}", parsed.Result)
	}
}

func TestErrorResponse(t *testing.T) {
	resp := NewErrorResponse(&RequestID{IsString: true, StringValue: "req-1"}, MethodNotFound, "unknown method")

	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed Response
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if parsed.Error.Code != MethodNotFound {
		t.Errorf("code = %d, want %d", parsed.Error.Code, MethodNotFound)
	}
	if parsed.Error.Message != "unknown method" {
		t.Errorf("message = %q, want %q", parsed.Error.Message, "unknown method")
	}
	if parsed.ID == nil || parsed.ID.StringValue != "req-1" {
		t.Errorf("id = %v, want req-1", parsed.ID)
	}
}

func TestBatchRequest(t *testing.T) {
	raw := `[{"jsonrpc":"2.0","method":"a","id":1},{"jsonrpc":"2.0","method":"b","id":2}]`
	var batch []Request
	if err := json.Unmarshal([]byte(raw), &batch); err != nil {
		t.Fatalf("unmarshal batch: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("len = %d, want 2", len(batch))
	}
	if batch[0].Method != "a" {
		t.Errorf("batch[0].method = %q, want a", batch[0].Method)
	}
	if batch[1].Method != "b" {
		t.Errorf("batch[1].method = %q, want b", batch[1].Method)
	}
}
