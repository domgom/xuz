package complete

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestScriptSubstitutesName(t *testing.T) {
	for _, sh := range Shells {
		script, ok := Script(sh, "cuz", "")
		if !ok {
			t.Errorf("Script(%q, \"cuz\", \"\") ok = false, want true", sh)
			continue
		}
		if strings.Contains(script, namePlaceholder) {
			t.Errorf("%s: unrendered %s in the script", sh, namePlaceholder)
		}
		if strings.Contains(script, cfgPlaceholder) {
			t.Errorf("%s: unrendered %s in the script", sh, cfgPlaceholder)
		}
		if !strings.Contains(script, "cuz") {
			t.Errorf("%s: the command name was not substituted", sh)
		}
	}
}

func TestScriptRegistrations(t *testing.T) {
	// Each script registers the completion under the generated command name
	// and reads the alias names from the config on every completion.
	bash, _ := Script("bash", "cuz", "")
	for _, want := range []string{
		`complete -F _xuz "cuz"`,
		`cuz ls --names`,
		`run|show)`,
	} {
		if !strings.Contains(bash, want) {
			t.Errorf("bash script missing %q", want)
		}
	}
	zsh, _ := Script("zsh", "cuz", "")
	for _, want := range []string{
		`compdef _xuz "cuz"`,
		`cuz ls --names`,
	} {
		if !strings.Contains(zsh, want) {
			t.Errorf("zsh script missing %q", want)
		}
	}
	fish, _ := Script("fish", "cuz", "")
	for _, want := range []string{
		`complete -c cuz`,
		`cuz ls --names`,
		`__fish_is_first_arg`,
		`__fish_seen_subcommand_from run show`,
	} {
		if !strings.Contains(fish, want) {
			t.Errorf("fish script missing %q", want)
		}
	}
}

func TestScriptUnknownShell(t *testing.T) {
	for _, sh := range []string{"", "tcsh", "powershell", "elvish", "BASHX"} {
		if _, ok := Script(sh, "xuz", ""); ok {
			t.Errorf("Script(%q, \"xuz\", \"\") ok = true, want false", sh)
		}
	}
	if _, ok := Script("BASH", "xuz", ""); !ok {
		t.Error("Script(\"BASH\", \"xuz\", \"\") ok = false, want true (case-insensitive)")
	}
}

// TestScriptDefaultName covers the empty name: the scripts fall back to
// "xuz".
func TestScriptDefaultName(t *testing.T) {
	script, ok := Script("bash", "", "")
	if !ok {
		t.Fatal("Script(\"bash\", \"\", \"\") ok = false, want true")
	}
	if !strings.Contains(script, `complete -F _xuz "xuz"`) {
		t.Errorf("bash script missing the xuz registration:\n%s", script)
	}
}

// TestScriptConfigPath covers a --config PATH given at generation time: the
// scripts pass it to every binary call, quoted for the shell.
func TestScriptConfigPath(t *testing.T) {
	for _, sh := range Shells {
		script, ok := Script(sh, "cuz", "/x/my config.yml")
		if !ok {
			t.Errorf("Script(%q, ...) ok = false, want true", sh)
			continue
		}
		if !strings.Contains(script, `--config '/x/my config.yml' ls --names`) {
			t.Errorf("%s: the config path is not passed to the binary:\n%s", sh, script)
		}
	}
	script, _ := Script("bash", "cuz", "/x/it's.yml")
	if !strings.Contains(script, `--config '/x/it'\''s.yml' ls --names`) {
		t.Errorf("bash: an embedded single quote is not escaped:\n%s", script)
	}
}

// TestBashScriptSyntax parses the generated bash script (bash -n).
func TestBashScriptSyntax(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	script, ok := Script("bash", "cuz", "")
	if !ok {
		t.Fatal("Script(\"bash\", \"cuz\", \"\") ok = false, want true")
	}
	cmd := exec.Command(bash, "-n")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("bash -n: %v\n%s", err, out)
	}
}

