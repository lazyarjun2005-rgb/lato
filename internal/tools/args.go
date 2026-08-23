package tools

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
