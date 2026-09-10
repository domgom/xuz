package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"

	"xuz/internal/config"
	"xuz/internal/history"
	"xuz/internal/theme"
)

const testYAML = `aliases:
  llama:
    options:
      model:
        - qwen-3.8-27B: !default /models/big.gguf
        - qwen-3.6-35B-A3B: /models/small.gguf
      context:
        - 64k: 65536
        - 256k: 262144
      command: llama-server -m "$MODEL" -c "$CONTEXT"
  other:
    options:
      model:
        - m1: v1
      command: echo other
`

var (
	tabKey       = tea.KeyMsg{Type: tea.KeyTab}
	shiftTabK    = tea.KeyMsg{Type: tea.KeyShiftTab}
	downKey      = tea.KeyMsg{Type: tea.KeyDown}
	upKey        = tea.KeyMsg{Type: tea.KeyUp}
	spaceKey     = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	enterKey     = tea.KeyMsg{Type: tea.KeyEnter}
	escKey       = tea.KeyMsg{Type: tea.KeyEscape}
	qKey         = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
	tKey         = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}
	nKey         = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	dKey         = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}
	cKey         = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}
	fKey         = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")}
	bKey         = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")}
	yKey         = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}
	questionKey  = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}
	ctrlSKey     = tea.KeyMsg{Type: tea.KeyCtrlS}                     // String() == "ctrl+s"
	slashKey     = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")} // String() == "/"
	ctrlSpaceKey = tea.KeyMsg{Type: tea.KeyCtrlAt}                    // String() == "ctrl+@"
	backspaceKey = tea.KeyMsg{Type: tea.KeyBackspace}                 // String() == "backspace"
)

// typeStr sends each rune of s to the model as a KeyRunes message.
func typeStr(t *testing.T, m *model, s string) {
	t.Helper()
	for _, r := range s {
		send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func loadCfg(t *testing.T) *config.Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(testYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func cfgFromYAML(t *testing.T, y string) *config.Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func newModel(t *testing.T, cfg *config.Config, start string) *model {
	t.Helper()
	m, err := New(Options{Cfg: cfg, StartAlias: start})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	return m
}

// newShortModel builds a compact-mode (Short) model on a 100x24 terminal.
func newShortModel(t *testing.T, cfg *config.Config, start string) *model {
	t.Helper()
	m, err := New(Options{Cfg: cfg, StartAlias: start, Short: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	return m
}

func send(t *testing.T, m *model, msg tea.Msg) {
	t.Helper()
	nm, _ := m.Update(msg)
	if nm != any(m) {
		t.Fatalf("model identity changed: got %T", nm)
	}
}

func mustContain(t *testing.T, view string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(view, w) {
			t.Errorf("view missing %q\n--- view ---\n%s", w, view)
		}
	}
}

func TestViewRendersAllColumns(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	view := m.View()
	if view == "" {
		t.Fatal("empty view")
	}
	// column titles (uppercased by the box label), alias names, option keys
	mustContain(t, view, "ALIAS", "llama", "other", "▸ llama", "MODEL", "CONTEXT",
		"qwen-3.8-27B", "qwen-3.6-35B-A3B", "64k", "256k")
	// default preselection: model !default tag -> qwen-3.8-27B (value /models/big.gguf),
	// context -> first (64k -> 65536). The preview shows the env prefix (the
	// assignments the command will run with) followed by the verbatim command.
	mustContain(t, view, "CONTEXT='65536'", "MODEL='/models/big.gguf'", "llama-server -m \"$MODEL\" -c \"$CONTEXT\"")
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > 100 {
			t.Errorf("line %d is %d columns wide (> 100): %q", i, w, line)
		}
	}
}

func TestViewEmptyBeforeWindowSize(t *testing.T) {
	cfg := loadCfg(t)
	m, err := New(Options{Cfg: cfg, StartAlias: "llama"})
	if err != nil {
		t.Fatal(err)
	}
	if v := m.View(); v != "" {
		t.Fatalf("expected empty view before WindowSizeMsg, got %q", v)
	}
}

func TestNavigationSelectAndRun(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View() // triggers ensureInited

	// StartAlias puts us in the model column already.
	send(t, m, downKey)
	send(t, m, spaceKey) // select qwen-3.6-35B-A3B
	send(t, m, tabKey)   // context column
	send(t, m, downKey)  // cursor -> 256k
	// Enter runs the cursor's row in the active column (256k) and the
	// previously selected option in the inactive column (qwen-3.6-35B-A3B).
	send(t, m, enterKey)
	if !m.doRun {
		t.Fatal("doRun not set after Enter")
	}
	if got := m.runVars["MODEL"]; got != "/models/small.gguf" {
		t.Errorf("MODEL = %q, want /models/small.gguf", got)
	}
	if got := m.runVars["CONTEXT"]; got != "262144" {
		t.Errorf("CONTEXT = %q, want 262144", got)
	}
	if m.runCmd != `llama-server -m "$MODEL" -c "$CONTEXT"` {
		t.Errorf("runCmd = %q", m.runCmd)
	}
	if len(m.entry.Sels) != 2 || m.entry.Sels[0] != (history.Selection{Group: "model", Key: "qwen-3.6-35B-A3B"}) || m.entry.Sels[1] != (history.Selection{Group: "context", Key: "256k"}) {
		t.Errorf("entry.Sels = %+v", m.entry.Sels)
	}
	if _, cmd := m.Update(tea.WindowSizeMsg{}); cmd == nil {
		t.Error("expected Quit command after doRun")
	}
}

func TestAliasColumnNavigation(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "") // start in the alias column
	m.View()
	if m.curCol != 0 || m.curAlias != 0 {
		t.Fatalf("start position col=%d alias=%d, want 0/0", m.curCol, m.curAlias)
	}
	send(t, m, downKey) // next alias, staying in the alias column
	if m.curAlias != 1 || m.curCol != 0 {
		t.Fatalf("after down: col=%d alias=%d, want col=0 alias=1", m.curCol, m.curAlias)
	}
	// The cursor moved to "other": its option columns are shown and the
	// command preview follows the cursor (the needle is hidden in the active
	// column; the cursor is the selection).
	mustContain(t, m.View(), "m1", "echo other")
}

// mouseClick sends a left-button press at the given screen coordinates.
func mouseClick(t *testing.T, m *model, x, y int) {
	t.Helper()
	send(t, m, tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

func TestMouseClickFocusAndSelect(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "") // alias column: llama (2 groups), other (1 group)
	m.View()
	if len(m.layout) != 3 {
		t.Fatalf("layout has %d columns, want 3 (alias+model+context)", len(m.layout))
	}

	// Clicking an option row focuses the column and selects that option.
	cl := m.layout[2]                  // context column (64k, 256k)
	mouseClick(t, m, cl.x+1, cl.y+1+1) // second row
	g := m.aliases[0].Groups[1]
	if m.curCol != 2 || g.Cursor != 1 || g.Selected != 1 {
		t.Errorf("after context click: col=%d cursor=%d selected=%d, want 2/1/1",
			m.curCol, g.Cursor, g.Selected)
	}

	cl = m.layout[1]                   // model column (qwen-3.8-27B, qwen-3.6-35B-A3B)
	mouseClick(t, m, cl.x+1, cl.y+1+0) // first row
	g = m.aliases[0].Groups[0]
	if m.curCol != 1 || g.Cursor != 0 || g.Selected != 0 {
		t.Errorf("after model click: col=%d cursor=%d selected=%d, want 1/0/0",
			m.curCol, g.Cursor, g.Selected)
	}

	// Clicking an alias row moves the alias cursor and focuses the alias column.
	cl = m.layout[0]
	mouseClick(t, m, cl.x+1, cl.y+1+1) // second alias: "other"
	if m.curAlias != 1 || m.curCol != 0 {
		t.Errorf("after alias click: alias=%d col=%d, want 1/0", m.curAlias, m.curCol)
	}
	m.View() // re-render for the newly selected alias
	if len(m.layout) != 2 {
		t.Fatalf("layout has %d columns after alias switch, want 2 (alias+model)", len(m.layout))
	}

	// Clicking a column's title bar only focuses the column (cursor untouched).
	cl = m.layout[1]
	g = m.aliases[1].Groups[0]
	before := g.Cursor
	mouseClick(t, m, cl.x+1, cl.y) // top border / title row
	if m.curCol != 1 || g.Cursor != before {
		t.Errorf("after title click: col=%d cursor=%d, want 1/%d", m.curCol, g.Cursor, before)
	}

	// Right clicks and motion events are ignored.
	send(t, m, tea.MouseMsg{X: cl.x + 1, Y: cl.y + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	send(t, m, tea.MouseMsg{X: cl.x + 1, Y: cl.y + 1, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if m.curCol != 1 || g.Cursor != before {
		t.Errorf("non-left/motion events changed state: col=%d cursor=%d, want 1/%d",
			m.curCol, g.Cursor, before)
	}
}

func TestColumnWidthsFitDistribute(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	if m.hasPeek || m.viewStart != 0 || m.hScroll != 0 {
		t.Fatalf("hasPeek=%v viewStart=%d hScroll=%d, want no overflow at 100 cols",
			m.hasPeek, m.viewStart, m.hScroll)
	}
	if len(m.layout) != 3 {
		t.Fatalf("layout has %d columns, want 3", len(m.layout))
	}
	total := 0
	for _, cl := range m.layout {
		total += cl.w
		if cl.w < minColW {
			t.Errorf("column at x=%d has width %d < minColW %d", cl.x, cl.w, minColW)
		}
	}
	if total > 100 {
		t.Errorf("columns overflow the terminal: %d > 100", total)
	}
	// The model column must be wide enough to show the longest key in full.
	if inner := m.layout[1].w - 4; inner < 2+17 {
		t.Errorf("model column inner width %d would cut the longest key (17 chars)", inner)
	}
	if v := m.View(); !strings.Contains(v, "qwen-3.6-35B-A3B") {
		t.Errorf("longest model key not fully rendered:\n%s", v)
	}
}

func TestColumnWidthsOverflowPeek(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  many:
    options:
      alpha:
        - alpha-1: value-1
        - alpha-2: value-2
      beta:
        - beta-1: value-1
        - beta-2: value-2
      gamma:
        - gamma-1: value-1
        - gamma-2: value-2
      delta:
        - delta-1: value-1
        - delta-2: value-2
      epsilon:
        - epsilon-1: value-1
        - epsilon-2: value-2
      zeta:
        - zeta-1: value-1
        - zeta-2: value-2
    command: echo many
`)
	m := newModel(t, cfg, "many")
	m.View()
	// 7 columns (alias + 6 groups) don't fit in 100 cols: expect a viewport
	// of full columns plus a narrow peek sliver of the next one.
	if !m.hasPeek {
		t.Fatal("expected a peek column when the columns overflow")
	}
	if len(m.layout) < 2 {
		t.Fatalf("layout has %d columns, want at least 2", len(m.layout))
	}
	total := 0
	for _, cl := range m.layout {
		total += cl.w
	}
	if total > 100 {
		t.Errorf("visible columns overflow the terminal: %d > 100", total)
	}
	if w := m.layout[len(m.layout)-1].w; w != peekColW {
		t.Errorf("last rendered column width = %d, want peek width %d", w, peekColW)
	}
	// The help line advertises the overflow while the peek is visible.
	if v := m.View(); !strings.Contains(v, "more columns to the right") {
		t.Errorf("peek hint missing from the help line:\n%s", v)
	}
	// Tab right until the last column: the viewport scrolls and the peek is
	// gone once the end is reached.
	for m.curCol < 6 {
		send(t, m, tabKey)
		m.View()
	}
	if m.hasPeek {
		t.Error("no peek expected once the last column is reached")
	}
	if last := m.viewStart + len(m.layout) - 1; last != 6 {
		t.Errorf("last rendered column is %d, want 6", last)
	}
	total = 0
	for _, cl := range m.layout {
		total += cl.w
	}
	if total > 100 {
		t.Errorf("visible columns overflow the terminal: %d > 100", total)
	}
}

func TestStartAliasNoGroupsClampsColumn(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  bare:
    command: echo hi
  full:
    options:
      g:
        - a: b
    command: echo full
`)
	m, err := New(Options{Cfg: cfg, StartAlias: "bare"})
	if err != nil {
		t.Fatal(err)
	}
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	v := m.View() // must not panic even though New() started on column 1
	if v == "" {
		t.Fatal("empty view")
	}
	if m.curCol != 0 {
		t.Errorf("curCol = %d, want 0 (clamped for a zero-group alias)", m.curCol)
	}
}

func TestShiftTabGoesBack(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, tabKey)
	send(t, m, tabKey)
	if m.curCol != 2 {
		t.Fatalf("curCol = %d, want 2", m.curCol)
	}
	send(t, m, shiftTabK)
	if m.curCol != 1 {
		t.Fatalf("after shift+tab curCol = %d, want 1", m.curCol)
	}
}

func TestEscQuitsFromAnyColumn(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama") // starts in the model column
	m.View()
	send(t, m, escKey)
	if m.curCol != 1 || !m.doQuit {
		t.Fatalf("esc from col 1: col=%d quit=%v, want 1/true", m.curCol, m.doQuit)
	}

	m0 := newModel(t, cfg, "") // alias column
	m0.View()
	send(t, m0, escKey)
	if !m0.doQuit {
		t.Fatal("esc from the alias column should quit")
	}
}

func TestQQuits(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, qKey)
	if !m.doQuit {
		t.Fatal("q should quit")
	}
}

func TestHistoryPreselection(t *testing.T) {
	cfg := loadCfg(t)
	hp := filepath.Join(t.TempDir(), "history")
	e := history.Entry{
		Time:  time.Now().UTC(),
		Alias: "llama",
		Sels:  []history.Selection{{Group: "model", Key: "qwen-3.6-35B-A3B"}, {Group: "context", Key: "256k"}},
	}
	if err := history.Append(hp, e, 10); err != nil {
		t.Fatal(err)
	}
	entries, err := history.Load(hp)
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Cfg: cfg, StartAlias: "llama", HistPath: hp})
	if err != nil {
		t.Fatal(err)
	}
	m.history = entries
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	view := m.View()
	// the model group carries a !default tag (qwen-3.8-27B): the default
	// beats the history preselection
	if m.aliases[0].Groups[0].Selected != 0 {
		t.Errorf("model preselection = %d, want 0 (!default beats history)", m.aliases[0].Groups[0].Selected)
	}
	if m.aliases[0].Groups[0].Src != config.SourceDefault {
		t.Errorf("model source = %q, want %q", m.aliases[0].Groups[0].Src, config.SourceDefault)
	}
	// the context group has no default: the history preselection (256k)
	// applies and carries the muted needle
	if m.aliases[0].Groups[1].Selected != 1 {
		t.Errorf("context preselection = %d, want 1 (from history)", m.aliases[0].Groups[1].Selected)
	}
	if m.aliases[0].Groups[1].Src != config.SourceHistory {
		t.Errorf("context source = %q, want %q", m.aliases[0].Groups[1].Src, config.SourceHistory)
	}
	mustContain(t, view, "/models/big.gguf", "262144")
	if line := lineContaining(t, view, "256k"); !strings.Contains(line, "▸") {
		t.Errorf("history preselection should carry the ▸ needle: %q", line)
	}
	if strings.Contains(view, "⧖") {
		t.Errorf("the hourglass must never render:\n%s", view)
	}
}

func TestDryRunSubstitutesAndQuits(t *testing.T) {
	cfg := loadCfg(t)
	m, err := New(Options{Cfg: cfg, StartAlias: "llama", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.View()
	send(t, m, enterKey)
	if !m.doQuit {
		t.Fatal("dry-run Enter should quit")
	}
	if m.dryCmd != `CONTEXT='65536' MODEL='/models/big.gguf' llama-server -m "$MODEL" -c "$CONTEXT"` {
		t.Errorf("dryCmd = %q", m.dryCmd)
	}
}

// TestTemplateDerivedVarsInPreview guards the optional per-alias template:
// it derives extra env vars from the selected options, and the preview shows
// them in the env prefix (the command text stays verbatim).
func TestTemplateDerivedVarsInPreview(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  llama:
    options:
      model:
        - qwen-3.8-27B: !default /models/big.gguf
      context:
        - 64k: 65536
      command: llama-server -m "$MODEL" -c "$CONTEXT" -t "$THREADS"
      template: |
        THREADS=4
        CTX_KB={{ div (int .CONTEXT) 1024 }}
`)
	m := newModel(t, cfg, "llama")
	view := m.View()
	// The derived vars (THREADS, CTX_KB) appear in the env prefix, sorted.
	mustContain(t, view, "CTX_KB='64'", "THREADS='4'", "llama-server -m \"$MODEL\"")
}

// TestTemplateErrorShowsStatus guards a template that fails to evaluate: the
// error is reported on the status line and the by-name vars still apply.
func TestTemplateErrorShowsStatus(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  llama:
    options:
      model:
        - qwen-3.8-27B: !default /models/big.gguf
      command: llama-server -m "$MODEL"
      template: |
        BAD={{ div 1 0 }}
`)
	m := newModel(t, cfg, "llama")
	m.View()
	if !strings.Contains(m.status, "template error") {
		t.Errorf("status = %q, want a template error", m.status)
	}
	// The by-name var still appears in the prefix (the preview is shown when
	// there is no status, but the var is still computed for the run).
	if got := m.fullVars(0, m.aliases[0]); got["MODEL"] != "/models/big.gguf" {
		t.Errorf("fullVars MODEL = %q, want /models/big.gguf", got["MODEL"])
	}
}

func TestThemeCycle(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	first := m.th.Name
	send(t, m, tKey)
	if m.th.Name == first {
		t.Fatal("theme did not change after t")
	}
	if m.status != "theme: "+m.th.Name {
		t.Errorf("status = %q", m.status)
	}
	view := m.View()
	if view == "" {
		t.Fatal("view empty after theme switch")
	}
}

func TestNoCommandAlias(t *testing.T) {
	cfg := loadCfg(t)
	cfg.Aliases[1].Command = "" // strip the command from the "other" alias
	m := newModel(t, cfg, "other")
	m.View()
	send(t, m, enterKey)
	if m.doRun || m.doQuit {
		t.Fatal("Enter without a command must not run or quit")
	}
	if m.status != "no command defined for this alias" {
		t.Errorf("status = %q", m.status)
	}
}

func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		s, pat string
		want   bool
	}{
		{"qwen-3.8-27B", "q38", true}, // subsequence, not substring
		{"qwen-3.6-35B-A3B", "q36", true},
		{"qwen-3.8-27B", "Q38", true}, // case-insensitive
		{"qwen-3.6-35B-A3B", "q38", false},
		{"llama", "ll", true},
		{"other", "ll", false},
		{"m1", "m1", true},
		{"anything", "", true},
		{"", "a", false},
	}
	for _, c := range cases {
		if got := fuzzyMatch(c.s, c.pat); got != c.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", c.s, c.pat, got, c.want)
		}
	}
}

