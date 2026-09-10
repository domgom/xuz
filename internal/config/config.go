// Package config loads and models xuz configuration files.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Pair is one selectable option inside an option group.
type Pair struct {
	Key string
	// Icon is the option's icon glyph: literal text (an emoji or a
	// nerd-font glyph, rendered as-is), "" when none. It is drawn in the
	// column's leading icon slot, left of the key.
	Icon string
	// IconColor is the normalized hex color ("#rrggbb", lowercase) from a
	// !color tag on the icon ("" when none): the glyph is rendered in that
	// color. Icons take !color only — !default does not apply to them.
	IconColor string
	// LongText is the option's text: shown next to the key when it differs
	// from it, and what the command receives as the group's env var. For a
	// no-pair option ("- dev:") it is the key text.
	LongText string
	// Color is the normalized hex color ("#rrggbb", lowercase) from a
	// !color tag on the long_text: the option's key is rendered in that
	// color in its column (the long_text, when shown next to the key,
	// keeps the muted value color). The long_text stays plain (commands
	// see "prod", not the tag).
	Color string
	// ColorBeforeDefault records that the source tag wrote a !color part
	// before the !default one (!colorXXXXXX+default), so Save keeps the
	// order the user wrote; !default+colorXXXXXX and single tags leave it
	// false.
	ColorBeforeDefault bool
}

// Value returns what the command receives when this option is selected:
// LongText, or "" for a no-pair option (LongText = the key text), so a bare
// "- key:" passes an empty value instead of the key itself.
func (p Pair) Value() string {
	if p.LongText == p.Key {
		return ""
	}
	return p.LongText
}

// defaultTag is the YAML tag that marks an option as the preselected option
// of its group: `- qwen-3.6-35B-A3B: !default /path/to/model.gguf` (in the
// object form the tag rides on the long_text).
const defaultTag = "!default"

// defaultPart is the "default" part of the compound tag form
// (!default+colorXXXXXX).
const defaultPart = "default"

// colorTagPrefix is the YAML tag that colors an option's long_text (or an
// icon) in its column: `- prod: !colorD30000 prod` (a leading '#' is
// accepted too: !color#D30000). Icons take !color only.
const colorTagPrefix = "!color"

// Alias is one launchable command with ordered option groups.
type Alias struct {
	Name string
	// Icon is the alias's icon glyph: literal text (an emoji or a
	// nerd-font glyph, rendered as-is), "" when none. It is drawn in the
	// alias column's leading icon slot, left of the name.
	Icon string
	// IconColor is the normalized hex color ("#rrggbb", lowercase) from a
	// !color tag on the icon ("" when none): the glyph is rendered in that
	// color. Icons take !color only — !default does not apply to them.
	IconColor    string
	Groups       []string          // option group names, in config order
	GroupPairs   map[string][]Pair // group name -> pairs, in config order
	Defaults     map[string]string // group -> preselected option key (!default tag)
	Command      string
	Vars         map[string]string // extra env var name -> group name (optional)
	// Template is an optional text/template (per alias) that derives extra
	// env vars from the selected options. It is evaluated with the by-name
	// vars as the dot context; the result must be a list of "NAME=value"
	// lines (one per line, '#' starts a comment). The derived vars are merged
	// on top of the by-name vars and exported to the command.
	Template     string
	RememberLast int               // per-alias history depth, 0 = use top level
	Warnings     []string
}

// Hint is a preselection: an option key, optionally bound to a group name.
// Source names where the hint came from (SourceHistory); the
// resolver copies it onto the winning selection so callers can display it.
type Hint struct {
	Group  string
	Key    string
	Source string
}

// Sources of a resolved selection (the Resolved.Source field).
const (
	SourceDefault = "default" // the !default-tagged option
	SourceHistory = "history" // the most recent launch in the history file
	SourceFirst   = "first"   // nothing matched: the first option
)

// Resolved is one group's resolved preselection: the option key and where it
// came from (see the Source* constants).
type Resolved struct {
	Key    string
	Source string
}

// defaultSelectionPrecedence is the built-in selection_precedence order, used
// when the config sets none (or its value fails to parse): the !default tag
// wins, then the most recent history launch, then the first option.
const defaultSelectionPrecedence = "default > history > first"

// selectionPrecedenceLevels is the set of valid levels a
// selection_precedence list may name (in no particular order).
var selectionPrecedenceLevels = map[string]bool{
	SourceDefault: true,
	SourceHistory: true,
	SourceFirst:   true,
}

// Precedence returns the configured selection_precedence list, or the
// built-in default (default > history > first) when none is set.
func (c *Config) Precedence() []string {
	if len(c.SelectionPrecedence) > 0 {
		return c.SelectionPrecedence
	}
	return DefaultPrecedence()
}

