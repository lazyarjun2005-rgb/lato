package permissions

import (
	"path/filepath"
	"strings"
)

// IsSensitivePath reports whether path looks like a credential or
// secret file that must not be auto-allowed by the permission policy.
//
// The check is purely lexical: it never relies on the model (or the
// user) to recognize secrets, and it never reads file contents. Any
// suspicious name forces explicit approval rather than running
// silently.
//
// Paths are normalized (separators folded, case folded) before matching
// so the rule behaves consistently across Linux, macOS, and Windows.
func IsSensitivePath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	slashPath := filepath.ToSlash(path)
	lowerPath := strings.ToLower(slashPath)
	base := filepath.Base(slashPath)
	lowerBase := strings.ToLower(base)

	// .env, .env.* (any file whose name starts with .env)
	if lowerBase == ".env" || strings.HasPrefix(lowerBase, ".env.") {
		return true
	}
	// Private keys / certificate stores
	if strings.HasSuffix(lowerBase, ".pem") || strings.HasSuffix(lowerBase, ".key") ||
		strings.HasSuffix(lowerBase, ".p12") || strings.HasSuffix(lowerBase, ".pfx") {
		return true
	}
	// SSH material (directory components or well-known filenames)
	if strings.Contains(lowerPath, "/.ssh/") || strings.HasSuffix(lowerPath, "/.ssh") {
		return true
	}
	switch lowerBase {
	case "id_rsa", "id_rsa.pub", "id_ed25519", "id_ed25519.pub",
		"id_ecdsa", "id_ecdsa.pub", "id_dsa", "authorized_keys", "known_hosts":
		return true
	}
	// Cloud / orchestrator credential locations
	for _, seg := range []string{"/.aws/", "/.azure/", "/.gcloud/", "/.kube/", "/.docker/", ".aws", ".azure", ".gcloud", ".kube", ".docker"} {
		if strings.Contains(lowerPath, seg) {
			return true
		}
	}
	if lowerBase == "credentials" && strings.Contains(lowerPath, ".aws") {
		return true
	}
	if lowerBase == "config" && (strings.Contains(lowerPath, ".aws") || strings.Contains(lowerPath, ".kube")) {
		return true
	}
	if lowerBase == ".netrc" || lowerBase == ".git-credentials" {
		return true
	}
	// Package-manager credentials
	if lowerBase == ".npmrc" || lowerBase == ".pypirc" {
		return true
	}
	return false
}
