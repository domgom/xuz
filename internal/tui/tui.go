// Package tui implements the xuz column-based option picker.
package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"

	"xuz/internal/cmdx"
	"xuz/internal/config"
	"xuz/internal/history"
	"xuz/internal/theme"
)

// Options configures one TUI session.
type Options struct {
	Cfg           *config.Config
	StartAlias    string // alias to start on; "" = start on the alias column
	DryRun        bool   // on Enter, print the resolved command instead of running it
	ThemeOverride string // wins over the config theme
	HistPath      string // history file ("" = history.Path())
	Short         bool   // compact inline mode (alias search + one column at a time); false = full-screen mode
}

// srcUser marks a selection made manually by the user (space, mouse, …), as
// opposed to one resolved from the config or the history in ensureInited.
const srcUser = "user"

type groupState struct {
	Name     string
	Pairs    []config.Pair
	Cursor   int
	Selected int
	Offset   int
	Src      string // where the current selection came from (config.Source*, or srcUser)
	// IconW is the column's leading icon slot width in cells: the max display
	// width of the icons set on the group's options, 0 when none (color tags
	// do not affect the width).
	IconW int
}

type aliasState struct {
	Name      string
	Icon      string // the alias's icon glyph, literal text ("" when none)
	IconColor string // !color tag on the icon ("" when none)
	Groups    []*groupState
	inited    bool
}

// promptKind selects the input mode of an active prompt.
type promptKind int

const (
	promptInput   promptKind = iota // free-text line (new value)
	promptConfirm                   // yes/no (delete value, save on quit)
)

// promptAction names what the prompt does on a positive answer.
type promptAction int

const (
	actNewValue promptAction = iota
	actDeletePair
	actSaveOnQuit
)

type prompt struct {
	kind   promptKind
	label  string
	buf    []rune
	action promptAction
}

// variablesState holds the state of the GLOBALS panel focus (entered with "v"
// in the full mode): the hovered row and column (key or value), whether an
// in-place edit is active on that cell, and the edit buffer. The panel edits
// the config's global variables directly; every change is flushed to the
// config file immediately.
type variablesState struct {
	Row     int // hovered row index (into the sorted variable names)
	Col     varCell // hovered column: the key or the value of the row
	Editing bool  // an in-place edit is active on the hovered cell
	Buf     []rune // the edit buffer of the hovered cell
}

// varCell selects which column of a GLOBALS panel row is hovered/edited.
type varCell int

const (
	varKey varCell = iota
	varValue
)

type model struct {
	cfg          *config.Config
	aliases      []*aliasState
	curAlias     int
	curCol       int    // 0 = alias column, 1..len(groups) = option columns
	selAlias     int    // selected alias: the ▸ needle row of the alias column
	selAliasSrc  string // where the selection came from (config.Source*, or srcUser)
	aliasSelInit bool   // selAlias resolved (ensureAliasSel) or set explicitly
	themes       []string
	themeIdx     int
	th           theme.Theme
	sty          theme.Styles
	needle       string // selection marker glyph (config needle, default ▸)
	needleW      int    // needle display width in cells
	dryRun       bool
	histPath     string
	history      []history.Entry
	width        int
	height       int
	status       string
	dirty        bool // config modified (new/deleted option, new default)
	prompt       *prompt
	search       bool   // fuzzy filter active on the current column
	searchBuf    []rune // filter pattern
	searchOff    int    // scroll offset of the filtered window
	showInfo     bool   // toggle extra info line with ?
	dryCmd       string
	doRun        bool
	doQuit       bool
	runCmd       string
	runVars      map[string]string
	background   bool        // b was pressed: exit and keep the user command running
	logPath      string      // where the backgrounded command's output is written
	layout       []colLayout // where the columns were drawn in the last View
	hScroll      int         // leftmost fully visible column when columns overflow
	viewStart    int         // index of the leftmost rendered column (set in View)
	hasPeek      bool        // a peek sliver was rendered (more columns to the right)
	entry        history.Entry
	short        bool // compact inline mode (alias search + one option column at a time)
	modeSwitch   bool // "/" was pressed: the Run loop restarts in the other mode
	aliasIconW   int  // alias column icon slot: the widest alias icon in cells (0 when none)
	varsFocus     *variablesState // the GLOBALS panel focus state (nil = the columns have focus)
	varsStatus    string          // transient message shown in the GLOBALS panel's bottom line only (never in the footer status line)
	varsDisplay   []string        // display order of the global variables: config-file order, new variables appended at the end (rebuilt when stale)
	globalsRect  rect            // where the GLOBALS panel was drawn in the last View (for mouse clicks)
	lastFrameLines int           // number of lines the compact mode rendered in the last View (set in shortView); used to clear the frame on exit
	konami       []string        // trailing keys of the easter-egg sequence so far (cleared on a mismatch)
	showEgg      bool            // the easter egg panel is open
}

// konamiSequence is the key sequence that opens the easter egg. It is the
// classic Konami code, and while testing it can be swapped for a shorter
// stand-in (for example ["?"]) — see SetKonamiSequence.
var konamiSequence = []string{
	"up", "up", "down", "down",
	"left", "right", "left", "right",
	"b", "a",
}

// SetKonamiSequence replaces the easter-egg trigger sequence (for testing).
func SetKonamiSequence(seq []string) {
	if len(seq) > 0 {
		konamiSequence = seq
	}
}

// colLayout records where one column was rendered in the last View so mouse
// clicks can be mapped back to a column and a row.
type colLayout struct {
	x, y         int   // screen position of the box (border included)
	w            int   // box width in cells
	contentLines int   // number of content rows inside the box
	top          int   // scroll offset of the content window
	items        []int // item indices in content order (alias idx or pair idx)
}

// rect is a screen rectangle (in cells, origin top-left, border included).
type rect struct {
	x, y, w, h int
}

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// currentGroup returns the option group under the cursor, or nil in the
// alias column.
func (m *model) currentGroup() *groupState {
	if m.curCol == 0 {
		return nil
	}
	return m.aliases[m.curAlias].Groups[m.curCol-1]
}

// currentAlias returns the config alias under the cursor.
func (m *model) currentAlias() *config.Alias {
	return m.cfg.Aliases[m.curAlias]
}

// clampCol keeps curCol within the current alias's columns: switching to an
// alias with fewer groups (or none) can leave curCol out of range.
func (m *model) clampCol() {
	if max := len(m.aliases[m.curAlias].Groups); m.curCol > max {
		m.curCol = max
	}
}

// New builds the initial model for one session.
func New(opts Options) (*model, error) {
	if len(opts.Cfg.Aliases) == 0 {
		return nil, fmt.Errorf("no aliases defined in %s", opts.Cfg.Path)
	}
	m := &model{
		cfg:      opts.Cfg,
		curCol:   0,
		themes:   theme.Builtins(),
		dryRun:   opts.DryRun,
		histPath: opts.HistPath,
		short:    opts.Short,
	}
	for _, a := range opts.Cfg.Aliases {
		// The icon is literal text (an emoji or a nerd-font glyph): no file
		// lookup, no cache. Whitespace runs are collapsed so it always
		// stays on the row.
		st := &aliasState{Name: a.Name, Icon: cleanIcon(a.Icon), IconColor: a.IconColor}
		if w := lipgloss.Width(st.Icon); w > m.aliasIconW {
			m.aliasIconW = w
		}
		for _, g := range a.Groups {
			gw := 0
			for _, p := range a.GroupPairs[g] {
				if w := lipgloss.Width(cleanIcon(p.Icon)); w > gw {
					gw = w
				}
			}
			st.Groups = append(st.Groups, &groupState{Name: g, Pairs: a.GroupPairs[g], IconW: gw})
		}
		m.aliases = append(m.aliases, st)
	}
	for name := range opts.Cfg.CustomThemes {
		if !inList(m.themes, name) {
			m.themes = append(m.themes, name)
		}
	}
	sort.Strings(m.themes[len(theme.Builtins()):])
	if opts.StartAlias != "" {
		found := false
		for i, a := range m.aliases {
			if a.Name == opts.StartAlias {
				m.curAlias = i
				m.curCol = 1
				// An explicit start alias is a user selection: the needle
				// marks it (green) and Enter will run it, whatever the
				// history says.
				m.selAlias = i
				m.selAliasSrc = srcUser
				m.aliasSelInit = true
				found = true
				break
			}
		}
		if !found {
			names := make([]string, 0, len(m.aliases))
			for _, a := range m.aliases {
				names = append(names, a.Name)
			}
			return nil, fmt.Errorf("unknown alias %q (available: %s)", opts.StartAlias, strings.Join(names, ", "))
		}
	}
	if opts.Short && m.curCol == 0 {
		// Short mode starts in the alias search phase; starting on an
		// named alias (curCol 1) goes straight to its first option column.
		m.search = true
	}
	themeName := opts.Cfg.Theme
	if opts.ThemeOverride != "" {
		themeName = opts.ThemeOverride
	}
	if themeName == "" {
		themeName = "default"
	}
	th, err := theme.Get(themeName, opts.Cfg.CustomThemes)
	if err != nil {
		return nil, err
	}
	m.th = th
	for i, n := range m.themes {
		if n == th.Name {
			m.themeIdx = i
		}
	}
	m.sty = theme.BuildStyles(th)
	m.needle = opts.Cfg.NeedleGlyph()
	m.needleW = lipgloss.Width(m.needle)
	return m, nil
}

// Init implements tea.Model.
func (m *model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		m.handleKey(msg)
	case tea.MouseMsg:
		m.handleMouse(msg)
	}
	if m.doRun || m.doQuit {
		return m, tea.Quit
	}
	return m, nil
}

// handleMouse maps a left-button click to the column and row under the
// pointer. Clicking a column focuses it; clicking a row in the alias column
// moves the alias cursor, and clicking an option row selects that option
// (same as pressing space). Clicks on a column's border, title bar or padding
// only focus the column. A click inside the GLOBALS panel focuses it and
// hovers the clicked row/cell (the left half is the key column, the right
// half the value column) — including while it already has the focus, so the
// mouse can steer the hover directly. It is a no-op while a prompt is open or
// during a search, where the keyboard keeps the meaning of every key.
func (m *model) handleMouse(msg tea.MouseMsg) {
	if m.short {
		return
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return
	}
	if m.prompt != nil || m.search {
		return
	}
	if m.globalsRect.contains(msg.X, msg.Y) {
		m.mouseGlobalsCell(msg)
		return
	}
	for i, cl := range m.layout {
		if msg.X < cl.x || msg.X >= cl.x+cl.w {
			continue
		}
		// Inside this column's box. A content row starts one row below the
		// top border. The layout only holds the rendered (visible) columns.
		row := msg.Y - (cl.y + 1)
		col := m.viewStart + i // 0 = alias column, 1.. = option columns
		m.blurVariables() // any click on a column takes the focus off the GLOBALS panel
		if row < 0 || row >= cl.contentLines || cl.top+row >= len(cl.items) {
			// Clicked the border, title bar or padding: only focus the column.
			m.curCol = col
			m.status = ""
			return
		}
		item := cl.items[cl.top+row]
		m.curCol = col
		if col == 0 {
			m.curAlias = item
			m.selectAlias(item)
			m.ensureInited(item)
		} else {
			g := m.aliases[m.curAlias].Groups[col-1]
			g.Cursor = item
			g.Selected = item
			g.Src = srcUser
			// Like space: selecting an option selects the alias too.
			m.selectCurrentAlias()
			m.status = selectionStatus(g, item)
		}
		return
	}
}

// mouseGlobalsCell handles a left click inside the GLOBALS panel: it gives
// the panel the keyboard focus and hovers the clicked row/cell — the left
// half of a row is its key cell, the right half its value cell (the same
// split the panel renders with). Clicking a border or the bottom status line
// only focuses the panel. The hover lands on the appended empty row when the
// click falls past the last variable, so "n" can be reached by mouse too.
func (m *model) mouseGlobalsCell(msg tea.MouseMsg) {
	r := m.globalsRect
	row := msg.Y - (r.y + 1) // content rows start below the top border
	m.blurVariables() // a click is always a fresh focus: no stale hover survives
	if m.varsFocus == nil {
		m.varsFocus = &variablesState{}
	}
	v := m.varsFocus
	keys := m.varsKeys()
	nrows := len(keys)
	if row < 0 || row >= globalsPanelMaxRows+1 || msg.X < r.x+2 || msg.X >= r.x+r.w-2 {
		// Border, title or bottom line: only focus the panel.
		v.Editing = false
		m.varsStatus = ""
		return
	}
	innerW := r.w - 4
	keyW := innerW / 2
	if row >= nrows {
		// Past the last variable (or no variables): hover the appended empty
		// row's key cell.
		v.Row = nrows
		v.Col = varKey
		v.Editing = false
		return
	}
	v.Row = row
	if msg.X-r.x-2 < keyW {
		v.Col = varKey
	} else {
		v.Col = varValue
	}
	v.Editing = false
	m.varsStatus = ""
}

