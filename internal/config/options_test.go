package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOptionObjectForm covers the new schema: key with an object of
// icon + long_text. long_text may be omitted (then it is the key text),
// !default / !color tags ride on the long_text, and icons take !color only.
func TestOptionObjectForm(t *testing.T) {
	yaml := `
aliases:
  a:
    options:
      model:
        - small:
            icon: 🐘
            long_text: /models/small.gguf
        - big:
            icon: !color00ff00 🦛
            long_text: !default /models/big.gguf
        - mid:
            icon: !colorxyz 🐎
        - multi:
            icon: !colorFF5555+color00ff00 🐐
        - badicon:
            icon: !default+colorFF5555 ⚙
      command: echo $MODEL
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Aliases[0]
	m := a.GroupPairs["model"]
	if len(m) != 5 {
		t.Fatalf("model pairs = %+v", m)
	}
	if m[0].Key != "small" || m[0].Icon != "🐘" || m[0].IconColor != "" || m[0].LongText != "/models/small.gguf" || m[0].Color != "" {
		t.Errorf("small = %+v, want icon 🐘 long_text /models/small.gguf", m[0])
	}
	if m[1].Key != "big" || m[1].Icon != "🦛" || m[1].IconColor != "#00ff00" || m[1].LongText != "/models/big.gguf" || m[1].Color != "" {
		t.Errorf("big = %+v, want colored icon + default long_text", m[1])
	}
	// Invalid icon color: the glyph stays, the color is dropped, it warns
	// like an invalid color on an option value.
	if m[2].Key != "mid" || m[2].Icon != "🐎" || m[2].IconColor != "" {
		t.Errorf("mid = %+v, want glyph kept and no color", m[2])
	}
	// Icon compound tag: icons take !color parts only, the last one wins
	// with a warning.
	if m[3].Key != "multi" || m[3].Icon != "🐐" || m[3].IconColor != "#00ff00" {
		t.Errorf("multi = %+v, want the last icon color #00ff00", m[3])
	}
	// A non-color part (!default) on an icon tag: malformed, the whole tag
	// is ignored with a warning.
	if m[4].Key != "badicon" || m[4].Icon != "⚙" || m[4].IconColor != "" {
		t.Errorf("badicon = %+v, want glyph kept and no color", m[4])
	}
	found := false
	foundMulti := false
	for _, w := range a.Warnings {
		if strings.Contains(w, "not a valid !color tag") {
			found = true
		}
		if strings.Contains(w, "multiple !color tags, using the last one") {
			foundMulti = true
		}
	}
	if !found {
		t.Errorf("expected an invalid-color warning, got %v", a.Warnings)
	}
	if !foundMulti {
		t.Errorf("expected a multiple-colors warning, got %v", a.Warnings)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("global warnings = %v", cfg.Warnings)
	}
	if a.Defaults["model"] != "big" {
		t.Errorf("default = %q, want big (tagged on the long_text)", a.Defaults["model"])
	}
	if sel := a.ResolveSelections(nil); sel["model"].Key != "big" || sel["model"].Source != SourceDefault {
		t.Errorf("model = %+v, want big/default", sel["model"])
	}
}

// TestOptionNoPairForm covers the no-pair form: "- key:" (an empty scalar)
// means long_text = the key text and no icon; a bare "- key" in the list
// does the same.
func TestOptionNoPairForm(t *testing.T) {
	yaml := `
aliases:
  a:
    options:
      env:
        - dev:
        - prod:
            icon: ⚠
            long_text: production
      bare:
        - x
      command: echo $ENV $BARE
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("warnings = %v", cfg.Warnings)
	}
	a := cfg.Aliases[0]
	env := a.GroupPairs["env"]
	if len(env) != 2 {
		t.Fatalf("env pairs = %+v", env)
	}
	if env[0].Key != "dev" || env[0].LongText != "dev" || env[0].Icon != "" {
		t.Errorf("dev = %+v, want no-pair: long_text = key text, no icon", env[0])
	}
	if env[1].Key != "prod" || env[1].LongText != "production" || env[1].Icon != "⚠" {
		t.Errorf("prod = %+v, want icon ⚠ long_text production", env[1])
	}
	b := a.GroupPairs["bare"]
	if len(b) != 1 || b[0].Key != "x" || b[0].LongText != "x" || b[0].Icon != "" {
		t.Errorf("bare = %+v, want long_text = key text, no icon", b)
	}
}

