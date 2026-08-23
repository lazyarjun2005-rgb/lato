package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lato/internal/tools"
)

// sse writes one server-sent event line.
func sse(w http.ResponseWriter, payload string) {
	w.Write([]byte("data: " + payload + "\n\n"))
}

// chatCompletionsPath is where every OpenAI-compatible provider POSTs.
const chatCompletionsPath = "/chat/completions"

func newOpenAICompatible(t *testing.T, url, key string) *NvidiaProvider {
	t.Helper()
	return NewOpenAICompatible(url, "test-model", key, nil)
}

// TestOpenAICompatibleListModels verifies GET {endpoint}/models parses
// standard responses and keeps model IDs opaque (slashes and colons
// intact).
func TestOpenAICompatibleListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-key" {
			t.Errorf("Authorization = %q, want bearer key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "vendor/model"},
				{"id": "cc/glm-4:variant", "context_length": 128000},
			},
		})
	}))
	defer server.Close()

	models, err := newOpenAICompatible(t, server.URL, "secret-key").ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	want := []string{"vendor/model", "cc/glm-4:variant"}
	if len(models) != len(want) {
		t.Fatalf("got %d models, want %d", len(models), len(want))
	}
	for i, id := range want {
		if models[i].ID != id || models[i].Name != id {
			t.Errorf("model %d = %+v, want opaque ID %q", i, models[i], id)
		}
	}
}

// TestOpenAICompatibleStreamTextAndDone covers plain content deltas and
// the data: [DONE] sentinel.
func TestOpenAICompatibleStreamTextAndDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{"choices":[{"delta":{"content":"Hello"}}]}`)
		sse(w, `{"choices":[{"delta":{"content":" world"}}]}`)
		sse(w, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	events := collectEvents(t, newOpenAICompatible(t, server.URL, ""), nil)
	want := []struct {
		text string
		done bool
	}{{"Hello", false}, {" world", false}, {"", true}}
	if len(events) != len(want) {
		t.Fatalf("received %d events (%+v), want %d", len(events), events, len(want))
	}
	for i, w := range want {
		if events[i].Err != nil {
			t.Fatalf("event %d error = %v", i, events[i].Err)
		}
		if events[i].Text != w.text || events[i].Done != w.done {
			t.Errorf("event %d = {Text:%q Done:%v}, want {Text:%q Done:%v}", i, events[i].Text, events[i].Done, w.text, w.done)
		}
	}
}

// TestOpenAICompatibleStreamedToolCallFragments pins the critical
// contract: argument fragments arriving across several SSE chunks are
// buffered and assembled into one executable providers.ToolCall.
func TestOpenAICompatibleStreamedToolCallFragments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"ec","arguments":""}}]}}]}`)
		sse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"ho","arguments":""}}]}}]}`)
		sse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"va"}}]}}]}`)
		sse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"lue\":\"hi\"}"}}]}}]}`)
		sse(w, `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	events := collectEvents(t, newOpenAICompatible(t, server.URL, ""), []tools.Definition{{
		Name: "echo", Description: "Echoes.", InputSchema: map[string]any{"type": "object"},
	}})

	var calls []ToolCall
	done := false
	for _, ev := range events {
		if ev.Err != nil {
			t.Fatalf("stream error = %v", ev.Err)
		}
		calls = append(calls, ev.ToolCalls...)
		done = done || ev.Done
	}
	if !done {
		t.Fatal("stream never reported done")
	}
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	call := calls[0]
	if call.ID != "call-1" || call.Name != "echo" {
		t.Errorf("tool call = %+v, want ID call-1 name echo", call)
	}
	if call.Arguments["value"] != "hi" {
		t.Errorf("arguments = %#v, want parsed {\"value\":\"hi\"}", call.Arguments)
	}
}

// TestOpenAICompatibleMultipleToolCalls verifies parallel calls arrive
// separately, keyed by their stream index.
func TestOpenAICompatibleMultipleToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{"choices":[{"delta":{"tool_calls":[`+
			`{"index":0,"id":"call-a","function":{"name":"first","arguments":"{\"n\":1}"}},`+
			`{"index":1,"id":"call-b","function":{"name":"second","arguments":""}}]}}]}`)
		sse(w, `{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"n\":2}"}}]}}]}`)
		sse(w, `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	events := collectEvents(t, newOpenAICompatible(t, server.URL, ""), nil)

	var calls []ToolCall
	for _, ev := range events {
		calls = append(calls, ev.ToolCalls...)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(calls))
	}
	if calls[0].Name != "first" || calls[0].Arguments["n"] != float64(1) {
		t.Errorf("call 0 = %+v, want first {\"n\":1}", calls[0])
	}
	if calls[1].Name != "second" || calls[1].Arguments["n"] != float64(2) {
		t.Errorf("call 1 = %+v, want second {\"n\":2}", calls[1])
	}
}

