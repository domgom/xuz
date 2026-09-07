package theme

import "testing"

func TestBuiltinsResolve(t *testing.T) {
	for _, n := range Builtins() {
		if _, err := Get(n, nil); err != nil {
			t.Errorf("Get(%q): %v", n, err)
		}
	}
}

func TestUnknownTheme(t *testing.T) {
	if _, err := Get("nope", nil); err == nil {
		t.Error("expected error for unknown theme")
	}
}

func TestNormalizeColor(t *testing.T) {
	cases := map[string]string{
		"":        "",
		"default": "",
		"#a1b2c3": "#a1b2c3",
		"#ABCDEF": "#abcdef",
		"red":     "#cc0000",
		"196":     "#ff0000",
		"0":       "#000000",
		"231":     "#ffffff", // cube corner
		"255":     "#eeeeee", // grayscale ramp top
		"256":     "",        // out of range -> default
		"garbage": "",
	}
	for in, want := range cases {
		if got := string(NormalizeColor(in)); got != want {
			t.Errorf("NormalizeColor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCustomThemeOverDefault(t *testing.T) {
	def := map[string]map[string]string{
		"mymix": {"bg": "#111111", "fg": "255"},
	}
	th, err := Get("mymix", def)
	if err != nil {
		t.Fatal(err)
	}
	if th.Bg != "#111111" {
		t.Errorf("bg = %q", th.Bg)
	}
	if th.Fg != "255" {
		t.Errorf("fg = %q", th.Fg)
	}
	if th.Border != builtinThemes["default"].Border {
		t.Errorf("missing fields should fall back to default; border = %q", th.Border)
	}
}

func TestCustomThemeShadowsBuiltin(t *testing.T) {
	def := map[string]map[string]string{
		"dracula": {"bg": "#000000"},
	}
	th, err := Get("dracula", def)
	if err != nil {
		t.Fatal(err)
	}
	if th.Bg != "#000000" {
		t.Errorf("bg = %q", th.Bg)
	}
	if th.Accent != builtinThemes["dracula"].Accent {
		t.Errorf("accent should come from builtin dracula, got %q", th.Accent)
	}
}
