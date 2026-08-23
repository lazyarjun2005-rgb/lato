package userconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleOpenCode = `{
  "$schema": "opencode.json",
  "provider": {
    "9router": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "http://localhost:20128/v1",
        "apiKey": "nine-secret"
      },
      "models": {
        "provider/model-id": { "name": "Provider Model" }
      }
    },
    "anthropic-native": {
      "npm": "@ai-sdk/anthropic",
      "options": { "baseURL": "https://api.anthropic.com", "apiKey": "nope" }
    }
  }
}`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseOpenCodeFile(t *testing.T) {
	path := writeTemp(t, "opencode.json", sampleOpenCode)
	conns, err := parseOpenCodeFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("got %d connections, want 1 (non-OpenAI SDK entries must be skipped)", len(conns))
	}
	c := conns[0]
	if c.ID != "9router" || c.Endpoint != "http://localhost:20128/v1" || c.APIKey != "nine-secret" {
		t.Errorf("connection = %+v", c)
	}
	if c.Source != "opencode" {
		t.Errorf("source = %q, want opencode", c.Source)
	}
	if len(c.Models) != 1 || c.Models[0].ID != "provider/model-id" || c.Models[0].Name != "Provider Model" {
		t.Errorf("models = %+v", c.Models)
	}
}

func TestParseOpenCodeJSONC(t *testing.T) {
	path := writeTemp(t, "opencode.jsonc", `{
  // a comment with "quotes" inside
  "provider": {
    /* block comment */
    "glm": {
      "npm": "@ai-sdk/openai-compatible",
      "options": { "baseURL": "http://localhost:8000/v1/" }
    }
  }
}`)
	conns, err := parseOpenCodeFile(path)
	if err != nil {
		t.Fatalf("parse jsonc: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("got %d connections, want 1", len(conns))
	}
	if conns[0].Endpoint != "http://localhost:8000/v1" {
		t.Errorf("endpoint = %q, trailing slash should be trimmed", conns[0].Endpoint)
	}
}

func TestDetectOpenCodeConfigsSkipsGarbage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	cfgDir := filepath.Join(dir, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	good := `{"provider":{"my-gw":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"http://localhost:1234/v1"}}}}`
	if err := os.WriteFile(filepath.Join(cfgDir, "opencode.json"), []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}

	// A project-local file with invalid JSON must not break detection.
	if err := os.WriteFile("opencode.json", []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove("opencode.json")

	conns := DetectOpenCodeConfigs()
	if len(conns) != 1 || conns[0].ID != "my-gw" {
		t.Fatalf("connections = %+v, want only my-gw", conns)
	}
}

// isolateHome points HOME/USERPROFILE at an empty temp directory so
// detection never reads the developer's real ~/.claude settings.
func isolateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func TestDetectClaudeGatewayNineRouterPort(t *testing.T) {
	dir := isolateHome(t)

	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	settings := `{"env":{"ANTHROPIC_BASE_URL":"http://localhost:20128/v1","ANTHROPIC_AUTH_TOKEN":"cc-token"}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}

	conn, ok, reason := DetectClaudeGateway()
	if !ok {
		t.Fatalf("expected import for 9Router gateway, reason = %q", reason)
	}
	if conn.ID != "9router" || !strings.Contains(conn.Endpoint, ":20128") || conn.APIKey != "cc-token" {
		t.Errorf("connection = %+v", conn)
	}
	if conn.Source != "claude" {
		t.Errorf("source = %q, want claude", conn.Source)
	}
}

func TestDetectClaudeGatewayRejectsNativeAnthropic(t *testing.T) {
	isolateHome(t)
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok")

	_, ok, reason := DetectClaudeGateway()
	if ok {
		t.Fatal("native Anthropic endpoint must not be imported as OpenAI-compatible")
	}
	if !strings.Contains(reason, "Anthropic") {
		t.Errorf("reason = %q, want an explanation mentioning Anthropic", reason)
	}
}

func TestDetectClaudeGatewayOtherLocalRouterPort(t *testing.T) {
	isolateHome(t)
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8787/v1")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok")

	conn, ok, _ := DetectClaudeGateway()
	if !ok {
		t.Fatal("/v1 local endpoint should be recognized as OpenAI-compatible")
	}
	if conn.ID == "9router" {
		t.Error("a non-20128 port must not overwrite the registry 9Router entry")
	}
	if !conn.Custom || conn.Source != "claude" {
		t.Errorf("connection = %+v", conn)
	}
}

func TestDetectClaudeGatewayAbsent(t *testing.T) {
	isolateHome(t)
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	_, ok, _ := DetectClaudeGateway()
	if ok {
		t.Fatal("detection must fail cleanly when nothing is configured")
	}
}