func (m *model) handleKey(msg tea.KeyMsg) {
	if m.prompt != nil {
		m.handlePromptKey(msg)
		return
	}
	if m.varsFocus != nil {
		m.handleVarsKey(msg)
		return
	}
	// Easter egg (short mode only): while the panel is open, "?" or esc
	// closes it. The check happens before any mode-specific routing so a close
	// press is not also counted as the start of a new sequence or swallowed by
	// the search handler.
	if m.short && m.showEgg && (msg.String() == "?" || msg.String() == "esc") {
		m.closeEgg()
		return
	}
	if m.short {
		m.trackKonami(msg.String())
	}
	if msg.String() == "/" {
		m.switchMode()
		return
	}
	if m.short {
		m.handleShortKey(msg)
		return
	}
	if m.search {
		if m.consumeSearchKey(msg) {
			return
		}
		// Search was exited by the key; fall through to normal handling.
	}
	switch msg.String() {
	case "ctrl+c", "q":
		m.requestQuit()
	case "esc":
		// Esc quits from any column (alias or option), as in the compact
		// mode's "󱊷  exit"; left/shift+tab still steps back a column.
		m.requestQuit()
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "left", "shift+tab":
		if m.curCol > 0 {
			m.curCol--
			m.status = ""
		}
	case "right", "tab":
		if m.curCol < len(m.aliases[m.curAlias].Groups) {
			m.curCol++
			m.status = ""
		}
	case "space", " ": // bubbletea renders the space key as " "
		if m.curCol == 0 {
			m.selectAlias(m.curAlias)
			break
		}
		g := m.aliases[m.curAlias].Groups[m.curCol-1]
		g.Selected = g.Cursor
		g.Src = srcUser
		// Selecting an option selects the alias of this column too (the ▸
		// needle follows).
		m.selectCurrentAlias()
		m.status = selectionStatus(g, g.Selected)
	case "enter", "return":
		m.prepareRun()
	case "t":
		m.themeIdx = (m.themeIdx + 1) % len(m.themes)
		if th, err := theme.Get(m.themes[m.themeIdx], m.cfg.CustomThemes); err == nil {
			m.th = th
			m.sty = theme.BuildStyles(th)
			m.status = "theme: " + th.Name
		}
	case "n":
		m.startNewValue()
	case "d":
		m.startDelete()
	case "c":
		m.copyCommand()
	case "b":
		m.toggleBackground()
	case "f":
		m.startSearch()
	case "v":
		m.focusVariables()
	case "ctrl+s":
		m.saveNow()
	case "ctrl+@": // ctrl+space is reported as null char (String() == "ctrl+@")
		if g := m.currentGroup(); g != nil {
			key := g.Pairs[g.Cursor].Key
			m.currentAlias().Defaults[g.Name] = key
			m.dirty = true
			m.status = fmt.Sprintf("%s: default set to %q", g.Name, key)
		}
	case "?":
		m.showInfo = !m.showInfo
		m.status = ""
	}
}

// trackKonami feeds one key into the easter-egg sequence matcher. When the
// full konamiSequence is entered (as a contiguous run of keys, whatever
// happens in between other keys), it opens the egg panel and the sequence is
// reset so it can be triggered again.
func (m *model) trackKonami(key string) {
	seq := konamiSequence
	if len(seq) == 0 {
		return
	}
	if key == seq[len(m.konami)] {
		m.konami = append(m.konami, key)
		if len(m.konami) == len(seq) {
			m.openEgg()
		}
	} else {
		// A mismatch: start over — but keep the run if the key itself
		// begins the sequence.
		m.konami = m.konami[:0]
		if key == seq[0] {
			m.konami = append(m.konami, key)
		}
	}
}

// openEgg shows the easter egg panel and resets the sequence so it can be
// triggered again.
func (m *model) openEgg() {
	m.showEgg = true
	m.konami = nil
}

// closeEgg hides the easter egg panel.
func (m *model) closeEgg() {
	m.showEgg = false
}

// switchMode toggles between the full-screen and compact modes: "/" in
// either mode. The picker state (alias, cursors, selections) is preserved;
// the Run loop restarts the tea program configured for the new mode. The
// typed alias filter survives the switch in both directions, always inert
// in the full mode — its alias column starts plain, like a values column,
// and only the filter shortcut enters the filter mode there — and active
// again when the alias filter is shown in the compact mode. An active
// filter of an option column is not an alias filter and is dropped.
func (m *model) switchMode() {
	m.background = false
	m.searchOff = 0
	if m.short {
		// Entering full mode: the alias column starts plain, never in the
		// filter mode; a typed alias filter carries over inert (search off,
		// buffer intact) and re-activates when the alias filter is shown
		// again in the compact mode.
		m.search = false
	} else {
		if m.curCol == 0 {
			m.search = true
		} else if m.search {
			// A filter of an option column: not an alias filter, drop it.
			m.exitSearch()
		}
		// else: no active search; a kept alias filter (searchBuf) carries
		// over to the compact mode's option column untouched.
	}
	m.short = !m.short
	m.modeSwitch = true
	m.doQuit = true
}

// handleShortKey routes one key in the compact mode.
func (m *model) handleShortKey(msg tea.KeyMsg) {
	if msg.Type == tea.KeyCtrlC { // by type: with Alt held, String() is "alt+ctrl+c"
		m.requestQuit()
		return
	}
	if m.search && m.curCol == 0 {
		// Phase 1: the alias search phase — the letters (including q)
		// belong to the filter, so only ctrl+c quits here.
		m.handleShortSearchKey(msg)
		return
	}
	switch msg.String() {
	case "q":
		m.requestQuit()
		return
	}
	m.handleShortGroupKey(msg)
}

// handleShortSearchKey handles one key in the alias search phase.
func (m *model) handleShortSearchKey(msg tea.KeyMsg) {
	switch {
	case msg.Type == tea.KeyRunes:
		m.searchBuf = append(m.searchBuf, msg.Runes...)
		m.searchSnapCursor()
	case msg.Type == tea.KeySpace, msg.String() == "right", msg.String() == "tab":
		if m.searchActive() && len(m.visibleAliasIdxs()) == 0 {
			// Nothing matches the filter: there is no alias to step into.
			m.status = "no matches to open"
			return
		}
		// Space / right / tab: step into the selected alias's first option
		// column (the "»" sign in the header points there).
		m.shortEnterGroup()
	case msg.Type == tea.KeyEnter:
		// Launch now with the preselected options (defaults, history, …).
		m.prepareRun()
	case msg.Type == tea.KeyBackspace, msg.Type == tea.KeyDelete:
		if len(m.searchBuf) > 0 {
			m.searchBuf = m.searchBuf[:len(m.searchBuf)-1]
			m.searchSnapCursor()
		}
	case msg.Type == tea.KeyUp:
		m.moveCursor(-1)
	case msg.Type == tea.KeyDown:
		m.moveCursor(1)
	case msg.Type == tea.KeyEsc:
		if len(m.searchBuf) > 0 {
			m.searchBuf = nil
			m.searchOff = 0
		} else {
			m.requestQuit()
		}
	}
}

// shortEnterGroup leaves the alias search and opens the alias under the
// cursor's first option column, making it the selected alias (the ▸ needle
// follows, as space does in the full mode); an alias without options just
// runs. The filter the user typed before entering is kept (it comes back
// with "back") but stays inactive while the column is open.
func (m *model) shortEnterGroup() {
	m.selectCurrentAlias()
	st := m.aliases[m.curAlias]
	if len(st.Groups) == 0 {
		m.prepareRun()
		return
	}
	m.search = false
	m.searchOff = 0
	m.curCol = 1
	m.status = ""
}

// shortBackToSearch returns to the alias search phase, keeping the filter
// the user typed before entering the column (it may be empty); the cursor —
// and the highlighted alias under it — is where it was.
func (m *model) shortBackToSearch() {
	m.search = true
	m.searchOff = 0
	m.curCol = 0
	m.status = ""
}

// handleShortGroupKey handles one key in an option column view.
func (m *model) handleShortGroupKey(msg tea.KeyMsg) {
	if msg.String() == "enter" || msg.String() == "return" {
		m.prepareRun()
		return
	}
	if m.curCol < 1 || m.curCol > len(m.aliases[m.curAlias].Groups) {
		// An alias without option groups has no column view.
		m.shortBackToSearch()
		return
	}
	switch msg.String() {
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "left", "shift+tab":
		if m.curCol > 1 {
			m.curCol--
			m.status = ""
		} else {
			m.shortBackToSearch()
		}
	case "right", "tab":
		if m.curCol < len(m.aliases[m.curAlias].Groups) {
			m.curCol++
			m.status = ""
		}
	case "space", " ":
		g := m.aliases[m.curAlias].Groups[m.curCol-1]
		g.Selected = g.Cursor
		g.Src = srcUser
		// Selecting an option selects the alias of this column too (the ▸
		// needle follows; heals a stale needle after a mode switch).
		m.selectCurrentAlias()
		m.status = selectionStatus(g, g.Selected)
	case "esc":
		m.shortBackToSearch()
	}
}

// selectionStatus is the transient status line after selecting option i of
// group g: "group: key → value" when the long_text is shown next to the
// key, "group: key" otherwise (including a no-pair option).
func selectionStatus(g *groupState, i int) string {
	p := g.Pairs[i]
	if v := p.Value(); v != "" {
		return fmt.Sprintf("%s: %s → %s", g.Name, p.Key, v)
	}
	return fmt.Sprintf("%s: %s", g.Name, p.Key)
}

// handlePromptKey routes a key while a prompt is open.
func (m *model) handlePromptKey(msg tea.KeyMsg) {
	p := m.prompt
	if p.kind == promptInput {
		switch {
		case msg.Type == tea.KeyEnter:
			m.submitNewValue()
		case msg.Type == tea.KeyEsc:
			m.cancelPrompt()
		case msg.Type == tea.KeyBackspace, msg.Type == tea.KeyDelete:
			if len(p.buf) > 0 {
				p.buf = p.buf[:len(p.buf)-1]
			}
		case msg.Type == tea.KeyRunes:
			p.buf = append(p.buf, msg.Runes...)
		case msg.Type == tea.KeySpace:
			p.buf = append(p.buf, ' ')
		}
		return
	}
	// Confirm prompt.
	switch msg.String() {
	case "y", "enter", "return":
		m.resolvePrompt(true)
	case "n", "q", "esc":
		m.resolvePrompt(false)
	}
}

// consumeSearchKey handles one key while the fuzzy search is active. It
// reports whether the key was fully consumed; a false return means the search
// was exited and the key must be processed by the normal handler.
func (m *model) consumeSearchKey(msg tea.KeyMsg) bool {
	switch {
	case msg.Type == tea.KeyRunes:
		m.searchBuf = append(m.searchBuf, msg.Runes...)
		m.searchSnapCursor()
		return true
	case msg.Type == tea.KeySpace:
		m.searchBuf = append(m.searchBuf, ' ')
		m.searchSnapCursor()
		return true
	case msg.Type == tea.KeyBackspace, msg.Type == tea.KeyDelete:
		if len(m.searchBuf) > 0 {
			m.searchBuf = m.searchBuf[:len(m.searchBuf)-1]
			m.searchSnapCursor()
		}
		return true
	case msg.Type == tea.KeyEsc:
		m.exitSearch()
		return true
	case msg.Type == tea.KeyEnter:
		m.exitSearch()
		m.prepareRun()
		return true
	case msg.Type == tea.KeyUp:
		m.moveCursor(-1)
		return true
	case msg.Type == tea.KeyDown:
		m.moveCursor(1)
		return true
	case msg.String() == "ctrl+c", msg.String() == "q":
		m.exitSearch()
		m.requestQuit()
		return true
	}
	// Any other key (tab, arrows via k/j, n, d, c, b, t, ?) exits the search
	// and keeps its normal meaning.
	m.exitSearch()
	return false
}

func (m *model) startSearch() {
	m.search = true
	m.searchBuf = nil
	m.searchOff = 0
	m.status = ""
}

func (m *model) exitSearch() {
	m.search = false
	m.searchBuf = nil
	m.searchOff = 0
	m.status = ""
}

// searchActive reports whether a filter is actually narrowing the list.
func (m *model) searchActive() bool {
	return m.search && len(m.searchBuf) > 0
}

// visibleAliasIdxs returns the indices of aliases matching the search buffer,
// or nil when no filter applies to the alias column (show everything). The
// filter only applies while the alias column is the active one, so exactly
// one column is ever narrowed at a time (mirrors visiblePairIdxs).
func (m *model) visibleAliasIdxs() []int {
	if !m.searchActive() || m.curCol != 0 {
		return nil
	}
	pat := string(m.searchBuf)
	out := []int{} // non-nil: a filter is active (nil means "show all")
	for i, a := range m.aliases {
		if fuzzyMatch(a.Name, pat) {
			out = append(out, i)
		}
	}
	return out
}

