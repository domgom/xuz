// Package cmdx resolves xuz commands against selected option values.
package cmdx

import (
	"regexp"
	"strings"
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

// Substitute replaces $VAR and ${VAR} references in command with values from
// vars. Unknown variables are left untouched so the shell can resolve them.
func Substitute(command string, vars map[string]string) string {
	return VarPattern.ReplaceAllStringFunc(command, func(match string) string {
		if v, ok := vars[Name(match)]; ok {
			return v
		}
		return match
	})
}