func TestAddNewValue(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, tabKey) // context column
	send(t, m, nKey)
	if m.prompt == nil || m.prompt.kind != promptInput {
		t.Fatal("n should open the new-value input prompt")
	}
	mustContain(t, m.View(), "new context option")
	typeStr(t, m, "32k 32768")
	send(t, m, enterKey)
	if m.prompt != nil {
		t.Fatal("prompt should close after enter")
	}
	if !m.dirty {
		t.Error("config should be dirty after adding a value")
	}
	got := cfg.Aliases[0].GroupPairs["context"]
	if len(got) != 3 || got[0] != (config.Pair{Key: "32k", LongText: "32768"}) {
		t.Errorf("context pairs = %+v", got)
	}
	// duplicate key: rejected, prompt stays open for editing
	send(t, m, nKey)
	typeStr(t, m, "32k 99999")
	send(t, m, enterKey)
	if m.prompt == nil {
		t.Fatal("duplicate should keep the prompt open")
	}
	if !strings.Contains(m.status, "already exists") {
		t.Errorf("status = %q", m.status)
	}
	if len(cfg.Aliases[0].GroupPairs["context"]) != 3 {
		t.Errorf("duplicate must not be added: %+v", cfg.Aliases[0].GroupPairs["context"])
	}
	send(t, m, escKey)
}

func TestDeleteValue(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	// cursor is on qwen-3.8-27B (index 0), which is also the default
	send(t, m, dKey)
	if m.prompt == nil || m.prompt.kind != promptConfirm {
		t.Fatal("d should open the delete confirm prompt")
	}
	if !strings.Contains(m.prompt.label, "qwen-3.8-27B") {
		t.Errorf("label = %q", m.prompt.label)
	}
	send(t, m, yKey)
	pairs := cfg.Aliases[0].GroupPairs["model"]
	if len(pairs) != 1 || pairs[0].Key != "qwen-3.6-35B-A3B" {
		t.Fatalf("model pairs = %+v", pairs)
	}
	if d, ok := cfg.Aliases[0].Defaults["model"]; ok {
		t.Errorf("default should be cleared, got %q", d)
	}
	if !m.dirty {
		t.Error("config should be dirty after delete")
	}
	// the last remaining pair cannot be deleted
	send(t, m, dKey)
	if m.prompt != nil {
		t.Fatal("deleting the last option must be refused")
	}
	if !strings.Contains(m.status, "cannot delete the last option") {
		t.Errorf("status = %q", m.status)
	}
}

func TestSetDefault(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, downKey) // cursor -> qwen-3.6-35B-A3B
	send(t, m, ctrlSpaceKey)
	if got := cfg.Aliases[0].Defaults["model"]; got != "qwen-3.6-35B-A3B" {
		t.Errorf("Defaults[model] = %q", got)
	}
	if !m.dirty {
		t.Error("config should be dirty after setting a default")
	}
}

func TestSaveConfig(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, downKey)
	send(t, m, ctrlSpaceKey) // make it dirty
	send(t, m, ctrlSKey)
	if m.dirty {
		t.Fatal("dirty should be cleared after ctrl+s")
	}
	if !strings.Contains(m.status, "config saved") {
		t.Errorf("status = %q", m.status)
	}
	reloaded, err := config.Load(cfg.Path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.Aliases[0].Defaults["model"]; got != "qwen-3.6-35B-A3B" {
		t.Errorf("reloaded Defaults[model] = %q", got)
	}
}

func TestCopyCommand(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()

	oldTools := clipboardTools
	clipboardTools = [][2]string{{"xuz-no-such-tool-xyz", ""}}
	var got string
	oldWrite := osc52Write
	osc52Write = func(s string) { got = s }
	defer func() { clipboardTools = oldTools; osc52Write = oldWrite }()

	send(t, m, cKey)
	if m.status != "command copied (OSC 52)" {
		t.Fatalf("status = %q", m.status)
	}
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(`CONTEXT='65536' MODEL='/models/big.gguf' llama-server -m "$MODEL" -c "$CONTEXT"`)) + "\a"
	if got != want {
		t.Errorf("OSC 52 payload = %q, want %q", got, want)
	}
}

// TestCopyCommandViaClipboardTool covers the tool-present path: the resolved
// command must actually be piped into the tool's stdin. wl-copy, xclip and
// xsel all read the text from stdin; without it they store an empty string
// while still exiting 0 (status says "copied", clipboard stays empty).
func TestCopyCommandViaClipboardTool(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()

	dir := t.TempDir()
	captured := filepath.Join(dir, "clipboard.txt")
	tool := filepath.Join(dir, "xuz-fake-clip")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\ncat > "+captured+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	oldTools := clipboardTools
	clipboardTools = [][2]string{{"xuz-fake-clip", ""}}
	defer func() { clipboardTools = oldTools }()

	send(t, m, cKey)
	if m.status != "command copied (xuz-fake-clip)" {
		t.Fatalf("status = %q", m.status)
	}
	got, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("clipboard tool captured nothing: %v", err)
	}
	want := `CONTEXT='65536' MODEL='/models/big.gguf' llama-server -m "$MODEL" -c "$CONTEXT"`
	if string(got) != want {
		t.Errorf("clipboard content = %q, want %q", got, want)
	}
}

// TestCopyCommandFailingToolFallsThrough: a tool that exits non-zero (e.g.
// wl-copy with no Wayland display) must not count as a copy; OSC 52 is used.
func TestCopyCommandFailingToolFallsThrough(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()

	dir := t.TempDir()
	tool := filepath.Join(dir, "xuz-failing-clip")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	oldTools := clipboardTools
	clipboardTools = [][2]string{{"xuz-failing-clip", ""}}
	var got string
	oldWrite := osc52Write
	osc52Write = func(s string) { got = s }
	defer func() { clipboardTools = oldTools; osc52Write = oldWrite }()

	send(t, m, cKey)
	if m.status != "command copied (OSC 52)" {
		t.Fatalf("status = %q", m.status)
	}
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(`CONTEXT='65536' MODEL='/models/big.gguf' llama-server -m "$MODEL" -c "$CONTEXT"`)) + "\a"
	if got != want {
		t.Errorf("OSC 52 payload = %q, want %q", got, want)
	}
}

func TestSearchFiltersOptionColumn(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama") // model column
	m.View()

	send(t, m, fKey)
	if !m.search {
		t.Fatal("f should start the filter")
	}
	// the filter input lives inside the filtered column's box
	mustContain(t, ansi.Strip(m.View()), "filter █", "exit filter")
	typeStr(t, m, "3.6")
	v := m.View()
	if !strings.Contains(v, "qwen-3.6-35B-A3B") || strings.Contains(v, "qwen-3.8-27B") {
		t.Errorf("filter \"3.6\" wrong:\n%s", v)
	}
	g := m.aliases[0].Groups[0]
	if g.Cursor != 1 {
		t.Errorf("cursor = %d, want 1 (snapped to first match)", g.Cursor)
	}
	send(t, m, downKey) // single match: cursor must stay on it
	if g.Cursor != 1 {
		t.Errorf("after down cursor = %d, want 1", g.Cursor)
	}
	typeStr(t, m, "zz")
	if !strings.Contains(m.View(), "(no matches)") {
		t.Error("expected a (no matches) row")
	}
	for i := 0; i < 2; i++ {
		send(t, m, backspaceKey) // buffer back to "3.6"
	}
	if v := m.View(); !strings.Contains(v, "qwen-3.6-35B-A3B") || strings.Contains(v, "qwen-3.8-27B") {
		t.Errorf("backspace should restore the match:\n%s", v)
	}

	// esc exits the search and restores the full list
	send(t, m, escKey)
	if m.search {
		t.Fatal("esc should exit the search")
	}
	if v := m.View(); !strings.Contains(v, "qwen-3.8-27B") || !strings.Contains(v, "qwen-3.6-35B-A3B") {
		t.Errorf("esc should restore the full list:\n%s", v)
	}

	// fuzzy subsequence: "q38" matches qwen-3.8-27B only
	send(t, m, fKey)
	typeStr(t, m, "q38")
	v = m.View()
	if !strings.Contains(v, "qwen-3.8-27B") || strings.Contains(v, "qwen-3.6-35B-A3B") {
		t.Errorf("fuzzy \"q38\" wrong:\n%s", v)
	}
	send(t, m, enterKey) // run with the filtered list
	if !m.doRun || m.search {
		t.Fatal("enter during search should exit the search and run")
	}
}

func TestSearchAliasColumn(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "")
	m.View()
	send(t, m, fKey)
	typeStr(t, m, "ll")
	v := m.View()
	if !strings.Contains(v, "llama") || strings.Contains(v, "other") {
		t.Errorf("alias filter \"ll\" wrong:\n%s", v)
	}
	send(t, m, escKey)
	v = m.View()
	if !strings.Contains(v, "llama") || !strings.Contains(v, "other") {
		t.Errorf("esc should restore the alias list:\n%s", v)
	}
}

// TestSearchOnlyFiltersActiveColumn guards against the filter leaking into
// other columns: whichever column is active is the only one narrowed.
func TestSearchOnlyFiltersActiveColumn(t *testing.T) {
	// Searching in an option column must not narrow the alias column.
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama") // model column active
	m.View()
	send(t, m, fKey)
	typeStr(t, m, "3.6")
	v := m.View()
	if !strings.Contains(v, "qwen-3.6-35B-A3B") || strings.Contains(v, "qwen-3.8-27B") {
		t.Errorf("model column should be filtered to 3.6:\n%s", v)
	}
	if !strings.Contains(v, "llama") || !strings.Contains(v, "other") {
		t.Errorf("alias column must stay unfiltered while searching the model column:\n%s", v)
	}
	if strings.Contains(v, "(no matches)") {
		t.Errorf("no column should be empty while searching the model column:\n%s", v)
	}
	send(t, m, escKey)

	// Searching in the alias column must not narrow the option columns,
	// and moving the cursor must keep focus in the alias column.
	cfg = loadCfg(t)
	m = newModel(t, cfg, "")
	m.View()
	send(t, m, fKey)
	typeStr(t, m, "ll")
	v = m.View()
	if !strings.Contains(v, "llama") || strings.Contains(v, "other") {
		t.Errorf("alias column should be filtered to llama:\n%s", v)
	}
	if !strings.Contains(v, "qwen-3.8-27B") || !strings.Contains(v, "qwen-3.6-35B-A3B") {
		t.Errorf("model column must stay unfiltered while searching the alias column:\n%s", v)
	}
	send(t, m, downKey)
	if m.curCol != 0 || m.curAlias != 0 {
		t.Errorf("after down: col=%d alias=%d, want col=0 alias=0 (single match)", m.curCol, m.curAlias)
	}
	if v := m.View(); !strings.Contains(v, "qwen-3.8-27B") || strings.Contains(v, "other") {
		t.Errorf("columns changed after moving the cursor during alias search:\n%s", v)
	}
}

// TestFullModeNavigationUnadvertised guards the full mode's column
// navigation: tab/shift+tab (and left/right) keep moving between columns —
// including each alias's rightmost boundary, as each alias carries its own
// number of groups — but the legend no longer advertises them: no "next
// column" or "previous column" item in any state, and the move item carries
// the same four-direction tokens as the compact mode.
func TestFullModeNavigationUnadvertised(t *testing.T) {
	cfg := loadCfg(t) // llama: model+context (2), other: model (1)

	// The legend never advertises the column navigation, in any state.
	for _, state := range []struct {
		name string
		fn   func(m *model)
	}{
		{"alias column", func(m *model) { m.curCol = 0 }},
		{"middle column", func(m *model) {}},
		{"rightmost column", func(m *model) { m.curCol = 2 }},
		{"filtering the alias column", func(m *model) { m.curCol = 0; m.search = true }},
		{"filtering the rightmost column", func(m *model) { m.curCol = 2; m.search = true }},
	} {
		m := newModel(t, cfg, "llama")
		state.fn(m)
		v := ansi.Strip(m.View())
		if strings.Contains(v, "next column") || strings.Contains(v, "previous column") {
			t.Errorf("%s: legend must not advertise column navigation:\n%s", state.name, v)
		}
		if !strings.Contains(v, "↑↓←→ move") {
			t.Errorf("%s: legend must keep the four-direction move item:\n%s", state.name, v)
		}
	}

	// ... while the keys keep working, unadvertised.
	m := newModel(t, cfg, "llama") // starts on llama's model column (1 of 2)
	send(t, m, tabKey)             // -> context: the rightmost column of llama
	if m.curCol != 2 {
		t.Fatalf("tab should land on column 2, got %d", m.curCol)
	}
	send(t, m, tabKey) // a no-op at the rightmost
	if m.curCol != 2 {
		t.Errorf("tab at the rightmost column must not move, col=%d", m.curCol)
	}
	send(t, m, shiftTabK)
	send(t, m, shiftTabK)
	if m.curCol != 0 {
		t.Fatalf("shift+tab twice should land on the alias column, got %d", m.curCol)
	}
	send(t, m, shiftTabK) // a no-op: nothing sits left of the alias column
	if m.curCol != 0 {
		t.Errorf("shift+tab at the alias column must not move, col=%d", m.curCol)
	}

	// different number of columns per alias: "other" has a single group,
	// so its first option column is already the rightmost one
	send(t, m, downKey) // cursor -> other
	if m.curAlias != 1 {
		t.Fatalf("cursor should be on other, got %d", m.curAlias)
	}
	send(t, m, tabKey) // into other's only group (curCol=1)
	if m.curCol != 1 {
		t.Fatalf("tab should land on column 1, got %d", m.curCol)
	}
	send(t, m, tabKey) // a no-op: other has no next column
	if m.curCol != 1 {
		t.Errorf("tab at other's only column must not move, col=%d", m.curCol)
	}

	// while filtering, tab exits the filter and keeps its normal meaning
	// (at other's rightmost column it moves nowhere)
	send(t, m, fKey)
	if !m.search {
		t.Fatal("f should start the filter")
	}
	send(t, m, tabKey)
	if m.search || m.curCol != 1 {
		t.Errorf("tab during the filter should exit it and stay put: search=%v col=%d",
			m.search, m.curCol)
	}

	// an alias with no groups has neither neighbor anywhere
	cfg2 := cfgFromYAML(t, "aliases:\n"+
		"  bare:\n"+
		"    options: {}\n"+
		"    command: echo bare\n")
	m2 := newModel(t, cfg2, "")
	send(t, m2, tabKey) // a no-op: nowhere to go
	if m2.curCol != 0 {
		t.Errorf("tab must not move on an alias without groups, col=%d", m2.curCol)
	}
}

// TestSearchInputInsideColumn guards the full mode's filter input placement:
// the filter is contextual to the searched column's content, so it renders
// as the bottom row inside that column's box — not on a footer line. The
// footer keeps its usual live command preview while searching.
func TestSearchInputInsideColumn(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama") // model column active
	m.View()
	send(t, m, fKey)
	v := m.View()
	lines := strings.Split(v, "\n")

	// the input sits on the last content row of the searched column's box
	cl := m.layout[1] // alias + model + context all fit at 100 columns
	if !strings.Contains(lines[cl.y+cl.contentLines], "filter █") {
		t.Errorf("filter input missing from the bottom of the model column:\n%s", v)
	}
	// ... and nowhere in the footer, which keeps the command preview
	foot := strings.Join(m.footerLines(), "\n")
	if strings.Contains(foot, "filter █") {
		t.Errorf("the search input must not appear outside the column:\n%s", foot)
	}
	if !strings.Contains(ansi.Strip(foot), "$ CONTEXT='65536' MODEL='/models/big.gguf' llama-server") {
		t.Errorf("footer should keep the live command preview while searching:\n%s", foot)
	}

	// typing filters inside the box, with the input following the filter
	typeStr(t, m, "3.6")
	if v := m.View(); !strings.Contains(v, "filter 3.6█") {
		t.Errorf("the in-column input should show the typed filter:\n%s", v)
	}

	// the same holds for the alias column's box
	send(t, m, escKey)    // exit the search
	send(t, m, shiftTabK) // back to the alias column
	send(t, m, fKey)
	m.View()
	al := m.layout[0]
	if !strings.Contains(strings.Split(m.View(), "\n")[al.y+al.contentLines], "filter █") {
		t.Errorf("filter input missing from the bottom of the alias column:\n%s", m.View())
	}

	// exiting the search removes the input and restores the full list
	send(t, m, escKey)
	if strings.Contains(m.View(), "filter █") {
		t.Error("the filter input should disappear when the filter exits")
	}
}

