package tools

// Validate reports whether args conform to the tool's declared JSON
// schema (one object with "properties" and "required"). It returns a
// non-nil error describing the first problem found; validation failures
// must prevent the tool from running at all.
//
// This is intentionally a narrow, non-generic validator: every built-in
// tool uses the same simple contract ("type": "object", with fields
// typed string|number|integer|boolean), so the validator only needs to
// enforce that contract. It is not a full JSON Schema engine.
func Validate(args map[string]any, schema map[string]any) error {
	if args == nil {
		args = map[string]any{}
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		return nil
	}
	required, _ := schema["required"].([]string)
	if required == nil {
		// Some schemas use []any for required (decoded from JSON); be
		// defensive even though every schema here writes []string.
		if arr, ok := schema["required"].([]any); ok {
			required = nil
			for _, v := range arr {
				if s, ok := v.(string); ok {
					required = append(required, s)
				}
			}
		}
	}

	for _, key := range required {
		v, ok := args[key]
		if !ok || v == nil {
			return &ArgumentError{Field: key, Reason: "is required"}
		}
		spec, _ := props[key].(map[string]any)
		expected, _ := spec["type"].(string)
		if err := checkType(key, v, expected); err != nil {
			return err
		}
		if expected == "string" {
			if s, _ := v.(string); s == "" {
				return &ArgumentError{Field: key, Reason: "must not be empty"}
			}
		}
	}

	// Present fields must match their declared type. Unknown keys are not
	// rejected because no built-in schema forbids them; they are ignored
	// by the argument readers as today.
	for key, v := range args {
		spec, _ := props[key].(map[string]any)
		if spec == nil {
			continue
		}
		expected, _ := spec["type"].(string)
		if err := checkType(key, v, expected); err != nil {
			return err
		}
	}
	return nil
}

func checkType(key string, v any, expected string) error {
	if v == nil {
		return &ArgumentError{Field: key, Reason: "must not be null"}
	}
	switch expected {
	case "string":
		if _, ok := v.(string); !ok {
			return &ArgumentError{Field: key, Reason: "must be a string"}
		}
	case "number":
		switch v.(type) {
		case int, int64, float64:
		default:
			return &ArgumentError{Field: key, Reason: "must be a number"}
		}
	case "integer":
		switch v.(type) {
		case int, int64, float64:
		default:
			return &ArgumentError{Field: key, Reason: "must be an integer"}
		}
	case "boolean":
		switch v.(type) {
		case bool:
		case string:
			if v != "true" && v != "false" {
				return &ArgumentError{Field: key, Reason: "must be a boolean"}
			}
		default:
			return &ArgumentError{Field: key, Reason: "must be a boolean"}
		}
	case "array":
		switch v.(type) {
		case []any, []map[string]any:
		default:
			return &ArgumentError{Field: key, Reason: "must be an array"}
		}
	}
	return nil
}

// StringArg reads a required string argument named key out of args.
func StringArg(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", &ArgumentError{Field: key, Reason: "is required"}
	}

	s, ok := v.(string)
	if !ok {
		return "", &ArgumentError{Field: key, Reason: "must be a string"}
	}

	return s, nil
}

// OptionalStringArg reads an optional string argument named key out of
// args, returning def if it's absent.
func OptionalStringArg(args map[string]any, key, def string) string {
	v, ok := args[key]
	if !ok {
		return def
	}

	s, ok := v.(string)
	if !ok {
		return def
	}

	return s
}

// OptionalIntArg reads an optional integer argument named key out of
// args. Args from a model arrive as float64 (JSON numbers), so both
// forms are accepted; the default is returned when the value is absent
// or of the wrong type.
func OptionalIntArg(args map[string]any, key string, def int) (int, error) {
	v, ok := args[key]
	if !ok {
		return def, nil
	}
	switch n := v.(type) {
	case int:
		return n, nil
	case float64:
		return int(n), nil
	case int64:
		return int(n), nil
	default:
		return def, &ArgumentError{Field: key, Reason: "must be a number"}
	}
}

// OptionalBoolArg reads an optional boolean argument named key out of
// args, returning false when it is absent or not a bool.
func OptionalBoolArg(args map[string]any, key string) (bool, error) {
	v, ok := args[key]
	if !ok {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		// Models sometimes send "true"/"false" strings; accept them.
		if s, isStr := v.(string); isStr {
			if s == "true" {
				return true, nil
			}
			if s == "false" {
				return false, nil
			}
		}
		return false, &ArgumentError{Field: key, Reason: "must be a boolean"}
	}
	return b, nil
}

// OptionalBoolArgDef is OptionalBoolArg with a caller-chosen default:
// def is returned when key is absent, so a tool can make an option
// enabled by default while still honoring an explicit false.
func OptionalBoolArgDef(args map[string]any, key string, def bool) (bool, error) {
	if _, ok := args[key]; !ok {
		return def, nil
	}
	return OptionalBoolArg(args, key)
}