// TestBashCompletionEndToEnd sources the generated script in a real bash
// (with a stub binary on PATH) and drives the completion function with
// COMP_WORDS, the same way the readline completion would. It checks that
// the user-configured alias names ("tf-plan", "tf-apply") are completed as
// the first argument and after "run", that the subcommands and flags are
// completed, and that the completion is registered under the generated
// command name (here "cuz") and for a shell alias that runs the binary.
func TestBashCompletionEndToEnd(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	dir := t.TempDir()
	bin := dir + "/bin"
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stand-in for the real binary: the completion script only ever calls
	// "cuz ls --names" and "cuz themes".
	stub := bin + "/cuz"
	if err := os.WriteFile(stub, []byte(`#!/bin/sh
if [ "$1" = ls ] && [ "$2" = --names ]; then
	printf 'tf-plan\ntf-apply\n'
elif [ "$1" = themes ]; then
	printf 'default\ndracula\n'
fi
`), 0o755); err != nil {
		t.Fatal(err)
	}
	script, ok := Script("bash", "cuz", "")
	if !ok {
		t.Fatal("Script(\"bash\", \"cuz\", \"\") ok = false, want true")
	}
	scriptPath := dir + "/completion.bash"
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	driver := `
export PATH="` + bin + `:$PATH"
# an alias that runs the binary, defined before the script is sourced (as in
# a real rc file): the script must register the completion for it too
alias other=cuz
. ` + scriptPath + `

replies() {
	local out="" c
	for c in "${COMPREPLY[@]}"; do out="$out$c|"; done
	printf '%s: %s\n' "$1" "$out"
}

# Each case sets COMP_WORDS/COMP_CWORD the way readline would: the word
# being completed is the last element (empty right after a space).
case1() { COMP_WORDS=(cuz tf-pl); COMP_CWORD=1; COMPREPLY=(); _xuz; replies "cuz tf-pl"; }
case2() { COMP_WORDS=(cuz ""); COMP_CWORD=1; COMPREPLY=(); _xuz; replies "cuz "; }
case3() { COMP_WORDS=(cuz run tf-pl); COMP_CWORD=2; COMPREPLY=(); _xuz; replies "cuz run tf-pl"; }
case4() { COMP_WORDS=(cuz --them); COMP_CWORD=1; COMPREPLY=(); _xuz; replies "cuz --them"; }
case5() { COMP_WORDS=(cuz --theme ""); COMP_CWORD=2; COMPREPLY=(); _xuz; replies "cuz --theme "; }
case6() { COMP_WORDS=(cuz completions ""); COMP_CWORD=2; COMPREPLY=(); _xuz; replies "cuz completions "; }
case7() { COMP_WORDS=(cuz list ""); COMP_CWORD=2; COMPREPLY=(); _xuz; replies "cuz list "; }
case8() { COMP_WORDS=(cuz run --dry-run tf-pl); COMP_CWORD=3; COMPREPLY=(); _xuz; replies "cuz run --dry-run tf-pl"; }
case1; case2; case3; case4; case5; case6; case7; case8
complete -p other
`
	cmd := exec.Command(bashPath, "-c", driver)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -c failed: %v\n%s", err, out)
	}
	text := string(out)
	t.Logf("terminal output:\n%s", text)
	for _, want := range []string{
		"cuz tf-pl: tf-plan|", // the feature-request example
		"cuz : run|list|ls|themes|init|show|completions|help|tf-plan|tf-apply|",
		"cuz run tf-pl: tf-plan|",
		"cuz --them: --theme|",
		"cuz --theme : default|dracula|",
		"cuz completions : bash|zsh|fish|",
		"cuz list : ", // list takes no argument
		"cuz run --dry-run tf-pl: tf-plan|",
		"complete -F _xuz other", // the alias got registered
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q", want)
		}
	}
}