func TestBackgroundArmedOnEnter(t *testing.T) {
	cfg := loadCfg(t)
	hist := filepath.Join(t.TempDir(), "history")
	m, err := New(Options{Cfg: cfg, StartAlias: "llama", HistPath: hist})
	if err != nil {
		t.Fatal(err)
	}
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.View()

	// b arms background mode without running anything
	send(t, m, bKey)
	if !m.background || m.doRun || m.doQuit {
		t.Fatalf("after b: background=%v run=%v quit=%v, want true/false/false",
			m.background, m.doRun, m.doQuit)
	}
	if m.logPath != "" {
		t.Errorf("b must not set a log path yet, got %q", m.logPath)
	}

	// Enter with background armed queues a background run
	send(t, m, enterKey)
	if !m.doRun || !m.background {
		t.Fatalf("after enter: run=%v background=%v, want true/true", m.doRun, m.background)
	}
	if !strings.HasPrefix(m.logPath, filepath.Dir(hist)) ||
		!strings.Contains(m.logPath, "llama-") ||
		!strings.HasSuffix(m.logPath, ".log") {
		t.Errorf("logPath = %q", m.logPath)
	}
}

func TestBackgroundToggleOff(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, bKey)
	send(t, m, bKey) // disarm again
	if m.background {
		t.Fatal("two b presses should leave background off")
	}
	send(t, m, enterKey)
	if !m.doRun || m.background {
		t.Fatalf("after enter: run=%v background=%v, want true/false (foreground)",
			m.doRun, m.background)
	}
	if m.logPath != "" {
		t.Errorf("foreground run must not set a log path, got %q", m.logPath)
	}
}

func TestStartDetachedWritesLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "detached.log")
	if code := startDetached(`echo hello-xuz "$FOO"`, map[string]string{"FOO": "bar"}, logPath); code != 0 {
		t.Fatalf("startDetached code = %d", code)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(data), "hello-xuz bar") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, _ := os.ReadFile(logPath)
	t.Errorf("log never received the output: %q", data)
}

func TestQuitSavePrompt(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, downKey)
	send(t, m, ctrlSpaceKey) // make it dirty

	send(t, m, qKey)
	if m.doQuit {
		t.Fatal("q with unsaved changes must not quit yet")
	}
	if m.prompt == nil || m.prompt.action != actSaveOnQuit {
		t.Fatal("expected the save-on-quit confirm prompt")
	}
	if !strings.Contains(m.prompt.label, "save to") {
		t.Errorf("label = %q", m.prompt.label)
	}
	send(t, m, yKey)
	if m.dirty || !m.doQuit {
		t.Fatalf("y should save and quit (dirty=%v quit=%v)", m.dirty, m.doQuit)
	}
	reloaded, err := config.Load(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Aliases[0].Defaults["model"]; got != "qwen-3.6-35B-A3B" {
		t.Errorf("saved default = %q", got)
	}

	// decline: quits without touching the file (fresh config on disk)
	cfg2 := loadCfg(t)
	m2 := newModel(t, cfg2, "llama")
	m2.View()
	send(t, m2, downKey)
	send(t, m2, ctrlSpaceKey)
	send(t, m2, qKey)
	if m2.prompt == nil {
		t.Fatal("expected the save prompt")
	}
	send(t, m2, nKey) // "n" in a confirm prompt answers no
	if !m2.doQuit || m2.prompt != nil {
		t.Fatalf("n should quit without saving (quit=%v prompt=%v)", m2.doQuit, m2.prompt)
	}
	if !m2.dirty {
		t.Error("dirty should remain true when the save is declined")
	}
	reloaded2, err := config.Load(cfg2.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded2.Aliases[0].Defaults["model"]; got != "qwen-3.8-27B" {
		t.Errorf("config on disk changed: default = %q", got)
	}
}

func TestFooterHelpLines(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	v := ansi.Strip(m.View())
	mustContain(t, v, "new", "delete", "set default",
		"filter", "copy", "ave", "background")

	m0 := newModel(t, cfg, "")
	v0 := ansi.Strip(m0.View())
	mustContain(t, v0, "filter", "select")
	if strings.Contains(v0, "new") {
		t.Error("alias column help must not list option-column keys")
	}

	// The exit item (the same glyph and legend as the compact mode, "󱊷  exit")
	// leads the picker legend, in the alias column and the option columns
	// alike, and the old "q quit" item is gone.
	for name, view := range map[string]string{"option column": v, "alias column": v0} {
		mustContain(t, view, "󱊷  exit")
		// The esc glyph only appears in the shortcut legend, so the line
		// carrying it is the first help line (the legend may wrap).
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "󱊷") && !strings.HasPrefix(line, "󱊷  exit") {
				t.Errorf("%s: the help line should start with the exit item, got %q", name, line)
			}
		}
		if strings.Contains(view, "quit") {
			t.Errorf("%s: picker legend must not list \"q quit\" anymore", name)
		}
	}

	// filter legend
	send(t, m0, fKey)
	if !strings.Contains(ansi.Strip(m0.View()), "exit filter") {
		t.Error("filter legend missing")
	}
	send(t, m0, escKey)

	// input prompt footer
	send(t, m, nKey)
	if !strings.Contains(ansi.Strip(m.View()), "\u21b5 add") {
		t.Error("input prompt footer missing")
	}
	send(t, m, escKey)

	// confirm prompt footer
	send(t, m, dKey)
	if !strings.Contains(ansi.Strip(m.View()), "yes") {
		t.Error("confirm prompt footer missing")
	}
	send(t, m, escKey)

	// info line
	send(t, m, questionKey)
	mustContain(t, m.View(), "config: ", "theme: ")
	send(t, m, questionKey)
	if strings.Contains(m.View(), "config: ") {
		t.Error("second ? should hide the info line")
	}

	// every line must fit the width
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 100 {
			t.Errorf("line %d is %d columns wide (> 100): %q", i, w, line)
		}
	}
}

// TestCommandPanelWrapsInsteadOfCutting guards the full mode's bottom COMMAND
// panel on a narrow terminal: a wide preview must overflow onto extra rows
// inside the bordered panel instead of being truncated, and the column boxes
// must shrink to make room.
func TestCommandPanelWrapsInsteadOfCutting(t *testing.T) {
	path := "/very/long/path/to/models/Unsloth_Qwen3.8-27B-UD-IQ4_XS.gguf"
	cfg := cfgFromYAML(t, "aliases:\n"+
		"  llama:\n"+
		"    options:\n"+
		"      model:\n"+
		"        - qwen: "+path+"\n"+
		"      command: exec llama-server -m \"$MODEL\" --flash-attn --no-mmap -c 65536\n")
	m, err := New(Options{Cfg: cfg, StartAlias: "llama"})
	if err != nil {
		t.Fatal(err)
	}
	const width, height = 42, 20
	send(t, m, tea.WindowSizeMsg{Width: width, Height: height})

	lines := m.commandPanelLines()
	// The panel is a bordered box: a top border carrying the COMMAND label,
	// content rows, then the bottom border.
	if got := ansi.Strip(lines[0]); !strings.HasPrefix(got, "┌─") || !strings.Contains(got, "COMMAND") || !strings.HasSuffix(got, "┐") {
		t.Errorf("panel top border = %q, want ┌─ ... COMMAND ... ┐", got)
	}
	if got := ansi.Strip(lines[len(lines)-1]); !strings.HasPrefix(got, "└") || !strings.HasSuffix(got, "┘") {
		t.Errorf("panel bottom border = %q, want └ ... ┘", got)
	}
	content := lines[1 : len(lines)-1]
	if len(content) < 2 {
		t.Fatalf("preview must wrap to multiple content rows at %d cols, got %d", width, len(content))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != width {
			t.Errorf("panel line %d is %d cols wide (want exactly %d)", i, w, width)
		}
	}
	// Wrapping must not lose or add any text (whitespace at wrap points may
	// differ, so compare with all whitespace compacted away). Strip the box
	// rails from each content row first.
	var got strings.Builder
	for _, l := range content {
		s := ansi.Strip(l)
		got.WriteString(strings.TrimSuffix(strings.TrimPrefix(s, "│ "), " │"))
	}
	compact := func(s string) string { return ansi.Strip(strings.ReplaceAll(s, " ", "")) }
	if g, w := compact(got.String()), compact(m.commandPreview()); g != w {
		t.Errorf("wrapped preview text differs:\n got %q\nwant %q", g, w)
	}
	// The frame stays within the width, and the boxes shrink for the extra
	// footer lines: view height = title + box + footer.
	view := m.View()
	for i, l := range strings.Split(view, "\n") {
		if w := lipgloss.Width(l); w > width {
			t.Errorf("view line %d is %d cols wide (> %d)", i, w, width)
		}
	}
	fn := len(m.footerLines())
	boxH := height - 1 - fn
	if boxH < 4 {
		boxH = 4
	}
	if n, want := len(strings.Split(view, "\n")), 1+boxH+fn; n != want {
		t.Errorf("view has %d lines, want %d (title + box %d + footer %d)", n, want, boxH, fn)
	}
}

// TestCommandPanelBottom guards the full mode's bordered COMMAND panel at the
// bottom of the screen: it encapsulates the live preview (env prefix +
// verbatim command) in a box styled like the columns, below them — and short
// mode has no such panel.
func TestCommandPanelBottom(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	view := m.View()
	lines := strings.Split(view, "\n")

	// The panel's top border carries the COMMAND label.
	topIdx := -1
	for i, l := range lines {
		if s := ansi.Strip(l); strings.HasPrefix(s, "┌─") && strings.Contains(s, "COMMAND") {
			topIdx = i
			break
		}
	}
	if topIdx < 0 {
		t.Fatalf("view missing the COMMAND panel top border:\n%s", view)
	}
	// Its content row holds the live preview inside the box rails.
	panel := lines[topIdx+1]
	if got := ansi.Strip(panel); !strings.Contains(got, "$ CONTEXT='65536' MODEL='/models/big.gguf' llama-server") {
		t.Errorf("panel content = %q", got)
	}
	// Single-line preview here: the bottom border follows the content row.
	if bot := ansi.Strip(lines[topIdx+2]); !strings.HasPrefix(bot, "└") || !strings.HasSuffix(bot, "┘") {
		t.Errorf("expected the panel bottom border at line %d, got %q", topIdx+2, bot)
	}
	// The panel sits below the column boxes: after every box bottom border
	// (scan only above the panel — its own bottom border starts with └ too).
	lastBoxBottom := -1
	for i, l := range lines[:topIdx] {
		if s := ansi.Strip(l); strings.HasPrefix(s, "└") {
			lastBoxBottom = i
		}
	}
	if topIdx <= lastBoxBottom {
		t.Errorf("panel top border (line %d) should be below the column boxes (last bottom at line %d)", topIdx, lastBoxBottom)
	}

	// Short mode: no COMMAND panel.
	sm, err := New(Options{Cfg: cfg, Short: true})
	if err != nil {
		t.Fatal(err)
	}
	send(t, sm, tea.WindowSizeMsg{Width: 100, Height: 24})
	if sv := sm.View(); strings.Contains(ansi.Strip(sv), "COMMAND") {
		t.Errorf("short mode must not show the COMMAND panel:\n%s", sv)
	}
}

// lineContaining returns the view line whose plain text contains sub.
func lineContaining(t *testing.T, view, sub string) string {
	t.Helper()
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(l), sub) {
			return l
		}
	}
	t.Fatalf("view has no line containing %q:\n%s", sub, view)
	return ""
}

// assertFullRowHighlight checks that every cell of the option row containing
// key in column colIdx of the view carries the theme's cursor background.
func assertFullRowHighlight(t *testing.T, m *model, name, key string, colIdx int) {
	t.Helper()
	view := m.View()
	line := lineContaining(t, view, key)
	assertRowCellsHighlight(t, m, name, line, colIdx)
}

// assertRowCellsHighlight checks that every cell of line between the box
// borders of column colIdx carries the theme's cursor background.
func assertRowCellsHighlight(t *testing.T, m *model, name, line string, colIdx int) {
	t.Helper()
	cl := m.layout[colIdx]
	buf := cellbuf.NewBuffer(lipgloss.Width(line), 1)
	cellbuf.SetContent(buf, line)
	hex := string(theme.NormalizeColor(m.th.CursorBg))
	n, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil {
		t.Fatalf("cursor_bg %q is not a hex color: %v", hex, err)
	}
	want := ansi.TrueColor(n)
	wr, wg, wb, _ := want.RGBA()
	// Row cells sit between the box borders of the column's box.
	for x := cl.x + 2; x <= cl.x+cl.w-3; x++ {
		c := buf.Cell(x, 0)
		if c == nil {
			t.Errorf("%s: no cell at x=%d", name, x)
			return
		}
		bg := c.Style.Bg
		if bg == nil {
			t.Errorf("%s: cell x=%d (%q) has no background", name, x, c.String())
			return
		}
		r, g, b, _ := bg.RGBA()
		if r != wr || g != wg || b != wb {
			t.Errorf("%s: cell x=%d (%q) background = %v, want %v", name, x, c.String(), bg, want)
			return
		}
	}
}

// aliasBoxRow returns the alias column's box line containing name. The title
// bar and the footer may name the alias too; only the box lines carry the │
// border.
func aliasBoxRow(t *testing.T, m *model, name string) string {
	t.Helper()
	view := m.View()
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "│") && strings.Contains(l, name) {
			return l
		}
	}
	t.Fatalf("no alias box row containing %q in view:\n%s", name, view)
	return ""
}

// ---------------------------------------------------------------------------
// Compact (short) mode
// ---------------------------------------------------------------------------

func TestShortModeInitialView(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	if !m.short || !m.search || m.curCol != 0 {
		t.Fatalf("start state short=%v search=%v col=%d, want true/true/0",
			m.short, m.search, m.curCol)
	}
	v := m.View()
	lines := strings.Split(v, "\n")
	// exactly: the header + 4 value rows + the "filter" line + the tooltip line
	if len(lines) != 7 {
		t.Fatalf("short view has %d lines, want 7:\n%s", len(lines), v)
	}
	mustContain(t, ansi.Strip(v), "filter █", "llama", "other",
		"󱊷  exit · 󱁐 select · ↑↓←→ move · ↵ launch · / switch")
	// The needle is hidden in the active column: the cursor highlight is the
	// selection indicator. The first row is the cursor row (llama).
	if !strings.Contains(lines[1], "llama") {
		t.Errorf("first row should be the cursor alias (llama), got %q", lines[1])
	}
	// the filter input sits at the bottom of the list, not in the header
	if !strings.Contains(lines[len(lines)-2], "filter █") {
		t.Errorf("filter line should be the line above the tooltip, got %q", lines[len(lines)-2])
	}
	if strings.Contains(lines[0], "filter") {
		t.Errorf("header should not carry the filter, got %q", lines[0])
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > 100 {
			t.Errorf("line %d is %d columns wide (> 100): %q", i, w, line)
		}
	}
}

func TestShortModeSearchFilter(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	typeStr(t, m, "ll")
	if v := m.View(); !strings.Contains(v, "llama") || strings.Contains(v, "other") {
		t.Errorf("alias filter \"ll\" wrong:\n%s", v)
	}
	// a filter with no matches shows a placeholder row
	typeStr(t, m, "zz")
	if !strings.Contains(m.View(), "(no matches)") {
		t.Errorf("expected a (no matches) row:\n%s", m.View())
	}
	// backspace edits the filter
	for i := 0; i < 2; i++ {
		send(t, m, backspaceKey)
	}
	if v := m.View(); !strings.Contains(v, "llama") || strings.Contains(v, "other") {
		t.Errorf("backspace should restore the filter:\n%s", v)
	}
	// esc clears the filter; a second esc (empty buffer) quits
	send(t, m, escKey)
	if len(m.searchBuf) != 0 {
		t.Errorf("esc should clear the filter, got %q", string(m.searchBuf))
	}
	if v := m.View(); !strings.Contains(v, "llama") || !strings.Contains(v, "other") {
		t.Errorf("cleared filter should list all aliases:\n%s", v)
	}
	send(t, m, escKey)
	if !m.doQuit {
		t.Error("esc with an empty filter should quit")
	}
}

// TestShortModeNoMatchCannotEnterColumn guards the alias search with a
// filter that matches nothing: there is no alias to step into, so
// space/right/tab stay in the search (nothing was selectable to begin with).
func TestShortModeNoMatchCannotEnterColumn(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	typeStr(t, m, "zz") // no alias matches
	if v := m.View(); !strings.Contains(v, "(no matches)") {
		t.Fatalf("expected the (no matches) row:\n%s", v)
	}
	for _, key := range []tea.Msg{spaceKey, tea.KeyMsg{Type: tea.KeyRight}, tabKey} {
		send(t, m, key)
		if !m.search || m.curCol != 0 {
			t.Fatalf("%v with no matches should stay in the alias search: search=%v col=%d",
				key, m.search, m.curCol)
		}
	}
	if m.status != "no matches to open" {
		t.Errorf("status = %q, want a hint", m.status)
	}
	// backspacing back to a matching filter reopens the column as usual
	for i := 0; i < 2; i++ {
		send(t, m, backspaceKey)
	}
	send(t, m, spaceKey)
	if m.search || m.curCol != 1 {
		t.Fatalf("space with a matching filter should enter the column: search=%v col=%d",
			m.search, m.curCol)
	}
}

func TestShortModeEnterLaunchesPreselection(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	// Enter runs the alias under the cursor. Moving the cursor to "other"
	// and pressing Enter runs "other", not the preselected alias.
	send(t, m, downKey) // cursor -> "other"
	if m.curAlias != 1 {
		t.Fatalf("after down: cursor=%d, want 1", m.curAlias)
	}
	send(t, m, enterKey) // launch the alias under the cursor
	if !m.doRun {
		t.Fatal("enter in the alias list should run")
	}
	if m.runCmd != `echo other` {
		t.Errorf("runCmd = %q, want the cursor alias' command (other)", m.runCmd)
	}
}

