package theme

import "testing"

func TestRequiredThemesAndVariantsExist(t *testing.T) {
	want := []string{
		"aura", "ayu", "carbonfox", "catppuccin", "catppuccin-frappe", "catppuccin-macchiato", "cobalt2", "cursor", "dracula", "electric-blue", "everforest", "flexoki", "github", "gruvbox", "kanagawa", "lucent-orng", "material", "matrix", "mercury", "monokai", "nightowl", "nord", "one-dark", "opencode", "orng", "osaka-jade", "palenight", "rosepine", "solarized", "synthwave84", "system", "tokyonight", "vercel", "vesper", "zenburn",
		"catppuccin-mocha", "catppuccin-latte", "ayu-mirage", "ayu-light", "tokyonight-storm", "tokyonight-light", "rosepine-moon", "rosepine-dawn", "gruvbox-light", "nord-light", "solarized-light", "github-light",
	}
	if len(Names()) != len(want) {
		t.Fatalf("theme count = %d, want %d", len(Names()), len(want))
	}
	for _, name := range want {
		if _, ok := Lookup(name); !ok {
			t.Errorf("missing theme %q", name)
		}
	}
}
