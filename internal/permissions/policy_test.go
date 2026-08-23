package permissions

import (
	"strings"
	"testing"
)

func TestReadOnlyAllowed(t *testing.T) {
	p := NewPolicy(t.TempDir())

	v := p.Decide(p.Classify("read_repo_file", map[string]any{"path": "main.go"}), "")
	if v.Decision != Allow || v.Class != ClassReadOnly {
		t.Fatalf("read_repo_file verdict = %+v, want allow/read_only", v)
	}

	v = p.Decide(p.Classify("search_repo", map[string]any{"query": "permissions"}), "")
	if v.Decision != Allow {
		t.Fatalf("search_repo verdict = %+v, want allow", v)
	}

	v = p.Decide(p.Classify("recall_project_memory", nil), "")
	if v.Decision != Allow {
		t.Fatalf("recall memory verdict = %+v, want allow", v)
	}
}

func TestWorkspaceWriteAllowedInside(t *testing.T) {
	root := t.TempDir()
	p := NewPolicy(root)

	v := p.Decide(p.Classify("create_file", map[string]any{"path": "src/new.go", "content": "x"}), "")
	if v.Decision != Allow || v.Class != ClassWorkspaceWrite {
		t.Fatalf("create_file inside workspace = %+v, want allow/workspace_write", v)
	}

	v = p.Decide(p.Classify("edit_file", map[string]any{"path": "./main.go"}), "")
	if v.Decision != Allow {
		t.Fatalf("edit_file inside workspace = %+v, want allow", v)
	}

	v = p.Decide(p.Classify("remember_project_fact", map[string]any{"content": "uses go 1.22"}), "")
	if v.Decision != Allow {
		t.Fatalf("memory write = %+v, want allow", v)
	}
}

func TestOutsideWorkspaceRequiresApproval(t *testing.T) {
	root := t.TempDir()
	p := NewPolicy(root)

	for _, tc := range []struct{ tool, path string }{
		{"create_file", "../outside.txt"},
		{"edit_file", "/home/dev/other-project/file.go"},
		{"write_file", root + "/../escape.md"},
	} {
		a := p.Classify(tc.tool, map[string]any{"path": tc.path})
		v := p.Decide(a, "")
		if v.Decision != Ask {
			t.Errorf("%s %q: decision = %s, want ask (verdict %+v)", tc.tool, tc.path, v.Decision, v)
		}
		if !strings.Contains(v.Reason, "outside") && !strings.Contains(v.Reason, "boundary") {
			t.Errorf("%s %q: reason %q should mention the boundary", tc.tool, tc.path, v.Reason)
		}
	}

	// Reads outside the workspace are also gated.
	a := p.Classify("read_file", map[string]any{"path": "/etc/passwd"})
	if v := p.Decide(a, ""); v.Decision != Ask {
		t.Errorf("read outside workspace = %+v, want ask", v)
	}
}

func TestUnknownToolFailsClosed(t *testing.T) {
	p := NewPolicy(t.TempDir())
	a := p.Classify("format_entire_disk", nil)
	v := p.Decide(a, "")
	if v.Decision != Ask || v.Class != ClassHighRisk {
		t.Fatalf("unknown tool verdict = %+v, want ask/high_risk", v)
	}
}

func TestCommandVerdicts(t *testing.T) {
	p := NewPolicy(t.TempDir())

	if v := p.Decide(p.Classify("run_command", map[string]any{"command": "go test ./..."}), ""); v.Decision != Allow {
		t.Fatalf("go test verdict = %+v, want allow", v)
	}
	if v := p.Decide(p.Classify("run_command", map[string]any{"command": "rm -rf ."}), ""); v.Decision != Ask || v.Class != ClassHighRisk {
		t.Fatalf("rm -rf verdict = %+v, want ask/high_risk", v)
	}
	if v := p.Decide(p.Classify("run_command", map[string]any{"command": "go test ./...", "dir": "../elsewhere"}), ""); v.Decision != Ask {
		t.Fatalf("out-of-workspace dir verdict = %+v, want ask", v)
	}
}