// TestShortModeSpaceSelectsAndEnters guards space in the alias search: it
// selects the alias under the cursor (the ▸ needle moves to it) and opens
// its first option column in one step; Enter then runs the selected alias.
func TestShortModeSpaceSelectsAndEnters(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	send(t, m, downKey)  // cursor -> "other"
	send(t, m, spaceKey) // select "other" and open its first option column
	if m.selAlias != 1 || m.curAlias != 1 {
		t.Fatalf("after space: selection=%d cursor=%d, want 1/1", m.selAlias, m.curAlias)
	}
	if m.search || m.curCol != 1 {
		t.Fatalf("space should select and enter the column: search=%v col=%d",
			m.search, m.curCol)
	}
	send(t, m, enterKey) // launch the selected alias
	if !m.doRun {
		t.Fatal("enter should run the selected alias")
	}
	if got := m.runVars["MODEL"]; got != "v1" {
		t.Errorf("MODEL = %q, want v1", got)
	}
	if m.runCmd != "echo other" {
		t.Errorf("runCmd = %q", m.runCmd)
	}
}

func TestShortModeSpaceOpensFirstColumn(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	send(t, m, spaceKey)
	if m.search || m.curCol != 1 {
		t.Fatalf("space should open the first column: search=%v col=%d",
			m.search, m.curCol)
	}
	v := m.View()
	lines := strings.Split(v, "\n")
	if len(lines) != 6 {
		t.Fatalf("short view has %d lines, want 6:\n%s", len(lines), v)
	}
	if !strings.Contains(lines[0], "model:") {
		t.Errorf("header should be the group name, got %q", lines[0])
	}
	mustContain(t, ansi.Strip(v), "qwen-3.8-27B", "qwen-3.6-35B-A3B", "󱊷  exit", "󱁐 select", "/ switch")
	// The needle is hidden in the active column: the cursor highlight is the
	// selection indicator. The default option (qwen-3.8-27B) is the cursor row.
	if line := lineContaining(t, v, "qwen-3.8-27B"); strings.Contains(line, "▸") {
		t.Errorf("the needle should be hidden in the active column: %q", line)
	}
}

func TestShortModeSelectAndRun(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	send(t, m, spaceKey) // -> model column
	send(t, m, downKey)  // cursor -> qwen-3.6-35B-A3B
	send(t, m, spaceKey) // select it
	if sel := m.aliases[0].Groups[0].Selected; sel != 1 {
		t.Fatalf("model Selected = %d, want 1", sel)
	}
	mustContain(t, m.View(), "model: qwen-3.6-35B-A3B → /models/small.gguf")
	send(t, m, tabKey) // -> context column
	if m.curCol != 2 {
		t.Fatalf("tab should move to the context column, got %d", m.curCol)
	}
	if !strings.Contains(m.View(), "context:") {
		t.Errorf("context header missing:\n%s", m.View())
	}
	send(t, m, downKey)  // cursor -> 256k
	send(t, m, spaceKey) // select 256k
	if sel := m.aliases[0].Groups[1].Selected; sel != 1 {
		t.Fatalf("context Selected = %d, want 1", sel)
	}
	send(t, m, enterKey)
	if !m.doRun {
		t.Fatal("enter should run")
	}
	if got := m.runVars["MODEL"]; got != "/models/small.gguf" {
		t.Errorf("MODEL = %q", got)
	}
	if got := m.runVars["CONTEXT"]; got != "262144" {
		t.Errorf("CONTEXT = %q", got)
	}
	if len(m.entry.Sels) != 2 || m.entry.Sels[0] != (history.Selection{Group: "model", Key: "qwen-3.6-35B-A3B"}) || m.entry.Sels[1] != (history.Selection{Group: "context", Key: "256k"}) {
		t.Errorf("entry.Sels = %+v", m.entry.Sels)
	}
}

func TestShortModeBackToSearch(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	send(t, m, spaceKey) // -> model column
	send(t, m, tabKey)   // -> context column
	send(t, m, escKey)   // back to the alias search
	if !m.search || m.curCol != 0 {
		t.Fatalf("esc should return to the alias search: search=%v col=%d",
			m.search, m.curCol)
	}
	// Nothing was typed before entering: the filter stays empty (no
	// prefill), the cursor and the needle stay on the entered alias.
	if got := string(m.searchBuf); got != "" {
		t.Errorf("filter should stay empty, got %q", got)
	}
	if m.curAlias != 0 || m.selAlias != 0 {
		t.Errorf("cursor/selection = %d/%d, want 0/0 (llama)", m.curAlias, m.selAlias)
	}
	v := m.View()
	lines := strings.Split(v, "\n")
	if !strings.Contains(lines[len(lines)-2], "filter █") {
		t.Errorf("the bottom filter line should show an empty filter, got %q", lines[len(lines)-2])
	}
	if strings.Contains(lines[0], "llama") {
		t.Errorf("header should not carry the alias rows, got %q", lines[0])
	}
	mustContain(t, v, "llama", "other") // all aliases listed; needle hidden in active column
	// left/shift+tab from the first column goes back as well
	send(t, m, spaceKey)  // -> model column again
	send(t, m, shiftTabK) // left from the first column
	if !m.search || m.curCol != 0 {
		t.Fatalf("shift+tab from the first column should return to the alias search: search=%v col=%d",
			m.search, m.curCol)
	}
	if got := string(m.searchBuf); got != "" {
		t.Errorf("filter should still be empty, got %q", got)
	}
}

// TestShortModeBackKeepsTypedFilter guards the filter round trip: what the
// user typed in the alias search is what comes back after entering a column
// and going back (left or esc).
func TestShortModeBackKeepsTypedFilter(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	typeStr(t, m, "ll") // filter "ll" -> only llama
	if m.curAlias != 0 {
		t.Fatalf("cursor = %d, want 0 (llama)", m.curAlias)
	}
	send(t, m, tea.KeyMsg{Type: tea.KeyRight}) // -> model column
	if m.search || m.curCol != 1 {
		t.Fatalf("right should open the column: search=%v col=%d", m.search, m.curCol)
	}
	if got := string(m.searchBuf); got != "ll" {
		t.Fatalf("filter should be kept while the column is open, got %q", got)
	}
	send(t, m, spaceKey) // select an option
	if got := string(m.searchBuf); got != "ll" {
		t.Fatalf("filter should survive the selection, got %q", got)
	}
	send(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // back to the alias search
	if !m.search || m.curCol != 0 {
		t.Fatalf("left should return to the alias search: search=%v col=%d",
			m.search, m.curCol)
	}
	if got := string(m.searchBuf); got != "ll" {
		t.Errorf("filter should come back as typed, got %q", got)
	}
	if m.curAlias != 0 || m.selAlias != 0 {
		t.Errorf("cursor/selection = %d/%d, want 0/0 (llama)", m.curAlias, m.selAlias)
	}
	lines := strings.Split(m.View(), "\n")
	if !strings.Contains(lines[len(lines)-2], "filter ll") {
		t.Errorf("the bottom filter line should show the kept filter:\n%s", m.View())
	}
	if strings.Contains(m.View(), "other") {
		t.Errorf("the kept filter should still narrow the list:\n%s", m.View())
	}
	// esc back-navigation keeps the filter too
	send(t, m, tea.KeyMsg{Type: tea.KeyRight}) // -> model column again
	send(t, m, escKey)                         // back
	if got := string(m.searchBuf); got != "ll" {
		t.Errorf("esc back should keep the filter, got %q", got)
	}
	// esc once clears the filter (user action), twice quits
	send(t, m, escKey)
	if len(m.searchBuf) != 0 {
		t.Errorf("esc should clear the kept filter, got %q", string(m.searchBuf))
	}
}

// TestShortModeBackKeepsEmptyFilterAndNeedle guards the reported scenario:
// with the history needle on another alias, navigating to an alias, entering
// its column, selecting an option and going back leaves the search empty,
// the same alias highlighted and the needle on it (not on the history one).
func TestShortModeBackKeepsEmptyFilterAndNeedle(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  llama:
    options:
      model:
        - qwen-3.8-27B: big
        - qwen-3.6-35B-A3B: small
      command: echo llama
  dsh:
    options:
      model:
        - m1: v1
      command: echo dsh
  ls:
    command: echo ls
  tf-plan:
    command: echo tfplan
`)
	hp := filepath.Join(t.TempDir(), "history")
	e := history.Entry{Time: time.Now().UTC(), Alias: "dsh"}
	if err := history.Append(hp, e, 10); err != nil {
		t.Fatal(err)
	}
	entries, err := history.Load(hp)
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Cfg: cfg, Short: true, HistPath: hp})
	if err != nil {
		t.Fatal(err)
	}
	m.history = entries
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.View()
	if m.selAlias != 1 || m.curAlias != 1 {
		t.Fatalf("initial cursor/selection = %d/%d, want 1/1 (dsh, from history)",
			m.curAlias, m.selAlias)
	}
	send(t, m, upKey) // cursor: dsh -> llama (needle stays on dsh)
	if m.curAlias != 0 {
		t.Fatalf("cursor = %d, want 0 (llama)", m.curAlias)
	}
	send(t, m, tea.KeyMsg{Type: tea.KeyRight}) // into llama's model column
	if m.curCol != 1 {
		t.Fatalf("col = %d, want 1", m.curCol)
	}
	g := m.aliases[0].Groups[0]
	if g.Cursor != 0 {
		send(t, m, upKey)
	}
	send(t, m, spaceKey) // select qwen-3.8-27B
	if m.selAlias != 0 {
		t.Fatalf("selection = %d, want 0 (llama)", m.selAlias)
	}
	send(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // back to the alias search
	if !m.search || m.curCol != 0 {
		t.Fatalf("back should return to the alias search: search=%v col=%d",
			m.search, m.curCol)
	}
	if got := string(m.searchBuf); got != "" {
		t.Errorf("search should be empty, got %q", got)
	}
	if m.curAlias != 0 || m.selAlias != 0 {
		t.Fatalf("cursor/selection = %d/%d, want 0/0 (llama, not dsh)",
			m.curAlias, m.selAlias)
	}
	v := ansi.Strip(m.View())
	// The needle is hidden in the active column: the cursor highlight is the
	// selection indicator. All aliases are listed; the cursor is on llama.
	for _, a := range []string{"llama", "dsh", "ls", "tf-plan"} {
		if !strings.Contains(v, a) {
			t.Errorf("empty search should list every alias, %q missing:\n%s", a, v)
		}
	}
}

func TestShortModeSlashSwitchModes(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	send(t, m, spaceKey) // -> model column
	send(t, m, downKey)  // move the cursor so there is state to preserve
	send(t, m, slashKey) // -> full mode
	if m.short || !m.modeSwitch || !m.doQuit {
		t.Fatalf("/ from short: short=%v switch=%v quit=%v",
			m.short, m.modeSwitch, m.doQuit)
	}
	if m.curAlias != 0 || m.curCol != 1 {
		t.Fatalf("state lost on switch: alias=%d col=%d", m.curAlias, m.curCol)
	}
	if m.search {
		t.Error("switching to full mode must not enter the filter mode")
	}
	// ...and back (the Run loop would restart the program in between).
	send(t, m, slashKey)
	if !m.short || !m.modeSwitch || !m.doQuit {
		t.Fatalf("/ from full: short=%v switch=%v quit=%v",
			m.short, m.modeSwitch, m.doQuit)
	}
	if m.curCol != 1 {
		t.Errorf("column lost on the way back: %d", m.curCol)
	}

	// From the alias search the typed filter carries over into full mode,
	// inert: the alias column starts plain, like a values column, and only
	// the filter shortcut enters the filter mode there.
	m2 := newShortModel(t, cfg, "")
	m2.View()
	typeStr(t, m2, "ll")
	send(t, m2, slashKey)
	if m2.short || !m2.modeSwitch {
		t.Fatalf("/ from the alias search: short=%v switch=%v",
			m2.short, m2.modeSwitch)
	}
	if m2.search {
		t.Error("full mode must start plain, not in the filter mode")
	}
	if got := string(m2.searchBuf); got != "ll" {
		t.Errorf("typed filter lost on the switch: %q", got)
	}
	// ...and it is active again on the way back to the short mode.
	send(t, m2, slashKey)
	if !m2.short || !m2.search || string(m2.searchBuf) != "ll" {
		t.Errorf("filter should be active again in the short mode: search=%v buf=%q",
			m2.search, string(m2.searchBuf))
	}
}

// TestShortModeSlashKeepsTypedFilter guards the filter across mode switches:
// typed in the alias search, kept while an option column is open, inert in
// the full mode, and back on the way to the short mode's alias search. An
// active search of an option column in the full mode is not an alias filter
// and does not come back.
func TestShortModeSlashKeepsTypedFilter(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	typeStr(t, m, "ll")                        // alias filter, still active in the search
	send(t, m, tea.KeyMsg{Type: tea.KeyRight}) // -> model column (filter kept)
	if m.search || string(m.searchBuf) != "ll" {
		t.Fatalf("in the column: search=%v buf=%q, want false/\"ll\"",
			m.search, string(m.searchBuf))
	}
	send(t, m, slashKey) // -> full mode
	if m.short || m.search {
		t.Fatalf("/ from a column: short=%v search=%v, want full/inert",
			m.short, m.search)
	}
	if got := string(m.searchBuf); got != "ll" {
		t.Fatalf("filter lost in the full mode: %q", got)
	}
	send(t, m, slashKey) // -> short mode again (option column)
	if !m.short || m.search || m.curCol != 1 {
		t.Fatalf("/ back: short=%v search=%v col=%d, want true/false/1",
			m.short, m.search, m.curCol)
	}
	send(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // back to the alias search
	if !m.search || string(m.searchBuf) != "ll" {
		t.Fatalf("round trip: search=%v buf=%q, want true/\"ll\"",
			m.search, string(m.searchBuf))
	}

	// An option-column search started in the full mode is dropped on the way
	// back (it is not an alias filter).
	m2 := newShortModel(t, cfg, "")
	m2.View()
	send(t, m2, tea.KeyMsg{Type: tea.KeyRight}) // -> model column, no filter
	send(t, m2, slashKey)                       // -> full mode
	send(t, m2, fKey)                           // start a filter in the column
	typeStr(t, m2, "q")
	if !m2.search || string(m2.searchBuf) != "q" {
		t.Fatalf("full mode column filter: search=%v buf=%q",
			m2.search, string(m2.searchBuf))
	}
	send(t, m2, slashKey) // -> short mode (option column)
	if got := string(m2.searchBuf); got != "" {
		t.Fatalf("option-column filter must not carry over, got %q", got)
	}
	send(t, m2, tea.KeyMsg{Type: tea.KeyLeft}) // back to the alias search
	if !m2.search || string(m2.searchBuf) != "" {
		t.Fatalf("alias search after dropped filter: search=%v buf=%q",
			m2.search, string(m2.searchBuf))
	}
}

func TestShortModeSlashOpensFullMode(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	send(t, m, spaceKey) // -> model column
	send(t, m, downKey)  // move the cursor so there is state to preserve
	// "/" switches to the full mode
	send(t, m, slashKey)
	if m.short || !m.modeSwitch || !m.doQuit {
		t.Fatalf("/ from short: short=%v switch=%v quit=%v",
			m.short, m.modeSwitch, m.doQuit)
	}
	if m.curAlias != 0 || m.curCol != 1 {
		t.Fatalf("state lost on switch: alias=%d col=%d", m.curAlias, m.curCol)
	}
	// alt held with ctrl+c still quits rather than doing anything
	m2 := newShortModel(t, cfg, "")
	m2.View()
	send(t, m2, tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true})
	if !m2.doQuit || m2.modeSwitch {
		t.Fatalf("alt+ctrl+c should quit, not switch: switch=%v quit=%v",
			m2.modeSwitch, m2.doQuit)
	}
}

func TestShortModeStartAlias(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "llama")
	if m.search || m.curCol != 1 || m.curAlias != 0 {
		t.Fatalf("StartAlias should open the first column: search=%v col=%d alias=%d",
			m.search, m.curCol, m.curAlias)
	}
	mustContain(t, m.View(), "model:", "qwen-3.8-27B")
}

func TestShortModeNoGroupsAliasRuns(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  bare:
    command: echo hi
`)
	m, err := New(Options{Cfg: cfg, Short: true})
	if err != nil {
		t.Fatal(err)
	}
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.View()
	if !strings.Contains(m.View(), "bare") {
		t.Errorf("alias list should show the alias:\n%s", m.View())
	}
	send(t, m, spaceKey) // alias without groups: space launches
	if !m.doRun {
		t.Fatal("space on a no-group alias should run")
	}
}

func TestShortModeQIsAFilterChar(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	send(t, m, qKey) // in the alias search "q" is typed, not quit
	if m.doQuit {
		t.Fatal("q in the alias search must not quit (it is a filter character)")
	}
	if got := string(m.searchBuf); got != "q" {
		t.Errorf("q should have been typed into the filter, got %q", got)
	}
	// in an option column q quits
	send(t, m, backspaceKey)
	send(t, m, spaceKey) // -> model column
	send(t, m, qKey)
	if !m.doQuit {
		t.Fatal("q in an option column should quit")
	}
}

