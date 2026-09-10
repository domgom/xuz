package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const saveRoundTripYAML = `
theme: dracula
remember_last: 5
themes:
  mytheme:
    title: bold red
    row: "#000000"
aliases:
  llama:
    options:
      model:
        - qwen-3.8-27B: !default /models/big.gguf
        - "yes: it's a value with : colon": weird
        - c: "# not a comment"
        - multi: "line1
          line2"
      context:
        - 64k: 65536
        - 256k: !color00ff00+default 262144
      vars:
        MODEL: model
        EXTRA: model
    command: llama-server -m "$MODEL" -c "$CONTEXT"
    remember_last: 2
  tf-plan:
    icon: ⚙
    options:
      env:
        - dev: !default+color7B42BC dev
        - prod:
            icon: ⚠
            long_text: !colorD30000 production
    command: tf plan
  bare:
    command: echo bare
`

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.yml")
	if err := os.WriteFile(src, []byte(saveRoundTripYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg1, err := Load(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range cfg1.Warnings {
		t.Errorf("unexpected warning in source load: %s", w)
	}
	for _, a := range cfg1.Aliases {
		for _, w := range a.Warnings {
			t.Errorf("unexpected warning for alias %s: %s", a.Name, w)
		}
	}

	out := filepath.Join(dir, "out.yml")
	if err := cfg1.Save(out); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(out)
	if err != nil {
		t.Fatal("reload of saved config failed:", err)
	}
	for _, w := range cfg2.Warnings {
		t.Errorf("warning after round trip: %s", w)
	}
	for _, a := range cfg2.Aliases {
		for _, w := range a.Warnings {
			t.Errorf("warning after round trip for alias %s: %s", a.Name, w)
		}
	}

	if cfg1.Theme != cfg2.Theme {
		t.Errorf("theme: %q != %q", cfg1.Theme, cfg2.Theme)
	}
	if cfg1.RememberLast != cfg2.RememberLast {
		t.Errorf("remember_last: %d != %d", cfg1.RememberLast, cfg2.RememberLast)
	}
	if !reflect.DeepEqual(cfg1.SelectionPrecedence, cfg2.SelectionPrecedence) {
		t.Errorf("selection_precedence: %v != %v", cfg1.SelectionPrecedence, cfg2.SelectionPrecedence)
	}
	if !reflect.DeepEqual(cfg1.CustomThemes, cfg2.CustomThemes) {
		t.Errorf("custom themes: %v != %v", cfg1.CustomThemes, cfg2.CustomThemes)
	}
	if len(cfg1.Aliases) != len(cfg2.Aliases) {
		t.Fatalf("alias count: %d != %d", len(cfg1.Aliases), len(cfg2.Aliases))
	}
	for i := range cfg1.Aliases {
		a1, a2 := cfg1.Aliases[i], cfg2.Aliases[i]
		if a1.Name != a2.Name {
			t.Fatalf("alias %d name: %q != %q", i, a1.Name, a2.Name)
		}
		if a1.Icon != a2.Icon {
			t.Errorf("alias %s icon: %q != %q", a1.Name, a1.Icon, a2.Icon)
		}
		if !reflect.DeepEqual(a1.Groups, a2.Groups) {
			t.Errorf("alias %s groups: %v != %v", a1.Name, a1.Groups, a2.Groups)
		}
		if !reflect.DeepEqual(a1.Defaults, a2.Defaults) {
			t.Errorf("alias %s defaults: %v != %v", a1.Name, a1.Defaults, a2.Defaults)
		}
		if !reflect.DeepEqual(a1.Vars, a2.Vars) {
			t.Errorf("alias %s vars: %v != %v", a1.Name, a1.Vars, a2.Vars)
		}
		if a1.Command != a2.Command {
			t.Errorf("alias %s command: %q != %q", a1.Name, a1.Command, a2.Command)
		}
		if a1.RememberLast != a2.RememberLast {
			t.Errorf("alias %s remember_last: %d != %d", a1.Name, a1.RememberLast, a2.RememberLast)
		}
		for _, g := range a1.Groups {
			if !reflect.DeepEqual(a1.GroupPairs[g], a2.GroupPairs[g]) {
				t.Errorf("alias %s group %s pairs:\n%v\n!=\n%v", a1.Name, g, a1.GroupPairs[g], a2.GroupPairs[g])
			}
		}
	}

	// Deterministic output: saving twice yields identical bytes.
	out2 := filepath.Join(dir, "out2.yml")
	if err := cfg2.Save(out2); err != nil {
		t.Fatal(err)
	}
	b1, _ := os.ReadFile(out)
	b2, _ := os.ReadFile(out2)
	if !bytes.Equal(b1, b2) {
		t.Errorf("save output not deterministic:\n%s\n---\n%s", b1, b2)
	}

	// The !color tag survives the round trip byte-for-byte.
	if !bytes.Contains(b1, []byte("long_text: !colorD30000 production")) {
		t.Errorf("saved config lost the !color tag:\n%s", b1)
	}
	// The icon key survives the round trip (literal glyph, no file).
	if !bytes.Contains(b1, []byte("icon: ⚙")) {
		t.Errorf("saved config lost the icon key:\n%s", b1)
	}
	// The object-form option (icon + long_text with the !color tag) survives.
	if !bytes.Contains(b1, []byte("- prod:\n            icon: ⚠\n            long_text: !colorD30000 production")) {
		t.Errorf("saved config lost the object-form option:\n%s", b1)
	}
	// The compound tag survives in the order it was written: default first
	// stays default first, color first stays color first.
	if !bytes.Contains(b1, []byte("- dev: !default+color7B42BC dev\n")) {
		t.Errorf("saved config lost the !default+color tag:\n%s", b1)
	}
	if !bytes.Contains(b1, []byte("- 256k: !color00FF00+default \"262144\"\n")) {
		t.Errorf("saved config lost the color-first compound tag:\n%s", b1)
	}
}

func TestSaveEmptyAliases(t *testing.T) {
	cfg := &Config{
		Theme:           "default",
		RememberLast:    10,
		CustomThemes:    map[string]map[string]string{},
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.yml")
	if err := cfg.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Aliases) != 0 {
		t.Errorf("expected no aliases, got %d", len(got.Aliases))
	}
}

func TestYamlScalar(t *testing.T) {
	cases := map[string]string{
		"plain":            "plain",
		"with space":       "with space",
		"yes":              `"yes"`,
		"123":              `"123"`,
		"# hash":           `'# hash'`,
		"a: b":             `'a: b'`,
		"line\nbreak":      `"line\nbreak"`,
		"trailing ":        `'trailing '`,
		"qwen-3.8-27B":     "qwen-3.8-27B",
		"/models/big.gguf": "/models/big.gguf",
		"64k":              "64k",
	}
	for in, want := range cases {
		if got := yamlScalar(in); got != want {
			t.Errorf("yamlScalar(%q) = %s, want %s", in, got, want)
		}
	}
}
