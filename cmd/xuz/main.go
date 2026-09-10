// Command xuz launches configurable commands from a multi-column TUI picker.
package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"xuz/internal/cmdx"
	"xuz/internal/complete"
	"xuz/internal/config"
	"xuz/internal/history"
	"xuz/internal/theme"
	"xuz/internal/tui"
)

// version is the build version.
var version = "0.1.0"

//go:embed sample_config.yml
var sampleConfig embed.FS

func main() {
	os.Exit(run(os.Args[1:]))
}

// normalizeArgs moves flags (and their values) to the front of the argument
// list so they are honored wherever the user placed them: both
// "xuz --dry-run llama" and "xuz llama --dry-run" work. The flag package
// stops parsing at the first positional argument, so without this a flag
// after the alias would be silently ignored.
func normalizeArgs(args []string) []string {
	valueFlags := map[string]bool{"-config": true, "--config": true, "-theme": true, "--theme": true}
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			if valueFlags[a] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		rest = append(rest, a)
	}
	return append(flags, rest...)
}

func run(args []string) int {
	args = normalizeArgs(args)
	flags := flag.NewFlagSet("xuz", flag.ContinueOnError)
	var (
		cfgPath  = flags.String("config", "", "config file (default $XUZ_CONFIG or ~/.xuz/config.yml)")
		themeOpt = flags.String("theme", "", "override theme for this run")
		dryRun   = flags.Bool("dry-run", false, "print the resolved command instead of running it")
		showVer  = flags.Bool("version", false, "print version and exit")
		full     = flags.Bool("f", false, "full-screen mode (default: short mode; same as --full)")
		fullLong = flags.Bool("full", false, "full-screen mode (same as -f)")
		names    = flags.Bool("names", false, "list: print the alias names only (shell completions)")
	)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	fullMode := *full || *fullLong
	if *showVer {
		fmt.Println("xuz", version)
		return 0
	}
	rest := flags.Args()
	if len(rest) == 0 {
		return startTUI("", *cfgPath, *themeOpt, *dryRun, fullMode)
	}
	switch rest[0] {
	case "list", "ls":
		return doList(*cfgPath, *names)
	case "completions":
		return doCompletions(rest[1:], *cfgPath)
	case "themes":
		return doThemes(*cfgPath)
	case "theme-sync":
		return doThemeSync(*cfgPath)
	case "init":
		return doInit(*cfgPath)
	case "show":
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xuz show <alias>")
			return 2
		}
		return doShow(rest[1], *cfgPath)
	case "run":
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, "usage: xuz run <alias>")
			return 2
		}
		return startTUI(rest[1], *cfgPath, *themeOpt, *dryRun, fullMode)
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		// xuz <alias>: open the picker on that alias
		return startTUI(rest[0], *cfgPath, *themeOpt, *dryRun, fullMode)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `xuz - launch configured commands through a multi-column TUI

usage:
  xuz [alias]                 open the picker (optionally starting on alias)
  xuz run <alias>             same as above
  xuz list | ls [--names]     list aliases and their options (--names:
                              the alias names only, for shell completions)
  xuz themes                  list available themes
  xuz theme-sync              sync current Omarchy theme into xuz config
  xuz init [--config PATH]    write a sample config
  xuz show <alias>            print resolved selections and command
  xuz completions [shell]     print a shell completion script (bash, zsh
                              or fish; guessed from $SHELL when omitted)
  xuz --version               print version

flags:
  -f, --full      full-screen mode with all options (default is the short mode)
  --config PATH   config file (default $XUZ_CONFIG or ~/.xuz/config.yml)
  --theme NAME    override theme
  --dry-run       print the resolved command instead of running it

modes:
  short (xuz)        no full screen: the alias list (four matches, the
                     "filter" line at the bottom of the list), then one
                     option column at a time (four values); the shortcut
                     line is always shown; «/» in the header point at the
                     columns left/right of the current one
  full (xuz -f)      the full-screen multi-column picker with all the options
  /                   in the short mode opens the full one; / in the
                     full mode switches back, keeping the selections
  after a run, xuz exits with the command's status (no reopen)

keys (short mode):
  typing           fuzzy-filter the aliases (fzf-style subsequence)
  up/down          move through the matches / the four values
  enter            launch with the preselected options (defaults, history, …)
  space / right /  in the alias list: open the selected alias's first column;
  tab              in a column: select the option under the cursor
  left/right       previous / next option column (left from the first one
                   goes back to the alias filter)
  /                switch to the full mode
  esc              clear the filter / go back to the alias filter
  q / ctrl-c       quit (asks to save if the config has unsaved changes)

keys (full mode):
  up/down          move cursor (in the alias column, switches alias)
  left/right, tab  previous / next column (shift+tab goes back); when the
                   columns overflow the width, a sliver of the next column is
                   shown at the right edge and right/tab scrolls it into view
  space            select the item under the cursor in the current column
  enter            run the command (works from any column)
  f                filter the options in the current column (esc exits)
  n                add a new option value to the current column
  d                delete the option under the cursor
  ctrl+space       make the option under the cursor the default
  c                copy the resolved command to the clipboard
  ctrl+s           save the config file
  b                toggle background mode; while armed, Enter runs the command
                   with " &" (it keeps running and xuz exits, output logged
                   next to the history file) instead of the foreground
  mouse (click)    left-click a column to focus it; click an option to select it
  t                cycle themes
  ?                show the config/theme info line
  /                short mode
  q / ctrl-c       quit (asks to save if the config has unsaved changes)
`)
}