func TestShortModePromptLine(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	send(t, m, spaceKey) // -> model column
	m.dirty = true       // as after editing the config in full mode
	send(t, m, qKey)     // quit with unsaved changes -> confirm prompt
	if m.prompt == nil || m.prompt.action != actSaveOnQuit {
		t.Fatal("expected the save-on-quit prompt")
	}
	v := m.View()
	if !strings.Contains(v, "save to") || !strings.Contains(v, "y/n") {
		t.Errorf("prompt should be shown in the tooltip line:\n%s", v)
	}
	if n := len(strings.Split(v, "\n")); n != 6 {
		t.Errorf("view has %d lines, want 6", n)
	}
	send(t, m, escKey) // decline
	if !m.doQuit || !m.dirty {
		t.Errorf("esc should quit without saving (quit=%v dirty=%v)", m.doQuit, m.dirty)
	}
}

func TestShortModeScrollsPastFourRows(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  many:
    options:
        g:
          - one: 1
          - two: 2
          - three: 3
          - four: 4
          - five: 5
          - six: 6
    command: echo many
`)
	m, err := New(Options{Cfg: cfg, StartAlias: "many", Short: true})
	if err != nil {
		t.Fatal(err)
	}
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	v := m.View()
	// only the first four values are shown
	mustContain(t, v, "one", "two", "three", "four")
	for _, gone := range []string{"five", "six"} {
		if strings.Contains(ansi.Strip(v), gone) {
			t.Errorf("row %q should not be visible in the 4-row window:\n%s", gone, v)
		}
	}
	// scroll down past the bottom: the window follows the cursor
	for i := 0; i < 5; i++ {
		send(t, m, downKey)
	}
	v = m.View()
	mustContain(t, v, "three", "four", "five", "six")
	if s := ansi.Strip(v); strings.Contains(s, "two") || strings.Contains(s, "one") {
		t.Errorf("the window should have scrolled past the top rows:\n%s", v)
	}
	// and up past the top
	for i := 0; i < 10; i++ {
		send(t, m, upKey)
	}
	v = m.View()
	mustContain(t, v, "one", "two", "three", "four")
	if strings.Contains(ansi.Strip(v), "five") {
		t.Errorf("the window should have scrolled back to the top:\n%s", v)
	}
	// select from the scrolled window and run
	send(t, m, downKey)  // cursor -> two
	send(t, m, spaceKey) // select two
	send(t, m, enterKey)
	if !m.doRun {
		t.Fatal("enter should run")
	}
	if got := m.runVars["G"]; got != "2" {
		t.Errorf("G = %q, want 2", got)
	}
}

func TestShortModeRightArrowOpensFirstColumn(t *testing.T) {
	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.search || m.curCol != 1 {
		t.Fatalf("right in the alias list should open the first column: search=%v col=%d",
			m.search, m.curCol)
	}
	if !strings.Contains(m.View(), "model:") {
		t.Errorf("first column header missing:\n%s", m.View())
	}
	// tab does the same
	m2 := newShortModel(t, cfg, "")
	m2.View()
	send(t, m2, tabKey)
	if m2.search || m2.curCol != 1 {
		t.Fatalf("tab in the alias list should open the first column: search=%v col=%d",
			m2.search, m2.curCol)
	}
	// and left/shift+tab from the first column goes back to the list
	send(t, m2, tea.KeyMsg{Type: tea.KeyLeft})
	if !m2.search || m2.curCol != 0 {
		t.Fatalf("left from the first column should return to the alias list: search=%v col=%d",
			m2.search, m2.curCol)
	}
}

func TestShortModeColumnSigns(t *testing.T) {
	cfg := loadCfg(t) // llama: model+context, other: model
	header := func(t *testing.T, m *model) string {
		t.Helper()
		return strings.Split(m.View(), "\n")[0]
	}
	m := newShortModel(t, cfg, "")
	m.View()
	// alias list of an alias with groups: only » (columns on the right)
	h := header(t, m)
	if !strings.Contains(h, "»") || strings.Contains(h, "«") {
		t.Errorf("alias list header should carry » only, got %q", h)
	}
	// first of two groups: « (alias list on the left) and » (next group)
	send(t, m, tea.KeyMsg{Type: tea.KeyRight})
	h = header(t, m)
	if !strings.Contains(h, "«") || !strings.Contains(h, "»") {
		t.Errorf("first-group header should carry « and », got %q", h)
	}
	// last group: only «
	send(t, m, tabKey)
	h = header(t, m)
	if !strings.Contains(h, "«") || strings.Contains(h, "»") {
		t.Errorf("last-group header should carry « only, got %q", h)
	}
	// an alias with a single group: « only
	m = newShortModel(t, cfg, "other")
	m.View()
	h = header(t, m)
	if !strings.Contains(h, "«") || strings.Contains(h, "»") {
		t.Errorf("single-group header should carry « only, got %q", h)
	}
	// an alias without groups: no signs at all
	cfgBare := cfgFromYAML(t, `aliases:
  bare:
    command: echo hi
`)
	m = newShortModel(t, cfgBare, "")
	m.View()
	h = header(t, m)
	if strings.Contains(h, "«") || strings.Contains(h, "»") {
		t.Errorf("no-group alias list should carry no signs, got %q", h)
	}
	// the signs sit at the right edge and the line stays within the width
	m = newShortModel(t, cfg, "")
	m.View()
	h = header(t, m)
	if i := strings.LastIndex(h, "»"); i < 60 {
		t.Errorf("» should be right-aligned, found at %d in %q", i, h)
	}
	if w := lipgloss.Width(h); w > 100 {
		t.Errorf("header is %d columns wide (> 100)", w)
	}
}

// TestCursorRowHighlightsFullLine guards the cursor row highlight: every cell
// of the row under the cursor carries the cursor background, whether or not
// the row also holds the selected option (the green dot).
func TestCursorRowHighlightsFullLine(t *testing.T) {
	// lipgloss only emits SGR sequences under a color profile; force
	// TrueColor so the escapes can be inspected. The setting is sticky for
	// the process, so keep this test at the end of the file.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama") // model column (layout index 1): cursor on row 0, also selected
	assertFullRowHighlight(t, m, "selected row under the cursor", "qwen-3.8-27B", 1)

	send(t, m, downKey) // cursor -> row 1 (not selected)
	assertFullRowHighlight(t, m, "unselected row under the cursor", "qwen-3.6-35B-A3B", 1)
}

// TestColorTagRendersKeyColor guards the !color tag: the option's key is
// rendered in the tag's color (the long text, when shown, keeps the muted
// value color), and the compound tag !default+colorXXXXXX preselects the
// option and colors its key.
func TestColorTagRendersKeyColor(t *testing.T) {
	// lipgloss only emits SGR sequences under a color profile; force
	// TrueColor so the escapes can be inspected. The setting is sticky for
	// the process, so keep this test at the end of the file.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	cfg := cfgFromYAML(t, `
aliases:
  tf-plan:
    options:
      env:
        - dev: !default+color7B42BC dev
        - stag: !colorFFD814 staging
        - prod: !colorD30000 prod
      model:
        - small: !color00ff00 /models/small.gguf
    command: tf plan -var-file env_vars/$ENV
`)
	m := newModel(t, cfg, "tf-plan")
	view := m.View()
	// The expected spans are rendered with the same styles the view uses,
	// so the assertions hold whatever SGR encoding the profile picks.
	devKey := theme.ColoredValueStyle(m.th, "#7b42bc", true).Render("dev") // cursor row
	stagKey := theme.ColoredValueStyle(m.th, "#ffd814", false).Render("stag")
	prodKey := theme.ColoredValueStyle(m.th, "#d30000", false).Render("prod")
	smallKey := theme.ColoredValueStyle(m.th, "#00ff00", false).Render("small")
	stagingVal := m.sty.Value.Render(" staging")
	// Compound tag: the key is colored and the option is preselected
	// (the value equals the key, so nothing else shows).
	if !strings.Contains(view, devKey) {
		t.Errorf("dev key not rendered in #7b42bc:\n%s", view)
	}
	// The needle is hidden in the active column: the cursor highlight is the
	// selection indicator. dev is the cursor row (the !default option).
	if !strings.Contains(ansi.Strip(view), "dev") {
		t.Errorf("dev (the !default+color option) not rendered:\n%s", view)
	}
	// The key is in the tag's color; the value stays in the muted value
	// color, not the tag's.
	if !strings.Contains(view, stagKey) {
		t.Errorf("stag key not rendered in #ffd814:\n%s", view)
	}
	if !strings.Contains(view, stagingVal) {
		t.Errorf("staging value not rendered in the muted value color:\n%s", view)
	}
	if strings.Contains(view, theme.ColoredValueStyle(m.th, "#ffd814", false).Render("staging")) {
		t.Errorf("staging value rendered in #ffd814:\n%s", view)
	}
	if !strings.Contains(view, smallKey) {
		t.Errorf("small key not rendered in #00ff00:\n%s", view)
	}
	if strings.Contains(view, theme.ColoredValueStyle(m.th, "#00ff00", false).Render(" /models/small.gguf")) {
		t.Errorf("small value rendered in #00ff00:\n%s", view)
	}
	// LongText equals the key: the key itself is wrapped in red.
	if !strings.Contains(view, prodKey) {
		t.Errorf("prod key not rendered in #d30000:\n%s", view)
	}
}

// plainLine returns the ANSI-stripped view line containing name.
func plainLine(t *testing.T, view, name string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if plain := ansi.Strip(line); strings.Contains(plain, name) {
			return plain
		}
	}
	t.Fatalf("no line containing %q in view\n%s", name, view)
	return ""
}

// TestAliasIconRendersLeftOfName guards the literal icon feature: the alias
// icon glyph sits at the left of the alias name, ahead of the ▸ marker, in
// the alias column's icon slot (no file lookup, no cache involved).
func TestAliasIconRendersLeftOfName(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    icon: ▓▒░
    command: echo tf
  also:
    icon: ▓▒░
    command: echo also
  plain:
    command: echo plain
`)
	// Full mode, starting on a plain alias: every icon alias shows its icon
	// left of its name. The needle is hidden in the active column; the cursor
	// highlight is the selection indicator.
	m := newModel(t, cfg, "plain")
	view := m.View()
	mustContain(t, view,
		"▓▒░  tf-plan", // icon, then the two marker spaces, then the name
		"▓▒░  also")
	// The cursor row (plain): blank slot (plain has no icon), then the name.
	// No needle in the active column.
	if line := plainLine(t, view, "plain"); strings.Contains(line, "▸") {
		t.Errorf("the needle should be hidden in the active column: %q", line)
	}

	// Move the cursor onto an icon alias: the highlight follows the cursor;
	// the icon stays left of the name, ahead of the marker slot.
	send(t, m, upKey) // cursor: plain -> also
	view = m.View()
	line := plainLine(t, view, "▓▒░  also")
	if iIcon, iMark := strings.Index(line, "▓▒░"), strings.Index(line, "  also"); iIcon < 0 || iMark < 0 || iIcon > iMark {
		t.Errorf("cursor row for an icon alias = %q, want icon then the marker spaces then the name", line)
	}
}

func TestAliasIconShortMode(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    icon: ▓▒░
    command: echo tf
  also:
    icon: ▓▒░
    command: echo also
  plain:
    command: echo plain
`)
	m := newShortModel(t, cfg, "") // starts in the alias search on tf-plan
	view := m.View()
	// The needle is hidden in the active column: the cursor highlight is the
	// selection indicator. tf-plan is the cursor row.
	mustContain(t, view, "▓▒░  tf-plan", "▓▒░  also", "     plain")
}

// TestAliasIconLiteralEmoji guards the emoji form of the icon key: the
// value is literal text (not a file), so it renders as-is and no cache
// directory is ever created.
func TestAliasIconLiteralEmoji(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yml")
	yaml := `aliases:
  tf-plan:
    icon: 🚀
    options:
      env:
        - dev: !default dev
    command: echo hi
  plain:
    command: echo plain
`
	if err := os.WriteFile(p, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	m, err := New(Options{Cfg: cfg, StartAlias: "plain"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	view := m.View()
	// The needle is hidden in the active column: the cursor highlight is the
	// selection indicator. plain is the cursor row.
	mustContain(t, view, "🚀  tf-plan", "plain")
	// No cache dir for a literal icon.
	if _, err := os.Stat(filepath.Join(dir, "icons_cache")); !os.IsNotExist(err) {
		t.Errorf("a literal (emoji) icon must not create a cache dir: %v", err)
	}
}

// colInners splits one view line into the inner content of each column box:
// the line is "│ <inner1> ││ <inner2> │…" (the middle borders are adjacent),
// so the boxes are the odd segments after splitting on │, with one border
// padding cell trimmed off each side.
func colInners(t *testing.T, line string) []string {
	t.Helper()
	line = ansi.Strip(line)
	parts := strings.Split(line, "│")
	var out []string
	for i := 1; i+1 < len(parts); i += 2 {
		if len(parts[i]) >= 2 {
			out = append(out, parts[i][1:len(parts[i])-1])
		}
	}
	return out
}

// colInnersRaw is colInners keeping the ANSI escapes (for color checks).
func colInnersRaw(t *testing.T, line string) []string {
	t.Helper()
	parts := strings.Split(line, "│")
	var out []string
	for i := 1; i+1 < len(parts); i += 2 {
		if len(parts[i]) >= 2 {
			out = append(out, parts[i][1:len(parts[i])-1])
		}
	}
	return out
}

// TestPairIconColumnSlot guards the per-column icon slots: every column
// (alias + each option column) carries its own leading icon slot, sized to
// the widest icon in that column (0 when the column has no icons), so all
// names/keys in a column start at the same cell.
func TestPairIconColumnSlot(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    icon: 🦙
    options:
      env:
        - dev:
            icon: 🏠
        - stag:
        - prod:
            icon: ⚠
    command: tf plan
  plain:
    command: echo plain
`)
	// The alias column has icons (slot 2: 🦙), the env column has icons too
	// (slot 2: 🏠); "stag" has no icon but its key still starts at the same
	// cell as dev/prod.
	m := newModel(t, cfg, "tf-plan")
	if m.aliasIconW != 2 {
		t.Errorf("alias icon slot = %d, want 2", m.aliasIconW)
	}
	if g := m.aliases[0].Groups[0]; g.IconW != 2 {
		t.Errorf("env icon slot = %d, want 2", g.IconW)
	}
	view := m.View()
	dev := colInners(t, plainLine(t, view, "dev"))
	stag := colInners(t, plainLine(t, view, "stag"))
	prod := colInners(t, plainLine(t, view, "prod"))
	if len(dev) != 2 || len(stag) != 2 || len(prod) != 2 {
		t.Fatalf("expected 2 column boxes per row: %q %q %q", dev, stag, prod)
	}
	d, s, p := dev[1], stag[1], prod[1]
	cellOf := func(row, key string) int {
		i := strings.Index(row, key)
		return lipgloss.Width(row[:i])
	}
	// The keys start at the same cell in all three env rows: slot (2) +
	// marker (2).
	if cd, cs, cp := cellOf(d, "dev"), cellOf(s, "stag"), cellOf(p, "prod"); cd != 4 || cs != 4 || cp != 4 {
		t.Errorf("env keys do not start at cell 4: dev@%d stag@%d prod@%d:\n%s\n%s\n%s", cd, cs, cp, d, s, p)
	}
	// The needle is hidden in the active column: the cursor highlight is the
	// selection indicator. The icon rows: icon + (padding) + marker spaces +
	// key; the no-icon row: the blank slot + marker spaces + key.
	if !strings.HasPrefix(d, "🏠  dev") {
		t.Errorf("dev row = %q, want icon 🏠 then marker spaces then the key", d)
	}
	if !strings.HasPrefix(p, "⚠   prod") {
		t.Errorf("prod row = %q, want icon ⚠ + slot padding + marker spaces", p)
	}
	if !strings.HasPrefix(s, "    stag") {
		t.Errorf("no-icon row must carry the blank slot + marker spaces: %q", s)
	}
	// The alias column is aligned the same way: names start at slot (2) +
	// marker (2).
	// Anchor on the icon glyph: the title bar also says "tf-plan".
	tf := colInners(t, plainLine(t, view, "🦙"))
	plainRow := colInners(t, plainLine(t, view, "plain"))
	if a, b := cellOf(tf[0], "tf-plan"), cellOf(plainRow[0], "plain"); a != 4 || b != 4 {
		t.Errorf("alias names do not start at cell 4: tf-plan@%d plain@%d", a, b)
	}
}

