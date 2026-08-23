package builtin

import (
	"errors"
	"testing"
)

func TestConnect_NoArgsOpensFlow(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewConnect().Execute(ctx, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !ctx.openedConnectFlow {
		t.Fatal("/connect did not open the connection flow")
	}
}

func TestConnect_ImportArgOpensImportFlow(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewConnect().Execute(ctx, []string{"import"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !ctx.openedImportFlow || ctx.openedConnectFlow {
		t.Fatalf("openedImportFlow = %v openedConnectFlow = %v", ctx.openedImportFlow, ctx.openedConnectFlow)
	}
}

func TestConnect_RejectsUnknownArgs(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewConnect().Execute(ctx, []string{"bogus"}); err == nil {
		t.Fatal("expected an error for an unknown argument")
	}
	if ctx.openedConnectFlow {
		t.Error("flow must not open for invalid arguments")
	}
}

func TestImportCmdOpensImportFlow(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewImportCmd().Execute(ctx, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !ctx.openedImportFlow {
		t.Fatal("/import did not open the import flow")
	}
}

func TestModelRefreshCallsContext(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewModel().Execute(ctx, []string{"refresh"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !ctx.refreshedModels {
		t.Fatal("/model refresh did not refresh models")
	}
}

func TestModelRefreshPropagatesError(t *testing.T) {
	ctx := &fakeContext{refreshErr: errors.New("offline")}
	if err := NewModel().Execute(ctx, []string{"refresh"}); err == nil {
		t.Fatal("expected the refresh error to propagate")
	}
}

func TestModelAddOpensFlow(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewModel().Execute(ctx, []string{"add"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !ctx.openedAddModelFlow {
		t.Fatal("/model add did not open the add-model flow")
	}
}

func TestModelAddExtraArgsRejected(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewModel().Execute(ctx, []string{"add", "extra"}); err == nil {
		t.Fatal("expected error for extra argument")
	}
	if ctx.openedAddModelFlow {
		t.Error("flow must not open for invalid arguments")
	}
}