// resolveSource returns the option key of group g from the named source, or
// "" when that source yields no candidate in this group. For SourceHistory
// the first hint (in hints order) whose key exists in the group wins; for
// SourceDefault a stale default (key gone) yields nothing. SourceFirst always
// yields a candidate (the first option).
func (a *Alias) resolveSource(g, source string, hints []Hint) string {
	pairs := a.GroupPairs[g]
	switch source {
	case SourceDefault:
		if d, ok := a.Defaults[g]; ok {
			for _, p := range pairs {
				if p.Key == d {
					return d // stale defaults (key gone) yield nothing
				}
			}
		}
	case SourceHistory:
		for _, h := range hints {
			if h.Source != source || (h.Group != "" && h.Group != g) {
				continue
			}
			for _, p := range pairs {
				if p.Key == h.Key {
					return h.Key
				}
			}
		}
	case SourceFirst:
		if len(pairs) > 0 {
			return pairs[0].Key
		}
	}
	return ""
}

// ResolveSelections picks one option key per group using the built-in
// precedence (default > history > first). The TUI and the non-interactive
// resolver use PrecedenceList + ResolveSelectionsWith so the configured
// selection_precedence applies.
func (a *Alias) ResolveSelections(hints []Hint) map[string]Resolved {
	return a.ResolveSelectionsWith(DefaultPrecedence(), hints)
}