// TestPairIconSlotZero guards the 0-slot case: a column without icons has no
// leading slot at all — the keys start right after the "▸ " marker.
func TestPairIconSlotZero(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev:
        - prod: !colorD30000 prod
    command: tf plan
`)
	m := newModel(t, cfg, "tf-plan")
	if m.aliasIconW != 0 {
		t.Errorf("alias icon slot = %d, want 0", m.aliasIconW)
	}
	if g := m.aliases[0].Groups[0]; g.IconW != 0 {
		t.Errorf("env icon slot = %d, want 0", g.IconW)
	}
	view := m.View()
	cellOf := func(row, key string) int {
		i := strings.Index(row, key)
		return lipgloss.Width(row[:i])
	}
	dev := colInners(t, plainLine(t, view, "dev"))
	prod := colInners(t, plainLine(t, view, "prod"))
	if i := cellOf(dev[1], "dev"); i != 2 {
		t.Errorf("dev key at cell %d, want 2 (no slot): %q", i, dev[1])
	}
	if i := cellOf(prod[1], "prod"); i != 2 {
		t.Errorf("prod key at cell %d, want 2 (no slot): %q", i, prod[1])
	}
}

// TestNeedleMutedFromHistory guards the needle color: the ▸ on the
// preselected option is in the theme "selected" color normally, and in the
// theme "muted" color when the preselection came from the history. The old
// hourglass badge is gone.
func TestNeedleMutedFromHistory(t *testing.T) {
	// Force TrueColor so the escapes can be inspected; the setting is sticky
	// for the process, so keep this test at the end of the file.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	cfg := loadCfg(t)
	hp := filepath.Join(t.TempDir(), "history")
	e := history.Entry{
		Time:  time.Now().UTC(),
		Alias: "llama",
		Sels:  []history.Selection{{Group: "model", Key: "qwen-3.6-35B-A3B"}, {Group: "context", Key: "256k"}},
	}
	if err := history.Append(hp, e, 10); err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Cfg: cfg, StartAlias: "llama", HistPath: hp})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := history.Load(hp)
	if err != nil {
		t.Fatal(err)
	}
	m.history = entries
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	view := m.View()

	// The RGB triples of the needle colors (theme "selected" #9ece6a and
	// theme "muted" #565f89, as lipgloss emits them).
	const selectedRGB = "38;2;158;206;105"
	const mutedRGB = "38;2;86;95;137"

	// The context group has no !default: the history preselection (256k)
	// carries the muted needle.
	if line := lineContaining(t, view, "256k"); !strings.Contains(line, "▸") || !strings.Contains(line, mutedRGB) {
		t.Errorf("history preselection must carry the muted needle:\n%s", view)
	}
	// The model group carries a !default tag: the default beats the history
	// preselection, the needle is green (cursor row: with the row background).
	if line := lineContaining(t, view, "qwen-3.8-27B"); !strings.Contains(line, selectedRGB) {
		t.Errorf("default preselection must carry the green needle:\n%s", view)
	}
	// The hourglass is gone, everywhere.
	if strings.Contains(view, "⧖") {
		t.Errorf("the hourglass must never render:\n%s", view)
	}
}

// TestIconColorTag guards the !color tag on icons (aliases and options): the
// glyph is rendered in the tag's color, cursor-aware like the option value
// colors (bold + row background on the cursor row), display-only (the
// key/long_text stay plain).
func TestIconColorTag(t *testing.T) {
	// Force TrueColor so the escapes can be inspected; the setting is sticky
	// for the process, so keep this test at the end of the file.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    icon: !colorFF5555 ⚙
    options:
      env:
        - dev:
            icon: !color00ff00 🏠
        - prod:
            icon: ⚠
    command: tf plan
`)
	const redCursor = "\x1b[1;38;2;255;85;85;48;2;36;40;59m" // #ff5555, cursor row
	const greenCursor = "\x1b[1;38;2;0;255;0;48;2;36;40;59m" // #00ff00, cursor row
	const green = "\x1b[38;2;0;255;0m"                       // #00ff00, plain row
	const greenRGB = "38;2;0;255;0"
	m := newModel(t, cfg, "tf-plan")
	view := m.View()
	// The alias icon (cursor row, tf-plan is the current alias): red. The
	// row is the alias row carrying the ⚙ glyph (the title bar also mentions
	// tf-plan, so anchor on the glyph).
	if line := lineContaining(t, view, "⚙"); !strings.Contains(line, redCursor+"⚙") {
		t.Errorf("alias icon not rendered in #ff5555:\n%s", view)
	}
	// The option icon under the cursor (dev): green, cursor-aware.
	if line := lineContaining(t, view, "🏠"); !strings.Contains(line, greenCursor+"🏠") {
		t.Errorf("option icon not rendered in #00ff00 (cursor row):\n%s", view)
	}
	// Move the cursor to prod: dev's icon leaves the cursor row, the plain
	// green style comes back.
	send(t, m, downKey)
	view = m.View()
	if line := lineContaining(t, view, "🏠"); !strings.Contains(line, green+"🏠") {
		t.Errorf("option icon not rendered in #00ff00 (plain row):\n%s", view)
	}
	// The untagged option icon stays plain: no green escapes on its row.
	if line := lineContaining(t, view, "⚠"); strings.Contains(line, greenRGB) {
		t.Errorf("untagged icon must not be colored:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Alias column: the needle marks the selected alias
// ---------------------------------------------------------------------------

// appendHistoryFor writes one history entry for alias to a fresh history
// file and returns the loaded entries.
func appendHistoryFor(t *testing.T, alias string) (string, []history.Entry) {
	t.Helper()
	hp := filepath.Join(t.TempDir(), "history")
	e := history.Entry{
		Time:  time.Now().UTC(),
		Alias: alias,
		Sels:  []history.Selection{{Group: "model", Key: "m1"}},
	}
	if err := history.Append(hp, e, 10); err != nil {
		t.Fatal(err)
	}
	entries, err := history.Load(hp)
	if err != nil {
		t.Fatal(err)
	}
	return hp, entries
}

// TestAliasNeedleHiddenInActiveColumn guards the alias column's needle: it
// is hidden in the active column (the cursor highlight is the selection
// indicator) and reappears in inactive columns on the previously selected
// alias.
func TestAliasNeedleHiddenInActiveColumn(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	cfg := loadCfg(t) // aliases: llama, other — no history
	m := newModel(t, cfg, "")
	view := m.View() // resolves the selection: history > first alias
	if m.selAlias != 0 || m.selAliasSrc != config.SourceFirst {
		t.Fatalf("selected alias = %d (%s), want 0 (%s)",
			m.selAlias, m.selAliasSrc, config.SourceFirst)
	}
	// The needle is hidden in the active column (the alias column): no ▸ in
	// the alias column's inner content.
	if inner := colInners(t, aliasBoxRow(t, m, "llama"))[0]; strings.Contains(inner, "▸") {
		t.Errorf("the needle must be hidden in the active column:\n%s", view)
	}

	// Move the cursor: the highlight follows; the needle stays hidden.
	send(t, m, downKey)
	if m.curAlias != 1 || m.selAlias != 0 {
		t.Fatalf("cursor=%d selection=%d, want 1/0",
			m.curAlias, m.selAlias)
	}
	view = m.View()
	if inner := colInners(t, aliasBoxRow(t, m, "other"))[0]; strings.Contains(inner, "▸") {
		t.Errorf("the needle must stay hidden in the active column:\n%s", view)
	}
	// The cursor row (other) is fully highlighted.
	assertRowCellsHighlight(t, m, "alias cursor row", aliasBoxRow(t, m, "other"), 0)
}

// TestAliasNeedleMutedFromHistory guards the alias needle color: the ▸ is in
// the theme's muted (gray) color when the selection came from the history,
// and in the theme's selected (green) color after a user selection.
func TestAliasNeedleMutedFromHistory(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	cfg := loadCfg(t)
	hp, entries := appendHistoryFor(t, "other")
	m, err := New(Options{Cfg: cfg, HistPath: hp})
	if err != nil {
		t.Fatal(err)
	}
	m.history = entries
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	view := m.View()

	// The RGB triples of the needle colors (theme "selected" #9ece6a and
	// theme "muted" #565f89, as lipgloss emits them).
	const selectedRGB = "38;2;158;206;105"
	const mutedRGB = "38;2;86;95;137"

	// The last launch was "other": the selection (and the cursor, which
	// starts on it) is there. The needle is hidden in the active column.
	if m.selAlias != 1 || m.selAliasSrc != config.SourceHistory {
		t.Fatalf("selection = %d (%s), want 1 (%s)",
			m.selAlias, m.selAliasSrc, config.SourceHistory)
	}
	if m.curAlias != 1 {
		t.Fatalf("cursor = %d, want 1 (starts on the selected alias)", m.curAlias)
	}
	// The needle is hidden in the active column: no ▸ in the alias column.
	if inner := colInners(t, aliasBoxRow(t, m, "other"))[0]; strings.Contains(inner, "▸") {
		t.Errorf("the needle must be hidden in the active column:\n%s", view)
	}
	if inner := colInners(t, aliasBoxRow(t, m, "llama"))[0]; strings.Contains(inner, "▸") {
		t.Errorf("llama must not carry the needle:\n%s", view)
	}

	// Space selects the alias under the cursor: the selection source becomes
	// user. The needle stays hidden in the active column.
	send(t, m, spaceKey)
	view = m.View()
	if m.selAlias != 1 || m.selAliasSrc != srcUser {
		t.Fatalf("selection = %d (%s), want 1 (%s)", m.selAlias, m.selAliasSrc, srcUser)
	}
	if m.status != "alias: other" {
		t.Errorf("status = %q, want %q", m.status, "alias: other")
	}
	if inner := colInners(t, aliasBoxRow(t, m, "other"))[0]; strings.Contains(inner, "▸") {
		t.Errorf("the needle must stay hidden in the active column:\n%s", view)
	}
	// The cursor row is fully highlighted.
	assertRowCellsHighlight(t, m, "cursor row", aliasBoxRow(t, m, "other"), 0)
}

// TestAliasPrecedenceFirst guards the selection_precedence on the alias
// column: a custom order with "first" first puts the needle on the first
// alias even when the history points elsewhere.
func TestAliasPrecedenceFirst(t *testing.T) {
	cfg := cfgFromYAML(t, `
selection_precedence: [first]
aliases:
  llama:
    options:
      model:
        - qwen-3.8-27B: /models/big.gguf
        - qwen-3.6-35B-A3B: /models/small.gguf
      command: llama-server -m "$MODEL"
  other:
    options:
      model:
        - m1: v1
      command: echo other
`)
	if len(cfg.SelectionPrecedence) != 1 || cfg.SelectionPrecedence[0] != "first" {
		t.Fatalf("precedence = %v, want [first]", cfg.SelectionPrecedence)
	}
	hp, entries := appendHistoryFor(t, "other")
	m, err := New(Options{Cfg: cfg, HistPath: hp})
	if err != nil {
		t.Fatal(err)
	}
	m.history = entries
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.View()
	// "first" wins over the history (other): the needle is on llama.
	if m.selAlias != 0 || m.selAliasSrc != config.SourceFirst {
		t.Fatalf("selection = %d (%s), want 0 (%s)",
			m.selAlias, m.selAliasSrc, config.SourceFirst)
	}
	if m.curAlias != 0 {
		t.Fatalf("cursor = %d, want 0 (starts on the selected alias)", m.curAlias)
	}
}

// TestCustomNeedle guards the configurable needle glyph: a needle: set in the
// config replaces the default ▸ in the inactive columns (the active column
// still shows the cursor highlight, never the needle).
func TestCustomNeedle(t *testing.T) {
	cfg := cfgFromYAML(t, `
needle: ★
aliases:
  llama:
    options:
      model:
        - qwen-3.8-27B: !default /models/big.gguf
        - qwen-3.6-35B-A3B: /models/small.gguf
      context:
        - 64k: 65536
        - 256k: 262144
      command: llama-server -m "$MODEL" -c "$CONTEXT"
  other:
    options:
      model:
        - m1: v1
      command: echo other
`)
	if cfg.Needle != "★" {
		t.Fatalf("needle = %q, want ★", cfg.Needle)
	}
	m := newModel(t, cfg, "llama")
	view := m.View()

	// The custom needle renders in the inactive columns: the alias column
	// (on the selected alias "llama") and the context column (on the
	// preselected "64k"). The model column is active, so no needle there.
	mustContain(t, view, "★ llama", "★ 64k")
	// The default needle is gone.
	if strings.Contains(view, "▸") {
		t.Errorf("the default needle must not render with a custom needle:\n%s", view)
	}
}

// TestAliasEnterRunsCursor guards Enter in the alias column: it launches the
// alias under the cursor with its preselected options.
func TestAliasEnterRunsCursor(t *testing.T) {
	cfg := loadCfg(t)
	hp, entries := appendHistoryFor(t, "other")
	m, err := New(Options{Cfg: cfg, HistPath: hp, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	m.history = entries
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.View()
	if m.curAlias != 1 {
		t.Fatalf("cursor = %d, want 1 (from history)", m.curAlias)
	}
	send(t, m, upKey) // cursor: other -> llama
	if m.curAlias != 0 {
		t.Fatalf("cursor = %d, want 0", m.curAlias)
	}
	send(t, m, enterKey)
	if !m.doQuit {
		t.Fatal("dry-run enter should quit")
	}
	if !strings.Contains(m.dryCmd, "llama-server") {
		t.Errorf("dryCmd = %q, want the cursor alias' command (llama)", m.dryCmd)
	}
}

// TestStartAliasSelects guards xuz <alias>: the named alias becomes the
// selected alias (green needle on it) even when the history points
// elsewhere.
func TestStartAliasSelects(t *testing.T) {
	cfg := loadCfg(t)
	hp, entries := appendHistoryFor(t, "other")
	m, err := New(Options{Cfg: cfg, StartAlias: "llama", HistPath: hp})
	if err != nil {
		t.Fatal(err)
	}
	m.history = entries
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	view := m.View()
	if m.selAlias != 0 || m.selAliasSrc != srcUser {
		t.Fatalf("selection = %d (%s), want 0 (%s)", m.selAlias, m.selAliasSrc, srcUser)
	}
	// Starting with StartAlias puts the cursor in the first option column
	// (curCol=1), so the alias column is inactive and shows the needle on the
	// start alias (llama). The option column is active and hides its needle.
	if !strings.Contains(view, "▸ llama") {
		t.Errorf("the start alias must carry the needle in the inactive alias column:\n%s", view)
	}
}

// TestCommandPreviewFollowsCursor guards the live command preview: it shows
// the alias under the cursor's command (what Enter will run).
func TestCommandPreviewFollowsCursor(t *testing.T) {
	cfg := loadCfg(t)
	hp, entries := appendHistoryFor(t, "other")
	m, err := New(Options{Cfg: cfg, HistPath: hp})
	if err != nil {
		t.Fatal(err)
	}
	m.history = entries
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.View()
	if got := ansi.Strip(m.commandPreview()); !strings.Contains(got, "echo other") {
		t.Errorf("preview = %q, want the cursor alias' command (other)", got)
	}
	send(t, m, upKey) // cursor: other -> llama
	if got := ansi.Strip(m.commandPreview()); !strings.Contains(got, "llama-server") {
		t.Errorf("preview = %q, want it to follow the cursor to llama", got)
	}
}

// TestNoPairOptionPassesEmptyValue guards the no-pair form's value semantics:
// a bare "- key:" option passes an empty env var (a no-op parameter), not the
// key text; an explicit different long text still passes that text. The
// column shows only the key in both cases.
func TestNoPairOptionPassesEmptyValue(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  a:
    options:
      env:
        - dev:
        - prod: production
      command: run --env "$ENV"
`)
	m := newModel(t, cfg, "a")
	m.View()
	// The no-pair option is the first (and preselected) row: the column shows
	// only the key, and the preview carries an empty ENV assignment.
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "ENV=''") {
		t.Errorf("preview must carry ENV='':\n%s", v)
	}
	if strings.Contains(v, "ENV='dev'") {
		t.Errorf("the no-pair option must not pass the key text:\n%s", v)
	}
	mustContain(t, v, "dev", "prod", "run --env \"$ENV\"")

	// Selecting the scalar option passes its long text.
	send(t, m, downKey) // cursor -> prod
	if got := ansi.Strip(m.commandPreview()); !strings.Contains(got, "ENV='production'") {
		t.Errorf("preview = %q, want ENV='production'", got)
	}

	// Enter runs with the selected value.
	send(t, m, upKey) // cursor -> dev (the no-pair option)
	send(t, m, enterKey)
	if !m.doRun {
		t.Fatal("enter should run")
	}
	if got := m.runVars["ENV"]; got != "" {
		t.Errorf("ENV = %q, want \"\" (no-pair option)", got)
	}
	if m.runCmd != `run --env "$ENV"` {
		t.Errorf("runCmd = %q", m.runCmd)
	}
}

// TestMouseClickAliasSelects guards a left click in the alias column: it
// moves the cursor there and makes the clicked alias the selected one, like
// a click in an option column selects the option under the cursor.
func TestMouseClickAliasSelects(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "")
	m.View()
	cl := m.layout[0]                  // alias column
	mouseClick(t, m, cl.x+1, cl.y+1+1) // second alias: "other"
	if m.curAlias != 1 || m.selAlias != 1 || m.selAliasSrc != srcUser {
		t.Errorf("after alias click: cursor=%d selection=%d (%s), want 1/1(%s)",
			m.curAlias, m.selAlias, m.selAliasSrc, srcUser)
	}
	if m.curCol != 0 {
		t.Errorf("after alias click: col=%d, want 0 (focus stays on the alias column)", m.curCol)
	}
}

// TestShortModeAliasNeedle guards the needle in the short mode's alias
// search: it marks the selected alias (here from the history) and does not
// follow the cursor; space selects the alias under the cursor and opens its
// first option column in one step.
func TestShortModeAliasNeedle(t *testing.T) {
	cfg := loadCfg(t)
	hp, entries := appendHistoryFor(t, "other")
	m, err := New(Options{Cfg: cfg, Short: true, HistPath: hp})
	if err != nil {
		t.Fatal(err)
	}
	m.history = entries
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	view := m.View()
	if m.selAlias != 1 || m.curAlias != 1 {
		t.Fatalf("selection/cursor = %d/%d, want 1/1 (from history)", m.selAlias, m.curAlias)
	}
	lines := strings.Split(view, "\n")
	// header + value rows + the filter line at the bottom + the tooltip line
	if len(lines) != 1+shortListRows+2 {
		t.Fatalf("short view has %d lines, want %d:\n%s", len(lines), 1+shortListRows+2, view)
	}
	// The needle is hidden in the active column: the cursor highlight is the
	// selection indicator. No ▸ anywhere in the alias list.
	for _, l := range lines {
		if strings.Contains(l, "▸") {
			t.Errorf("the needle must be hidden in the active column:\n%s", view)
		}
	}
	// Move the cursor: the highlight follows; the needle stays hidden.
	send(t, m, upKey) // cursor: other -> llama
	lines = strings.Split(m.View(), "\n")
	for _, l := range lines {
		if strings.Contains(l, "▸") {
			t.Errorf("the needle must stay hidden in the active column:\n%s", m.View())
		}
	}
	// Space selects the alias under the cursor and enters its first column.
	send(t, m, spaceKey)
	if m.selAlias != 0 || m.selAliasSrc != srcUser || m.curCol != 1 || m.search {
		t.Fatalf("after space: selection=%d(%s) col=%d search=%v, want 0(%s)/1/false",
			m.selAlias, m.selAliasSrc, m.curCol, m.search, srcUser)
	}
}

// TestFullModeOptionSelectsAlias guards the full mode: selecting an option
// (space) in a column selects the alias of that column - the needle follows
// to it (green, a user selection), and Enter runs that alias.
func TestFullModeOptionSelectsAlias(t *testing.T) {
	cfg := loadCfg(t)
	hp, entries := appendHistoryFor(t, "other")
	m, err := New(Options{Cfg: cfg, HistPath: hp, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	m.history = entries
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.View()
	if m.selAlias != 1 || m.curAlias != 1 {
		t.Fatalf("selection/cursor = %d/%d, want 1/1 (other, from history)",
			m.selAlias, m.curAlias)
	}
	send(t, m, upKey) // cursor: other -> llama; the needle stays on other
	if m.curAlias != 0 || m.selAlias != 1 {
		t.Fatalf("after up: cursor=%d selection=%d, want 0/1", m.curAlias, m.selAlias)
	}
	send(t, m, tabKey) // -> llama's model column
	if m.curCol != 1 {
		t.Fatalf("col = %d, want 1", m.curCol)
	}
	send(t, m, spaceKey) // select the option under the cursor
	g := m.aliases[0].Groups[0]
	if m.selAlias != 0 || m.selAliasSrc != srcUser {
		t.Errorf("selection = %d (%s), want 0 (%s)", m.selAlias, m.selAliasSrc, srcUser)
	}
	if g.Selected != g.Cursor || g.Src != srcUser {
		t.Errorf("model: selected=%d cursor=%d src=%q, want the user selection",
			g.Selected, g.Cursor, g.Src)
	}
	view := m.View()
	if !strings.Contains(ansi.Strip(view), "▸ llama") {
		t.Errorf("the needle must be on llama:\n%s", view)
	}
	if strings.Contains(ansi.Strip(view), "▸ other") {
		t.Errorf("the needle must not stay on other:\n%s", view)
	}
	send(t, m, enterKey)
	if !m.doQuit {
		t.Fatal("enter should run (dry)")
	}
	// dryCmd is resolved: llama's preselected options (the !default model,
	// the first context), shown as the env prefix + the verbatim command.
	if m.dryCmd != `CONTEXT='65536' MODEL='/models/big.gguf' llama-server -m "$MODEL" -c "$CONTEXT"` {
		t.Errorf("dryCmd = %q, want the selected alias' resolved command", m.dryCmd)
	}
}

// TestMouseClickOptionSelectsAlias guards the mouse equivalent: a left click
// on an option row selects the option and the alias of that column.
func TestMouseClickOptionSelectsAlias(t *testing.T) {
	cfg := loadCfg(t)
	hp, entries := appendHistoryFor(t, "other")
	m, err := New(Options{Cfg: cfg, HistPath: hp})
	if err != nil {
		t.Fatal(err)
	}
	m.history = entries
	send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.View()
	if m.curAlias != 1 || m.selAlias != 1 {
		t.Fatalf("selection/cursor = %d/%d, want 1/1 (other, from history)",
			m.curAlias, m.selAlias)
	}
	send(t, m, upKey) // cursor: other -> llama
	m.View()          // re-render llama's columns
	if len(m.layout) != 3 {
		t.Fatalf("layout has %d columns, want 3", len(m.layout))
	}
	cl := m.layout[1]                  // llama's model column
	mouseClick(t, m, cl.x+1, cl.y+1+0) // first row: qwen-3.8-27B
	g := m.aliases[0].Groups[0]
	if m.curCol != 1 || g.Selected != 0 || g.Src != srcUser {
		t.Errorf("after option click: col=%d selected=%d src=%q, want 1/0/%s",
			m.curCol, g.Selected, g.Src, srcUser)
	}
	if m.selAlias != 0 || m.selAliasSrc != srcUser {
		t.Errorf("selection = %d (%s), want 0 (%s)", m.selAlias, m.selAliasSrc, srcUser)
	}
}

// TestShortMenuTooltipStyles guards the compact mode's shortcut tooltip:
// each key glyph is highlighted (bold, in the accent color) while its
// explanation keeps the muted help color.
func TestShortMenuTooltipStyles(t *testing.T) {
	// lipgloss only emits SGR sequences under a color profile; force
	// TrueColor so the styled spans can be inspected. The setting is sticky
	// for the process, so keep this test at the end of the file.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	cfg := loadCfg(t)
	m := newShortModel(t, cfg, "")
	lines := strings.Split(m.View(), "\n")
	foot := lines[len(lines)-1] // the tooltip line (no prompt or status open)

	bold := lipgloss.NewStyle().Bold(true)
	for _, it := range shortMenuItems {
		// The exact spans shortMenu joins into the tooltip line, rendered
		// with the same styles: a standalone bold SGR around HelpKey's
		// accent-colored key glyph, and the muted explanation.
		keySpan := bold.Render(m.sty.HelpKey.Render(it.keys))
		if !strings.HasPrefix(keySpan, "\x1b[1m") {
			t.Fatalf("key span for %q lacks a standalone bold SGR: %q", it.keys, keySpan)
		}
		if !strings.Contains(foot, keySpan) {
			t.Errorf("tooltip missing highlighted (bold accent) key %q:\n%s", it.keys, foot)
		}
		labelSpan := m.sty.Help.Render(it.label)
		if !strings.Contains(foot, labelSpan) {
			t.Errorf("tooltip missing muted explanation %q:\n%s", it.label, foot)
		}
	}
}

// TestFullMenuTooltipStyles guards the full mode's shortcut legends: every
// item of the current state is rendered with its key tokens in bold accent
// and its label muted (the same two-tone format as the short menu).
func TestFullMenuTooltipStyles(t *testing.T) {
	// lipgloss only emits SGR sequences under a color profile; force
	// TrueColor so the styled spans can be inspected. The setting is sticky
	// for the process, so keep this test at the end of the file.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	cfg := loadCfg(t)
	states := []struct {
		name string
		fn   func(m *model)
	}{
		{"option column", func(m *model) {}},
		{"alias column", func(m *model) { m.curCol = 0 }},
		{"filter, option column", func(m *model) { m.search = true }},
		{"input prompt", func(m *model) { m.prompt = &prompt{kind: promptInput, label: "new value:"} }},
		{"confirm prompt", func(m *model) { m.prompt = &prompt{kind: promptConfirm, label: "delete?"} }},
	}
	for _, st := range states {
		m := newModel(t, cfg, "llama") // starts on the option column
		st.fn(m)
		view := strings.Join(m.helpLines(), "\n")
		for _, it := range m.fullHelpItems() {
			if it.keys != "" {
				keySpan := lipgloss.NewStyle().Bold(true).Render(m.sty.HelpKey.Render(it.keys))
				if !strings.HasPrefix(keySpan, "\x1b[1m") {
					t.Fatalf("%s: key span for %q lacks a standalone bold SGR: %q", st.name, it.keys, keySpan)
				}
			}
			want := m.renderHelpItem(it)
			if !strings.Contains(view, want) {
				t.Errorf("%s: legend missing item %q:\n%s", st.name, want, view)
			}
		}
	}
}

// --- GLOBALS panel (full mode, above COMMAND; "v" focuses it) --------------

func TestGlobalsPanelAlwaysVisibleAboveCommand(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
        - prod
      command: terraform plan ${ENV}
global_variables:
  ENV: dev
`)
	m := newModel(t, cfg, "tf-plan")
	view := m.View()
	// The panel is always present in the footer, above COMMAND.
	mustContain(t, view, "GLOBALS", "ENV", "dev")
	if !strings.Contains(view, "COMMAND") {
		t.Fatal("COMMAND panel should be visible")
	}
	if strings.Index(view, "GLOBALS") > strings.Index(view, "COMMAND") {
		t.Error("the GLOBALS panel should sit above the COMMAND panel")
	}
	// Without variables defined, the panel shows a hint row.
	m2 := newModel(t, cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
      command: terraform plan ${ENV}
`), "tf-plan")
	if !strings.Contains(m2.View(), "(no global variables") {
		t.Error("empty globals panel should show the hint row")
	}
}

func TestVariablesFocusAndLegend(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
        - prod
      command: terraform plan ${ENV}
global_variables:
  ENV: dev
`)
	m := newModel(t, cfg, "tf-plan")
	view := m.View()
	if strings.Contains(ansi.Strip(view), "edit") {
		t.Fatal("the edit legend should not be visible before v is pressed")
	}
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if m.varsFocus == nil {
		t.Fatal("varsFocus should be set after v")
	}
	view = m.View()
	mustContain(t, view, "GLOBALS", "ENV", "dev")
	// The legend switches to the panel's own, in the main menu format (no
	// "n"ew: new variables are created on the appended empty row).
	for _, want := range []string{"exit", "edit", "move", "elete"} {
		if !strings.Contains(ansi.Strip(view), want) {
			t.Errorf("legend missing %q", want)
		}
	}
	if strings.Contains(ansi.Strip(view), "ew") {
		t.Error("the legend must not advertise an n shortcut anymore")
	}
	// The picker's state is preserved underneath.
	if m.curAlias != 0 || m.curCol != 1 {
		t.Errorf("picker state changed: curAlias=%d curCol=%d", m.curAlias, m.curCol)
	}
}

func TestVariablesStatePreservedOnExit(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, tabKey) // into the option column
	send(t, m, downKey) // move the cursor
	curColBefore := m.curCol
	curAliasBefore := m.curAlias
	selBefore := m.aliases[curAliasBefore].Groups[curColBefore-1].Selected

	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if m.varsFocus == nil {
		t.Fatal("varsFocus should be set after v")
	}
	send(t, m, escKey) // blur without changes
	if m.varsFocus != nil {
		t.Fatal("varsFocus should be cleared after esc")
	}
	if m.curCol != curColBefore || m.curAlias != curAliasBefore {
		t.Errorf("picker state lost: col %d->%d alias %d->%d", curColBefore, m.curCol, curAliasBefore, m.curAlias)
	}
	if got := m.aliases[curAliasBefore].Groups[curColBefore-1].Selected; got != selBefore {
		t.Errorf("selection lost: %d -> %d", selBefore, got)
	}
}

func TestVariablesNewAndFlush(t *testing.T) {
	cfg := loadCfg(t) // cfg.Path is set
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if m.varsFocus == nil {
		t.Fatal("varsFocus should be set after v")
	}
	// No variables yet: the appended empty row is row 0; space on it creates
	// a new variable in place.
	send(t, m, spaceKey)
	v := m.varsFocus
	if !v.Editing || v.Col != varKey || v.Row != 0 {
		t.Fatalf("space on the appended row should start editing its key, got %+v", v)
	}
	typeStr(t, m, "ENV")
	send(t, m, enterKey) // commit the key cell
	if cfg.GlobalVariables["ENV"] != "" {
		t.Errorf("after committing the key, GlobalVariables = %+v", cfg.GlobalVariables)
	}
	if v.Editing {
		t.Fatal("editing should be done after committing the key")
	}
	// The hover re-lands on the committed row's key; right moves to its value.
	send(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if v.Col != varValue {
		t.Fatalf("right should hover the value cell, got %v", v.Col)
	}
	// Edit the value in place and commit it.
	send(t, m, spaceKey)
	if !v.Editing || v.Col != varValue {
		t.Fatal("space should start editing the hovered value cell")
	}
	typeStr(t, m, "dev")
	send(t, m, enterKey) // commit the value cell
	if cfg.GlobalVariables["ENV"] != "dev" {
		t.Errorf("GlobalVariables = %+v", cfg.GlobalVariables)
	}
	reloaded, err := config.Load(cfg.Path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.GlobalVariables["ENV"]; got != "dev" {
		t.Errorf("reloaded GlobalVariables[ENV] = %q", got)
	}
}

func TestVariablesInvalidKeyRejected(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	send(t, m, spaceKey) // create on the appended empty row
	typeStr(t, m, "1BAD")
	send(t, m, enterKey)
	if !m.varsFocus.Editing {
		t.Fatal("invalid key should keep the edit open")
	}
	if !strings.Contains(m.varsStatus, "not a valid variable name") {
		t.Errorf("varsStatus = %q", m.varsStatus)
	}
	// Clear the rejected buffer, then type a valid key.
	n := len(m.varsFocus.Buf)
	for i := 0; i < n; i++ {
		send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	typeStr(t, m, "OK_KEY")
	send(t, m, enterKey)
	if m.varsFocus.Editing {
		t.Fatal("valid key should commit and end the edit")
	}
	if _, ok := cfg.GlobalVariables["OK_KEY"]; !ok {
		t.Errorf("GlobalVariables = %+v", cfg.GlobalVariables)
	}
}

func TestVariablesDelete(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
      command: terraform plan ${ENV}
global_variables:
  ENV: dev
  REGION: eu-west-1
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	// Sorted: ENV (row 0), REGION (row 1).
	if got := cfg.GlobalVariables["REGION"]; got != "eu-west-1" {
		t.Fatalf("vars = %+v", cfg.GlobalVariables)
	}
	send(t, m, dKey)
	if m.prompt == nil || m.prompt.kind != promptConfirm {
		t.Fatal("d should open the delete confirm prompt")
	}
	if !strings.Contains(m.prompt.label, "ENV") {
		t.Errorf("label = %q", m.prompt.label)
	}
	send(t, m, yKey)
	if _, ok := cfg.GlobalVariables["ENV"]; ok {
		t.Error("ENV should be deleted from the config")
	}
	if got := cfg.GlobalVariables["REGION"]; got != "eu-west-1" {
		t.Errorf("REGION should survive, vars = %+v", cfg.GlobalVariables)
	}
	reloaded, err := config.Load(cfg.Path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.GlobalVariables["ENV"]; ok {
		t.Error("ENV should be gone from the saved config")
	}
}

func TestVariablesRenameMovesValue(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default vdev
      command: terraform plan ${ENV}
global_variables:
  ENV: dev
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	// Edit the key of the hovered row in place (ENV -> TARGET).
	send(t, m, spaceKey)
	if !m.varsFocus.Editing || m.varsFocus.Col != varKey {
		t.Fatal("space should start editing the hovered key cell")
	}
	// Clear the pre-filled buffer, then type the new name.
	n := len(m.varsFocus.Buf)
	for i := 0; i < n; i++ {
		send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	typeStr(t, m, "TARGET")
	send(t, m, enterKey) // commit the key cell (rename carries the value over)
	if cfg.GlobalVariables["TARGET"] != "dev" {
		t.Errorf("renamed var should keep its value: %+v", cfg.GlobalVariables)
	}
	if _, ok := cfg.GlobalVariables["ENV"]; ok {
		t.Errorf("old key should be gone: %+v", cfg.GlobalVariables)
	}
}

func TestVariablesUpDownStayInColumn(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
      command: terraform plan ${ENV}
global_variables:
  ALPHA: a
  BETA: b
  GAMMA: g
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	v := m.varsFocus
	// Hover the value column of row 0.
	send(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if v.Col != varValue {
		t.Fatalf("right should hover the value column, got %v", v.Col)
	}
	// Down stays in the value column, moving to the next row.
	send(t, m, downKey)
	if v.Row != 1 || v.Col != varValue {
		t.Fatalf("down should stay in the hovered column: row=%d col=%v", v.Row, v.Col)
	}
	// Up comes back.
	send(t, m, upKey)
	if v.Row != 0 || v.Col != varValue {
		t.Fatalf("up should stay in the hovered column: row=%d col=%v", v.Row, v.Col)
	}
	// Left switches to the key column of the same row.
	send(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if v.Row != 0 || v.Col != varKey {
		t.Fatalf("left should hover the key column: row=%d col=%v", v.Row, v.Col)
	}
}

func TestVariablesEscDoesNotQuitApp(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	send(t, m, escKey)
	if m.doQuit {
		t.Fatal("esc in the globals panel must not quit the app")
	}
	if m.varsFocus != nil {
		t.Fatal("focus should be cleared")
	}
}

func TestGlobalsPanelScrollsBeyondMaxRows(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
      command: terraform plan ${ENV}
global_variables:
  A1: v1
  A2: v2
  A3: v3
  A4: v4
  A5: v5
  A6: v6
  A7: v7
  A8: v8
`)
	m := newModel(t, cfg, "tf-plan")
	view := m.View()
	// Only globalsPanelMaxRows variable rows are rendered; the rest scroll.
	for i := 0; i < globalsPanelMaxRows; i++ {
		if !strings.Contains(view, fmt.Sprintf("A%d", i+1)) {
			t.Errorf("panel should show A%d (unfocused)", i+1)
		}
	}
	if strings.Contains(view, "A7") || strings.Contains(view, "A8") {
		t.Error("the panel must not show more than globalsPanelMaxRows rows unfocused")
	}
	// Focused: move to the last row; it scrolls into view.
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	for i := 0; i < 7; i++ {
		send(t, m, downKey)
	}
	if m.varsFocus.Row != 7 {
		t.Fatalf("row = %d, want 7", m.varsFocus.Row)
	}
	view = m.View()
	if !strings.Contains(view, "A8") {
		t.Error("the hovered last row (A8) should be scrolled into view")
	}
	if strings.Contains(view, "A1") {
		t.Error("A1 should have scrolled out of view")
	}
}

func TestGlobalsPanelInPlaceEditVisible(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
      command: terraform plan ${ENV}
global_variables:
  ENV: dev
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	send(t, m, spaceKey) // edit the hovered key cell in place
	typeStr(t, m, "X")
	view := ansi.Strip(m.View())
	// The buffer renders on the key cell itself (ENV + X█), not on a separate line.
	if !strings.Contains(view, "ENVX█") {
		t.Errorf("the in-place edit should render on the cell:\n%s", view)
	}
}

// --- Global variables in the command env -----------------------------------

func TestGlobalVarsExposedWhenReferenced(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default vdev
        - prod: vprod
      command: terraform plan ${ENV}
global_variables:
  ENV: global-dev
  REGION: eu-west-1
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	vars := m.currentVars(0, m.aliases[0])
	// The env column (uppercased) overrides the same-named global variable.
	if vars["ENV"] != "vdev" {
		t.Errorf("column value should win over the global, got %q", vars["ENV"])
	}
	if _, ok := vars["REGION"]; ok {
		t.Errorf("unreferenced global REGION must not be exposed, vars = %+v", vars)
	}
	// The preview prefix carries the column value, not the global.
	mustContain(t, m.commandPreview(), "ENV='vdev'", "terraform plan ${ENV}")
	if strings.Contains(m.commandPreview(), "REGION") {
		t.Errorf("preview should not mention REGION: %s", m.commandPreview())
	}
}

func TestGlobalVarsColumnPrecedence(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default vdev
        - prod: vprod
      command: terraform plan ${ENV}
global_variables:
  ENV: global-dev
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	// The env column (uppercased) overrides the same-named global variable.
	vars := m.currentVars(0, m.aliases[0])
	if vars["ENV"] != "vdev" {
		t.Errorf("column value should win over the global, got %q", vars["ENV"])
	}
	// The column's cursor (prod) wins in the preview too.
	send(t, m, downKey)
	if !strings.Contains(m.commandPreview(), "ENV='vprod'") {
		t.Errorf("preview = %s", m.commandPreview())
	}
}

func TestGlobalVarsLiteralNameReference(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default vdev
      command: terraform plan -var "env=${ENV:-null}"
global_variables:
  ENV: global-dev
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	vars := m.currentVars(0, m.aliases[0])
	// The column overrides the global; the literal name in the sophisticated
	// form still triggers the exposure (the column wins).
	if vars["ENV"] != "vdev" {
		t.Errorf("column value should win over the global, vars = %+v", vars)
	}
}

func TestGlobalVarsExposedWithoutColumnConflict(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default vdev
      command: terraform plan ${ENV} --region ${REGION}
global_variables:
  REGION: eu-west-1
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	vars := m.currentVars(0, m.aliases[0])
	// No column named REGION, so the global is exposed as-is.
	if vars["REGION"] != "eu-west-1" {
		t.Errorf("global REGION should be exposed, vars = %+v", vars)
	}
	mustContain(t, m.commandPreview(), "REGION='eu-west-1'", "ENV='vdev'")
}

func TestGlobalVarsTemplateReference(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  app:
    options:
      model:
        - m1: v1
      command: run $APP
      template: |
        APP={{.MODEL}}-{{.REGION}}
global_variables:
  REGION: eu-west-1
`)
	m := newModel(t, cfg, "app")
	m.View()
	vars := m.fullVars(0, m.aliases[0])
	if vars["REGION"] != "eu-west-1" {
		t.Errorf("template-referenced global should be exposed, vars = %+v", vars)
	}
	if vars["APP"] != "v1-eu-west-1" {
		t.Errorf("derived APP = %q", vars["APP"])
	}
}

func TestDryRunIncludesReferencedGlobals(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default vdev
      command: terraform plan ${ENV}
global_variables:
  ENV: global-dev
`)
	m := newModel(t, cfg, "tf-plan")
	m.dryRun = true
	m.View()
	send(t, m, enterKey)
	if !m.doQuit {
		t.Fatal("enter should quit in dry-run mode")
	}
	// The column value (vdev) wins over the global (global-dev).
	if !strings.Contains(m.dryCmd, "ENV='vdev' terraform plan ${ENV}") {
		t.Errorf("dryCmd = %q", m.dryCmd)
	}
}


// TestGlobalsPanelLineWidths guards against the overlay-compositing class of
// bug: every rendered line — with or without the GLOBALS panel focused — must
// be exactly the terminal width, whatever the size. Sliced ANSI sequences
// (a border offset by stray spaces, a leaked color code) show up here as a
// width mismatch.
func TestGlobalsPanelLineWidths(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
        - prod
      command: terraform plan ${ENV}
global_variables:
  ENV: dev
  REGION: eu-west-1
`)
	for _, w := range []int{60, 80, 100, 120} {
		m, err := New(Options{Cfg: cfg, StartAlias: "tf-plan"})
		if err != nil {
			t.Fatal(err)
		}
		send(t, m, tea.WindowSizeMsg{Width: w, Height: 24})
		check := func(view string, ctx string) {
			for i, l := range strings.Split(view, "\n") {
				if dw := lipgloss.Width(l); dw != w {
					t.Errorf("%s w=%d line %d has width %d: %.60q", ctx, w, i, dw, l)
				}
			}
		}
		check(m.View(), "plain")
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")}) // focus the panel
		check(m.View(), "focused")
		send(t, m, nKey) // editing a new variable (input line inside the box)
		typeStr(t, m, "FOO")
		check(m.View(), "editing")
	}
}

// TestGlobalsSaveMessageSingleInstance guards the double-message bug: after a
// change flushes to the config file, "variables saved to ..." must appear
// exactly once — in the GLOBALS panel's bottom line, never also in the footer
// status line above the panel.
func TestGlobalsSaveMessageSingleInstance(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "llama")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	send(t, m, spaceKey) // create on the appended empty row
	typeStr(t, m, "ENV")
	send(t, m, enterKey) // commit the key cell -> flush
	view := ansi.Strip(m.View())
	count := strings.Count(view, "variables saved to")
	if count != 1 {
		t.Errorf("the save message should appear exactly once, got %d:\n%s", count, view)
	}
	// The footer status line (above the panel) must be empty.
	if m.status != "" {
		t.Errorf("footer status should be empty, got %q", m.status)
	}
}

// TestGlobalsHoverHighlightsCellOnly guards the whole-line highlight bug:
// hovering a cell paints that cell's span with the row background — not the
// rest of the line. The plain (non-cursor) cells keep the default background.
func TestGlobalsHoverHighlightsCellOnly(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor) // force color so the highlight SGRs are emitted
	defer lipgloss.SetColorProfile(prev)
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
      command: terraform plan ${ENV}
global_variables:
  ENV: dev
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")}) // hover row 0 key cell
	panel := strings.Join(m.globalsPanelLines(), "\n")
	// The line must contain the row-cursor background (the hovered key span)
	// but must NOT end with a cursor-background run covering the value column:
	// after the last non-border character of the value cell, only plain spaces
	// may follow. Find the row line and check its tail is unstyled.
	for _, l := range strings.Split(panel, "\n") {
		if !strings.Contains(l, "ENV") {
			continue
		}
		// The row-cursor style wraps the whole hovered key cell (text + its
		// padding). Detect its background SGR directly: lipgloss emits
		// "\x1b[<fg>;<bg>m" and the reset may be "0m" or "m", so match on the
		// 48;2;r;g;b part only.
		bgHex := strings.TrimPrefix(string(theme.NormalizeColor(m.th.CursorBg)), "#")
		r, _ := strconv.ParseUint(bgHex[0:2], 16, 8)
		g, _ := strconv.ParseUint(bgHex[2:4], 16, 8)
		b2, _ := strconv.ParseUint(bgHex[4:6], 16, 8)
		cursorBgSGR := fmt.Sprintf(";48;2;%d;%d;%d", r, g, b2)
		if !strings.Contains(l, cursorBgSGR) {
			t.Fatalf("hovered key cell should carry the cursor background:\n%s", l)
		}
		// After the value text "dev" (up to the right border + padding) no
		// cursor-background span may follow: a whole-line highlight would put
		// one there.
		idx := strings.LastIndex(l, "dev")
		if idx < 0 {
			t.Fatalf("row missing the value text:\n%s", l)
		}
		tail := l[idx+3:]
		if strings.Contains(tail, cursorBgSGR) {
			t.Errorf("the value column's padding carries the cursor background (whole-line highlight):\n%s", l)
		}
		break
	}
}

// TestMouseClickSelectsGlobalsPanel guards that a left click inside the
// GLOBALS panel focuses it and hovers the clicked row/cell: the left half of
// a row is its key cell, the right half its value column. Clicking while the
// panel already has focus steers the hover directly (clicks are not swallowed
// then). A click on the panel's border only focuses it.
func TestMouseClickSelectsGlobalsPanel(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
      command: terraform plan ${ENV}
global_variables:
  ALPHA: a
  BETA: b
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	if m.globalsRect.h == 0 {
		t.Fatal("the globals panel rect should be recorded in View")
	}
	r := m.globalsRect
	// Click the key cell of the second row (BETA).
	mouseClick(t, m, r.x+2, r.y+1+1)
	if m.varsFocus == nil {
		t.Fatal("a click inside the globals panel should focus it")
	}
	if m.varsFocus.Row != 1 || m.varsFocus.Col != varKey {
		t.Errorf("after key click: row=%d col=%v, want 1/key", m.varsFocus.Row, m.varsFocus.Col)
	}
	// Click the value cell of the first row (ALPHA) — the panel already has
	// focus, so the hover must move there directly.
	innerW := r.w - 4
	keyW := innerW / 2
	mouseClick(t, m, r.x+2+keyW+1, r.y+1+0)
	if m.varsFocus.Row != 0 || m.varsFocus.Col != varValue {
		t.Errorf("after value click: row=%d col=%v, want 0/value", m.varsFocus.Row, m.varsFocus.Col)
	}
	// Click the panel's top border: re-focuses the panel fresh (the hover is
	// reset to the first row's key cell — no stale state survives a click).
	mouseClick(t, m, r.x+2, r.y)
	if m.varsFocus.Row != 0 || m.varsFocus.Col != varKey {
		t.Errorf("after border click: row=%d col=%v, want 0/key", m.varsFocus.Row, m.varsFocus.Col)
	}
	// Click the appended empty row (just past BETA): hovers it.
	mouseClick(t, m, r.x+2, r.y+1+2)
	if m.varsFocus.Row != 2 || m.varsFocus.Col != varKey {
		t.Errorf("after appended-row click: row=%d col=%v, want 2/key", m.varsFocus.Row, m.varsFocus.Col)
	}
}

// TestGlobalsAppendedRowCreateHint guards the appended empty row of the
// focused GLOBALS panel: moving past the last variable lands on it, and while
// it is hovered (not editing) the create hint shows in the panel's bottom
// line — the same position as the save messages. Space/enter on it starts
// creating a new variable in place; a created variable lands at the bottom of
// the list and the hover stays on it.
func TestGlobalsAppendedRowCreateHint(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
      command: terraform plan ${ENV}
global_variables:
  ALPHA: a
  BETA: b
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	// Move down past the last variable (BETA) onto the appended row.
	send(t, m, downKey) // row 0 -> 1
	send(t, m, downKey) // row 1 -> 2 (appended)
	if m.varsFocus.Row != 2 {
		t.Fatalf("row = %d, want 2 (the appended row)", m.varsFocus.Row)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "create") {
		t.Errorf("the create hint should show while the appended row is hovered:\n%s", view)
	}
	// The hint sits in the panel's bottom line (same place as save messages):
	// it must appear after the last variable row and before the closing border.
	panelLines := strings.Split(strings.Join(m.globalsPanelLines(), "\n"), "\n")
	hintIdx, betaIdx, closeIdx := -1, -1, -1
	for i, l := range panelLines {
		if strings.Contains(l, "create") {
			hintIdx = i
		}
		if strings.Contains(l, "BETA") {
			betaIdx = i
		}
		if strings.HasPrefix(l, "└") {
			closeIdx = i
		}
	}
	if !(hintIdx > betaIdx && hintIdx < closeIdx) {
		t.Errorf("create hint position wrong: hint=%d beta=%d close=%d", hintIdx, betaIdx, closeIdx)
	}
	// Space on the appended row starts creating a new variable in place.
	send(t, m, spaceKey)
	if !m.varsFocus.Editing || m.varsFocus.Col != varKey {
		t.Fatalf("space should start editing the appended row's key, got %+v", m.varsFocus)
	}
	typeStr(t, m, "ZETA")
	send(t, m, enterKey) // commit the key -> lands at the bottom (sorted)
	if m.varsFocus.Row != 2 {
		t.Fatalf("hover should stay on the new row, got %d", m.varsFocus.Row)
	}
	if cfg.GlobalVariables["ZETA"] != "" {
		t.Errorf("GlobalVariables = %+v", cfg.GlobalVariables)
	}
	// The panel shows ZETA at the bottom, above the appended row.
	view = ansi.Strip(m.View())
	if strings.Index(view, "ZETA") < 0 || strings.Index(view, "BETA") > strings.Index(view, "ZETA") {
		t.Errorf("ZETA should be listed after BETA:\n%s", view)
	}
}

// TestGlobalsNewVariableLandsAtBottom guards that a variable created on the
// appended row is listed at the bottom of the panel (after the last existing
// variable), whatever its name sorts to — and that the save message does not
// hide the appended row: moving down again shows it, with the create hint.
func TestGlobalsNewVariableLandsAtBottom(t *testing.T) {
	cfg := cfgFromYAML(t, `aliases:
  tf-plan:
    options:
      env:
        - dev: !default
      command: terraform plan ${ENV}
global_variables:
  MIDDLE: m
  ZLAST: z
`)
	m := newModel(t, cfg, "tf-plan")
	m.View()
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	// Move to the appended row (past MIDDLE and ZLAST) and create "AAA" — a
	// name that would sort first alphabetically.
	for i := 0; i < 2; i++ {
		send(t, m, downKey)
	}
	if m.varsFocus.Row != 2 {
		t.Fatalf("row = %d, want the appended row (2)", m.varsFocus.Row)
	}
	send(t, m, spaceKey)
	typeStr(t, m, "AAA")
	send(t, m, enterKey) // commit the key
	// The panel must list AAA at the bottom, after ZLAST.
	view := ansi.Strip(m.View())
	if strings.Index(view, "ZLAST") < 0 || strings.Index(view, "AAA") < 0 {
		t.Fatalf("panel missing rows:\n%s", view)
	}
	if strings.Index(view, "AAA") < strings.Index(view, "ZLAST") {
		t.Errorf("AAA must be listed at the bottom (after ZLAST):\n%s", view)
	}
	// The save message is transient: moving down onto the appended row again
	// brings it back with the create hint.
	send(t, m, downKey)
	if m.varsFocus.Row != 3 {
		t.Fatalf("row = %d, want the appended row (3)", m.varsFocus.Row)
	}
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "create") {
		t.Errorf("the appended row should be visible with the create hint:\n%s", view)
	}
	if strings.Count(view, "variables saved to") != 0 {
		t.Errorf("the save message should have cleared when the hover moved:\n%s", view)
	}
}

// sgrPrefix extracts the leading SGR escape sequence (up to and including the
// final "m") that lipgloss emits for a rendered span — used to compare
// border colors in views without hand-parsing hex values.
func sgrPrefix(rendered string) string {
	i := strings.Index(rendered, "\x1b[")
	if i < 0 {
		return ""
	}
	j := strings.Index(rendered[i:], "m")
	if j < 0 {
		return ""
	}
	return rendered[i : i+j+1]
}

// TestColumnsMutedWhileGlobalsFocused guards that focusing the GLOBALS panel
// ("v") mutes every column box (border and title drop to the inactive style),
// and that pressing esc restores the active style on the focused column.
func TestColumnsMutedWhileGlobalsFocused(t *testing.T) {
	cfg := loadCfg(t)
	m := newModel(t, cfg, "")
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	activeBorder := theme.NormalizeColor(m.th.BorderActive)
	inactiveBorder := theme.NormalizeColor(m.th.Border)
	if string(activeBorder) == string(inactiveBorder) {
		t.Skip("theme has no distinct active/inactive border colors")
	}
	// box() renders the border characters with a plain foreground style of the
	// (normalized) color, so build the expected SGR runs through the same
	// lipgloss code path (lipgloss applies its own color transform to hex
	// values — parsing them by hand would not match).
	activeSGR := sgrPrefix(lipgloss.NewStyle().Foreground(activeBorder).Render("x"))
	mutedSGR := sgrPrefix(lipgloss.NewStyle().Foreground(inactiveBorder).Render("x"))

	// Only the column boxes carry these border styles (the title bar and the
	// GLOBALS panel use other colors), so scan just those lines. The box
	// titles are separate styled spans from their borders, so match on the
	// title text; exclude command-content lines that happen to mention a
	// column name (e.g. "CONTEXT='65536'").
	columnBorder := func(view string) string {
		var out []string
		for _, l := range strings.Split(view, "\n") {
			if !strings.Contains(l, "ALIAS") && !strings.Contains(l, "MODEL") && !strings.Contains(l, "CONTEXT") {
				continue
			}
			if strings.Contains(l, "$ ") || strings.Contains(l, "='") {
				continue
			}
			out = append(out, l)
		}
		return strings.Join(out, "\n")
	}
	view := m.View()
	if !strings.Contains(columnBorder(view), activeSGR) {
		t.Fatalf("the focused column should carry the active border before v:\n%s", view)
	}
	send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	view = m.View()
	if strings.Contains(columnBorder(view), activeSGR) {
		t.Errorf("no column may carry the active border while GLOBALS has focus:\n%s", view)
	}
	if !strings.Contains(columnBorder(view), mutedSGR) {
		t.Errorf("the columns should be rendered with the muted border while GLOBALS has focus:\n%s", view)
	}
	send(t, m, escKey)
	view = m.View()
	if !strings.Contains(columnBorder(view), activeSGR) {
		t.Errorf("esc should restore the active border on the focused column:\n%s", view)
	}
}
