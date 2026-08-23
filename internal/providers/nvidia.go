package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"lato/internal/tools"
	"net/http"
	"strings"
)

// defaultNvidiaTimeout bounds how long a single request to NVIDIA NIM may
// take before it's aborted.
const defaultNvidiaTimeout = 0

// NvidiaProvider is Lato's generic OpenAI-compatible client: it speaks
// the standard Chat Completions API (SSE streaming, GET /models, Bearer
// auth) against any endpoint with that shape. NVIDIA NIM, LM Studio,
// OpenRouter, 9Router, and OmniRoute all use this one implementation.
type NvidiaProvider struct {
	Endpoint string
	Model    string
	APIKey   string
	client   *http.Client

	// effortMechanism/effortToken carry a provider-side effort setting
	// resolved through EffortCapabilityFor. Empty mechanism means no
	// effort field is ever added to requests — the default for every
	// provider that has not declared support.
	effortMechanism string
	effortToken     string
}

// EffortAware is implemented by providers that can carry an effort
// setting in their requests. The runtime applies it only after the
// capability layer resolved a real mechanism; implementations must not
// invent fallbacks for unknown mechanisms.
type EffortAware interface {
	ApplyEffort(mechanism, token string)
}

// ApplyEffort configures provider-side effort for subsequent requests.
// An empty mechanism clears any previous setting.
func (n *NvidiaProvider) ApplyEffort(mechanism, token string) {
	n.effortMechanism = mechanism
	n.effortToken = token
}

// EffortSetting reports the configured mechanism and token. Read-only:
// it exists for diagnostics and tests.
func (n *NvidiaProvider) EffortSetting() (string, string) {
	return n.effortMechanism, n.effortToken
}

// NewOpenAICompatible builds a provider for any endpoint implementing
// the standard OpenAI Chat Completions API at the given base URL (e.g.
// "https://openrouter.ai/api/v1"). This is the canonical constructor;
// NewNvidiaProvider delegates to it. If client is nil, a default client
// is used.
func NewOpenAICompatible(endpoint, model, apiKey string, client *http.Client) *NvidiaProvider {
	if client == nil {
		client = &http.Client{Timeout: defaultNvidiaTimeout}
	}
	return &NvidiaProvider{
		Endpoint: strings.TrimRight(endpoint, "/"),
		Model:    model,
		APIKey:   apiKey,
		client:   client,
	}
}

// NewNvidiaProvider builds the provider pointed at NVIDIA NIM and is
// kept as a compatibility wrapper around NewOpenAICompatible for
// existing callers and configs.
func NewNvidiaProvider(endpoint, model, apiKey string, client *http.Client) *NvidiaProvider {
	return NewOpenAICompatible(endpoint, model, apiKey, client)
}

// nvidiaModelsResponse is the subset of the OpenAI-compatible GET
// /models response we need: the list of available model IDs.
type nvidiaModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// ListModels queries the live endpoint's GET /models list (OpenAI shape,
// shared by NVIDIA NIM, LM Studio, OpenRouter, 9Router, and OmniRoute)
// and returns every available model. Model IDs are treated as opaque
// strings — they may contain "/" or ":" (e.g. "vendor/model:variant")
// and are never split or rewritten.
func (n *NvidiaProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	url := n.Endpoint + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request to %s: %w", url, err)
	}
	if n.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+n.APIKey)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", n.Endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s returned status %d listing models: %s", n.Endpoint, resp.StatusCode, string(body))
	}

	var list nvidiaModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode model list: %w", err)
	}

	models := make([]ModelInfo, 0, len(list.Data))
	for _, m := range list.Data {
		models = append(models, ModelInfo{Name: m.ID, ID: m.ID})
	}
	return models, nil
}