// DefaultPrecedence returns the built-in selection_precedence order
// (default > history > first), used when the config sets none.
func DefaultPrecedence() []string {
	out := make([]string, 0, 3)
	for _, p := range strings.Split(defaultSelectionPrecedence, ">") {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// ResolveSelectionsWith picks one option key per group walking the given
// precedence list: for each group, the first level that yields a candidate
// wins — !default for SourceDefault (a stale default, whose key no longer
// exists in the group, yields nothing), the first matching hint for
// SourceHistory, the first option for SourceFirst. The Resolved.Source of
// each group records which level won.
func (a *Alias) ResolveSelectionsWith(ordered []string, hints []Hint) map[string]Resolved {
	out := map[string]Resolved{}
	for _, g := range a.Groups {
		if len(a.GroupPairs[g]) == 0 {
			continue
		}
		for _, source := range ordered {
			if key := a.resolveSource(g, source, hints); key != "" {
				out[g] = Resolved{Key: key, Source: source}
				break
			}
		}
	}
	return out
}

// PrecedenceList returns the effective selection_precedence for this config:
// the configured list (with "first" always appended last as the final
// fallback), or the built-in default order when none is set. The TUI uses it
// to resolve both the option preselections and the initially selected alias
// (the needle row of the alias column).
func (c *Config) PrecedenceList() []string {
	if len(c.SelectionPrecedence) == 0 {
		return DefaultPrecedence()
	}
	out := make([]string, 0, len(c.SelectionPrecedence)+1)
	for _, s := range c.SelectionPrecedence {
		out = append(out, s)
	}
	if !inList(out, SourceFirst) {
		out = append(out, SourceFirst)
	}
	return out
}

func inList(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Config is the parsed top-level configuration.
type Config struct {
	Path                string
	Theme               string
	Needle              string
	RememberLast        int
	SelectionPrecedence []string // selection_precedence levels, in priority order
	Aliases             []*Alias
	CustomThemes        map[string]map[string]string
	// GlobalVariables are the global variables (name -> value) shared by all
	// aliases and column values. They are exposed to a command's environment
	// when the command references them, and they sit at the bottom of the
	// precedence order: a values column named identically to a global
	// variable takes precedence over it.
	GlobalVariables map[string]string
	Warnings        []string
}

// defaultNeedle is the selection marker glyph used when the config sets no
// needle.
const defaultNeedle = "▸"

// NeedleGlyph returns the selection marker glyph: the configured needle, or
// the default ▸ when none is set (an empty needle falls back to the default).
func (c *Config) NeedleGlyph() string {
	if c.Needle != "" {
		return c.Needle
	}
	return defaultNeedle
}

// PathOf resolves a --config flag value to a concrete path.
func PathOf(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	return DefaultPath()
}

// DefaultPath returns the default config path: $XUZ_CONFIG or ~/.xuz/config.yml.
func DefaultPath() string {
	if p := os.Getenv("XUZ_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".xuz/config.yml"
	}
	return home + "/.xuz/config.yml"
}

// Load reads and parses the config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Path = path
	if cfg.RememberLast <= 0 {
		cfg.RememberLast = 10
	}
	return cfg, nil
}

// Save writes the configuration back to path as YAML. It emits a canonical
// layout (theme, needle, remember_last, selection_precedence, themes,
// global_variables, aliases) that Load parses back without warnings,
// preserving alias, group, option and default order. Map keys whose order is
// not user-visible (vars, custom themes, global variables) are emitted in
// sorted order.
func (c *Config) Save(path string) error {
	var b strings.Builder
	b.WriteString("# xuz configuration (written by xuz)\n")
	if c.Theme != "" {
		b.WriteString("theme: " + yamlScalar(c.Theme) + "\n")
	}
	if c.Needle != "" {
		b.WriteString("needle: " + yamlScalar(c.Needle) + "\n")
	}
	b.WriteString("remember_last: " + strconv.Itoa(c.RememberLast) + "\n")
	if len(c.GlobalVariables) > 0 {
		b.WriteString("global_variables:\n")
		for _, k := range sortedKeys(c.GlobalVariables) {
			b.WriteString("  " + yamlScalar(k) + ": " + yamlScalar(c.GlobalVariables[k]) + "\n")
		}
	}
	if len(c.SelectionPrecedence) > 0 {
		b.WriteString("selection_precedence:\n")
		for _, s := range c.SelectionPrecedence {
			b.WriteString("  - " + yamlScalar(s) + "\n")
		}
	}
	if len(c.CustomThemes) > 0 {
		b.WriteString("themes:\n")
		for _, name := range sortedKeys(c.CustomThemes) {
			b.WriteString("  " + yamlScalar(name) + ":\n")
			for _, f := range sortedKeys(c.CustomThemes[name]) {
				b.WriteString("    " + yamlScalar(f) + ": " + yamlScalar(c.CustomThemes[name][f]) + "\n")
			}
		}
	}
	if len(c.Aliases) == 0 {
		b.WriteString("aliases: {}\n")
		return os.WriteFile(path, []byte(b.String()), 0o644)
	}
	b.WriteString("aliases:\n")
	for _, a := range c.Aliases {
		b.WriteString("  " + yamlScalar(a.Name) + ":\n")
		if a.Icon != "" {
			b.WriteString("    icon: " + aliasIconTagged(*a) + "\n")
		}
		if len(a.Groups) == 0 {
			b.WriteString("    options: {}\n")
		} else {
			b.WriteString("    options:\n")
			for _, g := range a.Groups {
				b.WriteString("      " + yamlScalar(g) + ":\n")
				defKey, hasDef := a.Defaults[g]
				for _, p := range a.GroupPairs[g] {
					isDef := hasDef && defKey == p.Key
					switch {
					case p.Icon != "":
						// Icon: the object form. long_text is omitted when it
						// equals the key and carries no tag (then it is the
						// key text on reload).
						b.WriteString("        - " + yamlScalar(p.Key) + ":\n")
						b.WriteString("            icon: " + iconTagged(p) + "\n")
						if p.LongText != p.Key || p.Color != "" || isDef {
							b.WriteString("            long_text: " + longTextTagged(p, isDef) + "\n")
						}
					case p.LongText == p.Key && p.Color == "" && !isDef:
						// No icon, long_text = key: the bare no-pair form.
						b.WriteString("        - " + yamlScalar(p.Key) + ":\n")
					default:
						// No icon, different long_text: the scalar form.
						b.WriteString("        - " + yamlScalar(p.Key) + ": " + longTextTagged(p, isDef) + "\n")
					}
				}
			}
			if len(a.Vars) > 0 {
				b.WriteString("      vars:\n")
				for _, v := range sortedKeys(a.Vars) {
					b.WriteString("        " + yamlScalar(v) + ": " + yamlScalar(a.Vars[v]) + "\n")
				}
			}
		}
		if a.Command != "" {
			b.WriteString("    command: " + yamlScalar(a.Command) + "\n")
		}
		if a.Template != "" {
			b.WriteString("    template: " + yamlScalar(a.Template) + "\n")
		}
		if a.RememberLast != c.RememberLast {
			b.WriteString("    remember_last: " + strconv.Itoa(a.RememberLast) + "\n")
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// yamlScalar renders one string as a single-line YAML scalar, quoting only
// when the plain form would not round-trip. Multi-line strings (which yaml.v3
// would emit as indented block scalars) are double-quoted instead, because a
// JSON string is a valid YAML double-quoted scalar.
func yamlScalar(s string) string {
	b, err := yaml.Marshal(s)
	if err != nil {
		return jsonQuote(s)
	}
	out := strings.TrimSuffix(string(b), "\n")
	if strings.Contains(out, "\n") {
		return jsonQuote(s)
	}
	// yaml.v3's emitter treats non-BMP characters (emoji, …) as
	// non-printable: it double-quotes the scalar and escapes the glyphs
	// (icon: "\U0001F999"). When the plain form is an unambiguous scalar,
	// emit the raw string instead so the file stays human-readable.
	if strings.HasPrefix(out, `"`) && strings.Contains(out, `\`) && plainScalarSafe(s) {
		return s
	}
	return out
}

// plainScalarSafe reports whether s can be written as a plain (unquoted)
// YAML scalar in a block mapping without changing its meaning: no leading
// indicator character, no "key: value" colon, no comment start, no control
// characters, no leading or trailing space.
func plainScalarSafe(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c < 0x20 || c == 0x7F:
			return false // control characters (incl. newline, tab)
		case c == ' ' && (i == 0 || i == len(s)-1):
			return false // leading or trailing space
		case c == ':' && (i == len(s)-1 || s[i+1] == ' '):
			return false // mapping indicator
		case c == '#' && i > 0 && s[i-1] == ' ':
			return false // start of a comment
		}
	}
	switch s[0] {
	case '-', '?', ':', ',', '[', ']', '{', '}', '#', '&', '*', '!',
		'|', '>', '"', '\'', '%', '@', '`':
		return false // indicator: the plain scalar would be ambiguous
	}
	return true
}

// jsonQuote double-quotes s using JSON escaping, which is valid YAML.
func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// parseColorTag parses a !color tag: the hex color follows the !color
// keyword, optionally prefixed with '#'. context names where the tag was
// found (e.g. "option group env" or "aliases.tf-plan icon") in the warning
// for an invalid color, which is returned with an empty color.
func parseColorTag(context, tag string) (string, string) {
	// The prefix and the hex digits are both case-insensitive.
	hex := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(tag), colorTagPrefix), "#")
	if (len(hex) == 3 || len(hex) == 6) && isHex(hex) {
		if len(hex) == 3 {
			hex = string(hex[0]) + string(hex[0]) + string(hex[1]) + string(hex[1]) + string(hex[2]) + string(hex[2])
		}
		return "#" + hex, ""
	}
	return "", fmt.Sprintf("%s: %q is not a valid %s tag (expected e.g. %sD30000), ignoring it", context, tag, colorTagPrefix, colorTagPrefix)
}

// colorTagPart renders a normalized color as the color part of a tag
// (colorXXXXXX, uppercase, no leading '#' or '!'), for the compound tag
// form.
func colorTagPart(c string) string {
	return strings.TrimPrefix(colorTagPrefix, "!") + strings.ToUpper(strings.TrimPrefix(c, "#"))
}

// colorTag renders a normalized color back into its YAML tag form
// (!colorXXXXXX, uppercase, no leading '#').
func colorTag(c string) string {
	return "!" + colorTagPart(c)
}

// iconTagged renders a pair's icon back into its YAML form, keeping the
// !color tag when the icon is colored.
func iconTagged(p Pair) string {
	if p.IconColor != "" {
		return colorTag(p.IconColor) + " " + yamlScalar(p.Icon)
	}
	return yamlScalar(p.Icon)
}

// aliasIconTagged renders an alias's icon back into its YAML form, keeping
// the !color tag when the icon is colored.
func aliasIconTagged(a Alias) string {
	if a.IconColor != "" {
		return colorTag(a.IconColor) + " " + yamlScalar(a.Icon)
	}
	return yamlScalar(a.Icon)
}

// longTextTagged renders a pair's long_text back into its YAML form,
// carrying the !default tag (when the pair is the group's preselection) and
// the !color tag (when the key is colored) — the same tags the parser
// accepts on option values. A pair that is both the preselection and
// colored carries the compound form, in the order the user wrote it
// (!default+colorXXXXXX by default, !colorXXXXXX+default when the source
// tag had the color part first).
func longTextTagged(p Pair, isDef bool) string {
	var tag string
	switch {
	case isDef && p.Color != "":
		if p.ColorBeforeDefault {
			tag = "!" + colorTagPart(p.Color) + "+" + defaultPart
		} else {
			tag = "!" + defaultPart + "+" + colorTagPart(p.Color)
		}
	case isDef:
		tag = defaultTag
	case p.Color != "":
		tag = colorTag(p.Color)
	}
	if tag != "" {
		return tag + " " + yamlScalar(p.LongText)
	}
	return yamlScalar(p.LongText)
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func parse(data []byte) (*Config, error) {
	root, err := yamlRoot(data)
	if err != nil {
		return nil, err
	}
	return parseRoot(root)
}

// yamlRoot unmarshals the config document. Compatibility: the !color tag is
// written !colorXXXXXX, but the spelling with a leading '#'
// (!color#XXXXXX) is common too and is invalid YAML inside a tag. Only files
// that fail to parse as-is are rewritten, and only the leading '#' of the
// !color tag is removed.
func yamlRoot(data []byte) (*yaml.Node, error) {
	var root yaml.Node
	err := yaml.Unmarshal(data, &root)
	if err == nil {
		return &root, nil
	}
	fixed := bytes.ReplaceAll(data, []byte("!color#"), []byte("!color"))
	if bytes.Equal(fixed, data) {
		return nil, err
	}
	if err2 := yaml.Unmarshal(fixed, &root); err2 != nil {
		return nil, err
	}
	return &root, nil
}

func parseRoot(root *yaml.Node) (*Config, error) {
	cfg := &Config{
		RememberLast:    10,
		CustomThemes:    map[string]map[string]string{},
		GlobalVariables: map[string]string{},
	}
	var top *yaml.Node
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return cfg, nil
		}
		top = root.Content[0]
	} else {
		top = root
	}
	if top.Kind != yaml.MappingNode {
		return cfg, nil // empty file: zero aliases, load error reported by caller
	}
	// Apply top-level settings before aliases so per-alias defaults (such as
	// remember_last) resolve against final values regardless of document order.
	topKeys := map[string]bool{}
	for _, e := range entries(top) {
		topKeys[e[0].Value] = true
	}
	if v, ok := findEntry(top, "theme"); ok {
		cfg.Theme = keyOf(v)
	}
	if v, ok := findEntry(top, "needle"); ok {
		cfg.Needle = keyOf(v)
	}
	if v, ok := findEntry(top, "remember_last"); ok {
		if n, err := strconv.Atoi(v.Value); err == nil {
			cfg.RememberLast = n
		} else {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("remember_last: %q is not a number, using 10", v.Value))
		}
	}
	if v, ok := findEntry(top, "selection_precedence"); ok {
		list, warns := parseSelectionPrecedence(v)
		cfg.SelectionPrecedence = list
		cfg.Warnings = append(cfg.Warnings, warns...)
	}
	for _, e := range entries(top) {
		key, val := e[0], e[1]
		switch key.Value {
		case "theme", "needle", "remember_last", "selection_precedence":
			// already applied above
		case "themes":
			parseThemes(val, cfg)
		case "global_variables":
			parseGlobalVariables(val, cfg)
		case "aliases":
			parseAliases(val, cfg, topKeys)
		default:
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("ignoring unknown top-level key %q", key.Value))
		}
	}
	return cfg, nil
}

