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

func TestResolveUnknownUsesElectricBlue(t *testing.T) {
	name, got := Resolve("does-not-exist")
	if name != DefaultName || got.Primary != "#0000FF" {
		t.Fatalf("Resolve unknown = %q, %+v", name, got)
	}
}

func TestSemanticColorsRemainDistinct(t *testing.T) {
	p, _ := Lookup(DefaultName)
	if p.Error == p.Warning || p.Warning == p.Success || p.Success == p.Muted {
		t.Fatalf("semantic colors are not distinct: %+v", p)
	}
}
