package tui

import (
	"strings"
	"testing"

	"lato/internal/effort"
)

func TestModalWidthClampsToTerminal(t *testing.T) {
	for _, tc := range []struct {
		terminal, want int
	}{
		{120, pickerWidth},
		{64, pickerWidth},
		{40, 36},
		{20, 16},
		{2, 1},
	} {
		if got := modalWidth(tc.terminal, pickerWidth); got != tc.want {
			t.Errorf("modalWidth(%d) = %d, want %d", tc.terminal, got, tc.want)
		}
	}
}

func TestFloatingPickersRenderAtConstrainedSizes(t *testing.T) {
	groups := testModelGroups()
	for _, size := range []struct{ width, height int }{{100, 30}, {40, 16}, {20, 8}, {8, 4}} {
		modelView := newSearchableModelPicker(groups, "ollama", "llama3.2", effort.Default).view(size.width, size.height)
		if strings.TrimSpace(modelView) == "" {
			t.Errorf("model picker rendered empty at %dx%d", size.width, size.height)
		}
		themeView := newThemePicker("electric-blue", nil).view(size.width, size.height)
		if strings.TrimSpace(themeView) == "" {
			t.Errorf("theme picker rendered empty at %dx%d", size.width, size.height)
		}
	}
}

func TestInputModalUsesCompleteInnerCardWidth(t *testing.T) {
	modal := newInputModal(inputStep{title: "Custom provider", prompt: "Provider name:"})
	view := modal.view(40, 12)
	if !strings.Contains(view, "Custom provider") || !strings.Contains(view, "Provider name:") {
		t.Fatalf("input modal lost title or prompt: %q", view)
	}
	if modalInnerWidth(modalWidth(40, pickerWidth)) != 30 {
		t.Fatalf("input width geometry changed unexpectedly")
	}
}
