package tools

import "testing"

func TestStringArg(t *testing.T) {
	got, err := StringArg(map[string]any{"path": "/tmp/x"}, "path")
	if err != nil {
		t.Fatalf("StringArg() unexpected error: %v", err)
	}
	if got != "/tmp/x" {
		t.Fatalf("StringArg() = %q, want %q", got, "/tmp/x")
	}
}

func TestStringArg_Missing(t *testing.T) {
	_, err := StringArg(map[string]any{}, "path")
	if err == nil {
		t.Fatal("StringArg() with missing key = nil error, want an error")
	}
}

func TestStringArg_WrongType(t *testing.T) {
	_, err := StringArg(map[string]any{"path": 42}, "path")
	if err == nil {
		t.Fatal("StringArg() with wrong type = nil error, want an error")
	}
}

func TestOptionalStringArg_Present(t *testing.T) {
	got := OptionalStringArg(map[string]any{"path": "here"}, "path", "default")
	if got != "here" {
		t.Fatalf("OptionalStringArg() = %q, want %q", got, "here")
	}
}

func TestOptionalStringArg_Missing(t *testing.T) {
	got := OptionalStringArg(map[string]any{}, "path", "default")
	if got != "default" {
		t.Fatalf("OptionalStringArg() = %q, want %q", got, "default")
	}
}

func TestOptionalStringArg_WrongTypeFallsBackToDefault(t *testing.T) {
	got := OptionalStringArg(map[string]any{"path": 42}, "path", "default")
	if got != "default" {
		t.Fatalf("OptionalStringArg() = %q, want %q", got, "default")
	}
}

func TestValidate(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
			"max":     map[string]any{"type": "integer"},
			"verbose": map[string]any{"type": "boolean"},
			"ratio":   map[string]any{"type": "number"},
		},
		"required": []string{"path"},
	}
	cases := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{"valid full", map[string]any{"path": "a.go", "content": "x", "max": float64(3), "verbose": true, "ratio": 1.5}, false},
		{"valid minimal required", map[string]any{"path": "a.go"}, false},
		{"valid bool string true", map[string]any{"path": "a", "verbose": "true"}, false},
		{"null args", nil, true}, // required missing
		{"missing required", map[string]any{}, true},
		{"required empty string", map[string]any{"path": ""}, true},
		{"required wrong type number", map[string]any{"path": 42}, true},
		{"wrong type content number", map[string]any{"path": "a", "content": 5}, true},
		{"wrong type bool", map[string]any{"path": "a", "verbose": "maybe"}, true},
		{"wrong type integer string", map[string]any{"path": "a", "max": "5"}, true},
		{"unknown extra key allowed", map[string]any{"path": "a", "extra": 1}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.args, schema)
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate(%v) err = %v, wantErr=%v", c.args, err, c.wantErr)
			}
		})
	}
}