func TestAllowOnceExecutesThenExpires(t *testing.T) {
	p := NewPolicy(t.TempDir())
	a := p.Classify("run_command", map[string]any{"command": "git push --force"})

	if got := p.Decide(a, "").Decision; got != Ask {
		t.Fatalf("first decision = %s, want ask", got)
	}

	sig := a.Signature()
	p.Approve(sig, ScopeOnce, "")

	// Same action passes once...
	if got := p.Decide(a, "").Decision; got != Allow {
		t.Fatalf("after allow-once = %s, want allow", got)
	}
	// ...and is consumed.
	if got := p.Decide(a, "").Decision; got != Ask {
		t.Fatalf("second time = %s, want ask again", got)
	}
}

func TestAllowForTaskScopesToTask(t *testing.T) {
	p := NewPolicy(t.TempDir())
	a := p.Classify("run_command", map[string]any{"command": "git clean -fdx"})
	p.Approve(a.Signature(), ScopeTask, "task1")

	if got := p.Decide(a, "task1").Decision; got != Allow {
		t.Fatalf("same task = %s, want allow", got)
	}
	// An unrelated future task must not inherit the approval.
	if got := p.Decide(a, "task2").Decision; got != Ask {
		t.Fatalf("other task = %s, want ask", got)
	}
	// Nor does it leak to taskless requests.
	if got := p.Decide(a, "").Decision; got != Ask {
		t.Fatalf("taskless = %s, want ask", got)
	}
}

func TestApprovalsDoNotCrossActions(t *testing.T) {
	p := NewPolicy(t.TempDir())
	approved := p.Classify("delete_directory", map[string]any{"path": "./build"})
	p.Approve(approved.Signature(), ScopeTask, "t1")

	other := p.Classify("delete_directory", map[string]any{"path": "./dist"})
	if got := p.Decide(other, "t1").Decision; got != Ask {
		t.Fatalf("different target = %s, want ask", got)
	}

	cmd := p.Classify("run_command", map[string]any{"command": "rm -rf build"})
	if got := p.Decide(cmd, "t1").Decision; got != Ask {
		t.Fatalf("equivalent command via shell = %s, want ask", got)
	}
}

func TestResetClearsEverything(t *testing.T) {
	p := NewPolicy(t.TempDir())
	a := p.Classify("run_command", map[string]any{"command": "git clean -fd"})
	b := p.Classify("delete_file", map[string]any{"path": "x.txt"})

	p.Approve(a.Signature(), ScopeTask, "taskA")
	p.Approve(b.Signature(), ScopeOnce, "")
	p.Approve(p.Classify("run_command", map[string]any{"command": "terraform apply"}).Signature(), ScopeSession, "")

	if n := p.Reset(); n < 3 {
		t.Fatalf("Reset cleared %d grants, want >= 3", n)
	}
	if got := p.Decide(a, "taskA").Decision; got != Ask {
		t.Errorf("task approval survived reset")
	}
	if got := p.Decide(b, "").Decision; got != Ask {
		t.Errorf("once approval survived reset")
	}
}

func TestSummaryMentionsWorkspaceAndCounts(t *testing.T) {
	root := t.TempDir()
	p := NewPolicy(root)

	s := p.Summary()
	if !strings.Contains(s, root) {
		t.Errorf("summary missing workspace root:\n%s", s)
	}
	if !strings.Contains(s, "Pending approval: none") {
		t.Errorf("summary missing pending line:\n%s", s)
	}

	p.Approve("run_command:x", ScopeTask, "abc123")
	if s := p.Summary(); !strings.Contains(s, "Task approvals: 1") {
		t.Errorf("summary missing task count:\n%s", s)
	}
}

func TestSummariesAreRedacted(t *testing.T) {
	p := NewPolicy(t.TempDir())
	a := p.Classify("run_command", map[string]any{
		"command": `curl -H "Authorization: Bearer supersecrettoken123" https://api.example.com`,
	})
	if strings.Contains(a.Summary, "supersecrettoken123") {
		t.Fatalf("action summary leaked the token: %s", a.Summary)
	}
}
