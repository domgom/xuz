package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	r.Close()
	return buf.String()
}

func TestDoCompletionsPrintsScript(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		out := captureStdout(t, func() {
			if code := doCompletions([]string{sh}, ""); code != 0 {
				t.Errorf("doCompletions(%q) = %d, want 0", sh, code)
			}
		})
		if !strings.Contains(out, "_xuz") {
			t.Errorf("%s: script does not define _xuz:\n%s", sh, out)
		}
		if strings.Contains(out, "__NAME__") {
			t.Errorf("%s: unrendered placeholder in the script", sh)
		}
		if strings.Contains(out, "__CFG__") {
			t.Errorf("%s: unrendered placeholder in the script", sh)
		}
	}
}

func TestDoCompletionsUnknownShell(t *testing.T) {
	if code := doCompletions([]string{"tcsh"}, ""); code != 2 {
		t.Errorf("doCompletions([\"tcsh\"]) = %d, want 2", code)
	}
}

func TestDoCompletionsGuessesShell(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/zsh")
	out := captureStdout(t, func() {
		if code := doCompletions(nil, ""); code != 0 {
			t.Errorf("doCompletions(nil) = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "compdef") {
		t.Errorf("expected the zsh script (compdef), got:\n%s", out)
	}
	t.Setenv("SHELL", "/usr/bin/tcsh")
	if code := doCompletions(nil, ""); code != 2 {
		t.Errorf("doCompletions(nil) with $SHELL=tcsh = %d, want 2", code)
	}
}

func TestDoCompletionsGeneratedForInvokedName(t *testing.T) {
	// The script is generated for the base name the binary was invoked
	// under: a binary installed as "cuz" and generating "cuz completions
	// bash" registers completions for "cuz" (the usual "xuz completions
	// bash" registers for "xuz").
	oldArgs0 := os.Args[0]
	os.Args[0] = "/bin/cuz"
	defer func() { os.Args[0] = oldArgs0 }()
	out := captureStdout(t, func() {
		doCompletions([]string{"bash"}, "")
	})
	if !strings.Contains(out, `complete -F _xuz "cuz"`) {
		t.Errorf("script not generated for the invoked name:\n%s", out)
	}
}

func TestDoCompletionsCarriesConfigPath(t *testing.T) {
	out := captureStdout(t, func() {
		doCompletions([]string{"bash"}, "/x/my config.yml")
	})
	if !strings.Contains(out, `--config '/x/my config.yml' ls --names`) {
		t.Errorf("script does not pass the config path:\n%s", out)
	}
}

func TestDoListNamesOnly(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yml")
	yaml := `
aliases:
  tf-plan:
    options:
      env:
        - dev: dev
    command: tf plan
  tf-apply:
    options:
      env:
        - dev: dev
    command: tf apply
`
	if err := os.WriteFile(cfg, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if code := doList(cfg, true); code != 0 {
			t.Errorf("doList names-only = %d, want 0", code)
		}
	})
	if out != "tf-plan\ntf-apply\n" {
		t.Errorf("names-only list = %q, want %q", out, "tf-plan\ntf-apply\n")
	}
	full := captureStdout(t, func() {
		doList(cfg, false)
	})
	for _, want := range []string{"tf-plan", "env", "dev = dev", "$ tf plan"} {
		if !strings.Contains(full, want) {
			t.Errorf("full list missing %q:\n%s", want, full)
		}
	}
}