// visiblePairIdxs returns the indices of a group's options matching the
// search buffer, or nil when no filter applies to that column.
func (m *model) visiblePairIdxs(g *groupState) []int {
	if !m.searchActive() || m.curCol == 0 || g != m.currentGroup() {
		return nil
	}
	pat := string(m.searchBuf)
	out := []int{} // non-nil: a filter is active (nil means "show all")
	for i, p := range g.Pairs {
		if fuzzyMatch(p.Key, pat) {
			out = append(out, i)
		}
	}
	return out
}

// fuzzyMatch reports whether pattern occurs in s as a case-insensitive
// subsequence (fzf-style: "q38" matches "qwen-3.8-27B").
func fuzzyMatch(s, pattern string) bool {
	s = strings.ToLower(s)
	pat := strings.ToLower(pattern)
	i := 0
	for _, r := range s {
		if i >= len(pat) {
			break
		}
		pr, size := utf8.DecodeRuneInString(pat[i:])
		if r == pr {
			i += size
		}
	}
	return i >= len(pat)
}

// searchSnapCursor keeps the cursor on a visible row after the pattern
// changed; if none matches it leaves the cursor where it was.
func (m *model) searchSnapCursor() {
	m.status = ""
	m.searchOff = 0
	if m.curCol == 0 {
		vis := m.visibleAliasIdxs()
		if vis == nil {
			return
		}
		if posIn(vis, m.curAlias) < 0 && len(vis) > 0 {
			m.curAlias = vis[0]
			m.ensureInited(m.curAlias)
		}
		return
	}
	g := m.currentGroup()
	vis := m.visiblePairIdxs(g)
	if vis == nil {
		return
	}
	if posIn(vis, g.Cursor) < 0 && len(vis) > 0 {
		g.Cursor = vis[0]
	}
}

func posIn(list []int, v int) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}

// searchWindowTop computes the starting offset of the visible window so the
// cursor stays in view. It treats m.searchOff as a sticky offset: it grows as
// the cursor moves past the bottom and shrinks when the cursor moves above.
// It is only consulted for the column that is currently filtered, so the
// single shared offset never conflicts.
func (m *model) searchWindowTop(visible []int, cursorIdx int, contentLines int) int {
	pos := posIn(visible, cursorIdx)
	if pos < 0 {
		// Cursor is not visible (no matches); show from the top.
		m.searchOff = 0
		return 0
	}
	if pos < m.searchOff {
		m.searchOff = pos
	}
	if pos >= m.searchOff+contentLines {
		m.searchOff = pos - contentLines + 1
	}
	if m.searchOff < 0 {
		m.searchOff = 0
	}
	return m.searchOff
}

func (m *model) moveCursor(delta int) {
	if m.curCol == 0 {
		if vis := m.visibleAliasIdxs(); vis != nil {
			if len(vis) == 0 {
				m.status = ""
				return
			}
			pos := posIn(vis, m.curAlias)
			if pos < 0 {
				m.curAlias = vis[0]
				m.ensureInited(m.curAlias)
				m.status = ""
				return
			}
			npos := pos + delta
			if npos < 0 || npos >= len(vis) {
				m.status = ""
				return
			}
			m.curAlias = vis[npos]
			m.ensureInited(m.curAlias)
			m.status = ""
			return
		}
		next := m.curAlias + delta
		if next < 0 || next >= len(m.aliases) {
			return
		}
		m.curAlias = next
		m.ensureInited(next)
		m.status = ""
		return
	}
	g := m.aliases[m.curAlias].Groups[m.curCol-1]
	if vis := m.visiblePairIdxs(g); vis != nil {
		if len(vis) == 0 {
			m.status = ""
			return
		}
		pos := posIn(vis, g.Cursor)
		if pos < 0 {
			g.Cursor = vis[0]
			m.status = ""
			return
		}
		npos := pos + delta
		if npos < 0 || npos >= len(vis) {
			m.status = ""
			return
		}
		g.Cursor = vis[npos]
		m.status = ""
		return
	}
	next := g.Cursor + delta
	if next < 0 || next >= len(g.Pairs) {
		return
	}
	g.Cursor = next
	m.status = ""
}

func (m *model) prepareRun() {
	// Enter runs the alias under the cursor with the option under the cursor
	// in the active column. The needle (▸) is hidden in the active column;
	// when the user moves to another column, the previous selection reappears
	// as the needle if it was not changed.
	m.ensureInited(m.curAlias)
	alias := m.cfg.Aliases[m.curAlias]
	st := m.aliases[m.curAlias]
	if alias.Command == "" {
		m.status = "no command defined for this alias"
		return
	}
	// The active column's cursor becomes its selection.
	if m.curCol > 0 {
		g := st.Groups[m.curCol-1]
		g.Selected = g.Cursor
		g.Src = srcUser
	}
	vars := m.fullVars(m.curAlias, st)
	if m.dryRun {
		cmd := alias.Command
		if m.background {
			cmd += " &"
		}
		m.dryCmd = cmdx.ExportPrefix(vars) + cmd
		m.doQuit = true
		return
	}
	m.runCmd = alias.Command
	m.runVars = vars
	if m.background {
		// Background mode is armed: the Run loop will launch the command with
		// " &" (keeping it running) and exit xuz.
		m.logPath = m.detachedLogPath()
	}
	m.entry = history.Entry{Time: time.Now(), Alias: alias.Name}
	for _, g := range st.Groups {
		m.entry.Sels = append(m.entry.Sels, history.Selection{Group: g.Name, Key: g.Pairs[g.Selected].Key})
	}
	m.status = ""
	m.doRun = true
}

// startNewValue opens the "new value" input prompt (option columns only).
func (m *model) startNewValue() {
	if m.curCol == 0 {
		m.status = "nothing to add in the alias column — tab into an option column first"
		return
	}
	g := m.currentGroup()
	m.prompt = &prompt{
		kind:   promptInput,
		label:  fmt.Sprintf("new %s option (key: long text)", g.Name),
		action: actNewValue,
	}
	m.status = ""
}

// submitNewValue parses "key: long text" (or "key longtext" / bare "key") from the
// input prompt and inserts the pair at the cursor position.
func (m *model) submitNewValue() {
	p := m.prompt
	m.prompt = nil
	line := strings.TrimSpace(string(p.buf))
	if line == "" {
		m.status = "nothing to add — prompt cancelled"
		return
	}
	g := m.currentGroup()
	if g == nil {
		m.status = "nothing to add in the alias column"
		return
	}
	key, value := line, ""
	if i := strings.Index(line, ":"); i >= 0 {
		key = strings.TrimSpace(line[:i])
		value = strings.TrimSpace(line[i+1:])
	} else if i := strings.IndexAny(line, " \t"); i >= 0 {
		key = strings.TrimSpace(line[:i])
		value = strings.TrimSpace(line[i+1:])
	}
	if key == "" {
		m.status = "need at least a key"
		return
	}
	if indexOfKey(g.Pairs, key) >= 0 {
		m.prompt = p // keep the prompt open so the line can be edited
		m.status = fmt.Sprintf("%s: option %q already exists", g.Name, key)
		return
	}
	ins := g.Cursor
	g.Pairs = append(g.Pairs, config.Pair{})
	copy(g.Pairs[ins+1:], g.Pairs[ins:])
	g.Pairs[ins] = config.Pair{Key: key, LongText: value}
	m.currentAlias().GroupPairs[g.Name] = g.Pairs
	g.Selected = ins
	g.Src = srcUser
	m.dirty = true
	m.status = fmt.Sprintf("%s: added %q", g.Name, key)
}

// startDelete opens the confirm prompt for the option under the cursor.
func (m *model) startDelete() {
	if m.curCol == 0 {
		m.status = "nothing to delete in the alias column — tab into an option column first"
		return
	}
	g := m.currentGroup()
	if len(g.Pairs) <= 1 {
		m.status = fmt.Sprintf("cannot delete the last option in %s", g.Name)
		return
	}
	m.prompt = &prompt{
		kind:   promptConfirm,
		label:  fmt.Sprintf("delete %q from %s? (y/n)", g.Pairs[g.Cursor].Key, g.Name),
		action: actDeletePair,
	}
	m.status = ""
}

func (m *model) cancelPrompt() {
	m.prompt = nil
	m.status = ""
}

func (m *model) resolvePrompt(yes bool) {
	p := m.prompt
	m.prompt = nil
	if p == nil {
		return
	}
	if !yes {
		if p.action == actSaveOnQuit {
			m.doQuit = true // decline the save, leave the app
		}
		m.status = ""
		return
	}
	switch p.action {
	case actDeletePair:
		if m.varsFocus != nil {
			m.varsDeletePrompt()
		} else {
			m.doDeletePair()
		}
	case actSaveOnQuit:
		m.saveThenQuit()
	}
}

// globalsPanelMaxRows is the maximum number of variable rows the GLOBALS
// panel shows: beyond that the panel scrolls (the hovered row is kept in
// view), so the footer — and with it the COMMAND panel below — stays a
// bounded size no matter how many variables are defined.
const globalsPanelMaxRows = 6

// focusVariables moves the keyboard focus to the GLOBALS panel ("v"): the
// hover starts on the first row's key cell (or the appended empty row when
// no variables are defined yet). The panel's contents are the config's
// global variables, edited in place; every change is flushed to the config
// file immediately (varsSaveNow).
func (m *model) focusVariables() {
	if m.varsFocus == nil {
		m.varsFocus = &variablesState{}
	}
	m.varsStatus = ""
}

// blurVariables returns the keyboard focus from the GLOBALS panel to the
// columns: all picker state was untouched while the panel had focus.
func (m *model) blurVariables() {
	if m.varsFocus == nil {
		return
	}
	m.varsFocus = nil
	m.varsStatus = ""
}

// varsKeys returns the global variable names in the GLOBALS panel's display
// order: existing variables keep the order they were added in (config-file
// order when first loaded), and new variables are appended at the end — they
// are always added at the bottom of the list. The order is cached in
// m.varsDisplay and rebuilt lazily when it goes stale (a name missing from
// it, or a length mismatch). Names that are not in the cache yet (new
// variables) are appended sorted, so a fresh rebuild stays deterministic.
func (m *model) varsKeys() []string {
	stale := len(m.varsDisplay) != len(m.cfg.GlobalVariables)
	if !stale {
		for _, k := range m.varsDisplay {
			if _, ok := m.cfg.GlobalVariables[k]; !ok {
				stale = true
				break
			}
		}
	}
	if stale {
		kept := make([]string, 0, len(m.cfg.GlobalVariables))
		seen := map[string]bool{}
		for _, k := range m.varsDisplay {
			if _, ok := m.cfg.GlobalVariables[k]; ok && !seen[k] {
				kept = append(kept, k)
				seen[k] = true
			}
		}
		var fresh []string
		for k := range m.cfg.GlobalVariables {
			if !seen[k] {
				fresh = append(fresh, k)
			}
		}
		sort.Strings(fresh)
		m.varsDisplay = append(kept, fresh...)
	}
	return m.varsDisplay
}

// varsRowKey returns the variable name of row i ("" past the end).
func (m *model) varsRowKey(i int) string {
	keys := m.varsKeys()
	if i < 0 || i >= len(keys) {
		return ""
	}
	return keys[i]
}

// varsHoveredCellText returns the text of the cell currently hovered (the row's
// key or value, or the edit buffer while an in-place edit is active).
func (m *model) varsHoveredCellText() string {
	v := m.varsFocus
	if v.Editing {
		return string(v.Buf)
	}
	key := m.varsRowKey(v.Row)
	if key == "" {
		return ""
	}
	if v.Col == varKey {
		return key
	}
	return m.cfg.GlobalVariables[key]
}

// varsSaveNow writes the config's global variables to the config file
// immediately (so they persist even if xuz never hits its normal save flow).
// The write failure path keeps the existing semantics: the config stays dirty
// and the user is offered a retry prompt on quit, as after any failed save.
func (m *model) varsSaveNow() {
	cfg := m.cfg
	if cfg.Path == "" {
		m.varsStatus = "no config path to save to"
		return
	}
	if err := cfg.Save(cfg.Path); err != nil {
		m.dirty = true
		m.prompt = &prompt{
			kind:   promptConfirm,
			label:  "save failed: " + err.Error() + " — retry? (y/n)",
			action: actSaveOnQuit,
		}
		return
	}
	m.dirty = false
	m.varsStatus = fmt.Sprintf("variables saved to %s", cfg.Path)
}

