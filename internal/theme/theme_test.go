package theme

import "testing"

func TestEveryThemeHasPalette(t *testing.T) {
	for _, name := range Names() {
		p, ok := Lookup(name)
		if !ok || p.Primary == "" && name != "system" {
			t.Errorf("theme %q has no usable palette", name)
		}
	}
}

func TestLookupIsCaseInsensitive(t *testing.T) {
	got, ok := Lookup("DrAcUlA")
	if !ok || got.Primary != "#BD93F9" {
		t.Fatalf("Lookup(DrAcUlA) = %+v, %v", got, ok)
	}
}

func TestResolveUnknownUsesLato(t *testing.T) {
	name, got := Resolve("does-not-exist")
	if name != DefaultName || got.Primary != "#0000FF" {
		t.Fatalf("Resolve unknown = %q, %+v, want lato blue", name, got)
	}
}

func TestLegacyAliasesResolveToCanonicalThemes(t *testing.T) {
	for legacy, want := range map[string]string{"electric-blue": "lato", "opencode": "lato-orange"} {
		name, got := Resolve(legacy)
		canonical, _ := Lookup(want)
		if name != want || got != canonical {
			t.Fatalf("Resolve(%q) = %q, %+v; want %q, %+v", legacy, name, got, want, canonical)
		}
	}
}

func TestSemanticColorsRemainDistinct(t *testing.T) {
	p, _ := Lookup(DefaultName)
	if p.Error == p.Warning || p.Warning == p.Success || p.Success == p.Muted {
		t.Fatalf("semantic colors are not distinct: %+v", p)
	}
}
