package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// iconRoundTripYAML exercises every icon form: literal emoji and nerd-font
// glyphs (the plain and the multi-glyph/spaced spellings), icons carrying a
// !color tag (aliases and options alike), and an alias with no icon.
const iconRoundTripYAML = `
theme: dracula
remember_last: 3
aliases:
  llama:
    icon: 🦙
    options:
      model:
        - big:
            icon: 🐘
            long_text: !default /models/big.gguf
        - small: /models/small.gguf
      vars:
        MODEL: model
    command: llama-server -m "$MODEL"
  rockets:
    icon: "🚀 🛠"
    command: echo hi
  tf-plan:
    icon: !colorFF5555 ⚙
    options:
      env:
        - dev:
        - prod:
            icon: ⚠
            long_text: production
    command: TF_ENV=$ENV tf plan
  bare:
    command: echo bare
`

func TestSaveRoundTripIcons(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.yml")
	if err := os.WriteFile(src, []byte(iconRoundTripYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	wantIcons := map[string][2]string{
		"llama":   {"🦙", ""},
		"rockets": {"🚀 🛠", ""},
		"tf-plan": {"⚙", "#ff5555"},
		"bare":    {"", ""},
	}

	cfg1, err := Load(src)
	if err != nil {
		t.Fatal(err)
	}
	checkIcons(t, cfg1, wantIcons)
	noWarnings(t, "source load", cfg1)
	// The option icons come through too.
	if got := cfg1.Aliases[0].GroupPairs["model"]; got[0].Icon != "🐘" || got[0].LongText != "/models/big.gguf" {
		t.Errorf("model[0] = %+v, want icon 🐘 long_text /models/big.gguf", got[0])
	}
	if got := cfg1.Aliases[2].GroupPairs["env"]; got[1].Icon != "⚠" || got[1].LongText != "production" {
		t.Errorf("env[1] = %+v, want icon ⚠ long_text production", got[1])
	}

	// Save -> reload: the icon of every kind must come back unchanged.
	out := filepath.Join(dir, "out.yml")
	if err := cfg1.Save(out); err != nil {
		t.Fatal(err)
	}
	b1, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(out)
	if err != nil {
		t.Fatalf("reload of saved config failed: %v", err)
	}
	noWarnings(t, "reload", cfg2)
	checkIcons(t, cfg2, wantIcons)
	if !reflect.DeepEqual(cfg1.Aliases, cfg2.Aliases) {
		t.Errorf("aliases differ after round trip:\n%+v\nvs\n%+v", cfg1.Aliases, cfg2.Aliases)
	}

	// The glyphs must stay literal in the saved file (no "\U0001F999"
	// escapes), and the icon's !color tag must survive.
	for _, needle := range []string{"icon: 🦙", "icon: 🚀 🛠", "icon: !colorFF5555 ⚙", "icon: ⚠"} {
		if !bytes.Contains(b1, []byte(needle)) {
			t.Errorf("saved config lost %q:\n%s", needle, b1)
		}
	}
	if bytes.Contains(b1, []byte("\\U")) {
		t.Errorf("saved config escaped a non-BMP character:\n%s", b1)
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

func checkIcons(t *testing.T, cfg *Config, want map[string][2]string) {
	t.Helper()
	if len(cfg.Aliases) != len(want) {
		t.Fatalf("alias count = %d, want %d", len(cfg.Aliases), len(want))
	}
	for _, a := range cfg.Aliases {
		w, ok := want[a.Name]
		if !ok {
			t.Errorf("unexpected alias %q", a.Name)
			continue
		}
		if a.Icon != w[0] {
			t.Errorf("alias %s icon = %q, want %q", a.Name, a.Icon, w[0])
		}
		if a.IconColor != w[1] {
			t.Errorf("alias %s icon color = %q, want %q", a.Name, a.IconColor, w[1])
		}
	}
}

func noWarnings(t *testing.T, where string, cfg *Config) {
	t.Helper()
	for _, w := range cfg.Warnings {
		t.Errorf("%s: warning: %s", where, w)
	}
	for _, a := range cfg.Aliases {
		for _, w := range a.Warnings {
			t.Errorf("%s: warning for %s: %s", where, a.Name, w)
		}
	}
}

func TestYamlScalarEmoji(t *testing.T) {
	// Non-BMP glyphs come out literal when the plain form is unambiguous.
	if got := yamlScalar("🦙"); got != "🦙" {
		t.Errorf("yamlScalar(🦙) = %s, want the literal glyph", got)
	}
	if got := yamlScalar("🚀 🛠"); got != "🚀 🛠" {
		t.Errorf("yamlScalar(🚀 🛠) = %s, want the literal glyphs", got)
	}
	// BMP non-ASCII was already literal.
	if got := yamlScalar("héllo"); got != "héllo" {
		t.Errorf("yamlScalar(héllo) = %s, want héllo", got)
	}
	// A plain-unsafe string keeps yaml.v3's quoting (still valid YAML).
	for _, in := range []string{"🦙: x", "🦙 ", " 🦙", "-🦙", "🦙 # comment"} {
		if got := yamlScalar(in); !strings.HasPrefix(got, `"`) {
			t.Errorf("yamlScalar(%q) = %s, want a quoted scalar", in, got)
		}
	}
}