// handleVarsKey routes one key while the GLOBALS panel has focus. The hover
// is on a cell (a row's key or value): up/down move within the hovered
// column, left/right switch columns, space/enter start the in-place edit of
// the hovered cell, n appends a new variable and starts editing its key, d
// deletes the hovered row, esc returns to the columns.
func (m *model) handleVarsKey(msg tea.KeyMsg) {
	v := m.varsFocus
	if v.Editing {
		m.handleVarsEditKey(msg)
		return
	}
	keys := m.varsKeys()
	nrows := len(keys)
	switch msg.String() {
	case "esc":
		m.blurVariables()
	case "up", "k":
		if v.Row > 0 {
			v.Row--
		}
		m.varsStatus = "" // the transient save message yields to the new hover
	case "down", "j":
		// The appended empty row (row nrows) sits just past the last
		// variable; down reaches it, and stays on it.
		if v.Row < nrows {
			v.Row++
		}
		m.varsStatus = "" // the transient save message yields to the new hover
	case "left", "shift+tab":
		if v.Col == varValue {
			v.Col = varKey
		}
	case "right", "tab":
		if v.Col == varKey && m.varsRowKey(v.Row) != "" {
			v.Col = varValue
		}
	case "space", " ", "enter", "return":
		key := m.varsRowKey(v.Row)
		if key == "" {
			// The appended empty row: create a new variable in place.
			m.varsStartNew()
			return
		}
		v.Editing = true
		if v.Col == varKey {
			v.Buf = []rune(key)
		} else {
			v.Buf = []rune(m.cfg.GlobalVariables[key])
		}
	case "d":
		key := m.varsRowKey(v.Row)
		if key == "" {
			m.varsStatus = "nothing to delete here"
			return
		}
		m.prompt = &prompt{
			kind:   promptConfirm,
			label:  fmt.Sprintf("delete %q? (y/n)", key),
			action: actDeletePair,
		}
	}
}

// handleVarsEditKey routes one key while an in-place edit is active on the
// hovered cell. Enter commits the buffer to that cell and ends the edit; a
// committed key cell renames the row (carrying its value over) and re-hovers
// it by name; esc cancels the edit without losing the original text.
func (m *model) handleVarsEditKey(msg tea.KeyMsg) {
	v := m.varsFocus
	switch {
	case msg.Type == tea.KeyEsc:
		// Abort the in-place edit: a brand-new variable (created on the
		// appended row) is discarded, an existing cell keeps its original
		// text. The hover stays on the list, back in normal mode.
		v.Editing = false
		if v.Row >= len(m.varsKeys()) {
			v.Row = max(0, len(m.varsKeys())-1)
		}
		m.varsStatus = ""
		return
	case msg.Type == tea.KeyEnter:
		m.varsCommitCell()
	case msg.Type == tea.KeyBackspace, msg.Type == tea.KeyDelete:
		if len(v.Buf) > 0 {
			out := make([]rune, len(v.Buf)-1)
			copy(out, v.Buf[:len(v.Buf)-1])
			v.Buf = out
		}
	case msg.Type == tea.KeyRunes:
		out := make([]rune, 0, len(v.Buf)+len(msg.Runes))
		out = append(out, v.Buf...)
		out = append(out, msg.Runes...)
		v.Buf = out
		m.varsStatus = ""
	case msg.Type == tea.KeySpace:
		out := make([]rune, 0, len(v.Buf)+1)
		out = append(out, v.Buf...)
		out = append(out, ' ')
		v.Buf = out
		m.varsStatus = ""
	}
}

// varsCommitCell commits the edit buffer to the hovered cell and ends the
// in-place edit: a key cell renames the row (an empty or invalid name is
// rejected with a status line and the edit stays open), a value cell writes
// the variable's value. The hover re-lands on the committed cell, so the next
// up/down move continues from there.
func (m *model) varsCommitCell() {
	v := m.varsFocus
	text := strings.TrimSpace(string(v.Buf))
	if v.Col == varKey {
		if text == "" {
			m.varsStatus = "need at least a key"
			return
		}
		if !cmdx.IsValidVarName(text) {
			m.varsStatus = fmt.Sprintf("%q is not a valid variable name (start with a letter or underscore, then letters, digits or underscores)", text)
			return
		}
		oldKey := m.varsRowKey(v.Row)
		val := ""
		if oldKey != "" {
			val = m.cfg.GlobalVariables[oldKey]
		}
		if oldKey != "" && oldKey != text {
			delete(m.cfg.GlobalVariables, oldKey)
		}
		m.cfg.GlobalVariables[text] = val
		// The hover stays on the created row: new variables are always added
		// at the bottom of the list, so the cursor rests there too (the panel
		// scrolls to keep it in view), whatever the name sorts to. Committing
		// a key moves the focus onto that row's value cell and starts editing
		// it — creating a new variable is key-then-value in one flow, so the
		// user can type the value right after the key.
		v.Col = varValue
		v.Editing = true
		v.Buf = []rune(val) // an empty buffer for a fresh variable; the carried-over value on a rename
		m.varsSaveNow()
		return
	}
	key := m.varsRowKey(v.Row)
	if key == "" {
		m.varsStatus = "no variable to edit"
		v.Editing = false
		return
	}
	m.cfg.GlobalVariables[key] = text
	v.Editing = false
	m.varsSaveNow()
}

// varsStartNew moves the hover to a new row just past the last one (last line
// +1) and starts the in-place edit of its key cell.
func (m *model) varsStartNew() {
	v := m.varsFocus
	keys := m.varsKeys()
	v.Row = len(keys)
	v.Col = varKey
	v.Editing = true
	v.Buf = nil
	m.varsStatus = ""
}

// varsDeletePrompt resolves the confirm prompt for deleting a global variable
// (d): it removes the hovered row, keeps the hover on a valid cell, and
// flushes to the config file. The shared actDeletePair action routes here
// while the GLOBALS panel has focus, and to doDeletePair otherwise.
func (m *model) varsDeletePrompt() {
	key := m.varsRowKey(m.varsFocus.Row)
	if key == "" {
		return
	}
	delete(m.cfg.GlobalVariables, key)
	keys := m.varsKeys()
	v := m.varsFocus
	if v.Row >= len(keys) {
		v.Row = max(0, len(keys)-1)
	}
	m.varsSaveNow()
}

func (m *model) doDeletePair() {
	g := m.currentGroup()
	if g == nil {
		return
	}
	key := g.Pairs[g.Cursor].Key
	g.Pairs = append(g.Pairs[:g.Cursor], g.Pairs[g.Cursor+1:]...)
	if g.Cursor >= len(g.Pairs) {
		g.Cursor = len(g.Pairs) - 1
	}
	if g.Selected >= len(g.Pairs) {
		g.Selected = g.Cursor
		g.Src = srcUser
	}
	if d, ok := m.currentAlias().Defaults[g.Name]; ok && d == key {
		delete(m.currentAlias().Defaults, g.Name)
	}
	m.currentAlias().GroupPairs[g.Name] = g.Pairs
	m.dirty = true
	m.status = fmt.Sprintf("%s: deleted %q", g.Name, key)
}

// saveNow writes the config back (ctrl+s).
func (m *model) saveNow() {
	if m.cfg.Path == "" {
		m.status = "no config path to save to"
		return
	}
	if err := m.cfg.Save(m.cfg.Path); err != nil {
		m.status = "save failed: " + err.Error()
		return
	}
	m.dirty = false
	m.status = "config saved to " + m.cfg.Path
}

// requestQuit quits, or first asks to save when the config changed.
func (m *model) requestQuit() {
	if m.dirty && m.cfg.Path != "" {
		m.prompt = &prompt{
			kind:   promptConfirm,
			label:  "config changed — save to " + m.cfg.Path + "? (y/n)",
			action: actSaveOnQuit,
		}
		m.status = ""
		return
	}
	m.doQuit = true
}

// saveThenQuit is the save-on-quit answer: on failure the user stays in the
// TUI with a retry prompt instead of losing the changes silently.
func (m *model) saveThenQuit() {
	if m.cfg.Path == "" {
		m.doQuit = true
		return
	}
	if err := m.cfg.Save(m.cfg.Path); err != nil {
		m.prompt = &prompt{
			kind:   promptConfirm,
			label:  "save failed: " + err.Error() + " — retry? (y/n)",
			action: actSaveOnQuit,
		}
		m.status = ""
		return
	}
	m.dirty = false
	m.doQuit = true
}

// resolvedCommand returns the selected alias's command line with the env
// prefix that will be in effect when it runs, or "" when the alias has no
// command. The command text is verbatim (the shell resolves the $VAR refs);
// the prefix makes the result copy-paste-runnable.
func (m *model) resolvedCommand() string {
	m.ensureAliasSel()
	m.ensureInited(m.selAlias)
	alias := m.cfg.Aliases[m.selAlias]
	if alias.Command == "" {
		return ""
	}
	return cmdx.ExportPrefix(m.fullVars(m.selAlias, m.aliases[m.selAlias])) + alias.Command
}

// clipboardTools are tried in order before falling back to OSC 52.
var clipboardTools = [][2]string{
	{"wl-copy", ""},
	{"xclip", "-selection clipboard"},
	{"xsel", "--clipboard --input"},
}

// osc52Write emits the OSC 52 escape sequence; tests replace it to capture
// the payload instead of touching a real terminal.
var osc52Write func(string) = func(s string) { _, _ = os.Stdout.WriteString(s) }

// copyCommand copies the resolved command to the clipboard (c).
func (m *model) copyCommand() {
	cmd := m.resolvedCommand()
	if cmd == "" {
		m.status = "no command defined for this alias"
		return
	}
	for _, tl := range clipboardTools {
		if _, err := exec.LookPath(tl[0]); err == nil {
			// wl-copy, xclip and xsel read the text from stdin; without it
			// they hit EOF, store an empty string and still exit 0.
			c := exec.Command(tl[0], strings.Fields(tl[1])...)
			c.Stdin = strings.NewReader(cmd)
			if err := c.Run(); err == nil {
				m.status = "command copied (" + tl[0] + ")"
				return
			}
			// tool present but failed (e.g. no display): fall through
		}
	}
	osc52Write("\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(cmd)) + "\a")
	m.status = "command copied (OSC 52)"
}

// toggleBackground arms or disarms background mode (b). It runs nothing: it
// only changes what Enter will do. While armed, Enter launches the user
// command in the background (suffixed with " &" in a new session, output to a
// log file) and exits xuz, instead of running it in the foreground.
func (m *model) toggleBackground() {
	m.background = !m.background
	if m.background {
		m.status = "background: on — Enter runs the command with \" &\" and xuz exits"
	} else {
		m.status = "background: off"
	}
}

// detachedLogPath returns <alias>-<timestamp>.log next to the history file
// (e.g. ~/.xuz/llama-20260207-123456.log), skipping an existing name.
func (m *model) detachedLogPath() string {
	base := m.histPath
	if base == "" {
		base = history.Path()
	}
	dir := filepath.Dir(base)
	name := m.aliases[m.curAlias].Name + "-" + time.Now().Format("20060102-150405") + ".log"
	p := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s-%d", name, i))
	}
}

// globalVarNames returns the names of the config's global variables.
func (m *model) globalVarNames() []string {
	out := make([]string, 0, len(m.cfg.GlobalVariables))
	for k := range m.cfg.GlobalVariables {
		out = append(out, k)
	}
	return out
}

// referencedGlobals returns the config's global variables whose names occur
// (as a literal substring — see cmdx.ReferencedNames) in the given command
// text. Global variables are only exposed to a command that references them;
// they sit at the bottom of the precedence order, so a values column named
// identically to one takes precedence over it without any warning.
func (m *model) referencedGlobals(command string) map[string]string {
	out := map[string]string{}
	if len(m.cfg.GlobalVariables) == 0 {
		return out
	}
	ref := cmdx.ReferencedNames(command, m.globalVarNames())
	for k, v := range m.cfg.GlobalVariables {
		if ref[k] {
			out[k] = v
		}
	}
	return out
}

// currentVars returns the env vars for alias i's state: the global variables
// referenced by the alias's command or template (lowest precedence), one per
// option group set to the group's selected (needle) option's value (its long
// text; "" for a no-pair option), plus the alias's extra vars. A values column
// named identically to a global variable overrides it, silently.
func (m *model) currentVars(i int, st *aliasState) map[string]string {
	alias := m.cfg.Aliases[i]
	// Globals referenced by the command or the template are exposed.
	text := alias.Command
	if alias.Template != "" {
		text += "\n" + alias.Template
	}
	vars := m.referencedGlobals(text)
	for _, g := range st.Groups {
		vars[strings.ToUpper(g.Name)] = g.Pairs[g.Selected].Value()
	}
	for varName, groupName := range alias.Vars {
		for _, g := range st.Groups {
			if g.Name == groupName {
				vars[varName] = g.Pairs[g.Selected].Value()
			}
		}
	}
	return vars
}

// fullVars returns the env vars the command will run with: the by-name vars
// (currentVars) plus any vars derived by the alias's optional template. A
// template that fails to evaluate or parse is reported on the status line and
// contributes no vars (the by-name vars still apply).
func (m *model) fullVars(i int, st *aliasState) map[string]string {
	vars := m.currentVars(i, st)
	alias := m.cfg.Aliases[i]
	if alias.Template == "" {
		return vars
	}
	out, err := cmdx.EvalTemplate(alias.Template, vars)
	if err != nil {
		m.status = "template error: " + err.Error()
		return vars
	}
	derived, err := cmdx.ParseDerivedVars(out)
	if err != nil {
		m.status = "template error: " + err.Error()
		return vars
	}
	for k, v := range derived {
		vars[k] = v
	}
	return vars
}