// findEntry returns the value node for key in a mapping, if present.
func findEntry(n *yaml.Node, key string) (*yaml.Node, bool) {
	for _, e := range entries(n) {
		if e[0].Value == key {
			return e[1], true
		}
	}
	return nil, false
}

// entries returns the content pairs of a mapping node.
func entries(n *yaml.Node) [][2]*yaml.Node {
	out := make([][2]*yaml.Node, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
	}
	return out
}

// keyOf returns a string representation of a value node (for simple scalars).
func keyOf(n *yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return ""
}

// scalar returns the string form of a value node: plain scalars as-is,
// single-entry mappings as "key: value".
func scalar(n *yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	case yaml.MappingNode:
		e := entries(n)
		if len(e) == 1 {
			return fmt.Sprintf("%s: %s", e[0][0].Value, scalar(e[0][1]))
		}
		var v any
		if err := n.Decode(&v); err == nil {
			return fmt.Sprintf("%v", v)
		}
		return n.Value
	case yaml.SequenceNode:
		return n.Value
	default:
		return n.Value
	}
}

// parseSelectionPrecedence parses the selection_precedence value: an ordered
// list of levels (default, history, first) — a YAML sequence or a single
// level — that decides which source preselects each option and alias. It
// returns the cleaned list ("" entries dropped, duplicates keep their first
// occurrence) plus warnings: unknown levels are warned about and dropped;
// when nothing valid remains, the built-in default stays in force.
func parseSelectionPrecedence(n *yaml.Node) ([]string, []string) {
	var raw []string
	switch n.Kind {
	case yaml.SequenceNode:
		for _, item := range n.Content {
			if s := keyOf(item); s != "" {
				raw = append(raw, strings.TrimSpace(s))
			}
		}
	case yaml.ScalarNode:
		raw = []string{strings.TrimSpace(n.Value)}
	default:
		return nil, []string{"selection_precedence: expected a list of levels (default, history, first), using the default order"}
	}
	if len(raw) == 0 {
		return nil, []string{"selection_precedence: empty list, using the default order"}
	}
	out := make([]string, 0, len(raw))
	var warns []string
	seen := map[string]bool{}
	for _, s := range raw {
		if !selectionPrecedenceLevels[s] {
			warns = append(warns, fmt.Sprintf("selection_precedence: unknown level %q (valid: default, history, first), ignoring it", s))
			continue
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, warns // the built-in default applies
	}
	return out, warns
}

func parseThemes(n *yaml.Node, cfg *Config) {
	if n.Kind != yaml.MappingNode {
		cfg.Warnings = append(cfg.Warnings, "themes: expected a map of theme name -> field -> color")
		return
	}
	for _, e := range entries(n) {
		name, val := e[0].Value, e[1]
		if val.Kind != yaml.MappingNode {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("themes.%s: expected a map of field -> color", name))
			continue
		}
		fields := map[string]string{}
		for _, fe := range entries(val) {
			fields[strings.ToLower(fe[0].Value)] = fe[1].Value
		}
		cfg.CustomThemes[name] = fields
	}
}

