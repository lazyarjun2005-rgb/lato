package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateUserConfig points the OS user-configuration directory at a
// temp dir, so tests never touch real memory and never write inside a
// repository.
func isolateUserConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))
	return dir
}

func TestEmptyStore(t *testing.T) {
	s, err := LoadFrom(filepath.Join(t.TempDir(), "m.json"), "proj1")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Fatal("fresh store should be empty")
	}
	if got := s.Relevant("database", 5); len(got) != 0 {
		t.Errorf("Relevant on empty store returned %d entries", len(got))
	}
}

func TestSaveAndLoadAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := LoadFrom(path, "proj1")
	if _, err := s.Add("The project uses Go 1.26.", "technology", KindDiscovered); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadFrom(path, "proj1")
	if err != nil {
		t.Fatal(err)
	}
	list := reloaded.List()
	if len(list) != 1 || list[0].Content != "The project uses Go 1.26." || list[0].Kind != KindDiscovered {
		t.Fatalf("after restart: %+v", list)
	}
}

func TestMultipleEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := LoadFrom(path, "p")
	for _, fact := range []string{"uses PostgreSQL", "API lives under internal/api", "tests use go test"} {
		if _, err := s.Add(fact, "", KindUser); err != nil {
			t.Fatal(err)
		}
	}
	reloaded, _ := LoadFrom(path, "p")
	if len(reloaded.List()) != 3 {
		t.Fatalf("entries = %d, want 3", len(reloaded.List()))
	}
}

