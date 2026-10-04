package tui

import "testing"

func TestPrimaryPaletteUsesElectricBlue(t *testing.T) {
	const want = "#0000FF"

	for name, got := range map[string]string{
		"accent":    string(colorAccent),
		"assistant": string(colorAssistant),
		"border":    string(colorBorder),
	} {
		if got != want {
			t.Errorf("%s color = %q, want %q", name, got, want)
		}
	}
}

func TestSemanticPaletteRemainsDistinct(t *testing.T) {
	if string(colorError) != "#FF6B6B" {
		t.Errorf("error color = %q, want #FF6B6B", colorError)
	}
	if string(colorMuted) != "#7A7A7A" {
		t.Errorf("muted color = %q, want #7A7A7A", colorMuted)
	}
	if string(colorText) != "#EAEAEA" {
		t.Errorf("text color = %q, want #EAEAEA", colorText)
	}
}
