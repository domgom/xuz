// Package cmdx renders xuz commands for display and builds the environment
// the command runs with. The command string itself is never rewritten for
// execution: it is passed verbatim to the shell, which resolves the $VAR and
// ${VAR} references from the environment. This package only produces the
// display form (the live preview, the dry-run output, the clipboard) and the
// env-var assignments, so every surface shows exactly what the shell will run.
package cmdx

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

// VarPattern matches $VAR and ${VAR} references inside a command string.
// Group 1 is the name for the ${VAR} form, group 2 for the $VAR form.
var VarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// Name extracts the variable name from a full match of VarPattern
// ("$MODEL" or "${MODEL}").
func Name(match string) string {
	name := strings.TrimPrefix(match, "${")
	return strings.TrimSuffix(strings.TrimPrefix(name, "$"), "}")
}

// Ref is one $VAR / ${VAR} reference found in a command string.
type Ref struct {
	Name  string
	Start int // byte offset of the reference in the command
	End   int // byte offset just past the reference
}

// Refs returns the variable references in command, in order of appearance.
func Refs(command string) []Ref {
	var out []Ref
	for _, loc := range VarPattern.FindAllStringSubmatchIndex(command, -1) {
		out = append(out, Ref{Name: Name(command[loc[0]:loc[1]]), Start: loc[0], End: loc[1]})
	}
	return out
}

// ReferencedNames reports which of the given variable names occur in command
// as a literal substring. It deliberately matches the bare name instead of a
// $VAR / ${VAR} reference: commands may use sophisticated shell forms
// (${NAME:-null}, ${!NAME}, indirect expansion, …) that no $-reference regex
// would match, and an unquoted occurrence still resolves against the
// environment. Names not in command are dropped.
func ReferencedNames(command string, names []string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		if n != "" && strings.Contains(command, n) {
			out[n] = true
		}
	}
	return out
}

// ShellQuote returns s quoted as a POSIX shell word: single-quoted with any
// embedded single quote escaped as '\''. The result is safe to place anywhere
// a shell word is expected (an assignment value, an argument, …).
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ExportPrefix renders the env assignments that will be in effect when the
// command runs, as a shell prefix: VAR='value' …, one per var, sorted by name
// (so the output is deterministic), joined by spaces, with a trailing space
// when non-empty. It is display-only: the real execution passes the same
// values through the child's environment, which is equivalent.
func ExportPrefix(vars map[string]string) string {
	if len(vars) == 0 {
		return ""
	}
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, k+"="+ShellQuote(vars[k]))
	}
	return strings.Join(parts, " ") + " "
}

// Render returns the display form of command: each $VAR / ${VAR} reference
// whose name is in vars is replaced by its value (for coloring in the TUI),
// references not in vars are left untouched (the shell resolves them), and
// the returned refs describe every reference in the original command so the
// caller can map the substituted spans back to their names.
func Render(command string, vars map[string]string) (string, []Ref) {
	refs := Refs(command)
	if len(refs) == 0 {
		return command, refs
	}
	var b strings.Builder
	last := 0
	for _, r := range refs {
		b.WriteString(command[last:r.Start])
		if v, ok := vars[r.Name]; ok {
			b.WriteString(v)
		} else {
			b.WriteString(command[r.Start:r.End])
		}
		last = r.End
	}
	b.WriteString(command[last:])
	return b.String(), refs
}

// TemplateFuncs is the function set available to an alias's template (the
// optional `template:` field). It is deliberately small and pure: no
// filesystem, network, or process access.
var TemplateFuncs = template.FuncMap{
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
	"title": strings.Title,
	"trim":  strings.TrimSpace,
	"trimall": func(a, b string) string {
		return strings.Trim(strings.Trim(a, b), b)
	},
	"replace": func(s, old, new string) string {
		return strings.ReplaceAll(s, old, new)
	},
	"contains": strings.Contains,
	"hasprefix": strings.HasPrefix,
	"hassuffix": strings.HasSuffix,
	"join": func(sep string, parts ...string) string {
		return strings.Join(parts, sep)
	},
	"split": func(s, sep string) []string {
		return strings.Split(s, sep)
	},
	"substr": func(s string, start, end int) string {
		if start < 0 {
			start = 0
		}
		if end > len(s) {
			end = len(s)
		}
		if start > end {
			return ""
		}
		return s[start:end]
	},
	"len": func(s string) int {
		return len(s)
	},
	"quote": ShellQuote,
	"int": func(s string) (int, error) {
		return strconv.Atoi(s)
	},
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"mul": func(a, b int) int { return a * b },
	"div": func(a, b int) (int, error) {
		if b == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return a / b, nil
	},
}

// EvalTemplate renders tmpl with vars as the dot context and returns the
// result. The template is parsed with the safe TemplateFuncs set. An empty
// template returns an empty string.
func EvalTemplate(tmpl string, vars map[string]string) (string, error) {
	if tmpl == "" {
		return "", nil
	}
	t, err := template.New("xuz").Funcs(TemplateFuncs).Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, vars); err != nil {
		return "", err
	}
	return b.String(), nil
}

// ParseDerivedVars parses the output of an alias template into env vars. The
// expected form is a list of "NAME=value" lines, one per line; blank lines
// and lines starting with '#' are ignored. A line without '=' is an error.
// The returned map is empty (not nil) when there are no vars.
func ParseDerivedVars(s string) (map[string]string, error) {
	out := map[string]string{}
	for i, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq <= 0 {
			return nil, fmt.Errorf("line %d: expected NAME=value, got %q", i+1, line)
		}
		name := strings.TrimSpace(line[:eq])
		if !isValidVarName(name) {
			return nil, fmt.Errorf("line %d: %q is not a valid variable name", i+1, name)
		}
		out[name] = line[eq+1:]
	}
	return out, nil
}

// IsValidVarName reports whether s is a valid shell variable name: it starts
// with a letter or underscore, then letters, digits, or underscores.
func IsValidVarName(s string) bool {
	return isValidVarName(s)
}

// isValidVarName reports whether s is a valid shell variable name: it starts
// with a letter or underscore, then letters, digits, or underscores.
func isValidVarName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9', r == '_':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