func (m *model) ensureInited(idx int) {
	st := m.aliases[idx]
	if st.inited {
		return
	}
	alias := m.cfg.Aliases[idx]
	var hints []config.Hint
	if e := history.LatestFor(m.history, st.Name); e != nil {
		for _, s := range e.Sels {
			hints = append(hints, config.Hint{Group: s.Group, Key: s.Key, Source: config.SourceHistory})
		}
	}
	res := alias.ResolveSelectionsWith(m.cfg.PrecedenceList(), hints)
	for _, g := range st.Groups {
		i := indexOfKey(g.Pairs, res[g.Name].Key)
		if i < 0 {
			i = 0
		}
		g.Cursor = i
		g.Selected = i
		g.Src = res[g.Name].Source
	}
	st.inited = true
}

// ensureAliasSel resolves the selected alias (the ▸ needle row of the alias
// column) once, walking the configured selection_precedence with the same
// sources as the option groups: a "history" level picks the most recent
// launch in the history file (the needle is then rendered in the theme's
// muted color), a "first" level the first alias (green). The built-in order
// (default > history > first) resolves to history, else first — aliases
// carry no !default tag. An explicit start alias (xuz <alias>) already set
// it. The cursor starts on the selected alias, as group cursors start on
// their preselected option.
func (m *model) ensureAliasSel() {
	if m.aliasSelInit {
		return
	}
	m.aliasSelInit = true
	for _, source := range m.cfg.PrecedenceList() {
		switch source {
		case config.SourceHistory:
			for i := len(m.history) - 1; i >= 0; i-- {
				if idx := m.aliasIndexByName(m.history[i].Alias); idx >= 0 {
					m.selAlias = idx
					m.selAliasSrc = config.SourceHistory
					break
				}
			}
		case config.SourceFirst:
			if m.selAliasSrc == "" && len(m.aliases) > 0 {
				m.selAlias = 0
				m.selAliasSrc = config.SourceFirst
			}
		}
		if m.selAliasSrc != "" {
			break
		}
	}
	m.curAlias = m.selAlias
}

// aliasIndexByName returns the index of the alias with the given name, or -1.
func (m *model) aliasIndexByName(name string) int {
	for i, a := range m.aliases {
		if a.Name == name {
			return i
		}
	}
	return -1
}

// selectAlias makes alias i the selected alias (the ▸ needle row of the
// alias column), like space does in an option column: the needle moves to
// it and turns green (a user selection).
func (m *model) selectAlias(i int) {
	m.selAlias = i
	m.selAliasSrc = srcUser
	m.aliasSelInit = true
	m.status = "alias: " + m.aliases[i].Name
}

// selectCurrentAlias makes the alias under the cursor the selected alias
// (the ▸ needle, green) without touching the status line. Selecting an
// option in one of its columns selects the alias of that column too.
func (m *model) selectCurrentAlias() {
	m.selAlias = m.curAlias
	m.selAliasSrc = srcUser
	m.aliasSelInit = true
}

// View implements tea.Model.
func (m *model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	m.clampCol()
	m.ensureAliasSel()
	m.ensureInited(m.curAlias)
	m.ensureInited(m.selAlias)
	if m.short {
		m.globalsRect = rect{} // the compact mode has no globals panel
		return m.shortView()
	}
	var b strings.Builder
	b.WriteString(m.titleBar())
	b.WriteByte('\n')
	b.WriteString(m.columns())
	b.WriteByte('\n')
	// The footer is assembled in fixed order (status, GLOBALS, COMMAND, info,
	// help) so the GLOBALS panel's screen position is known — mouse clicks
	// use it to land on a row and cell. The columns' box height is
	// height-1-len(footer), clamped to at least 4 like in columns(), so the
	// footer starts right after it.
	allFooter := m.footerLines()
	statusLines := m.footerStatusLines()
	globalsLines := m.globalsPanelLines()
	commandLines := m.commandPanelLines()
	helpLines := m.helpLines()
	boxH := m.height - 1 - len(allFooter)
	if boxH < 4 {
		boxH = 4
	}
	m.globalsRect = rect{
		x: 0,
		y: 1 + boxH + len(statusLines),
		w: m.width,
		h: len(globalsLines),
	}
	var footer []string
	footer = append(footer, statusLines...)
	footer = append(footer, globalsLines...)
	footer = append(footer, commandLines...)
	if l := m.infoLine(); l != "" {
		footer = append(footer, l)
	}
	footer = append(footer, helpLines...)
	b.WriteString(strings.Join(footer, "\n"))
	return b.String()
}

// globalsPanelLines is the GLOBALS panel of the full mode: a bordered box
// (styled like the COMMAND panel) listing the config's global variables as
// key/value rows. It sits in the footer just above the COMMAND panel and is
// always present — when it has the keyboard focus ("v") it becomes the
// in-place editor for the variables: the hovered cell (a row's key or value)
// is highlighted, up/down move within the hovered column, left/right switch
// columns, space/enter edits the hovered cell in place (the buffer renders on
// the cell itself), n appends a new variable and starts editing its key, d
// deletes the hovered row, and esc returns to the columns. More than
// globalsPanelMaxRows variables scroll: the hovered row is kept in view.
// Every change is flushed to the config file immediately. When no variables
// are defined the panel holds a single muted hint row.
func (m *model) globalsPanelLines() []string {
	innerW := m.width - 4
	if innerW < 10 {
		innerW = 10
	}
	keyW := innerW / 2
	valW := innerW - keyW - 1
	focus := m.varsFocus != nil
	keys := m.varsKeys()
	nrows := len(keys)
	// The appended empty row (row nrows) is always part of the list while the
	// panel has focus: it sits just past the last variable, is where a new
	// variable is created in place, and shows the create hint when hovered.
	totalRows := nrows
	if focus {
		totalRows++ // the appended row
	}
	if m.varsFocus != nil && m.varsFocus.Row > totalRows-1 {
		m.varsFocus.Row = totalRows - 1
	}
	visible := globalsPanelMaxRows
	if totalRows < visible {
		visible = totalRows
	}
	// Keep the hovered row in view. While focused, the window always ends on
	// the appended row: it is rendered as the last slot of the panel, so when
	// the hover reaches it the list scrolls one more position (the first
	// visible variable scrolls out) and the key entry plus the hint stay
	// visible — nothing above is obscured.
	top := 0
	if focus && totalRows > 0 {
		r := m.varsFocus.Row
		if r >= nrows {
			r = max(0, nrows-1)
		}
		if r < top {
			top = r
		}
		if r >= top+visible {
			top = r - visible + 1
		}
		if m.varsFocus.Row >= nrows {
			top = totalRows - visible // the appended row is the last slot
		}
		if top < 0 {
			top = 0
		}
	}
	var rows []string
	for i := top; i < top+visible && i < totalRows; i++ {
		if i == nrows {
			break // the appended row is rendered after the loop
		}
		key := keys[i]
		val := m.cfg.GlobalVariables[key]
		hoverRow := focus && i == m.varsFocus.Row
		editKey := hoverRow && m.varsFocus.Editing && m.varsFocus.Col == varKey
		editVal := hoverRow && m.varsFocus.Editing && m.varsFocus.Col == varValue
		keyText, valText := key, val
		if editKey {
			keyText = string(m.varsFocus.Buf) + "█"
		}
		if editVal {
			valText = string(m.varsFocus.Buf) + "█"
		}
		var line string
		if hoverRow && !m.varsFocus.Editing {
			// Only the hovered cell carries the row background — never the
			// whole line: hovering the key column must not paint the value
			// column (and vice versa). The highlighted span always covers the
			// full cell width, so an empty value still shows its selection.
			if m.varsFocus.Col == varKey {
				line = m.sty.RowCursor.Render(padRight(truncateStr(keyText, keyW), keyW))
				line += " "
				line += m.sty.Value.Render(padRight(truncateStr(valText, valW), valW))
			} else {
				line = m.sty.Key.Render(truncateStr(keyText, keyW))
				line += strings.Repeat(" ", keyW-lipgloss.Width(truncateStr(keyText, keyW)))
				line += " "
				line += m.sty.RowCursor.Render(padRight(truncateStr(valText, valW), valW))
			}
		} else if editKey || editVal {
			// In-place edit: the buffer renders on the cell itself. Only the
			// edited cell's span carries the row background — the other cell
			// and its padding stay plain, like a plain hover.
			if editKey {
				line = m.sty.RowCursor.Render(padRight(truncateStr(keyText, keyW), keyW))
				line += " "
				line += m.sty.Value.Render(truncateStr(valText, valW))
			} else {
				line = m.sty.Key.Render(truncateStr(keyText, keyW))
				line += strings.Repeat(" ", keyW-lipgloss.Width(truncateStr(keyText, keyW)))
				line += " "
				line += m.sty.RowCursor.Render(padRight(truncateStr(valText, valW), valW))
			}
		} else {
			line = m.sty.Key.Render(truncateStr(keyText, keyW))
			line += strings.Repeat(" ", keyW-lipgloss.Width(truncateStr(keyText, keyW)))
			line += " "
			line += m.sty.Value.Render(truncateStr(valText, valW))
		}
		rows = append(rows, padRight(line, innerW))
	}
	if !focus && len(rows) == 0 {
		hint := "  (no global variables — press v to focus and create one)"
		rows = append(rows, m.sty.Muted.Render(padRight(hint, innerW)))
	}
	if focus && top+visible > nrows {
		// The appended empty row: blank when not hovered, the in-place key
		// buffer while a new variable is being created on it.
		hoverAppended := m.varsFocus.Row == nrows
		editAppended := hoverAppended && m.varsFocus.Editing && m.varsFocus.Col == varKey
		if editAppended {
			line := m.sty.RowCursor.Render(padRight(truncateStr(string(m.varsFocus.Buf)+"█", keyW), keyW))
			line += " "
			line += m.sty.RowCursor.Render(strings.Repeat(" ", valW))
			rows = append(rows, line)
		} else if hoverAppended {
			line := m.sty.RowCursor.Render(padRight("", keyW))
			line += " "
			line += m.sty.RowCursor.Render(strings.Repeat(" ", valW))
			rows = append(rows, line)
		} else {
			rows = append(rows, strings.Repeat(" ", innerW))
		}
	}
	bottom := padRight("", innerW) // box() needs every row exactly innerW wide
	if focus {
		if m.varsStatus != "" {
			st := truncateStr(m.varsStatus, innerW)
			bottom = m.sty.Status.Render(st + strings.Repeat(" ", innerW-lipgloss.Width(st)))
		} else if !m.varsFocus.Editing && m.varsFocus.Row >= nrows {
			// Hovering the appended empty row: the create hint, in the same
			// position as the save messages and in the legend's language —
			// highlighted shortcut glyphs, a plain separator, muted words.
			bottom = m.createHintLine(innerW)
		}
	}
	return strings.Split(m.box("globals", append(rows, bottom), innerW, focus), "\n")
}

// createHintLine is the "↵/ select create" hint shown while the appended
// empty row is hovered: the shortcut glyphs in the legend's highlighted key
// style, the "/" separator in plain text, and the words muted — the same
// language as the shortcut legends.
func (m *model) createHintLine(w int) string {
	bold := lipgloss.NewStyle().Bold(true)
	enterSpan := bold.Render(m.sty.HelpKey.Render(keyEnter))
	spaceSpan := bold.Render(m.sty.HelpKey.Render(keySpace))
	mid := m.sty.Help.Render(" / ")
	tail := m.sty.Help.Render(" new")
	line := enterSpan + mid + spaceSpan + tail
	return padRight(line, w)
}

// shortListRows is the number of value rows the compact mode shows.
const shortListRows = 4

// shortView renders the compact mode without a full screen: a header line
// (the alias search or the current option group's name, with «/» signs for
// the columns next to the current one), shortListRows value rows — and, in
// the alias search, the filter input on its own line at the bottom of the
// list, as in the full mode's columns — then the shortcut tooltip line.
func (m *model) shortView() string {
	var b strings.Builder
	b.WriteString(m.shortHeader())
	b.WriteByte('\n')
	phase1 := m.search && m.curCol == 0
	var rows []string
	if phase1 {
		rows = m.shortAliasRows()
	} else {
		rows = m.shortGroupRows()
	}
	for _, r := range rows {
		b.WriteString(r)
		b.WriteByte('\n')
	}
	if phase1 {
		b.WriteString(m.searchLine(m.width))
		b.WriteByte('\n')
	}
	if m.showEgg {
		b.WriteString(m.eggPanelLines())
		b.WriteByte('\n')
	}
	b.WriteString(m.shortFooter())
	m.lastFrameLines = strings.Count(b.String(), "\n") + 1 // lines rendered (trailing \n is not a line)
	return b.String()
}