// parseGlobalVariables parses the global_variables value: a map of variable
// name -> value. A non-mapping value is warned about and ignored; each value
// is taken as its scalar text (an empty mapping entry gives "").
func parseGlobalVariables(n *yaml.Node, cfg *Config) {
	if n.Kind != yaml.MappingNode {
		cfg.Warnings = append(cfg.Warnings, "global_variables: expected a map of variable name -> value")
		return
	}
	for _, e := range entries(n) {
		name := e[0].Value
		var val string
		if e[1].Kind == yaml.ScalarNode {
			val = e[1].Value
		}
		cfg.GlobalVariables[name] = val
	}
}

func parseAliases(n *yaml.Node, cfg *Config, topKeys map[string]bool) {
	if n.Kind != yaml.MappingNode {
		cfg.Warnings = append(cfg.Warnings, "aliases: expected a map of alias name -> definition")
		return
	}
	// Convenience: theme / needle / remember_last / selection_precedence may
	// also sit directly inside aliases: (next to the alias names); they then
	// have the same meaning as at the top level, and the top level wins when
	// both exist. remember_last is applied before aliases are created so
	// that per-alias defaults pick it up regardless of document order.
	if !topKeys["remember_last"] {
		if v, ok := findEntry(n, "remember_last"); ok {
			if nn, err := strconv.Atoi(v.Value); err == nil {
				cfg.RememberLast = nn
			} else {
				cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("remember_last: %q is not a number, using 10", v.Value))
			}
		}
	}
	if !topKeys["theme"] {
		if v, ok := findEntry(n, "theme"); ok {
			cfg.Theme = keyOf(v)
		}
	}
	if !topKeys["needle"] {
		if v, ok := findEntry(n, "needle"); ok {
			cfg.Needle = keyOf(v)
		}
	}
	if !topKeys["selection_precedence"] {
		if v, ok := findEntry(n, "selection_precedence"); ok {
			list, warns := parseSelectionPrecedence(v)
			cfg.SelectionPrecedence = list
			cfg.Warnings = append(cfg.Warnings, warns...)
		}
	}
	for _, e := range entries(n) {
		name, val := e[0].Value, e[1]
		switch name {
		case "remember_last", "theme", "needle", "selection_precedence":
			continue // handled above
		}
		alias := &Alias{
			Name:         name,
			GroupPairs:   map[string][]Pair{},
			Defaults:     map[string]string{},
			Vars:         map[string]string{},
			RememberLast: cfg.RememberLast,
		}
		parseAliasBody(name, val, alias)
		cfg.Aliases = append(cfg.Aliases, alias)
	}
}