// TestOpenAICompatibleMalformedSSE verifies malformed stream payloads
// surface as a stream error instead of crashing or hanging.
func TestOpenAICompatibleMalformedSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{not json at all`)
	}))
	defer server.Close()

	events := collectEvents(t, newOpenAICompatible(t, server.URL, ""), nil)
	if len(events) == 0 || events[len(events)-1].Err == nil {
		t.Fatalf("events = %+v, want a terminal stream error", events)
	}
}

// TestOpenAICompatibleHTTPError verifies non-200 responses produce a
// clear error that does NOT contain the API key.
func TestOpenAICompatibleHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	p := newOpenAICompatible(t, server.URL, "super-secret-key-value")
	_, err := p.StreamChat(context.Background(), []Message{{Role: UserRole, Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected an error for HTTP 401, got none")
	}
	if msg := err.Error(); !strings.Contains(msg, "401") {
		t.Errorf("error %q does not mention status 401", msg)
	}
	if strings.Contains(err.Error(), "super-secret-key-value") {
		t.Error("error message leaked the API key")
	}
}

// TestOpenAICompatibleProviderErrorChunk verifies an in-stream error
// object terminates the stream with an error event.
func TestOpenAICompatibleProviderErrorChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{"error":{"message":"model not found"}}`)
	}))
	defer server.Close()

	events := collectEvents(t, newOpenAICompatible(t, server.URL, ""), nil)
	found := false
	for _, ev := range events {
		if ev.Err != nil && strings.Contains(ev.Err.Error(), "model not found") {
			found = true
		}
	}
	if !found {
		t.Fatalf("events = %+v, want an error mentioning the provider message", events)
	}
}

// TestOpenAICompatibleUnavailableEndpoint verifies connection failures
// produce a wrapped, clear error rather than a panic.
func TestOpenAICompatibleUnavailableEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close() // nothing is listening anymore

	p := newOpenAICompatible(t, url, "")
	if _, err := p.StreamChat(context.Background(), []Message{{Role: UserRole, Content: "hi"}}, nil); err == nil {
		t.Fatal("expected a dial error for a closed endpoint")
	}
}

// collectEvents runs one StreamChat call against the given provider and
// drains the resulting channel.
func collectEvents(t *testing.T, p *NvidiaProvider, defs []tools.Definition) []StreamEvent {
	t.Helper()
	stream, err := p.StreamChat(context.Background(), []Message{{Role: UserRole, Content: "test"}}, defs)
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	var out []StreamEvent
	for ev := range stream {
		out = append(out, ev)
	}
	return out
}

// TestOpenAICompatibleRequestShape verifies auth header, model, tools,
// and streaming flag as sent to the endpoint.
func TestOpenAICompatibleRequestShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != chatCompletionsPath {
			t.Errorf("path = %q, want %s", r.URL.Path, chatCompletionsPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q, want Bearer sk-test", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
			Tools  []struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "vendor/model:variant" || !req.Stream {
			t.Errorf("request model=%q stream=%v, want opaque model and streaming", req.Model, req.Stream)
		}
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "search_repo" || req.Tools[0].Type != "function" {
			t.Errorf("tools = %#v, want one function definition", req.Tools)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	defs := []tools.Definition{{
		Name:        "search_repo",
		Description: "Search.",
		InputSchema: map[string]any{"type": "object"},
	}}
	p := newOpenAICompatible(t, server.URL, "sk-test")
	p.Model = "vendor/model:variant"
	for ev := range mustStream(t, p, defs) {
		if ev.Err != nil {
			t.Fatalf("stream error = %v", ev.Err)
		}
	}
}

func mustStream(t *testing.T, p *NvidiaProvider, defs []tools.Definition) <-chan StreamEvent {
	t.Helper()
	stream, err := p.StreamChat(context.Background(), []Message{{Role: UserRole, Content: "go"}}, defs)
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	return stream
}