// TestOptionScalarCompat covers the old scalar form: the scalar is the
// long_text, no icon; the tags keep their meaning on the scalar.
func TestOptionScalarCompat(t *testing.T) {
	yaml := `
aliases:
  a:
    options:
      model:
        - qwen-3.8-27B: /models/big.gguf
        - qwen-3.6-35B-A3B: !default /models/small.gguf
        - tiny: !colorD30000 tiny.gguf
      command: echo $MODEL
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("warnings = %v", cfg.Warnings)
	}
	a := cfg.Aliases[0]
	m := a.GroupPairs["model"]
	if m[0].Icon != "" || m[0].LongText != "/models/big.gguf" || m[0].Color != "" {
		t.Errorf("qwen-3.8-27B = %+v, want plain scalar form", m[0])
	}
	if m[1].Icon != "" || m[1].LongText != "/models/small.gguf" || m[1].Color != "" {
		t.Errorf("qwen-3.6-35B-A3B = %+v, want !default scalar form", m[1])
	}
	if m[2].Icon != "" || m[2].LongText != "tiny.gguf" || m[2].Color != "#d30000" {
		t.Errorf("tiny = %+v, want !color scalar form", m[2])
	}
	if a.Defaults["model"] != "qwen-3.6-35B-A3B" {
		t.Errorf("default = %q, want qwen-3.6-35B-A3B", a.Defaults["model"])
	}
}

// TestOptionObjectWarnings covers the warnings of the object form: unknown
// keys inside the object, a missing long_text falling back to the key text,
// and multiple !default tags (on long_texts).
func TestOptionObjectWarnings(t *testing.T) {
	yaml := `
aliases:
  a:
    options:
      model:
        - m1:
            icon: 🐘
            label: not a key
        - m2:
            long_text: !default v2
        - m3:
            long_text: !default v3
      command: echo $MODEL
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Aliases[0]
	m := a.GroupPairs["model"]
	if m[0].LongText != "m1" {
		t.Errorf("m1 long_text = %q, want the key text (long_text omitted)", m[0].LongText)
	}
	unknown, multi := false, false
	for _, w := range a.Warnings {
		if strings.Contains(w, "ignoring unknown key \"label\"") {
			unknown = true
		}
		if strings.Contains(w, "multiple !default tags") {
			multi = true
		}
	}
	if !unknown {
		t.Errorf("expected an unknown-key warning, got %v", a.Warnings)
	}
	if !multi {
		t.Errorf("expected a multiple-defaults warning, got %v", a.Warnings)
	}
	if a.Defaults["model"] != "m2" {
		t.Errorf("default = %q, want m2 (first tag)", a.Defaults["model"])
	}
}

// TestSavePairForms covers the save round-trip of every pair form:
// icon -> object form (long_text omitted when it equals the key, the icon
// keeps its !color tag); no icon + long_text = key -> bare "- key:";
// no icon + different long_text -> scalar form.
func TestSavePairForms(t *testing.T) {
	yaml := `
aliases:
  a:
    options:
      model:
        - dev:
        - small: /models/small.gguf
        - big:
            icon: 🦛
            long_text: !default+colorFF5555 /models/big.gguf
        - tiny:
            icon: !colorFF5555 🐘
      command: echo $MODEL
`
	dir := t.TempDir()
	src := filepath.Join(dir, "src.yml")
	if err := os.WriteFile(src, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg1, err := Load(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg1.Warnings) != 0 || len(cfg1.Aliases[0].Warnings) != 0 {
		t.Errorf("unexpected warnings: %v / %v", cfg1.Warnings, cfg1.Aliases[0].Warnings)
	}
	out := filepath.Join(dir, "out.yml")
	if err := cfg1.Save(out); err != nil {
		t.Fatal(err)
	}
	b1, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	saved := string(b1)

	// No icon, long_text = key: the bare no-pair form.
	if !strings.Contains(saved, "- dev:\n") {
		t.Errorf("dev must be saved as the bare form:\n%s", saved)
	}
	// No icon, different long_text: the scalar form.
	if !strings.Contains(saved, "- small: /models/small.gguf\n") {
		t.Errorf("small must be saved as the scalar form:\n%s", saved)
	}
	// Icon: the object form, long_text carries the !default tag.
	if !strings.Contains(saved, "- big:\n            icon: 🦛\n            long_text: !default+colorFF5555 /models/big.gguf\n") {
		t.Errorf("big must be saved as the object form with the compound tag:\n%s", saved)
	}
	// Icon with a !color tag and long_text = key: the tag is kept on the
	// icon and the long_text line is omitted.
	if !strings.Contains(saved, "- tiny:\n            icon: !colorFF5555 🐘\n    command:") {
		t.Errorf("tiny must be saved with the icon !color tag and no long_text:\n%s", saved)
	}

	// Reload: every pair comes back unchanged.
	cfg2, err := Load(out)
	if err != nil {
		t.Fatalf("reload of saved config failed: %v", err)
	}
	if len(cfg2.Warnings) != 0 {
		t.Errorf("reload warnings = %v", cfg2.Warnings)
	}
	for _, a := range cfg2.Aliases {
		if len(a.Warnings) != 0 {
			t.Errorf("reload warnings for %s = %v", a.Name, a.Warnings)
		}
	}
	want := map[string][]Pair{
		"model": {
			{Key: "dev", LongText: "dev"},
			{Key: "small", LongText: "/models/small.gguf"},
			{Key: "big", Icon: "🦛", Color: "#ff5555", LongText: "/models/big.gguf"},
			{Key: "tiny", Icon: "🐘", IconColor: "#ff5555", LongText: "tiny"},
		},
	}
	got := cfg2.Aliases[0].GroupPairs["model"]
	if len(got) != len(want["model"]) {
		t.Fatalf("model pairs = %+v, want %+v", got, want["model"])
	}
	for i := range want["model"] {
		if got[i] != want["model"][i] {
			t.Errorf("model[%d] = %+v, want %+v", i, got[i], want["model"][i])
		}
	}
	if d := cfg2.Aliases[0].Defaults["model"]; d != "big" {
		t.Errorf("default = %q, want big", d)
	}

	// Saving the reloaded config is byte-for-byte deterministic.
	out2 := filepath.Join(dir, "out2.yml")
	if err := cfg2.Save(out2); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(out2)
	if string(b1) != string(b2) {
		t.Errorf("save output not deterministic:\n%s\n---\n%s", b1, b2)
	}
}