// nvidiaMessage mirrors the shape the OpenAI-compatible chat API expects
// per message.
type nvidiaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`

	ToolCalls []nvidiaToolCall `json:"tool_calls,omitempty"`

	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

type nvidiaTool struct {
	Type     string         `json:"type"`
	Function nvidiaFunction `json:"function"`
}

type nvidiaFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// nvidiaToolCall is a complete (non-streamed) tool call, used when sending
// prior assistant messages back to the API.
type nvidiaToolCall struct {
	ID       string             `json:"id"`
	Function nvidiaFunctionCall `json:"function"`
}

type nvidiaFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// nvidiaChatRequest is the request body for POST /chat/completions.
// The effort fields are only populated when the active provider's
// capability entry declared the matching mechanism; unsupported
// providers never see these keys on the wire.
type nvidiaChatRequest struct {
	Model    string          `json:"model"`
	Messages []nvidiaMessage `json:"messages"`
	Tools    []nvidiaTool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`

	ReasoningEffort *string          `json:"reasoning_effort,omitempty"`
	Reasoning       *nvidiaReasoning `json:"reasoning,omitempty"`
}

// nvidiaReasoning is the router-normalized effort object
// ("reasoning": {"effort": "..."}).
type nvidiaReasoning struct {
	Effort string `json:"effort,omitempty"`
}

// applyEffortTo populates req's effort fields from the configured
// mechanism, ignoring mechanisms this client does not implement so an
// unknown future mechanism can never leak a stray field upstream.
func (req *nvidiaChatRequest) applyEffortTo(mechanism, token string) {
	switch EffortMechanism(mechanism) {
	case EffortReasoningField:
		t := token
		req.ReasoningEffort = &t
	case EffortReasoningObject:
		if token != "" {
			req.Reasoning = &nvidiaReasoning{Effort: token}
		}
	default:
		// EffortNone or unimplemented: deliberately nothing is sent.
	}
}

