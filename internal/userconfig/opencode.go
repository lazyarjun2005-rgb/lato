package userconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenCode provider-import support. Lato only parses OpenCode's
// configuration files; it never executes or depends on OpenCode at
// runtime.

// opencodeFile mirrors the subset of an OpenCode configuration that can
// describe OpenAI-compatible providers:
//
//	{
//	  "provider": {
//	    "9router": {
//	      "npm": "@ai-sdk/openai-compatible",
//	      "options": { "baseURL": "http://localhost:20128/v1", "apiKey": "..." },
//	      "models": { "provider/model-id": { "name": "provider/model-id" } }
//	    }
//	  }
//	}
type opencodeFile struct {
	Provider map[string]opencodeProvider `json:"provider"`
}

type opencodeProvider struct {
	NPM    string                  `json:"npm"`
	Option opencodeProviderOptions `json:"options"`
	Models map[string]struct {
		Name string `json:"name"`
	} `json:"models"`
}

type opencodeProviderOptions struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"`
}

// openaiSDKPackages are the OpenCode SDK identifiers Lato recognizes as
// speaking the standard OpenAI chat-completions shape. Anything else is
// skipped rather than guessed.
var openaiSDKPackages = map[string]bool{
	"@ai-sdk/openai-compatible": true,
	"@ai-sdk/openai":            true,
}

// DetectOpenCodeConfigs scans the well-known OpenCode configuration
// locations and returns every recognized OpenAI-compatible provider as
// a ready-to-save Connection (Source "opencode"). Detection never saves
// anything and never requires OpenCode to be installed.
func DetectOpenCodeConfigs() []Connection {
	var out []Connection
	for _, path := range opencodeConfigPaths() {
		conns, err := parseOpenCodeFile(path)
		if err != nil {
			continue // unreadable or non-JSON candidates are simply skipped
		}
		out = append(out, conns...)
	}
	return out
}

// opencodeConfigPaths lists candidate locations, most specific first:
// the user-level config, then a project-local file.
func opencodeConfigPaths() []string {
	var paths []string
	if base, err := os.UserConfigDir(); err == nil {
		paths = append(paths,
			filepath.Join(base, "opencode", "opencode.json"),
			filepath.Join(base, "opencode", "opencode.jsonc"),
		)
	}
	paths = append(paths, "opencode.json")
	return paths
}

// parseOpenCodeFile extracts OpenAI-compatible providers from one
// OpenCode configuration file. Only providers whose npm package is in
// openaiSDKPackages are converted.
func parseOpenCodeFile(path string) ([]Connection, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f opencodeFile
	if err := json.Unmarshal(stripJSONComments(raw), &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var out []Connection
	for key, p := range f.Provider {
		if !openaiSDKPackages[p.NPM] {
			continue
		}
		conn := Connection{
			ID:       CustomNameToID(key),
			Name:     key,
			Class:    "@openai-compatible",
			Endpoint: strings.TrimRight(p.Option.BaseURL, "/"),
			APIKey:   p.Option.APIKey,
			Source:   "opencode",
		}
		conn.Class = "openai-compatible"
		for id, m := range p.Models {
			name := m.Name
			if name == "" {
				name = id
			}
			conn.Models = append(conn.Models, Model{ID: id, Name: name})
		}
		out = append(out, conn)
	}
	return out, nil
}

// stripJSONComments removes // and /* */ comments so .jsonc variants
// parse with the standard library. String literals are respected.
func stripJSONComments(raw []byte) []byte {
	var b strings.Builder
	b.Grow(len(raw))
	inString, escaped := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case inString:
			b.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
		case c == '"':
			inString = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(raw) && raw[i+1] == '/':
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			if i < len(raw) {
				b.WriteByte('\n')
			}
		case c == '/' && i+1 < len(raw) && raw[i+1] == '*':
			i += 2
			for i+1 < len(raw) && !(raw[i] == '*' && raw[i+1] == '/') {
				i++
			}
			i++ // consume the '/'
		default:
			b.WriteByte(c)
		}
	}
	return []byte(b.String())
}