func loadConfig(pathFlag string) (*config.Config, error) {
	path := config.PathOf(pathFlag)
	cfg, err := config.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config file at %s — run 'xuz init' to create a sample, or pass --config PATH", path)
		}
		return nil, err
	}
	warnAll(cfg)
	return cfg, nil
}

func warnAll(cfg *config.Config) {
	for _, w := range cfg.Warnings {
		fmt.Fprintf(os.Stderr, "xuz: %s\n", w)
	}
	for _, a := range cfg.Aliases {
		for _, w := range a.Warnings {
			fmt.Fprintf(os.Stderr, "xuz: %s\n", w)
		}
	}
}

func startTUI(alias, pathFlag, themeOpt string, dryRun, fullMode bool) int {
	cfg, err := loadConfig(pathFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 1
	}
	// No controlling terminal: --dry-run can still work, resolving the
	// preselected options non-interactively (scripting, CI, pipes).
	if dryRun && !hasTTY() {
		return dryRunNonInteractive(alias, cfg)
	}
	opts := tui.Options{
		Cfg:           cfg,
		StartAlias:    alias,
		DryRun:        dryRun,
		ThemeOverride: themeOpt,
		Short:         !fullMode,
	}
	return tui.Run(opts)
}

// hasTTY reports whether the process has a controlling terminal.
func hasTTY() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// dryRunNonInteractive resolves the preselected options for alias and prints
// the substituted command without opening the TUI.
func dryRunNonInteractive(alias string, cfg *config.Config) int {
	if alias == "" {
		if len(cfg.Aliases) == 1 {
			alias = cfg.Aliases[0].Name
		} else {
			fmt.Fprintln(os.Stderr, "xuz: no TTY available; pass an alias name to --dry-run")
			return 1
		}
	}
	a, _, vars, err := resolveAlias(alias, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 1
	}
	if a.Command == "" {
		fmt.Fprintln(os.Stderr, "xuz: no command defined for alias "+a.Name)
		return 1
	}
	fmt.Println(cmdx.ExportPrefix(vars) + a.Command)
	return 0
}

func doList(pathFlag string, namesOnly bool) int {
	cfg, err := loadConfig(pathFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 1
	}
	if len(cfg.Aliases) == 0 {
		if !namesOnly {
			fmt.Println("no aliases defined in", cfg.Path)
		}
		return 0
	}
	for _, a := range cfg.Aliases {
		if namesOnly {
			// One alias name per line: the shell completion scripts read
			// the list with "xuz ls --names".
			fmt.Println(a.Name)
			continue
		}
		fmt.Printf("%s\n", a.Name)
		for _, g := range a.Groups {
			fmt.Printf("  %s\n", g)
			for _, p := range a.GroupPairs[g] {
				fmt.Printf("    %s = %s\n", p.Key, p.LongText)
			}
		}
		fmt.Printf("  $ %s\n", a.Command)
	}
	return 0
}

