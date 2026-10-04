package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/workspace"
)

// TestReadIndexedFileRefusesSensitiveFiles is the regression test for
// the gap this change closes. read_repo_file returned the cached body of
// any indexed path, so an unignored .env or id_rsa in the workspace could
// be read straight into the model context, even though search_repo
// filtered the same paths from its results.
func TestReadIndexedFileRefusesSensitiveFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("API_KEY=super-secret-value\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "id_rsa"), []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "server.pem"), []byte("-----BEGIN CERTIFICATE-----\n"), 0o644)

	rt := newTestRuntime(nil)
	rt.workspace = workspace.DiscoverDir(dir)

	for _, secret := range []string{".env", "id_rsa", "server.pem"} {
		body, err := rt.ReadIndexedFile(context.Background(), secret)
		if err == nil {
			t.Errorf("ReadIndexedFile(%q) returned no error; want refusal", secret)
		}
		if strings.Contains(body, "super-secret-value") || strings.Contains(body, "PRIVATE KEY") {
			t.Errorf("ReadIndexedFile(%q) leaked credential content: %q", secret, body)
		}
	}
}

// TestReadIndexedFileStillReadsOrdinaryFiles confirms the guard rejects
// only credential-shaped paths.
func TestReadIndexedFileStillReadsOrdinaryFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)

	rt := newTestRuntime(nil)
	rt.workspace = workspace.DiscoverDir(dir)

	body, err := rt.ReadIndexedFile(context.Background(), "main.go")
	if err != nil {
		t.Fatalf("ReadIndexedFile(main.go) error = %v", err)
	}
	if !strings.Contains(body, "package main") {
		t.Errorf("body = %q, want the indexed source", body)
	}
}

// TestReadIndexedFileRejectsEmptyPath keeps the pre-existing argument
// validation intact.
func TestReadIndexedFileRejectsEmptyPath(t *testing.T) {
	rt := newTestRuntime(nil)

	if _, err := rt.ReadIndexedFile(context.Background(), ""); err == nil {
		t.Error("ReadIndexedFile(\"\") error = nil, want an error")
	}
}

// TestRepositorySnapshotOmitsSensitiveFiles covers the prompt-facing
// path: RelevantFiles backs the "Repository index" block, so a
// credential file must not be named there either.
func TestRepositorySnapshotOmitsSensitiveFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("API_KEY=super-secret-value\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Demo\n"), 0o644)

	rt := newTestRuntime(nil)
	rt.workspace = workspace.DiscoverDir(dir)

	snapshot := rt.repositorySnapshot("")
	if strings.Contains(snapshot, ".env") {
		t.Errorf("repository snapshot names a credential file:\n%s", snapshot)
	}
}
