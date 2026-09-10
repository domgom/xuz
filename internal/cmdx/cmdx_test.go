package cmdx

import (
	"testing"
)

func TestRenderSubstitutesKnownVars(t *testing.T) {
	vars := map[string]string{"MODEL": "/m/a.gguf", "CONTEXT": "65536"}
	got, refs := Render(`exec llama-server -m "$MODEL" -c "$CONTEXT" -t 4`, vars)
	want := `exec llama-server -m "/m/a.gguf" -c "65536" -t 4`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if len(refs) != 2 || refs[0].Name != "MODEL" || refs[1].Name != "CONTEXT" {
		t.Errorf("refs = %+v", refs)
	}
}

func TestRenderLeavesUnknownVars(t *testing.T) {
	vars := map[string]string{"MODEL": "/m/a.gguf"}
	got, _ := Render(`echo ${MODEL} ${MISSING} $HOME`, vars)
	want := `echo /m/a.gguf ${MISSING} $HOME`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRenderNoVars(t *testing.T) {
	got, refs := Render(`echo hello`, nil)
	if got != `echo hello` || len(refs) != 0 {
		t.Errorf("got %q refs=%+v", got, refs)
	}
}

func TestRefsSpans(t *testing.T) {
	cmd := `a $X b ${Y} c`
	refs := Refs(cmd)
	if len(refs) != 2 {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0].Name != "X" || cmd[refs[0].Start:refs[0].End] != "$X" {
		t.Errorf("ref0 = %+v", refs[0])
	}
	if refs[1].Name != "Y" || cmd[refs[1].Start:refs[1].End] != "${Y}" {
		t.Errorf("ref1 = %+v", refs[1])
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":        "'plain'",
		"with space":   "'with space'",
		"it's":         "'it'\\''s'",
		"semi;colon":   "'semi;colon'",
		"$(cmd)":       "'$(cmd)'",
		"back`tick":    "'back`tick'",
		"":             "''",
		"a\"b":         "'a\"b'",
	}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExportPrefix(t *testing.T) {
	got := ExportPrefix(map[string]string{"CONTEXT": "65536", "MODEL": "/m/a.gguf"})
	want := "CONTEXT='65536' MODEL='/m/a.gguf' "
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got := ExportPrefix(nil); got != "" {
		t.Errorf("empty prefix = %q, want empty", got)
	}
	// Values with shell metacharacters are quoted.
	got = ExportPrefix(map[string]string{"A": "x y'z"})
	want = "A='x y'\\''z' "
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestEvalTemplate(t *testing.T) {
	vars := map[string]string{"CONTEXT": "65536", "MODEL": "/m/a.gguf"}
	got, err := EvalTemplate(`ctx_kb={{ div (int .CONTEXT) 1024 }}`, vars)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ctx_kb=64" {
		t.Errorf("got %q", got)
	}
	got, err = EvalTemplate(`{{ .MODEL | upper }}`, vars)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/M/A.GGUF" {
		t.Errorf("got %q", got)
	}
}

func TestEvalTemplateEmpty(t *testing.T) {
	got, err := EvalTemplate("", nil)
	if err != nil || got != "" {
		t.Errorf("got %q err=%v", got, err)
	}
}

func TestEvalTemplateError(t *testing.T) {
	// A malformed template (unclosed action) is a parse error.
	if _, err := EvalTemplate(`{{ .X`, map[string]string{}); err == nil {
		t.Error("expected a parse error for an unclosed action")
	}
	// A division by zero is an execution error.
	if _, err := EvalTemplate(`{{ div 1 0 }}`, map[string]string{}); err == nil {
		t.Error("expected an error for division by zero")
	}
}

func TestParseDerivedVars(t *testing.T) {
	got, err := ParseDerivedVars("CTX_KB=64\nMODEL_BASE=/m\n# a comment\n\nOTHER=x y")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"CTX_KB": "64", "MODEL_BASE": "/m", "OTHER": "x y"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseDerivedVarsEmpty(t *testing.T) {
	got, err := ParseDerivedVars("")
	if err != nil || len(got) != 0 {
		t.Errorf("got %v err=%v", got, err)
	}
}

func TestParseDerivedVarsError(t *testing.T) {
	if _, err := ParseDerivedVars("no equals sign"); err == nil {
		t.Error("expected an error for a line without '='")
	}
	if _, err := ParseDerivedVars("1BAD=x"); err == nil {
		t.Error("expected an error for an invalid var name")
	}
}

func TestTemplateFuncsPure(t *testing.T) {
	// The function set must not expose anything that touches the system.
	for _, name := range []string{"exec", "open", "read", "http", "env"} {
		if _, ok := TemplateFuncs[name]; ok {
			t.Errorf("TemplateFuncs must not expose %q", name)
		}
	}
	// A few of the safe functions are present and behave as documented.
	if TemplateFuncs["upper"].(func(string) string)("ab") != "AB" {
		t.Error("upper func missing or wrong")
	}
	if TemplateFuncs["join"].(func(string, ...string) string)(",", "a", "b") != "a,b" {
		t.Error("join func missing or wrong")
	}
}