// doCompletions prints a shell completion script (bash, zsh or fish) that
// completes the flags, the subcommands and the alias names from the config.
// The script is generated for the name the binary was invoked under (its
// base name): running it as "xuz" registers completions for "xuz". When
// a --config PATH was given, the script passes it to the binary. Without a
// shell argument it guesses one from $SHELL.
func doCompletions(args []string, configPath string) int {
	shell := ""
	if len(args) > 0 {
		shell = args[0]
	}
	if shell == "" {
		shell = shellFromEnv()
		if shell == "" {
			fmt.Fprintln(os.Stderr, "usage: xuz completions <bash|zsh|fish>")
			return 2
		}
	}
	script, ok := complete.Script(shell, commandName(), configPath)
	if !ok {
		fmt.Fprintf(os.Stderr, "xuz: unsupported shell %q (want bash, zsh or fish)\n", shell)
		return 2
	}
	fmt.Print(script)
	return 0
}

// shellFromEnv guesses the user's shell from $SHELL, "" when it is not one
// of the supported ones.
func shellFromEnv() string {
	switch filepath.Base(os.Getenv("SHELL")) {
	case "bash":
		return "bash"
	case "zsh":
		return "zsh"
	case "fish":
		return "fish"
	default:
		return ""
	}
}

// commandName is the base name of the binary as invoked ("xuz", or another
// name when it was installed under one), which the completion scripts call
// and register the completions under.
func commandName() string {
	switch base := filepath.Base(os.Args[0]); base {
	case "", ".", string(filepath.Separator):
		return "xuz"
	default:
		return base
	}
}

func doThemes(pathFlag string) int {
	cfg, err := loadConfig(pathFlag)
	active := ""
	if err == nil {
		active = cfg.Theme
	}
	if active == "" {
		active = "default"
	}
	for _, n := range theme.Builtins() {
		mark := ""
		if n == active {
			mark = "  (active)"
		}
		fmt.Printf("%s%s\n", n, mark)
	}
	if cfg != nil && len(cfg.CustomThemes) > 0 {
		fmt.Println("custom:")
		var names []string
		for n := range cfg.CustomThemes {
			names = append(names, n)
		}
		sortStrings(names)
		for _, n := range names {
			mark := ""
			if n == active {
				mark = "  (active)"
			}
			fmt.Printf("%s%s\n", n, mark)
		}
	}
	return 0
}

func doThemeSync(pathFlag string) int {
	// Read the current Omarchy theme slug.
	themePath := os.Getenv("HOME") + "/.local/state/omarchy/current/theme.name"
	data, err := os.ReadFile(themePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xuz: could not read current Omarchy theme — is Omarchy running?")
		return 1
	}
	slug := strings.ToLower(strings.TrimSpace(string(data)))

	// Find the theme's colors.toml (user theme first, then stock).
	var colorsPath string
	userPath := os.Getenv("HOME") + "/.config/omarchy/themes/" + slug + "/colors.toml"
	stockPath := "/usr/share/omarchy/themes/" + slug + "/colors.toml"
	if _, err := os.Stat(userPath); err == nil {
		colorsPath = userPath
	} else if _, err := os.Stat(stockPath); err == nil {
		colorsPath = stockPath
	} else {
		fmt.Fprintf(os.Stderr, "xuz: no colors.toml found for theme %q\n", slug)
		return 1
	}

	// Parse colors.toml fields.
	pairs, err := parseOmarchyColors(colorsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "xuz: failed to parse %s: %v\n", colorsPath, err)
		return 1
	}

	// Load existing xuz config.
	cfg, err := loadConfig(pathFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 1
	}

	// Map Omarchy colors to xuz theme fields.
	xuzFields := map[string]string{
		"bg":            pairs["background"],
		"fg":            pairs["foreground"],
		"accent":        pairs["accent"],
		"selected":      pairs["green"],
		"cursor_bg":     pairs["lighter_background"],
		"cursor_fg":     pairs["foreground"],
		"border":        pairs["selection"],
		"border_active": pairs["accent"],
		"title":         pairs["accent"],
		"muted":         pairs["dark_foreground"],
		"status":        pairs["accent"],
		"help":          pairs["dark_foreground"],
		"error":         pairs["red"],
		"success":       pairs["green"],
	}

	// Build a map of only the non-empty fields.
	themeFields := map[string]string{}
	for k, v := range xuzFields {
		if v != "" {
			themeFields[k] = v
		}
	}

	if len(themeFields) == 0 {
		fmt.Fprintln(os.Stderr, "xuz: no useful colors found in "+colorsPath)
		return 1
	}

	// Merge into the config's custom themes, overwriting any previous sync.
	cfg.CustomThemes[slug] = themeFields
	cfg.Theme = slug

	// Save the config.
	if err := cfg.Save(cfg.Path); err != nil {
		fmt.Fprintln(os.Stderr, "xuz: save failed: "+err.Error())
		return 1
	}

	fmt.Printf("synced xuz theme from Omarchy %q → %s\n", slug, cfg.Path)
	return 0
}

