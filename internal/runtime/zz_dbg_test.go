package runtime

import (
	"context"
	"testing"

	"lato/internal/providers"
)

func TestDbgRecovery(t *testing.T) {
	p := &scriptedProvider{}
	rt, _, _ := taskRuntimeWithProvider(t, p)
	if err := rt.manager.Register(failingProbe{name: "probe_fail"}); err != nil {
		t.Fatal(err)
	}
	p.turns = [][]providers.StreamEvent{
		planTurn("c1", "probe_fail"),
		toolTurn("c2", "probe_fail"),
		toolTurn("c3", "echo"),
		finalTurn("Task complete: fixed and verified."),
	}
	events, err := rt.StreamChat(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: complexGoal},
	})
	if err != nil {
		t.Fatal(err)
	}
	for ev := range events {
		switch ev.Type {
		case EventToolFinish:
			t.Logf("TOOLFINISH ok=%v err=%v content=%.40q", ev.ToolResult.Success, ev.ToolResult.Err, ev.ToolResult.Content)
		case EventError:
			t.Logf("ERROR: %v", ev.Err)
		case EventDone:
			t.Logf("DONE: %.60q", ev.Response.Content)
		default:
			t.Logf("event type=%d", ev.Type)
		}
	}
	store, _ := taskStoreForRoot(rt.workspace.Root)
	t.Logf("workspace root=%q tasks=%d", rt.workspace.Root, len(store.All()))
	for _, tk := range store.All() {
		t.Logf("task status=%q steps=%d files=%v", tk.Status, len(tk.Steps), tk.FilesChanged)
	}
}