// parseIcon parses an icon value: literal text (an emoji or a nerd-font
// glyph, rendered as-is) with an optional !color tag that colors the glyph.
// The tag may be compound, but icons take !color parts only: several of
// them are allowed (the last one wins, with a warning), any other part
// (such as !default) makes the tag malformed — nothing is applied and it
// is reported with the line it was found on. A single tag that is not a
// !color tag is ignored (as before).
func parseIcon(context string, v *yaml.Node, alias *Alias) (icon, color string) {
	icon = v.Value
	if tag := v.Tag; tag != "" && strings.HasPrefix(tag, "!") && !strings.HasPrefix(tag, "!!") {
		c, w := colorPartsFromTag(tagContext(context, v), tag)
		color = c // a warning may accompany the color (several parts)
		if w != "" {
			alias.Warnings = append(alias.Warnings, w)
		}
	}
	return icon, color
}

// tagContext returns context with the YAML line of v appended, so a tag
// warning names the line of the file the tag was found on.
func tagContext(context string, v *yaml.Node) string {
	if v.Line > 0 {
		return fmt.Sprintf("%s (line %d)", context, v.Line)
	}
	return context
}

// colorPartsFromTag parses a tag that carries only !color parts: a single
// !colorXXXXXX tag or a compound one with several of them (the last wins,
// with a warning). A malformed tag — a part that is not a valid !color —
// applies nothing and is reported. A tag with no !color part at all is not
// a color tag: it is returned silently as "no color".
func colorPartsFromTag(context, tag string) (color, warn string) {
	if !strings.Contains(tag, "+") {
		if strings.HasPrefix(strings.ToLower(tag), colorTagPrefix) {
			return parseColorTag(context, tag)
		}
		return "", ""
	}
	colors := 0
	var w string
	for _, part := range strings.Split(tag[1:], "+") {
		lp := strings.ToLower(part)
		if !strings.HasPrefix("!"+lp, colorTagPrefix) {
			return "", fmt.Sprintf("%s: %q is not a valid %s tag (expected one or more %sXXXXXX parts joined by '+'), ignoring the whole tag", context, tag, colorTagPrefix, colorTagPrefix)
		}
		c, w2 := parseColorTag(context, "!"+lp)
		if w2 != "" {
			return "", w2
		}
		colors++
		if colors > 1 {
			w = fmt.Sprintf("%s: %q has multiple %s tags, using the last one", context, tag, colorTagPrefix)
		}
		color = c // the last color part wins
	}
	return color, w
}