// parseOmarchyColors reads a simple key = "value" TOML file and returns a map.
func parseOmarchyColors(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pairs := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			// Strip surrounding quotes.
			if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
				val = val[1 : len(val)-1]
			}
			pairs[key] = val
		}
	}
	return pairs, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func doInit(pathFlag string) int {
	path := config.PathOf(pathFlag)
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintln(os.Stderr, "xuz: config already exists at "+path)
		return 1
	}
	data, err := sampleConfig.ReadFile("sample_config.yml")
	if err != nil {
		fmt.Fprintln(os.Stderr, "xuz: embedded sample missing: "+err.Error())
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 1
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 1
	}
	fmt.Printf("wrote sample config to %s\n", path)
	fmt.Println("edit it to match your models, then run: xuz")
	return 0
}

// resolveAlias applies the preselection priority (!default tag > history >
// last_used > first option) to the named alias and returns the alias, its
// resolved preselection per group (option key + source), and the resolved
// environment variables.
func resolveAlias(aliasName string, cfg *config.Config) (*config.Alias, map[string]config.Resolved, map[string]string, error) {
	var alias *config.Alias
	for _, a := range cfg.Aliases {
		if a.Name == aliasName {
			alias = a
			break
		}
	}
	if alias == nil {
		names := make([]string, 0, len(cfg.Aliases))
		for _, a := range cfg.Aliases {
			names = append(names, a.Name)
		}
		return nil, nil, nil, fmt.Errorf("unknown alias %q (available: %s)", aliasName, strings.Join(names, ", "))
	}
	hints := []config.Hint{}
	if entries, err := history.Load(history.Path()); err == nil {
		if e := history.LatestFor(entries, aliasName); e != nil {
			for _, s := range e.Sels {
				hints = append(hints, config.Hint{Group: s.Group, Key: s.Key, Source: config.SourceHistory})
			}
		}
	}
	lastUsed := cfg.LastUsed
	if per, ok := cfg.LastUsedByAlias[aliasName]; ok {
		lastUsed = per
	}
	for _, k := range lastUsed {
		hints = append(hints, config.Hint{Key: k, Source: config.SourceLastUsed})
	}
	sel := alias.ResolveSelections(hints)
	vars := map[string]string{}
	for _, g := range alias.Groups {
		vars[strings.ToUpper(g)] = alias.GroupPairs[g][0].LongText
		for _, p := range alias.GroupPairs[g] {
			if p.Key == sel[g].Key {
				vars[strings.ToUpper(g)] = p.LongText
			}
		}
	}
	for varName, groupName := range alias.Vars {
		if g, ok := sel[groupName]; ok {
			for _, p := range alias.GroupPairs[groupName] {
				if p.Key == g.Key {
					vars[varName] = p.LongText
				}
			}
		}
	}
	// Template-derived vars (when the alias has a template). A template that
	// fails to evaluate or parse is reported on stderr and contributes no
	// vars (the by-name vars still apply).
	if alias.Template != "" {
		out, err := cmdx.EvalTemplate(alias.Template, vars)
		if err != nil {
			fmt.Fprintf(os.Stderr, "xuz: template error: %v\n", err)
		} else if derived, err := cmdx.ParseDerivedVars(out); err != nil {
			fmt.Fprintf(os.Stderr, "xuz: template error: %v\n", err)
		} else {
			for k, v := range derived {
				vars[k] = v
			}
		}
	}
	return alias, sel, vars, nil
}

func doShow(aliasName, pathFlag string) int {
	cfg, err := loadConfig(pathFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 1
	}
	alias, sel, vars, err := resolveAlias(aliasName, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 1
	}
	for _, g := range alias.Groups {
		pair := alias.GroupPairs[g][0]
		for _, p := range alias.GroupPairs[g] {
			if p.Key == sel[g].Key {
				pair = p
			}
		}
		fmt.Printf("%s = %s # %s\n", g, pair.Key, pair.LongText)
	}
	fmt.Println("$ " + cmdx.ExportPrefix(vars) + alias.Command)
	return 0
}