// nvidiaToolCallDelta is a partial tool call as it appears in a streamed
// delta. Index identifies which tool call this fragment belongs to;
// ID/Name/Arguments may each be empty and are appended incrementally.
type nvidiaToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// nvidiaStreamChunk is the relevant subset of a streamed
// /chat/completions response chunk (Server-Sent Events, OpenAI shape).
type nvidiaStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string                `json:"content"`
			ToolCalls []nvidiaToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`

	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// nvidiaToolCallBuf accumulates a streamed tool call's fragments (ID, name,
// and raw argument text) until finish_reason confirms it is complete.
type nvidiaToolCallBuf struct {
	ID      string
	Name    string
	ArgsBuf string
}

// StreamChat sends the conversation to NVIDIA NIM and returns a channel
// that emits StreamEvent objects as the model generates its reply. The
// channel is closed when the model is done, the context is cancelled, or
// an error occurs.
func (n *NvidiaProvider) StreamChat(ctx context.Context, messages []Message, tools []tools.Definition) (<-chan StreamEvent, error) {
	reqBody := nvidiaChatRequest{
		Model:    n.Model,
		Messages: toNvidiaMessages(messages),
		Tools:    toNvidiaTools(tools),
		Stream:   true,
	}
	reqBody.applyEffortTo(n.effortMechanism, n.effortToken)

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("encode chat completions request: %w", err)
	}

	url := n.Endpoint + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request to %s: %w", url, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if n.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+n.APIKey)
	}

	resp, err := n.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", n.Endpoint, err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		return nil, fmt.Errorf(
			"provider %s returned status %d for model %q: %s",
			n.Endpoint,
			resp.StatusCode,
			n.Model,
			string(body),
		)
	}

	events := make(chan StreamEvent)

	go func() {
		defer resp.Body.Close()
		defer close(events)

		send := func(event StreamEvent) bool {
			select {
			case events <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		// pending buffers partial tool calls by their stream index until
		// their arguments are complete.
		pending := map[int]*nvidiaToolCallBuf{}
		order := []int{}

		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}

			line := strings.TrimSpace(scanner.Text())
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}

			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				send(StreamEvent{Done: true})
				return
			}

			var chunk nvidiaStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				send(StreamEvent{Err: fmt.Errorf("decode stream chunk: %w", err)})
				return
			}

			if chunk.Error.Message != "" {
				send(StreamEvent{Err: errors.New(chunk.Error.Message)})
				return
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			choice := chunk.Choices[0]

			if choice.Delta.Content != "" {
				if !send(StreamEvent{Text: choice.Delta.Content}) {
					return
				}
			}

			for _, tc := range choice.Delta.ToolCalls {
				buf, ok := pending[tc.Index]
				if !ok {
					buf = &nvidiaToolCallBuf{}
					pending[tc.Index] = buf
					order = append(order, tc.Index)
				}
				if tc.ID != "" {
					buf.ID += tc.ID
				}
				if tc.Function.Name != "" {
					buf.Name += tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					buf.ArgsBuf += tc.Function.Arguments
				}
			}

			switch choice.FinishReason {
			case "":
				// turn not finished yet, keep reading
			case "tool_calls":
				calls, err := finalizeNvidiaToolCalls(pending, order)
				if err != nil {
					send(StreamEvent{Err: fmt.Errorf("decode streamed tool call arguments: %w", err)})
					return
				}
				if len(calls) > 0 {
					if !send(StreamEvent{ToolCalls: calls}) {
						return
					}
				}
				send(StreamEvent{Done: true})
				return
			case "stop", "length", "content_filter":
				send(StreamEvent{Done: true})
				return
			default:
				// Unknown finish reason: treat the turn as complete rather
				// than looping forever, but don't fail the whole stream.
				send(StreamEvent{Done: true})
				return
			}
		}

		if err := scanner.Err(); err != nil {
			send(StreamEvent{Err: err})
		}
	}()

	return events, nil
}

// finalizeNvidiaToolCalls parses the buffered argument strings into
// ToolCall.Arguments now that streaming is complete, in the order the
// tool calls first appeared.
func finalizeNvidiaToolCalls(pending map[int]*nvidiaToolCallBuf, order []int) ([]ToolCall, error) {
	calls := make([]ToolCall, 0, len(order))
	for _, idx := range order {
		buf := pending[idx]
		var args map[string]any
		if buf.ArgsBuf != "" {
			if err := json.Unmarshal([]byte(buf.ArgsBuf), &args); err != nil {
				return nil, err
			}
		}
		calls = append(calls, ToolCall{ID: buf.ID, Name: buf.Name, Arguments: args})
	}
	return calls, nil
}

// Helper conversions
func toNvidiaMessages(messages []Message) []nvidiaMessage {
	nvidiaMessages := make([]nvidiaMessage, 0, len(messages))
	for _, msg := range messages {
		var toolCalls []nvidiaToolCall
		if len(msg.ToolCalls) > 0 {
			toolCalls = make([]nvidiaToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				args, err := json.Marshal(tc.Arguments)
				if err != nil {
					args = []byte("{}")
				}
				toolCalls = append(toolCalls, nvidiaToolCall{
					ID: tc.ID,
					Function: nvidiaFunctionCall{
						Name:      tc.Name,
						Arguments: string(args),
					},
				})
			}
		}

		nvidiaMessages = append(nvidiaMessages, nvidiaMessage{
			Role:       string(msg.Role),
			Content:    msg.Content,
			ToolCalls:  toolCalls,
			ToolCallID: msg.ToolCallID,
			Name:       msg.Name,
		})
	}
	return nvidiaMessages
}

func toNvidiaTools(tools []tools.Definition) []nvidiaTool {
	nvidiaTools := make([]nvidiaTool, 0, len(tools))
	for _, tool := range tools {
		nvidiaTools = append(nvidiaTools, nvidiaTool{
			Type: "function",
			Function: nvidiaFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}
	return nvidiaTools
}
