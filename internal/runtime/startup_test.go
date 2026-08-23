package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"lato/internal/providers"
)

// TestNilProviderSurfacesActionableError pins the M14 startup contract:
// a runtime whose initial provider could not be built must not crash.
// Requests fail with the recorded, actionable reason instead, so the
// TUI can stay open while the user fixes configuration via /connect.
func TestNilProviderSurfacesActionableError(t *testing.T) {
	rt := newTestRuntime(nil)
	want := errors.New("OPENROUTER_API_KEY is not set — run /connect openrouter or set it")
	rt.provider = nil
	rt.providerErr = want

	if err := rt.StartError(); !errors.Is(err, want) {
		t.Fatalf("StartError() = %v, want %v", err, want)
	}

	_, err := rt.activeProvider()
	if !errors.Is(err, want) {
		t.Errorf("activeProvider() error = %v, want the recorded startup error", err)
	}

	_, err = rt.Models(context.Background())
	if !errors.Is(err, want) {
		t.Errorf("Models() error = %v, want the recorded startup error", err)
	}

	resp, err := rt.RunContext(context.Background(), []providers.Message{
		{Role: providers.UserRole, Content: "hello"},
	})
	if !errors.Is(err, want) {
		t.Fatalf("RunContext() error = %v, want the recorded startup error", err)
	}
	if resp.Content != "" {
		t.Errorf("unexpected partial response %q", resp.Content)
	}
}

// TestActiveProviderFailsClosedWithoutReason verifies the fallback
// message when no provider and no recorded reason exist — it must name
// the recovery path instead of being a bare internal failure.
func TestActiveProviderFailsClosedWithoutReason(t *testing.T) {
	rt := newTestRuntime(nil)
	rt.provider = nil
	rt.providerErr = nil

	_, err := rt.activeProvider()
	if err == nil {
		t.Fatal("activeProvider() with no provider at all should fail")
	}
	if !strings.Contains(err.Error(), "/connect") {
		t.Errorf("error %q does not tell the user how to recover", err)
	}
}