// TestProjectIdentityIsolatesProjects pins requirement: same basename,
// different absolute roots → different project IDs and no shared memory.
func TestProjectIdentityIsolatesProjects(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	dirA := filepath.Join(a, "project") // identical basenames on purpose
	dirB := filepath.Join(b, "project")

	idA := ProjectID(dirA)
	idB := ProjectID(dirB)
	if idA == idB {
		t.Fatal("distinct roots with equal basenames share a project ID")
	}
	if ProjectID(dirA) != idA {
		t.Error("project ID must be stable for the same root")
	}
	if strings.ContainsAny(idA, `/\`) {
		t.Errorf("ID %q is not filesystem-safe", idA)
	}
}

func TestProjectIsolationOfStores(t *testing.T) {
	isolateUserConfig(t)

	sa, err := Load(ProjectID("/tmp/alpha/project"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sa.Add("alpha secret sauce", "", KindUser); err != nil {
		t.Fatal(err)
	}

	sb, err := Load(ProjectID("/tmp/beta/project"))
	if err != nil {
		t.Fatal(err)
	}
	if got := sb.Relevant("secret sauce", 5); len(got) != 0 {
		t.Errorf("project B sees project A's memory: %+v", got)
	}
}

func TestUserAndDiscoveredKinds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := LoadFrom(path, "p")
	u, _ := s.Add("Use PostgreSQL for production.", "technology", KindUser)
	d, _ := s.Add("Tests use go test ./...", "command", KindDiscovered)
	if u.Kind != KindUser || d.Kind != KindDiscovered {
		t.Fatalf("kinds = %q/%q", u.Kind, d.Kind)
	}
}

func TestUpdateExistingMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := LoadFrom(path, "p")
	e, _ := s.Add("Build with make", "command", KindDiscovered)

	if _, err := s.Update(e.ID[:4], "Build with go build ./..."); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := LoadFrom(path, "p")
	list := reloaded.List()
	if len(list) != 1 || list[0].Content != "Build with go build ./..." {
		t.Fatalf("update failed: %+v", list)
	}
}

func TestRemoveAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := LoadFrom(path, "p")
	e1, _ := s.Add("fact one", "", KindUser)
	e2, _ := s.Add("fact two", "", KindUser)

	if err := s.Remove(e1.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 1 || s.List()[0].ID != e2.ID {
		t.Fatalf("remove failed: %+v", s.List())
	}
	n, err := s.Clear()
	if err != nil || n != 1 {
		t.Fatalf("clear = %d entries, err %v", n, err)
	}
	if len(s.List()) != 0 {
		t.Fatal("clear left entries behind")
	}
	if err := s.Remove(e2.ID); err == nil {
		t.Error("removing from an empty store must fail cleanly")
	}
}

func TestDuplicateHandling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := LoadFrom(path, "p")
	first, _ := s.Add("Uses chi as the HTTP router.", "architecture", KindDiscovered)

	// Same fact, different case/punctuation → dedupe, not a new entry.
	again, err := s.Add("uses CHI as the HTTP router!", "architecture", KindDiscovered)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatalf("duplicate created new entry %s (original %s)", again.ID, first.ID)
	}
	if len(s.List()) != 1 {
		t.Fatalf("%d entries after duplicate add", len(s.List()))
	}

	// User stating the same fact upgrades it to user-provided.
	upgraded, err := s.Add("Uses chi as the HTTP router.", "architecture", KindUser)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Kind != KindUser {
		t.Errorf("user statement did not promote discovered memory: %q", upgraded.Kind)
	}
}

func TestBoundedEntrySize(t *testing.T) {
	s, _ := LoadFrom(filepath.Join(t.TempDir(), "m.json"), "p")
	huge := strings.Repeat("x", maxEntryChars+1)
	if _, err := s.Add(huge, "", KindUser); err == nil {
		t.Fatal("oversized entry accepted")
	}
	if len(s.List()) != 0 {
		t.Error("rejected content was stored anyway")
	}
	ok := strings.Repeat("x", maxEntryChars)
	if _, err := s.Add(ok, "", KindUser); err != nil {
		t.Fatalf("max-size entry rejected: %v", err)
	}
}

func TestBoundedMemoryCount(t *testing.T) {
	s, _ := LoadFrom(filepath.Join(t.TempDir(), "m.json"), "p")
	for i := 0; i < maxEntries; i++ {
		fact := "fact number " + strings.Repeat("x", 3) + " " + string(rune('a'+i%26)) + " " + itoa(i)
		if _, err := s.Add(fact, "", KindUser); err != nil {
			t.Fatalf("entry %d rejected early: %v", i, err)
		}
	}
	if _, err := s.Add("one fact too many", "", KindUser); err == nil {
		t.Fatal("store exceeded its entry bound")
	}
}

func itoa(n int) string { return fmtIntStr(n) }

func fmtIntStr(n int) string {
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestLexicalRelevanceRanking(t *testing.T) {
	s, _ := LoadFrom(filepath.Join(t.TempDir(), "m.json"), "p")
	s.Add("Tests run with go test ./...", "command", KindUser)
	s.Add("The API lives under internal/api.", "structure", KindUser)
	s.Add("PostgreSQL is the production database.", "technology", KindUser)

	hits := s.Relevant("how do we run the tests?", 5)
	if len(hits) == 0 {
		t.Fatal("relevant memory not found for testing query")
	}
	if !strings.Contains(hits[0].Content, "go test") {
		t.Errorf("top hit = %q, want the test-command fact", hits[0].Content)
	}

	dbHits := s.Relevant("which database does production use?", 5)
	if len(dbHits) == 0 || !strings.Contains(dbHits[0].Content, "PostgreSQL") {
		t.Errorf("database query hits = %+v", dbHits)
	}

	apiHits := s.Relevant("where is the api code located?", 5)
	if len(apiHits) == 0 || !strings.Contains(apiHits[0].Content, "internal/api") {
		t.Errorf("api query hits = %+v", apiHits)
	}
}

func TestIrrelevantMemoryNotReturned(t *testing.T) {
	s, _ := LoadFrom(filepath.Join(t.TempDir(), "m.json"), "p")
	s.Add("PostgreSQL is the production database.", "technology", KindUser)

	if got := s.Relevant("kubernetes deployment strategy", 5); len(got) != 0 {
		t.Errorf("irrelevant query injected memory: %+v", got)
	}
	if got := s.Relevant("", 5); len(got) != 0 {
		t.Errorf("empty query injected memory: %+v", got)
	}
}

func TestRetrievalOutputBounded(t *testing.T) {
	s, _ := LoadFrom(filepath.Join(t.TempDir(), "m.json"), "p")
	for i := 0; i < 20; i++ {
		s.Entries = append(s.Entries, Entry{
			ID:       strings.Repeat("a", 4) + itoa(i),
			Category: "fact",
			Kind:     KindUser,
			Content:  "shared topic detail variant " + itoa(i) + " " + strings.Repeat("y", 300),
		})
	}

	got := s.Relevant("shared topic", maxInjectCount)
	if len(got) > maxInjectCount {
		t.Errorf("retrieved %d entries, cap is %d", len(got), maxInjectCount)
	}
	block := RenderBlock(got)
	if len(block) > maxInjectBytes+64 { // small header slack
		t.Errorf("rendered block = %d bytes, exceeds budget", len(block))
	}
	if !strings.Contains(block, "## Project memory") {
		t.Error("rendered block missing header")
	}
	if b := RenderBlock(nil); b != "" {
		t.Error("RenderBlock(empty) should be empty")
	}
}

func TestSecretRejection(t *testing.T) {
	cases := []string{
		"my key is sk-live-abcd1234567890abcdef",
		"API_KEY=ghp_1234567890abcdef",
		"password=hunter2000",
		"bearer token: eyJhbGciOiJIUzI1NiIsInR5",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----",
	}
	for _, content := range cases {
		s, _ := LoadFrom(filepath.Join(t.TempDir(), "m.json"), "p")
		_, err := s.Add(content, "", KindUser)
		if err == nil {
			t.Errorf("credential-shaped content stored: %q", content)
			continue
		}
		if strings.Contains(err.Error(), "sk-") || strings.Contains(err.Error(), "hunter2000") ||
			strings.Contains(err.Error(), "eyJ") || strings.Contains(err.Error(), "MIIE") {
			t.Errorf("error echoed secret material: %q", err)
		}
		if len(s.List()) != 0 {
			t.Error("rejected content persisted")
		}
	}
}

// TestStorageStaysOutsideRepository pins local-first storage: Load()
// resolves under the user configuration directory, never the working
// directory, even when the process runs inside a git repo.
func TestStorageStaysOutsideRepository(t *testing.T) {
	repo := t.TempDir()
	cfgRoot := isolateUserConfig(t)

	if _, err := Load(ProjectID(repo)); err != nil {
		t.Fatal(err)
	}
	var found []string
	filepath.Walk(repo, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	if len(found) != 0 {
		t.Fatalf("memory files leaked into the repository: %v", found)
	}
	wantPrefix := filepath.Join(cfgRoot, ".config", "lato", "memory")
	if d, err := Dir(); err != nil || !strings.HasPrefix(d, cfgRoot) || strings.Contains(d, wantPrefix) == false {
		t.Logf("memory dir = %q (config root %q)", d, cfgRoot)
	}
}

func TestCategoryNormalization(t *testing.T) {
	s, _ := LoadFrom(filepath.Join(t.TempDir(), "m.json"), "p")
	e, _ := s.Add("something odd", "NotACategory", KindUser)
	if e.Category != "fact" {
		t.Errorf("unknown category kept: %q", e.Category)
	}
	e2, _ := s.Add("known one", "TECHNOLOGY", KindUser)
	if e2.Category != "technology" {
		t.Errorf("case folding failed: %q", e2.Category)
	}
}