// shortHeader is the compact mode's header line: the column name on the left
// (the alias search, or the current option group) and, right-aligned, the «/»
// signs pointing at the columns next to the current one — the alias search is
// one column to the left of any group, and the remaining groups sit to the
// right. The alias search's filter input is not part of the header; it
// renders on its own line at the bottom of the list (shortView).
func (m *model) shortHeader() string {
	phase1 := m.search && m.curCol == 0
	var label string
	var left, right bool
	if phase1 {
		label = m.sty.Title.Render("alias:")
		right = len(m.aliases[m.curAlias].Groups) > 0
	} else if m.curCol >= 1 && m.curCol <= len(m.aliases[m.curAlias].Groups) {
		label = m.sty.Title.Render(m.aliases[m.curAlias].Groups[m.curCol-1].Name + ":")
		left = true
		right = m.curCol < len(m.aliases[m.curAlias].Groups)
	} else {
		label = m.sty.Title.Render(m.aliases[m.curAlias].Name + ":")
	}
	signs := ""
	if left {
		signs += "«"
	}
	if right {
		signs += "»"
	}
	if signs == "" {
		return truncateStr(label, m.width)
	}
	signs = m.sty.Muted.Render(signs)
	lw, sw := lipgloss.Width(label), lipgloss.Width(signs)
	if lw+sw+1 >= m.width {
		return truncateStr(label, m.width-sw-1) + signs
	}
	return label + strings.Repeat(" ", m.width-lw-sw) + signs
}

// shortAliasRows is the window of up to shortListRows alias matches for the
// current filter (all aliases when the filter is empty).
func (m *model) shortAliasRows() []string {
	var idxs []int
	if vis := m.visibleAliasIdxs(); vis != nil {
		idxs = vis
	} else {
		idxs = make([]int, 0, len(m.aliases))
		for i := range m.aliases {
			idxs = append(idxs, i)
		}
	}
	top := m.searchWindowTop(idxs, m.curAlias, shortListRows)
	rows := []string{}
	for _, i := range idxs[top:] {
		if len(rows) >= shortListRows {
			break
		}
		if i == m.curAlias {
			rows = append(rows, m.sty.RowCursor.Render(m.aliasLine(i)))
		} else {
			rows = append(rows, m.sty.Row.Render(m.aliasLine(i)))
		}
	}
	if len(rows) == 0 {
		rows = append(rows, m.sty.Muted.Render("  (no matches)"))
	}
	for len(rows) < shortListRows {
		rows = append(rows, "")
	}
	return rows
}

// shortGroupRows is the window of up to shortListRows option values of the
// current group, keeping the cursor in view.
func (m *model) shortGroupRows() []string {
	st := m.aliases[m.curAlias]
	rows := []string{}
	if m.curCol >= 1 && m.curCol <= len(st.Groups) {
		g := st.Groups[m.curCol-1]
		top := g.Offset
		if g.Cursor < top {
			top = g.Cursor
		}
		if g.Cursor >= top+shortListRows {
			top = g.Cursor - shortListRows + 1
		}
		if top < 0 {
			top = 0
		}
		g.Offset = top
		for i := top; i < len(g.Pairs) && len(rows) < shortListRows; i++ {
			rows = append(rows, m.pairLine(g, i, m.width, true))
		}
		if len(rows) == 0 {
			rows = append(rows, m.sty.Muted.Render("  (no options)"))
		}
	}
	for len(rows) < shortListRows {
		rows = append(rows, "")
	}
	return rows
}

// shortFooter is the tooltip line of the compact mode: an open prompt, a
// transient status, or the keyboard shortcut hints.
func (m *model) shortFooter() string {
	if m.prompt != nil {
		if m.prompt.kind == promptInput {
			return m.sty.Help.Render(truncateStr(m.prompt.label+" "+string(m.prompt.buf)+"█", m.width))
		}
		return m.sty.Help.Render(truncateStr(m.prompt.label, m.width))
	}
	if m.status != "" {
		return m.sty.Status.Render(truncateStr(m.status, m.width))
	}
	return m.shortMenu()
}

// Key glyphs for the shortcut legends: nerd-font keycap icons where the font
// has them, plain unicode carets otherwise.
const (
	keyEsc   = "\U000F12B7" // nerd-font esc icon
	keySpace = "\U000F1050" // nerd-font space icon
	keyEnter = "↵"          // return
	keyMove  = "↑↓←→"       // move: all four directions (shared by both modes)
	keyCtrl  = "⌃"          // ctrl caret
	keyDel   = "\uEE23"     // nerd-font delete key icon
	keyClick = "\U0000F245" // nerd-font mouse pointer icon
)

// helpItem is one entry of a shortcut legend: highlighted key tokens (joined
// by "/") followed by a muted label. The label carries its own leading space
// when it is a separate explanation (" move"); without one it completes the
// word of a hotkey letter ("opy" for c-opy, "es" for y-es). An item with no
// keys renders as a plain muted note.
type helpItem struct {
	keys  string // highlighted key tokens ("" renders a plain muted note)
	label string
}

// renderHelpItem renders one legend item: the key tokens in bold accent and
// the label muted. The key span gets an explicit bold wrapper on top of
// HelpKey's own styles so the stream carries a standalone \x1b[1m before the
// color sequence (a single style emits both attributes combined in one).
func (m *model) renderHelpItem(it helpItem) string {
	if len(it.keys) == 0 {
		return m.sty.Help.Render(it.label)
	}
	bold := lipgloss.NewStyle().Bold(true)
	keySpan := bold.Render(m.sty.HelpKey.Render(it.keys))
	return lipgloss.JoinHorizontal(lipgloss.Left, keySpan, m.sty.Help.Render(it.label))
}

// helpExit is the exit entry of the shortcut legends, shared by both modes
// so the same glyph and legend appear everywhere. It carries two leading
// spaces in its label so the line reads "󱊷  exit", as in the spec.
var helpExit = helpItem{keyEsc, "  exit"}

// shortMenuItems are the compact mode's shortcut tooltip entries.
var shortMenuItems = []helpItem{
	helpExit,
	{keySpace, " select"},
	{keyMove, " move"},
	{keyEnter, " launch"},
	{"/", " switch"},
}