func parseAliasBody(name string, val *yaml.Node, alias *Alias) {
	if val.Kind != yaml.MappingNode {
		alias.Warnings = append(alias.Warnings, fmt.Sprintf("aliases.%s: expected a mapping", name))
		return
	}
	sawOptions := false
	for _, e := range entries(val) {
		key, v := e[0], e[1]
		switch key.Value {
		case "options":
			sawOptions = true
			parseOptions(name, v, alias)
		case "icon":
			// Literal icon glyph (emoji or nerd-font glyph), rendered as-is
			// at the left of the alias name. A !color tag colors it;
			// !default does not apply to icons.
			alias.Icon, alias.IconColor = parseIcon(fmt.Sprintf("aliases.%s icon", name), v, alias)
		case "command":
			alias.Command = scalar(v)
		case "template":
			alias.Template = scalar(v)
		case "remember_last":
			if n, err := strconv.Atoi(v.Value); err == nil {
				alias.RememberLast = n
			}
		default:
			alias.Warnings = append(alias.Warnings, fmt.Sprintf("aliases.%s: ignoring unknown key %q", name, key.Value))
		}
	}
	if !sawOptions {
		// Allow a flat alias body: groups + command directly under the alias.
		parseOptions(name, val, alias)
	}
}

func parseOptions(name string, n *yaml.Node, alias *Alias) {
	if n.Kind != yaml.MappingNode {
		return
	}
	// Option groups. A !default tag on a pair's value marks that key as the
	// group's preselection.
	tagged := map[string]string{}
	for _, e := range entries(n) {
		key, v := e[0], e[1]
		// Reserved: command (accepted here for convenience)
		if key.Value == "command" {
			if alias.Command == "" {
				alias.Command = scalar(v)
			}
			continue
		}
		// Removed: <group>_default. The !default tag on an option's value is
		// the only way to mark a group's preselection.
		if strings.HasSuffix(key.Value, "_default") {
			alias.Warnings = append(alias.Warnings, fmt.Sprintf("aliases.%s: %q is no longer supported; tag the option's value with !default instead", name, key.Value))
			continue
		}
		// Reserved: remember_last (handled at alias level)
		if key.Value == "remember_last" {
			if n, err := strconv.Atoi(v.Value); err == nil {
				alias.RememberLast = n
			}
			continue
		}
		// Reserved: icon (handled at alias level; a flat body is parsed as
		// groups, so keep it from becoming an option group)
		if key.Value == "icon" {
			alias.Icon, alias.IconColor = parseIcon(fmt.Sprintf("aliases.%s icon", name), v, alias)
			continue
		}
		// Reserved: vars (env var name -> group name)
		if key.Value == "vars" {
			if v.Kind == yaml.MappingNode {
				for _, ve := range entries(v) {
					alias.Vars[ve[0].Value] = ve[1].Value
				}
			} else {
				alias.Warnings = append(alias.Warnings, fmt.Sprintf("aliases.%s: vars: expected a map of env var -> group", name))
			}
			continue
		}
		// Reserved: template (optional text/template that derives extra env
		// vars from the selected options; the result is a list of NAME=value
		// lines). It is an alias attribute, not an option group.
		if key.Value == "template" {
			alias.Template = scalar(v)
			continue
		}
		// Otherwise: an option group, expected to be a list of single-entry maps.
		pairs, warns, defKey := pairsFromSequence(key.Value, v)
		for _, w := range warns {
			alias.Warnings = append(alias.Warnings, fmt.Sprintf("aliases.%s: %s", name, w))
		}
		if len(pairs) == 0 {
			continue
		}
		alias.Groups = append(alias.Groups, key.Value)
		alias.GroupPairs[key.Value] = pairs
		if defKey != "" {
			tagged[key.Value] = defKey
		}
	}
	for g, k := range tagged {
		alias.Defaults[g] = k
	}
}

