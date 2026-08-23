package lsptrace

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
)

func raw(s string) *json.RawMessage {
	b := json.RawMessage(s)
	return &b
}

func TestRecordsRequestResponseAndNotification(t *testing.T) {
	r := New(10)

	r.RecordRequest(FromClient, &jsonrpc2.Request{
		Method: "initialize",
		ID:     jsonrpc2.ID{Num: 1},
		Params: raw(`{"rootUri":"file:///tmp"}`),
	})
	r.RecordResponse(FromServer, &jsonrpc2.Response{
		ID:     jsonrpc2.ID{Num: 1},
		Result: raw(`{"capabilities":{}}`),
	})
	r.RecordRequest(FromServer, &jsonrpc2.Request{
		Method: "window/showMessage",
		Notif:  true,
		Params: raw(`{"type":3,"message":"hi"}`),
	})
	r.RecordResponse(FromServer, &jsonrpc2.Response{
		ID:    jsonrpc2.ID{Num: 1},
		Error: &jsonrpc2.Error{Code: -32601, Message: "method not supported: bogus"},
	})

	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(r.messages))
	}

	req := r.messages[0]
	if req.Kind != KindRequest || req.Direction != FromClient || req.Method != "initialize" {
		t.Errorf("unexpected request record: %+v", req)
	}
	if string(req.ID) != "1" {
		t.Errorf("expected numeric id 1, got %s", req.ID)
	}
	if string(req.Params) != `{"rootUri":"file:///tmp"}` {
		t.Errorf("params not preserved verbatim: %s", req.Params)
	}

	resp := r.messages[1]
	if resp.Kind != KindResponse || resp.Direction != FromServer || string(resp.Result) != `{"capabilities":{}}` {
		t.Errorf("unexpected response record: %+v", resp)
	}

	notif := r.messages[2]
	if notif.Kind != KindNotification || notif.Direction != FromServer || notif.ID != nil {
		t.Errorf("notification should have no id: %+v", notif)
	}

	errResp := r.messages[3]
	if string(errResp.Error) == "" {
		t.Errorf("expected error payload, got none")
	}
	var e jsonrpc2.Error
	if err := json.Unmarshal(errResp.Error, &e); err != nil {
		t.Fatalf("error payload is not a jsonrpc2 error: %v", err)
	}
	if e.Code != -32601 {
		t.Errorf("expected code -32601, got %d", e.Code)
	}
}

func TestStringIDPreserved(t *testing.T) {
	r := New(10)
	r.RecordRequest(FromClient, &jsonrpc2.Request{
		Method: "custom/method",
		ID:     jsonrpc2.ID{Str: "abc", IsString: true},
	})
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(r.messages))
	}
	if string(r.messages[0].ID) != `"abc"` {
		t.Errorf("expected string id \"abc\", got %s", r.messages[0].ID)
	}
}

func TestRingBufferDropsOldest(t *testing.T) {
	r := New(3)
	for i := range 5 {
		r.RecordRequest(FromClient, &jsonrpc2.Request{Method: fmt.Sprintf("m%d", i)})
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.messages) != 3 {
		t.Fatalf("expected 3 retained messages, got %d", len(r.messages))
	}
	if r.messages[0].Method != "m2" {
		t.Errorf("expected oldest retained to be m2, got %s", r.messages[0].Method)
	}
	if r.messages[2].Method != "m4" {
		t.Errorf("expected newest to be m4, got %s", r.messages[2].Method)
	}
	if r.seq != 5 {
		t.Errorf("expected seq 5, got %d", r.seq)
	}
}

func TestAPIReturnsMessagesAndHonorsSince(t *testing.T) {
	r := New(10)
	if err := r.Serve("127.0.0.1:0"); err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer r.srv.Close()

	for i := range 3 {
		r.RecordRequest(FromClient, &jsonrpc2.Request{Method: fmt.Sprintf("m%d", i)})
	}

	var first struct {
		Seq      uint64    `json:"seq"`
		Messages []Message `json:"messages"`
	}
	getJSON(t, "http://"+r.Addr()+"/api/messages", &first)
	if first.Seq != 3 || len(first.Messages) != 3 {
		t.Fatalf("expected seq 3 and 3 messages, got seq %d and %d messages", first.Seq, len(first.Messages))
	}

	// A message arriving after the first fetch shows up only with since=.
	r.RecordRequest(FromClient, &jsonrpc2.Request{Method: "m3"})
	var second struct {
		Seq      uint64    `json:"seq"`
		Messages []Message `json:"messages"`
	}
	getJSON(t, fmt.Sprintf("http://%s/api/messages?since=%d", r.Addr(), first.Seq), &second)
	if second.Seq != 4 || len(second.Messages) != 1 || second.Messages[0].Method != "m3" {
		t.Fatalf("expected only m3 after since=%d, got %+v", first.Seq, second)
	}

	// Clearing drops messages but keeps seq increasing.
	resp, err := http.NewRequest(http.MethodDelete, "http://"+r.Addr()+"/api/messages", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 on clear, got %d", res.StatusCode)
	}
	var cleared struct {
		Seq      uint64    `json:"seq"`
		Messages []Message `json:"messages"`
	}
	getJSON(t, "http://"+r.Addr()+"/api/messages", &cleared)
	if len(cleared.Messages) != 0 {
		t.Errorf("expected no messages after clear, got %d", len(cleared.Messages))
	}
	if cleared.Seq != 4 {
		t.Errorf("expected seq to stay 4 after clear, got %d", cleared.Seq)
	}
}

func TestServeIsIdempotent(t *testing.T) {
	r := New(10)
	if err := r.Serve("127.0.0.1:0"); err != nil {
		t.Fatalf("first serve: %v", err)
	}
	addr := r.Addr()
	if err := r.Serve("127.0.0.1:0"); err != nil {
		t.Fatalf("second serve must be a no-op, got error: %v", err)
	}
	if r.Addr() != addr {
		t.Errorf("second Serve changed the address: %s -> %s", addr, r.Addr())
	}
}

func TestServeReturnsBindError(t *testing.T) {
	r1 := New(10)
	if err := r1.Serve("127.0.0.1:0"); err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer r1.srv.Close()

	r2 := New(10)
	if err := r2.Serve(r1.Addr()); err == nil {
		t.Errorf("expected bind error on a busy address, got nil")
	}
	if r2.Addr() != "" {
		t.Errorf("failed Serve must not mark the recorder started, got addr %q", r2.Addr())
	}
}

func TestUIServedAtRoot(t *testing.T) {
	r := New(10)
	if err := r.Serve("127.0.0.1:0"); err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer r.srv.Close()

	res, err := http.Get("http://" + r.Addr() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("expected html content type, got %q", ct)
	}

	// The inline script must keep its core functions; a broken UI is a
	// regression even though Go cannot see inside the HTML.
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{
		"function buildItems()",
		"function render()",
		"function poll()",
		"fetch(\"/api/messages?since=\"",
	} {
		if !strings.Contains(string(body), fn) {
			t.Errorf("embedded UI missing %q", fn)
		}
	}
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d", url, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}