// shortMenu is the keyboard shortcut hints of the compact mode's tooltip
// line: the shortMenuItems joined by " · ".
func (m *model) shortMenu() string {
	var parts []string
	for i, it := range shortMenuItems {
		if i > 0 {
			parts = append(parts, m.sty.Help.Render(" · "))
		}
		parts = append(parts, m.renderHelpItem(it))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func (m *model) titleBar() string {
	st := m.aliases[m.curAlias]
	left := m.sty.Title.Render(" xuz · " + st.Name)
	right := m.sty.Muted.Render(" " + m.th.Name)
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if lw+rw >= m.width {
		return truncateStr(left+" "+right, m.width)
	}
	return left + strings.Repeat(" ", m.width-lw-rw) + right
}

// Column width policy: fully visible columns take a content-based desired
// width within [minColW, maxColW] and share the leftover space fairly. When
// the columns do not all fit, a viewport of full columns is shown plus a
// narrow "peek" sliver of the next column, so the user can tell that more
// columns are reachable with right/tab (the active column is always kept
// fully visible, which is what scrolls the viewport).
const (
	minColW  = 10
	maxColW  = 48
	peekColW = 8
)

// needleSlot is the cells the needle prefix occupies on a row: the glyph plus
// a trailing space, at least two (the cursor/blank slot when the needle is
// hidden in the active column).
func (m *model) needleSlot() int {
	if s := m.needleW + 1; s > 2 {
		return s
	}
	return 2
}

// needleBlank is the needle slot as plain spaces, needleSlot() cells wide —
// the slot when the needle is hidden (the cursor row styles it, the others
// leave it blank). Keeping it the same width as the needle row keeps every
// name/key in a column starting at the same cell.
func (m *model) needleBlank() string {
	return strings.Repeat(" ", m.needleSlot())
}

// columnDesired returns the preferred width of column i (0 = alias column)
// based on its content, clamped to [minColW, maxColW].
func (m *model) columnDesired(i int) int {
	st := m.aliases[m.curAlias]
	var w int
	if i == 0 {
		for _, a := range m.aliases {
			// The alias column's icon slot, the needle prefix, a little
			// margin + the frame.
			if lw := lipgloss.Width(a.Name) + m.aliasIconW + m.needleSlot() + 6; lw > w {
				w = lw
			}
		}
	} else {
		g := st.Groups[i-1]
		maxKey, maxVal := 0, 0
		for _, p := range g.Pairs {
			if lw := lipgloss.Width(p.Key); lw > maxKey {
				maxKey = lw
			}
			if p.LongText != "" && p.LongText != p.Key {
				if lw := lipgloss.Width(p.LongText); lw > maxVal {
					maxVal = lw
				}
			}
		}
		if maxVal > 24 {
			maxVal = 24 // long texts are truncated anyway; don't ask for more
		}
		// column icon slot + needle slot + key + " " + (capped) long_text + frame
		w = g.IconW + m.needleSlot() + maxKey + 1 + maxVal + 4
	}
	return clampInt(w, minColW, maxColW)
}

// columnWidths decides which columns are visible and how wide each one is.
// It returns the index of the leftmost rendered column, the width of every
// rendered column (full columns followed, on overflow, by a narrow peek of
// the next one), and whether a peek is included. The viewport is sticky: it
// only scrolls when the active column moves past an edge.
func (m *model) columnWidths() (start int, widths []int, peek bool) {
	st := m.aliases[m.curAlias]
	n := len(st.Groups) + 1
	avail := m.width
	des := make([]int, n)
	total := 0
	for i := 0; i < n; i++ {
		des[i] = m.columnDesired(i)
		total += des[i]
	}
	if total <= avail {
		// Everything fits: desired widths, leftover shared fairly so no
		// single column (e.g. the alias column) swallows all the slack.
		widths := des
		extra := avail - total
		for extra > 0 {
			grew := false
			for i := 0; i < n && extra > 0; i++ {
				if widths[i] < maxColW {
					widths[i]++
					extra--
					grew = true
				}
			}
			if !grew {
				break
			}
		}
		// Very wide terminal: everything is at maxColW; fill the rest.
		for extra > 0 {
			for i := 0; i < n && extra > 0; i++ {
				widths[i]++
				extra--
			}
		}
		m.hScroll = 0
		return 0, widths, false
	}
	// Overflow: viewport of full-width columns plus a peek when more remain.
	start = m.hScroll
	if start < 0 {
		start = 0
	}
	if start > n-1 {
		start = n - 1
	}
	fit := func(s, k int) bool {
		used := 0
		for i := 0; i < k; i++ {
			used += des[s+i]
		}
		if s+k < n {
			used += peekColW
		}
		return used <= avail
	}
	count := 0
	for count < n-start && fit(start, count+1) {
		count++
	}
	// Keep the active column fully visible (sticky otherwise). When nothing
	// fits at the current start, or the active column is off the viewport,
	// anchor the viewport on the active column.
	if count == 0 || m.curCol < start || m.curCol > start+count-1 {
		start = m.curCol
		if start < 0 {
			start = 0
		}
		if start > n-1 {
			start = n - 1
		}
		count = 0
		for count < n-start && fit(start, count+1) {
			count++
		}
		if count == 0 {
			count = 1
		}
		if maxStart := n - count; start > maxStart {
			start = maxStart
		}
	}
	m.hScroll = start
	widths = make([]int, count)
	for i := 0; i < count; i++ {
		widths[i] = des[start+i]
	}
	// The viewport must fit exactly within the available width: grow the
	// columns to fill leftover space, or shrink them (a single forced column
	// can be wider than the terminal) down to the frame minimum.
	peek = start+count < n && avail >= peekColW+4
	delta := avail - sumInts(widths)
	if peek {
		delta -= peekColW
	}
	i := 0
	for delta > 0 {
		widths[i%count]++
		delta--
		i++
	}
	for delta < 0 {
		moved := false
		for k := 0; k < count && delta < 0; k++ {
			if widths[(i+k)%count] > 4 {
				widths[(i+k)%count]--
				delta++
				moved = true
			}
		}
		i++
		if !moved {
			break
		}
	}
	if peek {
		widths = append(widths, peekColW)
	}
	return start, widths, peek
}

func sumInts(v []int) int {
	s := 0
	for _, x := range v {
		s += x
	}
	return s
}

func (m *model) columns() string {
	st := m.aliases[m.curAlias]
	boxH := m.height - 1 - len(m.footerLines())
	if boxH < 4 {
		boxH = 4
	}
	contentLines := boxH - 2
	// The box top sits one row below the title bar.
	start, widths, peek := m.columnWidths()
	m.viewStart = start
	m.hasPeek = peek
	cols := make([]string, 0, len(widths))
	m.layout = make([]colLayout, 0, len(widths))
	x := 0
	for j, w := range widths {
		col := start + j
		var s string
		var top int
		var items []int
		if col == 0 {
			s, top, items = m.aliasColumn(w, boxH)
		} else {
			s, top, items = m.optionColumn(st.Groups[col-1], w, boxH, col)
		}
		cols = append(cols, s)
		m.layout = append(m.layout, colLayout{x: x, y: 1, w: w, contentLines: contentLines, top: top, items: items})
		x += w
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

// iconSlot renders the leading icon slot of a row: the icon itself (colored
// when it carries a !color tag) or blank padding, right-padded up to the
// column's slot width w so that every name/key in the column starts at the
// same cell. When the column has no icons, w is 0 and the slot is empty. On
// the cursor row the padding carries the row's background like the other
// spans do.
func (m *model) iconSlot(icon, color string, w int, cursor bool) string {
	if w <= 0 {
		return ""
	}
	var rendered string
	if icon != "" {
		if color != "" {
			rendered = theme.ColoredValueStyle(m.th, color, cursor).Render(icon)
		} else {
			rendered = icon
		}
	}
	if dw := lipgloss.Width(rendered); dw < w {
		pad := strings.Repeat(" ", w-dw)
		if cursor {
			return rendered + m.sty.RowCursor.Render(pad)
		}
		return rendered + pad
	}
	return rendered
}

// aliasLine renders one alias row: the alias column's icon slot (the
// alias's own glyph or blank padding), a ▸ marker on the selected alias
// (green, in the theme's muted color when the selection came from the
// history — two spaces on every other row), then the alias name. The needle
// is hidden in the active column: the cursor highlight is the selection
// indicator there, and Enter runs the cursor's row. In inactive columns the
// needle reappears on the previously selected alias. The row is built from
// styled spans like pairLine: on the cursor row every span carries the row's
// background itself, so a nested style's terminating reset cannot cut the
// highlight short.
func (m *model) aliasLine(i int) string {
	a := m.aliases[i]
	cursor := i == m.curAlias
	active := m.curCol == 0
	var line string
	line += m.iconSlot(a.Icon, a.IconColor, m.aliasIconW, cursor)
	// The needle is hidden in the active column: the cursor highlight is the
	// selection indicator there. In inactive columns the needle reappears on
	// the previously selected alias.
	if !active && i == m.selAlias {
		muted := m.selAliasSrc == config.SourceHistory
		if muted {
			line += m.sty.MarkerMuted.Render(m.needle)
		} else {
			line += m.sty.Marker.Render(m.needle)
		}
		line += " "
	} else if cursor {
		line += m.sty.RowCursor.Render(m.needleBlank())
	} else {
		line += m.needleBlank()
	}
	if cursor {
		line += m.sty.KeyCursor.Render(a.Name)
	} else {
		line += m.sty.Key.Render(a.Name)
	}
	return line
}

// searchLine renders the filter input: the word "filter" in the highlighted
// title color, then the typed pattern and the cursor, padded to w. In the
// full mode it occupies the bottom row of the filtered column's box; in the
// compact mode it is the line at the bottom of the alias list.
func (m *model) searchLine(w int) string {
	return padRight(m.sty.Title.Render("filter")+" "+string(m.searchBuf)+"█", w)
}

func (m *model) aliasColumn(w, h int) (string, int, []int) {
	// While the GLOBALS panel has the focus ("v"), the columns are muted.
	active := m.varsFocus == nil && m.curCol == 0
	contentLines := h - 2
	innerW := w - 4
	searching := m.search && active
	if searching {
		contentLines-- // the bottom row is the search input
	}
	var rows []string
	// Build the ordered list of visible alias indices (nil = unfiltered).
	var idxs []int
	if vis := m.visibleAliasIdxs(); vis != nil {
		idxs = vis
	} else {
		for i := range m.aliases {
			idxs = append(idxs, i)
		}
	}
	// Keep the cursor within the visible window; scroll the top when needed.
	top := m.searchWindowTop(idxs, m.curAlias, contentLines)
	for _, i := range idxs[top:] {
		if len(rows) >= contentLines {
			break
		}
		if i == m.curAlias {
			// The cursor row is built from spans that each carry the row's
			// background; the trailing padding gets its own span, because a
			// nested style's terminating reset would cut the outer RowCursor
			// wrap off (same as pairLine).
			line := m.aliasLine(i)
			if dw := innerW - lipgloss.Width(line); dw > 0 {
				line += m.sty.RowCursor.Render(strings.Repeat(" ", dw))
			}
			rows = append(rows, m.sty.RowCursor.Render(line))
		} else {
			rows = append(rows, m.sty.Row.Render(padRight(m.aliasLine(i), innerW)))
		}
	}
	if len(rows) == 0 {
		rows = append(rows, m.sty.Muted.Render(padRight("  (no matches)", innerW)))
	}
	for len(rows) < contentLines {
		rows = append(rows, strings.Repeat(" ", innerW))
	}
	if searching {
		rows = append(rows, m.searchLine(innerW))
	}
	return m.box("alias", rows, innerW, active), top, idxs
}

func (m *model) optionColumn(g *groupState, w, h, colIdx int) (string, int, []int) {
	// While the GLOBALS panel has the focus ("v"), the columns are muted.
	active := m.varsFocus == nil && m.curCol == colIdx
	contentLines := h - 2
	innerW := w - 4
	searching := m.search && active
	if searching {
		contentLines-- // the bottom row is the search input
	}
	// Visible pair indices (nil = unfiltered).
	var idxs []int
	filtered := false
	if vis := m.visiblePairIdxs(g); vis != nil {
		idxs = vis
		filtered = true
	} else {
		for i := range g.Pairs {
			idxs = append(idxs, i)
		}
	}
	// Choose the window top: sticky scroll for the filtered column, the
	// classic g.Offset behavior otherwise.
	var top int
	if filtered {
		top = m.searchWindowTop(idxs, g.Cursor, contentLines)
	} else {
		top = g.Offset
		if g.Cursor < top {
			top = g.Cursor
		}
		if g.Cursor >= top+contentLines {
			top = g.Cursor - contentLines + 1
		}
		if top < 0 {
			top = 0
		}
		g.Offset = top
	}
	var rows []string
	for _, i := range idxs[top:] {
		if len(rows) >= contentLines {
			break
		}
		rows = append(rows, m.pairLine(g, i, innerW, active))
	}
	if len(rows) == 0 {
		rows = append(rows, m.sty.Muted.Render(padRight("  (no matches)", innerW)))
	}
	for len(rows) < contentLines {
		rows = append(rows, strings.Repeat(" ", innerW))
	}
	if searching {
		rows = append(rows, m.searchLine(innerW))
	}
	return m.box(g.Name, rows, innerW, active), top, idxs
}

// pairLine renders one option row of visible width w: the column's leading
// icon slot (the option's own glyph or blank padding), a ▸ marker for the
// selected option (green normally, in the theme's muted color when the
// preselection came from the history), the key (in the tag's color when the
// option carries a !color tag), and the long_text when it differs from the
// key (always in the muted value color). The needle is hidden in the active
// column: the cursor highlight is the selection indicator there, and Enter
// runs the cursor's row. In inactive columns the needle reappears on the
// previously selected option. The cursor row is built from spans that each
// carry the cursor background themselves: a nested style's escapes end with
// a full reset, so wrapping a pre-styled line in RowCursor would cut the
// highlight off at the first inner reset. With the background baked into
// every span the whole line is highlighted.
func (m *model) pairLine(g *groupState, i, w int, active bool) string {
	p := g.Pairs[i]
	cursor := active && i == g.Cursor
	var line string
	line += m.iconSlot(cleanIcon(p.Icon), p.IconColor, g.IconW, cursor)
	// The needle is hidden in the active column: the cursor highlight is the
	// selection indicator there. In inactive columns the needle reappears on
	// the previously selected option.
	if !active && i == g.Selected {
		muted := g.Src == config.SourceHistory
		if muted {
			line += m.sty.MarkerMuted.Render(m.needle)
		} else {
			line += m.sty.Marker.Render(m.needle)
		}
		line += " "
	} else if cursor {
		line += m.sty.RowCursor.Render(m.needleBlank())
	} else {
		line += m.needleBlank()
	}
	keySt := m.sty.Key
	if cursor {
		keySt = m.sty.KeyCursor
	}
	if p.Color != "" {
		// !color tag: the key is shown in its own color.
		keySt = theme.ColoredValueStyle(m.th, p.Color, cursor)
	}
	line += keySt.Render(p.Key)
	if p.LongText != "" && p.LongText != p.Key {
		avail := w - lipgloss.Width(line) - 1
		if avail >= 2 {
			// The long_text keeps the muted value color, even when the
			// option carries a !color tag (the tag colors the key).
			valSt := m.sty.Value
			if cursor {
				valSt = m.sty.ValueCursor
			}
			line += valSt.Render(" " + truncateStr(p.LongText, avail))
		}
	}
	if cursor {
		if dw := w - lipgloss.Width(line); dw > 0 {
			line += m.sty.RowCursor.Render(strings.Repeat(" ", dw))
		} else {
			// The key is wider than the column: cut it to fit and close
			// the open span so the next line starts with a clean state.
			line = truncateStr(line, w) + "\x1b[0m"
		}
	} else {
		line = m.sty.Row.Render(padRight(line, w))
	}
	return line
}

// box draws a bordered column with a label embedded in the top border.
// rows must all be exactly innerW wide; the frame is innerW+4 wide.
func (m *model) box(title string, rows []string, innerW int, active bool) string {
	// The colors are normalized so lipgloss always picks the same color space
	// (a raw uppercase hex like "#FF8931" would otherwise fall back to the
	// ANSI 256 palette while the titles use truecolor).
	var b, t lipgloss.Color
	if active {
		b, t = theme.NormalizeColor(m.th.BorderActive), theme.NormalizeColor(m.th.Title)
	} else {
		b, t = theme.NormalizeColor(m.th.Border), theme.NormalizeColor(m.th.Muted)
	}
	bord := lipgloss.NewStyle().Foreground(b)
	titleS := lipgloss.NewStyle().Foreground(t)
	if active {
		titleS = titleS.Bold(true)
	}
	label := truncateStr(strings.ToUpper(title), innerW-1)
	labelW := lipgloss.Width(label)
	fill := innerW + 1 - labelW
	if fill < 1 {
		fill = 1
	}
	var bld strings.Builder
	bld.WriteString(bord.Render("┌─") + titleS.Render(label) + bord.Render(strings.Repeat("─", fill)+"┐"))
	for _, r := range rows {
		bld.WriteString("\n" + bord.Render("│ ") + r + bord.Render(" │"))
	}
	bld.WriteString("\n" + bord.Render("└"+strings.Repeat("─", innerW+2)+"┘"))
	return bld.String()
}

// footerLines returns every footer line, top to bottom: the status/prompt
// line(s), the GLOBALS panel (always present in the full mode, above COMMAND —
// it is also the "v"ariables editor when focused), the live command panel,
// the optional info line, then the context help line(s).
func (m *model) footerLines() []string {
	var lines []string
	lines = append(lines, m.footerStatusLines()...)
	lines = append(lines, m.globalsPanelLines()...)
	lines = append(lines, m.commandPanelLines()...)
	if l := m.infoLine(); l != "" {
		lines = append(lines, l)
	}
	lines = append(lines, m.helpLines()...)
	return lines
}

func (m *model) footer() string {
	return strings.Join(m.footerLines(), "\n")
}

// footerStatusLines is the top footer line(s): an open prompt or a transient
// status. The GLOBALS and COMMAND panels follow on their own (footerLines);
// the active search renders no footer line here: its input lives inside the
// searched column (searchLine), as it is contextual to that column's content.
func (m *model) footerStatusLines() []string {
	if m.prompt != nil {
		if m.prompt.kind == promptInput {
			return []string{m.sty.Help.Render(truncateStr(m.prompt.label+" "+string(m.prompt.buf)+"█", m.width))}
		}
		return []string{m.sty.Help.Render(truncateStr(m.prompt.label, m.width))}
	}
	if m.status != "" {
		return []string{m.sty.Status.Render(padRight(m.status, m.width))}
	}
	return nil
}

// commandPanelLines is the live command preview as a full-width bordered
// bottom panel: a "COMMAND" box (styled like the column boxes) whose content
// rows are the wrapped preview text. A preview wider than the terminal's
// inner width is wrapped (word boundaries, with long words hard-broken)
// instead of truncated, so it overflows onto extra rows inside the panel and
// is never cut; the column boxes shrink to make room because their height is
// derived from the footer line count.
func (m *model) commandPanelLines() []string {
	innerW := m.width - 4
	if innerW < 2 {
		innerW = 2
	}
	// A trailing newline in the command (e.g. a YAML block scalar) would make
	// Wrap emit one empty final row; it carries no content, drop it.
	preview := strings.TrimRight(m.commandPreview(), "\n")
	var rows []string
	for _, l := range strings.Split(cellbuf.Wrap(preview, innerW, ""), "\n") {
		rows = append(rows, padRight(l, innerW))
	}
	return strings.Split(m.box("command", rows, innerW, false), "\n")
}

// eggArt is the easter egg panel's ASCII art (the XUZ banner).
const eggArt = `░██    ░██ ░██     ░██ ░█████████
 ░██  ░██  ░██     ░██       ░██
  ░██░██   ░██     ░██      ░██
   ░███    ░██     ░██    ░███
  ░██░██   ░██     ░██   ░██
 ░██  ░██   ░██   ░██   ░██
░██    ░██   ░██████   ░█████████`

// eggPanelLines renders the easter egg panel: a full-width bordered box (the
// same frame style as the COMMAND and GLOBALS panels) holding the XUZ ASCII
// art, the project name, the repository URL and the license line. It is opened
// by the Konami code (see konamiSequence) in the short mode and closed with
// "?" or esc.
func (m *model) eggPanelLines() string {
	innerW := m.width - 4
	if innerW < 2 {
		innerW = 2
	}
	var rows []string
	for _, l := range strings.Split(eggArt, "\n") {
		rows = append(rows, padRight(l, innerW))
	}
	repo := "Repository: https://github.com/domgom/xuz"
	if m.width > 0 && lipgloss.Width(repo) > innerW {
		repo = truncateStr(repo, innerW)
	}
	rows = append(rows, padRight("", innerW))
	rows = append(rows, padRight("XUZ (Choose)", innerW))
	rows = append(rows, padRight(repo, innerW))
	rows = append(rows, padRight("2026 - MIT License", innerW))
	return m.box("xuz · easter egg", rows, innerW, false)
}

// infoLine is the optional detail line toggled with "?".
func (m *model) infoLine() string {
	if !m.showInfo {
		return ""
	}
	cfgPath := m.cfg.Path
	if cfgPath == "" {
		cfgPath = "(none)"
	}
	parts := []string{"config: " + cfgPath, "theme: " + m.th.Name}
	if m.dryRun {
		parts = append(parts, "dry-run")
	}
	if m.dirty {
		parts = append(parts, "unsaved changes")
	}
	return m.sty.Muted.Render(truncateStr(strings.Join(parts, " · "), m.width))
}

// fullHelpItems returns the shortcut legend entries for the current full
// mode state: the open prompt (add a value, or a yes/no confirm), the active
// filter, or the plain picker (alias column vs. option column). The same key
// always carries the same legend, in every menu. The exit item (helpExit,
// the same glyph and legend as the compact mode) is always the first entry
// of the picker legend, whatever the column: esc quits from anywhere. The
// column navigation (tab/shift+tab, left/right) is deliberately
// unadvertised: the move item carries the same four-direction tokens as the
// compact mode, and no next/previous column items are listed.
func (m *model) fullHelpItems() []helpItem {
	if m.prompt != nil {
		if m.prompt.kind == promptInput {
			return []helpItem{
				{keyEnter, " add"},
				{keyEsc, " cancel"},
			}
		}
		return []helpItem{
			{"y", "es"},
			{"n", "o"},
		}
	}
	if m.varsFocus != nil {
		// The GLOBALS panel has the keyboard focus ("v"): its own legend, in
		// the same format as the main menu. Esc returns to the columns; every
		// change is saved to the config file immediately.
		items := []helpItem{
			helpExit,
			{keySpace, " edit"},
			{keyMove, " move"},
			{"d", "elete"},
		}
		if m.varsFocus.Editing {
			items = []helpItem{
				{keyEnter, " next/commit"},
				{keyEsc, " cancel"},
			}
		}
		return items
	}
	if m.search {
		items := []helpItem{
			{keyEsc, " exit filter"},
			{keyMove, " move"},
		}
		if m.curCol != 0 {
			items = append(items, helpItem{keySpace, " select"})
		}
		items = append(items, helpItem{keyEnter, " run"})
		return items
	}
	items := []helpItem{
		helpExit,
		{keyMove, " move"},
		{keySpace, " select"},
	}
	if m.curCol == 0 {
		items = append(items,
			helpItem{keyClick, " focus/select"},
			helpItem{keyEnter, " run"},
			helpItem{"f", "ilter"},
			helpItem{"v", "ariables"},
			helpItem{"c", "opy"},
			helpItem{keyCtrl + "s", "ave"},
			helpItem{"b", "ackground"},
			helpItem{"t", "heme"},
			helpItem{"/", " switch"},
		)
	} else {
		items = append(items,
			helpItem{keyCtrl + keySpace, " set default"},
			helpItem{keyClick, " focus/select"},
			helpItem{keyEnter, " run"},
			helpItem{"f", "ilter"},
			helpItem{"v", "ariables"},
			helpItem{"n", "ew"},
			helpItem{keyDel, " delete"},
			helpItem{"c", "opy"},
			helpItem{keyCtrl + "s", "ave"},
			helpItem{"b", "ackground"},
			helpItem{"t", "heme"},
			helpItem{"/", " switch"},
		)
	}
	if m.hasPeek {
		items = append(items, helpItem{label: "» more columns to the right"})
	}
	return items
}

// helpLines returns the context-sensitive shortcut line(s) for the current
// state, wrapped to the terminal width.
func (m *model) helpLines() []string {
	return m.wrapHelpItems(m.fullHelpItems())
}

// wrapHelpItems joins rendered legend items with " · " into lines that fit
// the terminal width, keeping whole items together.
func (m *model) wrapHelpItems(items []helpItem) []string {
	var lines []string
	cur := ""
	for _, it := range items {
		s := m.renderHelpItem(it)
		cand := s
		if cur != "" {
			cand = cur + " · " + s
		}
		if cur != "" && lipgloss.Width(cand) > m.width-1 {
			lines = append(lines, padRight(cur, m.width))
			cur = s
		} else {
			cur = cand
		}
	}
	if cur != "" {
		lines = append(lines, padRight(cur, m.width))
	}
	return lines
}

// commandPreview shows what Enter will run: the alias under the cursor's
// command with the env prefix that will be in effect (the needle is hidden in
// the active column; the cursor is the selection). The command text is
// verbatim — the shell resolves the $VAR refs — and the prefix shows the
// exact assignments, so the preview is copy-paste-runnable.
func (m *model) commandPreview() string {
	m.ensureInited(m.curAlias)
	alias := m.cfg.Aliases[m.curAlias]
	if alias.Command == "" {
		return m.sty.Error.Render("$ (no command defined for this alias)")
	}
	// Build a temporary copy of the state with the active column's cursor
	// applied as its selection, so the preview matches what Enter will run.
	st := m.aliases[m.curAlias]
	vars := m.currentVars(m.curAlias, st)
	if m.curCol > 0 {
		g := st.Groups[m.curCol-1]
		vars[strings.ToUpper(g.Name)] = g.Pairs[g.Cursor].Value()
		for varName, groupName := range alias.Vars {
			if groupName == g.Name {
				vars[varName] = g.Pairs[g.Cursor].Value()
			}
		}
	}
	// The template-derived vars are part of the env the command will run
	// with, so include them in the prefix (a template error is reported on
	// the status line and contributes nothing).
	vars = m.withDerivedVars(alias, vars)
	out := m.sty.Status.Render("$ ")
	out += m.sty.Value.Render(cmdx.ExportPrefix(vars))
	out += alias.Command
	return out
}

// withDerivedVars returns vars plus the alias's template-derived vars (when
// the alias has a template). A template that fails to evaluate or parse is
// reported on the status line and contributes no vars.
func (m *model) withDerivedVars(alias *config.Alias, vars map[string]string) map[string]string {
	if alias.Template == "" {
		return vars
	}
	out, err := cmdx.EvalTemplate(alias.Template, vars)
	if err != nil {
		m.status = "template error: " + err.Error()
		return vars
	}
	derived, err := cmdx.ParseDerivedVars(out)
	if err != nil {
		m.status = "template error: " + err.Error()
		return vars
	}
	for k, v := range derived {
		vars[k] = v
	}
	return vars
}

// cleanIcon collapses whitespace runs in an icon glyph (literal text: an
// emoji or a nerd-font glyph) so it always stays on one row.
func cleanIcon(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func padRight(s string, w int) string {
	dw := lipgloss.Width(s)
	if dw >= w {
		return truncateStr(s, w)
	}
	return s + strings.Repeat(" ", w-dw)
}

// truncateStr cuts a string to visible width w (ANSI-aware via lipgloss).
func truncateStr(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func inList(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func indexOfKey(pairs []config.Pair, key string) int {
	for i, p := range pairs {
		if p.Key == key {
			return i
		}
	}
	return -1
}

// appendHistory records the run that is about to happen.
func (m *model) appendHistory() {
	remember := m.cfg.Aliases[m.selAlias].RememberLast
	if remember <= 0 {
		remember = m.cfg.RememberLast
	}
	if remember <= 0 {
		remember = 10
	}
	if err := history.Append(m.histPath, m.entry, remember); err != nil {
		fmt.Fprintf(os.Stderr, "xuz: warning: could not write history: %v\n", err)
	}
}

// startDetached launches the command in background mode — the command is
// suffixed with " &" so the shell starts it in the background — in a new
// session (setsid) with stdio redirected to logPath, and does not wait for
// it. It returns the exit code to report (0 on success).
func startDetached(command string, vars map[string]string, logPath string) int {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 1
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 1
	}
	defer f.Close()
	// Background mode: suffix " &" so the shell launches the command in the
	// background rather than waiting for it in the foreground.
	cmd := exec.Command("sh", "-c", command+" &")
	env := os.Environ()
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	cmd.Stdin = nil // null device: never steal the TTY
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 127
	}
	pid := cmd.Process.Pid
	fmt.Printf("$ %s\n", command)
	fmt.Printf("PID: %d\n", pid)
	return 0
}

// Run opens the picker and returns the process exit code. The picker never
// reopens after a launched command: xuz runs it and exits with its status.
// The mode of each pass is m.short: the full mode gets the alternate screen
// and the mouse, the compact mode renders a few inline lines on the normal
// screen. "/" (m.modeSwitch) restarts the loop with a program configured
// for the other mode, preserving all picker state.
func Run(opts Options) int {
	m, err := New(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 1
	}
	if m.histPath == "" {
		m.histPath = history.Path()
	}
	if entries, err := history.Load(m.histPath); err == nil {
		m.history = entries
	}
	// A short-mode pass renders inline on the main screen and leaves its frame
	// there when it exits, so after any short-mode pass (a "/" switch or a
	// final exit) the screen is cleared with a full erase + home. This is done
	// AFTER p.Run returns (never while a program is alive, so it cannot race
	// the renderer's exit sequence) and uses a full screen clear rather than
	// erasing a fixed number of lines, because the cursor's final row is not
	// guaranteed and after a mode switch the visible frame may be from the
	// other mode. A full-mode pass runs on the alternate screen, which
	// bubbletea restores on exit, so it needs no clear. On a piped stdout
	// nothing was rendered (View returned ""), so there is never anything to
	// clear.
	for {
		var progOpts []tea.ProgramOption
		if !m.short {
			progOpts = []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion()}
		}
		p := tea.NewProgram(m, progOpts...)
		final, err := p.Run()
		if err != nil {
			fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
			return 1
		}
		m = final.(*model)
		// The pass that just ran was in short mode and left its inline frame
		// on the main screen; clear it before the next program (a "/" switch)
		// or the exit output. A full-mode pass needs no clear (the alternate
		// screen is restored by bubbletea).
		if m.short && m.lastFrameLines > 0 {
			clearScreenHome()
		}
		if m.modeSwitch {
			// "/": restart in the other mode with the state preserved. doQuit
			// is what ended the previous program; clear it so the new one does
			// not immediately quit on its first message.
			m.modeSwitch = false
			m.doQuit = false
			continue
		}
		if m.doRun {
			m.appendHistory()
		}
		if m.doRun && m.background {
			// Background run (Enter with background mode armed): launch the user
			// command with " &" so it keeps running, then exit xuz.
			code := startDetached(m.runCmd, m.runVars, m.logPath)
			if code != 0 {
				return code
			}
			fmt.Printf("xuz: running in background — log: %s\n", m.logPath)
			return 0
		}
		if m.doQuit {
			if m.dryCmd != "" {
				fmt.Println(m.dryCmd)
			}
			return 0
		}
		if !m.doRun {
			// Esc / q: nothing was run, so leave the screen clean.
			return 0
		}
		// The picker never reopens after a run, whatever the outcome: xuz
		// exits with the command's status.
		return runChild(m.runCmd, m.runVars)
	}
}

// clearScreenHome erases the whole screen and moves the cursor to the top-left
// corner, so the next program or the exit output starts from a clean slate. It
// is called after a short-mode pass has exited and left its inline frame on
// the main screen; unlike erasing a fixed number of lines, it does not depend
// on where bubbletea left the cursor (which is not guaranteed) or on how many
// lines the frame had (the visible frame may be from the other mode after a
// "/" switch). On a piped stdout nothing was rendered, so this is never called.
func clearScreenHome() {
	fmt.Print("\x1b[2J\x1b[H")
}

// runChild executes the command via sh -c with the selected options exported
// as environment variables. stdio is inherited; SIGINT/SIGTERM are forwarded.
func runChild(command string, vars map[string]string) int {
	cmd := exec.Command("sh", "-c", command)
	env := os.Environ()
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "xuz: "+err.Error())
		return 127
	}
	pid := cmd.Process.Pid
	fmt.Printf("$ %s\n", command)
	fmt.Printf("PID: %d\n", pid)
	go func() {
		for s := range sig {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(s)
			}
		}
	}()
	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}
