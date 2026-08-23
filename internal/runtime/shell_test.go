package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"lato/internal/providers"
	"lato/internal/workspace"
)

// newShellTestRuntime builds a Runtime pointed at a real temp workspace
// with the shell tools registered against it. No provider is needed:
// tool execution goes through the manager directly.
func newShellTestRuntime(t *testing.T) (*Runtime, string, string) {
	t.Helper()
	dir := t.TempDir()

	rt := newTestRuntime(&scriptedProvider{})
	rt.workspace = workspace.DiscoverDir(dir)
	if err := rt.RegisterShellTools(); err != nil {
		t.Fatalf("RegisterShellTools: %v", err)
	}

	return rt, dir, buildProcessHelper(t)
}

// buildProcessHelper compiles the portable test double used by both the
// process engine tests and these integration tests.
func buildProcessHelper(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lato-runtime-helper")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "../../internal/process/testdata/helper")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building test helper: %v\n%s", err, out)
	}
	return bin
}

// TestShellToolRegisteredOnRuntime verifies run_command is exposed by
// the runtime's manager alongside the rest of the toolset.
func TestShellToolRegisteredOnRuntime(t *testing.T) {
	rt, _, _ := newShellTestRuntime(t)

	names := map[string]bool{}
	for _, d := range rt.manager.Definitions() {
		names[d.Name] = true
	}
	if !names["run_command"] {
		t.Errorf("runtime tools missing %q (have %v)", "run_command", names)
	}
}

// TestRunCommandExecutesInTargetWorkspace pins the M7 headline rule:
// commands run in the discovered target workspace even though the test
// binary (like the Lato binary in production) lives elsewhere.
func TestRunCommandExecutesInTargetWorkspace(t *testing.T) {
	rt, dir, helper := newShellTestRuntime(t)

	res, err := rt.manager.Execute(context.Background(), "run_command", map[string]any{
		"command": helper + " -write proof.txt",
	})
	if err != nil || res.IsError {
		t.Fatalf("run_command failed: res=%+v err=%v", res, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "proof.txt")); err != nil {
		t.Fatalf("command did not execute in the target workspace: %v", err)
	}
	if !strings.Contains(res.Content, "SUCCESS") {
		t.Errorf("unexpected result:\n%s", res.Content)
	}
}

// TestRunCommandResultFlowsToConversation verifies a completed run feeds
// the agent loop as a tool result message carrying the structured
// outcome, exactly like every other tool.
func TestRunCommandResultFlowsToConversation(t *testing.T) {
	dir := t.TempDir()
	helper := buildProcessHelper(t)

	provider := &scriptedProvider{turns: [][]providers.StreamEvent{
		{
			{ToolCalls: []providers.ToolCall{{
				ID:        "call-run-1",
				Name:      "run_command",
				Arguments: map[string]any{"command": helper + " -stdout loop-check -fail 7"},
			}}},
			{Done: true},
		},
		{
			{Text: "The command failed."},
			{Done: true},
		},
	}}
	rt := newTestRuntime(provider)
	rt.workspace = workspace.DiscoverDir(dir)
	if err := rt.RegisterShellTools(); err != nil {
		t.Fatal(err)
	}

	response, err := rt.Run([]providers.Message{{Role: providers.UserRole, Content: "run the helper"}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Content != "The command failed." {
		t.Errorf("response.Content = %q, want the post-tool continuation", response.Content)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2 (tool turn + continuation)", provider.calls)
	}

	continued := provider.messages[1]
	var toolMsg *providers.Message
	for i := range continued {
		if continued[i].Role == providers.ToolRole && continued[i].ToolCallID == "call-run-1" {
			toolMsg = &continued[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("no tool result message for the run_command call")
	}
	for _, want := range []string{"FAILURE", "exit code 7", "loop-check"} {
		if !strings.Contains(toolMsg.Content, want) {
			t.Errorf("tool message missing %q:\n%s", want, toolMsg.Content)
		}
	}
}

// TestRunCommandInvalidatesCachedIndex pins the invalidation contract:
// a command may create or change workspace files, so the cached index
// must be dropped after every started run.
func TestRunCommandInvalidatesCachedIndex(t *testing.T) {
	rt, dir, helper := newShellTestRuntime(t)

	first := rt.Index()
	res, err := rt.manager.Execute(context.Background(), "run_command", map[string]any{
		"command": helper + " -write generated.txt",
	})
	if err != nil || res.IsError {
		t.Fatalf("run_command failed: res=%+v err=%v", res, err)
	}

	second := rt.Index()
	if second == first {
		t.Fatal("cached index survived a command run; expected invalidation")
	}
	if _, ok := second.Lookup("generated.txt"); !ok {
		t.Error("rebuilt index does not see the command-created file")
	}
	_ = dir
}
