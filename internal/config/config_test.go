package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleYAML is the user's example config, with the context list fixed so it
// is valid YAML ("- 256k: 262144").
const sampleYAML = `
theme: dracula
remember_last: 7
aliases:
  llama:
    options:
      model:
        - qwen-3.8-27B: /home/models/Unsloth_mtp-Qwen3.8-27B-Q4_0.gguf
        - qwen-3.6-35B-A3B: !default localweights_Qwen3.6-35B-A3B-MTP-IMAT-IQ4_XS-Q8nextn.gguf
      context:
        - 64k: 65536
        - 256k: 262144
      command: exec llama-server -m "$MODEL" -c "$CONTEXT"
  whisper:
    options:
      model:
        - large-v3: /home/models/whisper-large-v3.pt
      command: whisper "$MODEL"
`

func writeCfg(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSample(t *testing.T) {
	cfg, err := Load(writeCfg(t, sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "dracula" {
		t.Errorf("theme = %q", cfg.Theme)
	}
	if cfg.RememberLast != 7 {
		t.Errorf("remember_last = %d", cfg.RememberLast)
	}
	if len(cfg.Aliases) != 2 {
		t.Fatalf("aliases = %d", len(cfg.Aliases))
	}
	a := cfg.Aliases[0]
	if a.Name != "llama" {
		t.Errorf("name = %q", a.Name)
	}
	if len(a.Groups) != 2 || a.Groups[0] != "model" || a.Groups[1] != "context" {
		t.Errorf("groups = %v", a.Groups)
	}
	if len(a.GroupPairs["model"]) != 2 {
		t.Errorf("model pairs = %v", a.GroupPairs["model"])
	}
	if got := a.GroupPairs["model"][1]; got.Key != "qwen-3.6-35B-A3B" || got.LongText != "localweights_Qwen3.6-35B-A3B-MTP-IMAT-IQ4_XS-Q8nextn.gguf" {
		t.Errorf("model[1] = %+v", got)
	}
	if got := a.GroupPairs["context"][0]; got.Key != "64k" || got.LongText != "65536" {
		t.Errorf("context[0] = %+v", got)
	}
	if got := a.GroupPairs["context"][1]; got.Key != "256k" || got.LongText != "262144" {
		t.Errorf("context[1] = %+v", got)
	}
	if a.Defaults["model"] != "qwen-3.6-35B-A3B" {
		t.Errorf("default = %q", a.Defaults["model"])
	}
	if d, ok := a.Defaults["context"]; ok && d != "" {
		t.Errorf("context should have no default, got %q", d)
	}
	if a.ResolveSelections(nil)["model"].Key != "qwen-3.6-35B-A3B" || a.ResolveSelections(nil)["context"].Key != "64k" {
		t.Errorf("selections = %v", a.ResolveSelections(nil))
	}
	if a.Command != `exec llama-server -m "$MODEL" -c "$CONTEXT"` {
		t.Errorf("command = %q", a.Command)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("warnings = %v", cfg.Warnings)
	}
}

func TestNeedle(t *testing.T) {
	// Unset: Needle is empty, NeedleGlyph falls back to the default.
	cfg, err := Load(writeCfg(t, sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Needle != "" {
		t.Errorf("needle = %q, want empty", cfg.Needle)
	}
	if got := cfg.NeedleGlyph(); got != "▸" {
		t.Errorf("NeedleGlyph = %q, want ▸", got)
	}

	// Top-level needle is parsed.
	cfg, err = Load(writeCfg(t, `
needle: ★
aliases:
  a:
    options:
      model:
        - m1: v1
      command: echo $MODEL
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Needle != "★" {
		t.Errorf("needle = %q, want ★", cfg.Needle)
	}
	if got := cfg.NeedleGlyph(); got != "★" {
		t.Errorf("NeedleGlyph = %q, want ★", got)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("warnings = %v", cfg.Warnings)
	}

	// needle under aliases: is parsed (and is not treated as an alias).
	cfg, err = Load(writeCfg(t, `
aliases:
  needle: ★
  a:
    options:
      model:
        - m1: v1
      command: echo $MODEL
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Needle != "★" {
		t.Errorf("needle = %q, want ★", cfg.Needle)
	}
	if len(cfg.Aliases) != 1 || cfg.Aliases[0].Name != "a" {
		t.Errorf("aliases = %+v, want just [a]", cfg.Aliases)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("warnings = %v", cfg.Warnings)
	}

	// Top level wins when both are present.
	cfg, err = Load(writeCfg(t, `
needle: ★
aliases:
  needle: ●
  a:
    options:
      model:
        - m1: v1
      command: echo $MODEL
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Needle != "★" {
		t.Errorf("needle = %q, want ★ (top level wins)", cfg.Needle)
	}
	if len(cfg.Aliases) != 1 || cfg.Aliases[0].Name != "a" {
		t.Errorf("aliases = %+v, want just [a]", cfg.Aliases)
	}
}

func TestNeedleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.yml")
	yaml := `
theme: dracula
needle: ★
remember_last: 3
aliases:
  llama:
    options:
      model:
        - big: /models/big.gguf
      command: llama-server -m "$MODEL"
`
	if err := os.WriteFile(src, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg1, err := Load(src)
	if err != nil {
		t.Fatal(err)
	}
	if cfg1.Needle != "★" {
		t.Fatalf("needle = %q, want ★", cfg1.Needle)
	}
	noWarnings(t, "source load", cfg1)

	out := filepath.Join(dir, "out.yml")
	if err := cfg1.Save(out); err != nil {
		t.Fatal(err)
	}
	b1, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// The needle must be written literally (no escapes) and come back.
	if !bytes.Contains(b1, []byte("needle: ★")) {
		t.Errorf("saved config lost the needle:\n%s", b1)
	}
	cfg2, err := Load(out)
	if err != nil {
		t.Fatalf("reload of saved config failed: %v", err)
	}
	noWarnings(t, "reload", cfg2)
	if cfg2.Needle != "★" {
		t.Errorf("needle after round trip = %q, want ★", cfg2.Needle)
	}
	if got := cfg2.NeedleGlyph(); got != "★" {
		t.Errorf("NeedleGlyph after round trip = %q, want ★", got)
	}

	// Saving the reloaded config is byte-for-byte deterministic.
	out2 := filepath.Join(dir, "out2.yml")
	if err := cfg2.Save(out2); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(out2)
	if !bytes.Equal(b1, b2) {
		t.Errorf("save output not deterministic:\n%s\n---\n%s", b1, b2)
	}
}

func TestResolveSelections(t *testing.T) {
	a := &Alias{
		Name:   "x",
		Groups: []string{"model", "ctx"},
		GroupPairs: map[string][]Pair{
			"model": {{Key: "small", LongText: "s"}, {Key: "big", LongText: "b"}},
			"ctx":   {{Key: "s", LongText: "10"}, {Key: "l", LongText: "20"}},
		},
		Defaults: map[string]string{"ctx": "l"},
	}
	// the group-bound history hint wins for model (no default there); the
	// !default tag on ctx beats the history hint that matches "s"
	sel := a.ResolveSelections([]Hint{
		{Group: "model", Key: "big", Source: SourceHistory},
		{Key: "s", Source: SourceHistory},
	})
	if sel["model"].Key != "big" || sel["model"].Source != SourceHistory {
		t.Errorf("model = %+v, want big/history", sel["model"])
	}
	if sel["ctx"].Key != "l" || sel["ctx"].Source != SourceDefault {
		t.Errorf("ctx = %+v, want l/default (the tag beats the hint)", sel["ctx"])
	}
	// no hints: defaults, then first item
	sel = a.ResolveSelections(nil)
	if sel["model"].Key != "small" || sel["model"].Source != SourceFirst {
		t.Errorf("model = %+v, want small/first", sel["model"])
	}
	if sel["ctx"].Key != "l" || sel["ctx"].Source != SourceDefault {
		t.Errorf("ctx = %+v, want l/default", sel["ctx"])
	}
}

func TestStaleDefaultFallsThrough(t *testing.T) {
	// A !default tag whose key no longer exists in the group is ignored: the
	// resolution falls through to the history hint, then the first option.
	a := &Alias{
		Groups:     []string{"model"},
		GroupPairs: map[string][]Pair{"model": {{Key: "small", LongText: "s"}}},
		Defaults:   map[string]string{"model": "gone"},
	}
	sel := a.ResolveSelections([]Hint{{Key: "small", Source: SourceHistory}})
	if sel["model"].Key != "small" || sel["model"].Source != SourceHistory {
		t.Errorf("model = %+v, want small/history", sel["model"])
	}
}

// TestDefaultTag covers the !default tag schema: the tagged option's key is
// preselected, an untagged group falls back to its first option, and a quoted
// value that merely contains the text "!default" is not a tag.
func TestDefaultTag(t *testing.T) {
	yaml := `
aliases:
  llama:
    options:
      model:
        - qwen-3.8-27B: /home/models/big.gguf
        - qwen-3.6-35B-A3B: !default /home/models/small.gguf
      context:
        - 64k: 65536
        - 256k: 262144
    command: echo $MODEL $CONTEXT
  dsh:
    options:
      host:
        - local: 127.0.0.1
        - tailscale: !default edge.tail5e1eaf.ts.net
    command: echo $HOST
  literal:
    options:
      host:
        - a: "!default not a tag"
        - b: value
    command: echo $HOST
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("warnings = %v", cfg.Warnings)
	}
	a := cfg.Aliases[0]
	if a.Defaults["model"] != "qwen-3.6-35B-A3B" {
		t.Errorf("llama model default = %q", a.Defaults["model"])
	}
	if d, ok := a.Defaults["context"]; ok && d != "" {
		t.Errorf("context should have no default, got %q", d)
	}
	sel := a.ResolveSelections(nil)
	if sel["model"].Key != "qwen-3.6-35B-A3B" || sel["model"].Source != SourceDefault {
		t.Errorf("llama model = %+v, want qwen-3.6-35B-A3B/default", sel["model"])
	}
	if sel["context"].Key != "64k" || sel["context"].Source != SourceFirst {
		t.Errorf("llama context = %+v, want 64k/first", sel["context"])
	}
	d := cfg.Aliases[1]
	if sel := d.ResolveSelections(nil); sel["host"].Key != "tailscale" {
		t.Errorf("dsh host = %+v", sel["host"])
	}
	l := cfg.Aliases[2]
	if d, ok := l.Defaults["host"]; ok && d != "" {
		t.Errorf("quoted literal treated as tag: %q", d)
	}
	if sel := l.ResolveSelections(nil); sel["host"].Key != "a" {
		t.Errorf("literal host = %+v", sel["host"])
	}
}

// TestSelectionPrecedenceConfig covers the selection_precedence setting:
// parsing (top level and inside aliases), the custom order changing the
// resolution, warnings for unknown levels, and the save round trip.
func TestSelectionPrecedenceConfig(t *testing.T) {
	// Top-level list, reversed: history beats the !default tag.
	yaml := `
selection_precedence: [history, default]
aliases:
  a:
    options:
      model:
        - small: s
        - big: b
      command: echo $MODEL
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("warnings = %v", cfg.Warnings)
	}
	want := []string{"history", "default"}
	if len(cfg.SelectionPrecedence) != len(want) {
		t.Fatalf("precedence = %v, want %v", cfg.SelectionPrecedence, want)
	}
	for i := range want {
		if cfg.SelectionPrecedence[i] != want[i] {
			t.Errorf("precedence[%d] = %q, want %q", i, cfg.SelectionPrecedence[i], want[i])
		}
	}
	a := cfg.Aliases[0]
	// "first" is always appended as the final fallback.
	if got := cfg.PrecedenceList(); len(got) != 3 || got[2] != SourceFirst {
		t.Errorf("effective precedence = %v, want [history default first]", got)
	}
	hints := []Hint{{Group: "model", Key: "big", Source: SourceHistory}}
	sel := a.ResolveSelectionsWith(cfg.PrecedenceList(), hints)
	if sel["model"].Key != "big" || sel["model"].Source != SourceHistory {
		t.Errorf("model = %+v, want big/history (custom order)", sel["model"])
	}

	// Unknown level: warned and dropped; a duplicate keeps its first slot.
	yaml2 := `
selection_precedence:
  - history
  - bogus
  - history
aliases:
  a:
    options:
      model:
        - m1: v1
      command: echo $MODEL
`
	cfg2, err := Load(writeCfg(t, yaml2))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg2.SelectionPrecedence; len(got) != 1 || got[0] != "history" {
		t.Errorf("precedence = %v, want [history]", got)
	}
	found := false
	for _, w := range cfg2.Warnings {
		if strings.Contains(w, `unknown level "bogus"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an unknown-level warning, got %v", cfg2.Warnings)
	}

	// All levels bogus: the built-in default stays in force.
	yaml3 := `
selection_precedence:
  - bogus
aliases:
  a:
    options:
      model:
        - m1: v1
      command: echo $MODEL
`
	cfg3, err := Load(writeCfg(t, yaml3))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg3.SelectionPrecedence) != 0 {
		t.Errorf("precedence = %v, want empty (default applies)", cfg3.SelectionPrecedence)
	}
	if got := cfg3.PrecedenceList(); len(got) != 3 || got[0] != SourceDefault || got[2] != SourceFirst {
		t.Errorf("effective precedence = %v, want the built-in default", got)
	}

	// Inside aliases: (next to the alias names), top level wins when both.
	yaml4 := `
selection_precedence: [default, first]
aliases:
  a:
    options:
      model:
        - m1: v1
      command: echo $MODEL
  selection_precedence: [history, default]
`
	cfg4, err := Load(writeCfg(t, yaml4))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg4.SelectionPrecedence; len(got) != 2 || got[0] != "default" || got[1] != "first" {
		t.Errorf("precedence = %v, want [default first] (top level wins)", got)
	}

	// Save round trip: the value is written back and comes through.
	dir := t.TempDir()
	src := filepath.Join(dir, "src.yml")
	if err := os.WriteFile(src, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg1, err := Load(src)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.yml")
	if err := cfg1.Save(out); err != nil {
		t.Fatal(err)
	}
	b1, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b1, []byte("selection_precedence:\n  - history\n  - default\n")) {
		t.Errorf("saved config lost the precedence list:\n%s", b1)
	}
	cfg2b, err := Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg2b.SelectionPrecedence) != 2 || cfg2b.SelectionPrecedence[0] != "history" {
		t.Errorf("precedence after round trip = %v", cfg2b.SelectionPrecedence)
	}
}

// TestResolveSelectionsWithCustomOrder covers the resolver under a custom
// selection_precedence: history above the !default tag.
func TestResolveSelectionsWithCustomOrder(t *testing.T) {
	a := &Alias{
		Name:   "x",
		Groups: []string{"model"},
		GroupPairs: map[string][]Pair{
			"model": {{Key: "small", LongText: "s"}, {Key: "big", LongText: "b"}},
		},
		Defaults: map[string]string{"model": "big"},
	}
	hints := []Hint{{Group: "model", Key: "small", Source: SourceHistory}}
	// Built-in order: the !default tag (big) wins over the history hint.
	sel := a.ResolveSelections(hints)
	if sel["model"].Key != "big" || sel["model"].Source != SourceDefault {
		t.Errorf("model = %+v, want big/default", sel["model"])
	}
	// history first: the history hint (small) beats the default tag.
	sel = a.ResolveSelectionsWith([]string{SourceHistory, SourceDefault}, hints)
	if sel["model"].Key != "small" || sel["model"].Source != SourceHistory {
		t.Errorf("model = %+v, want small/history", sel["model"])
	}
}

func TestDefaultKeyRemoved(t *testing.T) {
	// The <group>_default key is no longer supported: it warns, sets no
	// default, and the preselection falls back to the first option.
	yaml := `
aliases:
  a:
    options:
      model:
        - m1: v1
        - m2: v2
      model_default: m2
    command: echo $MODEL
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Aliases[0]
	if a.Defaults["model"] != "" {
		t.Errorf("default = %q, want none", a.Defaults["model"])
	}
	if got := a.ResolveSelections(nil)["model"]; got.Key != "m1" {
		t.Errorf("selection = %+v, want m1", got)
	}
	found := false
	for _, w := range a.Warnings {
		if strings.Contains(w, "no longer supported") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a removal warning, got %v", a.Warnings)
	}
}

func TestMultipleDefaultTags(t *testing.T) {
	yaml := `
aliases:
  a:
    options:
      model:
        - m1: !default v1
        - m2: !default v2
    command: echo $MODEL
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Aliases[0]
	if a.Defaults["model"] != "m1" {
		t.Errorf("default = %q, want m1 (first tag)", a.Defaults["model"])
	}
	found := false
	for _, w := range a.Warnings {
		if strings.Contains(w, "multiple !default tags") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a multiple-tags warning, got %v", a.Warnings)
	}
}

func TestColorTag(t *testing.T) {
	yaml := `
aliases:
  tf-plan:
    options:
      env:
        - dev: dev
        - prod: !colorD30000 prod
      host:
        - green: !color#00ff00 ok
        - short: !colorabc tiny
        - broken: !colorxyz nope
        - quoted: "!colorD30000 not a tag"
    command: echo $ENV $HOST
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("global warnings = %v", cfg.Warnings)
	}
	a := cfg.Aliases[0]
	env := a.GroupPairs["env"]
	if len(env) != 2 {
		t.Fatalf("env pairs = %v", env)
	}
	if env[0].Key != "dev" || env[0].LongText != "dev" || env[0].Color != "" {
		t.Errorf("dev = %+v, want plain dev", env[0])
	}
	if env[1].Key != "prod" || env[1].LongText != "prod" || env[1].Color != "#d30000" {
		t.Errorf("prod = %+v, want value prod color #d30000", env[1])
	}
	host := a.GroupPairs["host"]
	if host[0].Color != "#00ff00" {
		t.Errorf("green color = %q, want #00ff00", host[0].Color)
	}
	if host[1].Color != "#aabbcc" {
		t.Errorf("short color = %q, want #aabbcc (3-digit expanded)", host[1].Color)
	}
	if host[2].Color != "" || host[2].LongText != "nope" {
		t.Errorf("broken = %+v, want no color and value kept", host[2])
	}
	if host[3].Color != "" || host[3].LongText != "!colorD30000 not a tag" {
		t.Errorf("quoted = %+v, want plain value, no color", host[3])
	}
	found := false
	for _, w := range a.Warnings {
		if strings.Contains(w, "not a valid") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an invalid-color warning, got %v", a.Warnings)
	}
	// Colors are display-only: resolution sees the plain values.
	if sel := a.ResolveSelections(nil); sel["env"].Key != "dev" {
		t.Errorf("env = %+v, want dev/first", sel["env"])
	}
}

// TestCompoundTag covers the compound tag form: !tag1+tag2+...+tagN, the
// parts in any order. !default and !color combine; several !color parts
// keep the last one with a warning; a malformed tag (an unknown part or an
// invalid color) is ignored whole, with a warning that names the line.
func TestCompoundTag(t *testing.T) {
	yaml := `
aliases:
  tf-plan:
    options:
      env:
        - dev: !default+color7B42BC dev
        - prod: !colorD30000 prod
      host:
        - stag: !colorFFD814+default staging
        - dup: !colorD30000+colorFFD814 dup
        - bad1: !default+colorxyz broken
        - bad2: !foo+default nope
    command: echo $ENV $HOST
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("global warnings = %v", cfg.Warnings)
	}
	a := cfg.Aliases[0]
	// !default+colorXXXXXX: both effects apply, default written first.
	env := a.GroupPairs["env"]
	if len(env) != 2 {
		t.Fatalf("env pairs = %v", env)
	}
	if env[0].Key != "dev" || env[0].Color != "#7b42bc" || env[0].ColorBeforeDefault {
		t.Errorf("dev = %+v, want color #7b42bc, default first", env[0])
	}
	if d := a.Defaults["env"]; d != "dev" {
		t.Errorf("env default = %q, want dev", d)
	}
	// !colorXXXXXX+default: the same, color written first.
	host := a.GroupPairs["host"]
	if len(host) != 4 {
		t.Fatalf("host pairs = %v", host)
	}
	if host[0].Key != "stag" || host[0].Color != "#ffd814" || !host[0].ColorBeforeDefault {
		t.Errorf("stag = %+v, want color #ffd814, color first", host[0])
	}
	if d := a.Defaults["host"]; d != "stag" {
		t.Errorf("host default = %q, want stag", d)
	}
	// Several !color parts: the last one wins, with a warning.
	if host[1].Key != "dup" || host[1].Color != "#ffd814" {
		t.Errorf("dup = %+v, want the last color #ffd814", host[1])
	}
	// Malformed tags: nothing applies, the value is kept, it warns with the
	// line.
	if host[2].Color != "" || host[2].LongText != "broken" {
		t.Errorf("bad1 = %+v, want no color, value kept", host[2])
	}
	if host[3].Color != "" || host[3].LongText != "nope" {
		t.Errorf("bad2 = %+v, want no color, value kept", host[3])
	}
	if len(a.Warnings) != 3 {
		t.Fatalf("warnings = %v, want 3", a.Warnings)
	}
	for _, line := range []string{"(line 10)", "(line 11)", "(line 12)"} {
		found := false
		for _, w := range a.Warnings {
			if strings.Contains(w, line) {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a warning naming %s, got %v", line, a.Warnings)
		}
	}
	foundMulti := false
	for _, w := range a.Warnings {
		if strings.Contains(w, "multiple !color tags, using the last one") {
			foundMulti = true
		}
	}
	if !foundMulti {
		t.Errorf("expected a multiple-colors warning, got %v", a.Warnings)
	}
	// The compound !default part preselects: resolution sees the defaults.
	sel := a.ResolveSelections(nil)
	if sel["env"].Key != "dev" || sel["env"].Source != SourceDefault {
		t.Errorf("env = %+v, want dev/default", sel["env"])
	}
	if sel["host"].Key != "stag" || sel["host"].Source != SourceDefault {
		t.Errorf("host = %+v, want stag/default", sel["host"])
	}
}

func TestUnknownKeysWarn(t *testing.T) {
	yaml := `
bogus: 1
aliases:
  a:
    options:
      model:
        - m1: v1
    nonsense: true
    command: echo $MODEL
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) == 0 || len(cfg.Aliases[0].Warnings) == 0 {
		t.Errorf("expected warnings, got %v / %v", cfg.Warnings, cfg.Aliases[0].Warnings)
	}
}

// TestSettingsInsideAliases covers the user's real layout: remember_last and
// selection_precedence sit directly under aliases:, next to the alias names.
func TestSettingsInsideAliases(t *testing.T) {
	yaml := `
aliases:
  llama:
    options:
      model:
        - qwen-3.8-27B: /home/models/Unsloth_mtp-Qwen3.8-27B-Q4_0.gguf
        - qwen-3.6-35B-A3B: !default localweights_Qwen3.6-35B-A3B-MTP-IMAT-IQ4_XS-Q8nextn.gguf
      context:
        - 64k: 65536
        - 256k: 262144
      command: exec llama-server -m "$MODEL" -c "$CONTEXT"
  selection_precedence: [history, default]
  remember_last: 10
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("warnings = %v", cfg.Warnings)
	}
	if len(cfg.Aliases) != 1 || cfg.Aliases[0].Name != "llama" {
		t.Fatalf("aliases = %+v", cfg.Aliases)
	}
	if got := cfg.SelectionPrecedence; len(got) != 2 || got[0] != "history" || got[1] != "default" {
		t.Errorf("selection_precedence = %v, want [history default]", got)
	}
	if cfg.RememberLast != 10 {
		t.Errorf("remember_last = %d", cfg.RememberLast)
	}
	if cfg.Aliases[0].RememberLast != 10 {
		t.Errorf("alias remember_last = %d", cfg.Aliases[0].RememberLast)
	}
}

// TestSettingsInsideAliasesTopLevelWins: when the same setting exists at both
// levels, the top-level value wins — even when it appears later in the file.
func TestSettingsInsideAliasesTopLevelWins(t *testing.T) {
	yaml := `
aliases:
  a:
    options:
      model:
        - m1: v1
      command: echo $MODEL
  selection_precedence: [history, default]
  remember_last: 10
selection_precedence: [default, first]
remember_last: 5
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RememberLast != 5 {
		t.Errorf("remember_last = %d", cfg.RememberLast)
	}
	if got := cfg.SelectionPrecedence; len(got) != 2 || got[0] != "default" || got[1] != "first" {
		t.Errorf("selection_precedence = %v, want [default first]", got)
	}
	if cfg.Aliases[0].RememberLast != 5 {
		t.Errorf("alias remember_last = %d", cfg.Aliases[0].RememberLast)
	}
}

func TestCommandAtAliasLevel(t *testing.T) {
	yaml := `
aliases:
  a:
    options:
      model:
        - m1: v1
    command: echo $MODEL
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Aliases[0].Command != "echo $MODEL" {
		t.Errorf("command = %q", cfg.Aliases[0].Command)
	}
}

// TestIconKey covers the literal icon glyph: it renders as-is (no file
// lookup, no cache), a !color tag colors it (icons take !color only), and an
// invalid color tag warns like it does on option values.
func TestIconKey(t *testing.T) {
	yaml := `
aliases:
  tf-plan:
    icon: ⚙
    options:
      env:
        - dev: !default dev
        - stag: staging
        - prod: !colorD30000 prod
    command: exec tf plan -var-file=env_vars/workloads-$ENV.tfvars
  colored:
    icon: !colorFF5555 🦙
    command: echo colored
  broken:
    icon: !colorxyz ⚙
    command: echo broken
  flat:
    icon: 🚀
    host:
      - local: 127.0.0.1
    command: echo $HOST
  bare:
    command: echo bare
`
	cfg, err := Load(writeCfg(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("global warnings = %v", cfg.Warnings)
	}
	if got := cfg.Aliases[0].Icon; got != "⚙" {
		t.Errorf("tf-plan icon = %q, want ⚙", got)
	}
	if cfg.Aliases[0].IconColor != "" {
		t.Errorf("tf-plan icon color = %q, want none", cfg.Aliases[0].IconColor)
	}
	// The icon is a display-only alias attribute: it does not become an
	// option group and it does not disturb the groups.
	if got := len(cfg.Aliases[0].Groups); got != 1 || cfg.Aliases[0].Groups[0] != "env" {
		t.Errorf("tf-plan groups = %v, want [env]", got)
	}
	if len(cfg.Aliases[0].Warnings) != 0 {
		t.Errorf("tf-plan warnings = %v", cfg.Aliases[0].Warnings)
	}
	if got := cfg.Aliases[1].Icon; got != "🦙" {
		t.Errorf("colored icon = %q, want 🦙", got)
	}
	if got := cfg.Aliases[1].IconColor; got != "#ff5555" {
		t.Errorf("colored icon color = %q, want #ff5555", got)
	}
	if len(cfg.Aliases[1].Warnings) != 0 {
		t.Errorf("colored warnings = %v", cfg.Aliases[1].Warnings)
	}
	if got := cfg.Aliases[2].Icon; got != "⚙" {
		t.Errorf("broken icon = %q, want ⚙ (the glyph stays)", got)
	}
	if cfg.Aliases[2].IconColor != "" {
		t.Errorf("broken icon color = %q, want none", cfg.Aliases[2].IconColor)
	}
	found := false
	for _, w := range cfg.Aliases[2].Warnings {
		if strings.Contains(w, "not a valid !color tag") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an invalid-color warning, got %v", cfg.Aliases[2].Warnings)
	}
	if got := cfg.Aliases[3].Icon; got != "🚀" {
		t.Errorf("flat icon = %q, want 🚀", got)
	}
	if got := len(cfg.Aliases[3].Groups); got != 1 || cfg.Aliases[3].Groups[0] != "host" {
		t.Errorf("flat groups = %v, want [host]", got)
	}
	if got := cfg.Aliases[4].Icon; got != "" {
		t.Errorf("bare icon = %q, want empty", got)
	}
}
