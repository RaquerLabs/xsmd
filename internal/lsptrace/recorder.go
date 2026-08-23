// Package lsptrace records every JSON-RPC message exchanged between the LSP
// server and its client and serves a debug web UI for inspecting them.
//
// The recorder only becomes reachable when the caller enables it: it always
// stores messages, but the HTTP server binds its port only after Serve is
// called. The LSP server wires this to debug mode, so the web UI is never
// reachable unless debug = true.
package lsptrace

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

// DefaultAddr is the address the debug web UI listens on.
const DefaultAddr = "127.0.0.1:8666"

// DefaultMaxMessages caps how many messages the recorder keeps in memory.
// Oldest messages are dropped first.
const DefaultMaxMessages = 2000

// Direction names the origin of a traced message, relative to the LSP server.
type Direction string

const (
	// FromClient marks a message received from the LSP client.
	FromClient Direction = "client"
	// FromServer marks a message sent by the LSP server.
	FromServer Direction = "server"
)

// Kind classifies a traced message.
type Kind string

const (
	KindRequest      Kind = "request"
	KindNotification Kind = "notification"
	KindResponse     Kind = "response"
)

// Message is one JSON-RPC message exchanged between the LSP client and server.
// Payloads stay raw JSON so the web UI can pretty-print them verbatim.
type Message struct {
	Seq       uint64          `json:"seq"`
	Time      time.Time       `json:"time"`
	Direction Direction       `json:"direction"`
	Kind      Kind            `json:"kind"`
	ID        json.RawMessage `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

// Recorder keeps a bounded, in-memory history of LSP messages and serves it
// over HTTP. All methods are safe for concurrent use.
type Recorder struct {
	mu       sync.RWMutex
	messages []Message
	seq      uint64
	max      int
	addr     string
	srv      *http.Server
	started  bool
}

// New creates a Recorder that keeps up to maxMessages messages. Values <= 0
// fall back to DefaultMaxMessages. No HTTP server starts until Serve is called.
func New(maxMessages int) *Recorder {
	if maxMessages <= 0 {
		maxMessages = DefaultMaxMessages
	}
	return &Recorder{max: maxMessages}
}

// RecordRequest traces a request or notification received from or sent to the
// peer. req.Notif decides whether the message is a notification.
func (r *Recorder) RecordRequest(dir Direction, req *jsonrpc2.Request) {
	m := Message{Direction: dir, Method: req.Method, Params: cloneRaw(req.Params)}
	if req.Notif {
		m.Kind = KindNotification
	} else {
		m.Kind = KindRequest
		m.ID = rawOf(req.ID)
	}
	r.append(m)
}

// RecordResponse traces a response received from or sent to the peer.
func (r *Recorder) RecordResponse(dir Direction, resp *jsonrpc2.Response) {
	m := Message{
		Direction: dir,
		Kind:      KindResponse,
		ID:        rawOf(resp.ID),
		Result:    cloneRaw(resp.Result),
	}
	if resp.Error != nil {
		if b, err := json.Marshal(resp.Error); err == nil {
			m.Error = b
		}
	}
	r.append(m)
}

// Serve starts the HTTP server on addr, serving the web UI and message API.
// It is idempotent: later calls are no-ops, so repeated debug-change events
// cannot double-bind the port. A bind failure is returned to the caller.
func (r *Recorder) Serve(addr string) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/messages", r.handleMessages)
	mux.HandleFunc("/", r.handleIndex)

	srv := &http.Server{Handler: mux}
	r.mu.Lock()
	r.started = true
	r.addr = ln.Addr().String()
	r.srv = srv
	r.mu.Unlock()

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("lsptrace: debug UI failed: %v", err)
		}
	}()
	return nil
}

// Addr returns the bound address once Serve has started, or "" otherwise.
func (r *Recorder) Addr() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.addr
}

// Clear drops every recorded message. Sequence numbers keep increasing so
// clients polling with ?since= stay consistent.
func (r *Recorder) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = r.messages[:0]
}

func (r *Recorder) append(m Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	m.Seq = r.seq
	m.Time = time.Now()
	if len(r.messages) == r.max {
		copy(r.messages, r.messages[1:])
		r.messages[len(r.messages)-1] = m
		return
	}
	r.messages = append(r.messages, m)
}

func (r *Recorder) handleMessages(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		since, _ := strconv.ParseUint(req.URL.Query().Get("since"), 10, 64)
		r.mu.RLock()
		seq := r.seq
		msgs := make([]Message, 0, len(r.messages))
		for _, m := range r.messages {
			if m.Seq > since {
				msgs = append(msgs, m)
			}
		}
		r.mu.RUnlock()
		writeJSON(w, struct {
			Seq      uint64    `json:"seq"`
			Messages []Message `json:"messages"`
		}{seq, msgs})

	case http.MethodDelete:
		r.Clear()
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *Recorder) handleIndex(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(uiHTML)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("lsptrace: encode response: %v", err)
	}
}

func cloneRaw(m *json.RawMessage) json.RawMessage {
	if m == nil {
		return nil
	}
	return append(json.RawMessage(nil), (*m)...)
}

// rawOf renders a JSON-RPC ID as its original JSON form (number or string).
func rawOf(id jsonrpc2.ID) json.RawMessage {
	if b, err := json.Marshal(id); err == nil {
		return b
	}
	return nil
}
