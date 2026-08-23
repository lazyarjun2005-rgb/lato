package userconfig

import "strings"

// mask is the fixed placeholder shown wherever a secret would otherwise
// appear. It deliberately carries no information about the key.
const mask = "***"

// Redact renders a secret for display: never the value itself.
func Redact(secret string) string {
	if secret == "" {
		return "(not set)"
	}
	return mask
}

// Sanitize removes any occurrence of secret from text so provider
// errors (which occasionally echo request material) can be shown to the
// user without leaking credentials.
func Sanitize(text, secret string) string {
	if secret == "" || text == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, mask)
}
