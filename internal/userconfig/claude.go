package userconfig

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Claude Code gateway-import support. Claude Code can be pointed at a
// local OpenAI-compatible gateway (such as 9Router) through
// ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN. Lato recognizes that
// specific pattern; it does NOT claim general Anthropic API support and
// never imports an unknown Anthropic endpoint as OpenAI-compatible.
//
// Detection reads only configuration files and the process environment.
// Nothing is executed.

// ninerouterPort is 9Router's documented local port; a Claude Code
// base URL aimed at it clearly indicates the OpenAI-compatible gateway
// this milestone targets.
const ninerouterPort = "20128"

// DetectClaudeGateway looks for ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN
// in ~/.claude/settings.json first, then in the process environment. It
// returns a connection only when the URL clearly aims at a known
// OpenAI-compatible router; otherwise it reports ok=false with a reason.
func DetectClaudeGateway() (Connection, bool, string) {
	baseURL, token := readClaudeEnv()
	if baseURL == "" {
		return Connection{}, false, "no ANTHROPIC_BASE_URL found"
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return Connection{}, false, "ANTHROPIC_BASE_URL is not a valid URL"
	}

	if !clearlyOpenAICompatible(u) {
		return Connection{}, false,
			"endpoint does not identify a known OpenAI-compatible gateway; native Anthropic endpoints are not supported by Lato providers"
	}

	id := "9router"
	name := "9Router"
	port := u.Port()
	if port != "" && port != ninerouterPort {
		// A different local router port: import as a custom provider so
		// the registry's 9Router defaults are not silently rewritten.
		id = CustomNameToID("claude-gateway-" + port)
		name = "Claude Gateway (" + port + ")"
	}

	return Connection{
		ID:       id,
		Name:     name,
		Class:    "openai-compatible",
		Endpoint: strings.TrimRight(baseURL, "/"),
		APIKey:   token,
		Custom:   id != "9router",
		Source:   "claude",
	}, true, ""
}

// clearlyOpenAICompatible reports whether the URL unambiguously points
// at a router Lato knows speaks the OpenAI chat-completions shape: the
// documented 9Router port, or a path ending in the versioned "/v1"
// prefix used by every OpenAI-compatible server.
func clearlyOpenAICompatible(u *url.URL) bool {
	if p := u.Port(); p != "" && p == ninerouterPort {
		return true
	}
	path := strings.TrimSuffix(u.Path, "/")
	return strings.HasSuffix(path, "/v1")
}

// readClaudeEnv resolves the Claude settings pair: ~/.claude/settings.json's
// "env" block wins over process environment variables.
func readClaudeEnv() (baseURL, token string) {
	if home, err := os.UserHomeDir(); err == nil {
		raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
		if err == nil {
			var settings struct {
				Env map[string]string `json:"env"`
			}
			if json.Unmarshal(raw, &settings) == nil {
				baseURL = settings.Env["ANTHROPIC_BASE_URL"]
				token = settings.Env["ANTHROPIC_AUTH_TOKEN"]
				if baseURL != "" {
					return baseURL, token
				}
			}
		}
	}
	return os.Getenv("ANTHROPIC_BASE_URL"), os.Getenv("ANTHROPIC_AUTH_TOKEN")
}