// pairsFromSequence turns a YAML sequence of option items into ordered
// pairs. Each item is one of:
//
//   - a bare key              -> the no-pair form: long_text = the key text,
//     no icon ("- dev:")
//   - key: <scalar>           -> the scalar form: the scalar is the long_text
//     ("!default" / "!colorXXXXXX" tags ride on it)
//   - key: {icon, long_text}   -> the object form; long_text may be omitted
//     (then it is the key text)
//
// Tags keep their meaning and are placed on the value: !default and
// !colorXXXXXX on the long_text, !colorXXXXXX on the icon (icons take
// !color only). It also returns any warnings and the key of the first pair
// whose long_text carries the !default tag ("" when no pair is tagged).
func pairsFromSequence(group string, n *yaml.Node) ([]Pair, []string, string) {
	var pairs []Pair
	var warns []string
	defKey := ""
	markDefault := func(key string, def bool) {
		if !def {
			return
		}
		if defKey == "" {
			defKey = key
		} else {
			warns = append(warns, fmt.Sprintf("option group %s: multiple !default tags, using %q", group, defKey))
		}
	}
	switch n.Kind {
	case yaml.SequenceNode:
		for i, item := range n.Content {
			switch item.Kind {
			case yaml.MappingNode:
				e := entries(item)
				if len(e) == 1 {
					key := e[0][0].Value
					p := Pair{Key: key, LongText: key}
					def := false
					switch v := e[0][1]; v.Kind {
					case yaml.MappingNode:
						// The object form: icon + long_text (either may be
						// omitted; a missing long_text is the key text).
						for _, oe := range entries(v) {
							switch oe[0].Value {
							case "icon":
								ic, icc, w := iconFromNode(fmt.Sprintf("option group %s, option %q", group, key), oe[1])
								if w != "" {
									warns = append(warns, w)
								}
								p.Icon, p.IconColor = ic, icc
							case "long_text":
								lt, d, c, cbd, w := longTextFromNode(fmt.Sprintf("option group %s", group), oe[1])
								if w != "" {
									warns = append(warns, w)
								}
								if lt != "" {
									p.LongText = lt
								}
								p.Color = c
								p.ColorBeforeDefault = cbd
								def = d
							default:
								warns = append(warns, fmt.Sprintf("option group %s: option %q: ignoring unknown key %q (known: icon, long_text)", group, key, oe[0].Value))
							}
						}
					case yaml.ScalarNode:
						// The scalar form (the old "key: value"). An empty
						// scalar is the no-pair form: long_text = the key
						// text.
						lt, d, c, cbd, w := longTextFromNode(fmt.Sprintf("option group %s", group), v)
						if w != "" {
							warns = append(warns, w)
						}
						if v.Value != "" {
							p.LongText = lt
						}
						p.Color = c
						p.ColorBeforeDefault = cbd
						def = d
					}
					pairs = append(pairs, p)
					markDefault(key, def)
					continue
				}
				pairs = append(pairs, Pair{Key: fmt.Sprintf("item %d", i+1), LongText: scalar(item)})
			case yaml.ScalarNode:
				pairs = append(pairs, Pair{Key: item.Value, LongText: item.Value})
			}
		}
		return pairs, warns, defKey
	case yaml.ScalarNode:
		return []Pair{{Key: n.Value, LongText: n.Value}}, nil, ""
	default:
		return nil, []string{fmt.Sprintf("option group %s: expected a list of \"key: value\" items", group)}, ""
	}
}

// longTextFromNode parses a pair's long_text: the scalar's text with the
// !default / !color tags on it (a leading '#' on the color is accepted).
// The tag may be compound: !tag1+tag2+...+tagN, the parts in any order
// (!default+colorXXXXXX and !colorXXXXXX+default are equivalent; several
// !color parts are allowed, the last one wins with a warning). A malformed
// tag — an unknown part or an invalid color — is ignored whole and
// reported with the line it was found on. A single tag keeps the
// historical behavior: !default marks the preselection, a !color tag colors
// the option's key, anything else is ignored. colorBeforeDefault records
// that the source tag wrote a !color part before the !default one, so
// Save can keep the order the user wrote.
func longTextFromNode(context string, v *yaml.Node) (longText string, def bool, color string, colorBeforeDefault bool, warn string) {
	longText = v.Value
	tag := v.Tag
	if tag == "" || !strings.HasPrefix(tag, "!") || strings.HasPrefix(tag, "!!") {
		return longText, false, "", false, "" // a plain scalar: no tags
	}
	ctx := tagContext(context, v)
	if !strings.Contains(tag, "+") {
		// The single-tag form.
		if tag == defaultTag {
			return longText, true, "", false, ""
		}
		if strings.HasPrefix(strings.ToLower(tag), colorTagPrefix) {
			c, w := parseColorTag(ctx, tag)
			return longText, false, c, false, w
		}
		return longText, false, "", false, "" // an unknown tag: ignored
	}
	// The compound form: the parts, in any order, are "default" or colors.
	colors := 0
	seenColor := false
	var w string
	for _, part := range strings.Split(tag[1:], "+") {
		lp := strings.ToLower(part)
		switch {
		case lp == "default":
			def = true
			if seenColor {
				colorBeforeDefault = true
			}
		case strings.HasPrefix("!"+lp, colorTagPrefix):
			c, w2 := parseColorTag(ctx, "!"+lp)
			if w2 != "" {
				return longText, false, "", false, w2 // malformed: nothing applies
			}
			colors++
			if colors > 1 {
				w = fmt.Sprintf("%s: %q has multiple %s tags, using the last one", ctx, tag, colorTagPrefix)
			}
			color = c // the last color part wins
			seenColor = true
		default:
			return longText, false, "", false, fmt.Sprintf("%s: %q is not a valid tag (expected %s and/or %sXXXXXX, joined by '+'), ignoring the whole tag", ctx, tag, defaultTag, colorTagPrefix)
		}
	}
	return longText, def, color, colorBeforeDefault, w
}

// iconFromNode parses a pair's icon: literal text with an optional !color
// tag (icons take !color parts only; several joined by '+' are allowed, the
// last one wins with a warning; any other part is malformed and reported
// with the line). A single tag that is not a !color tag is ignored (as
// before).
func iconFromNode(context string, v *yaml.Node) (icon, color, warn string) {
	icon = v.Value
	if tag := v.Tag; tag != "" && strings.HasPrefix(tag, "!") && !strings.HasPrefix(tag, "!!") {
		c, w := colorPartsFromTag(tagContext(context, v), tag)
		color = c // a warning may accompany the color (several parts)
		if w != "" {
			warn = w
		}
	}
	return icon, color, warn
}
