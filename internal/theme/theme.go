// Package theme defines xuz color themes and their rendering styles.
package theme

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds one theme's raw color specs.
type Theme struct {
	Name         string
	Bg           string
	Fg           string
	Accent       string
	Selected     string
	CursorBg     string
	CursorFg     string
	Border       string
	BorderActive string
	Title        string
	Muted        string
	Status       string
	Help         string
	Error        string
	Success      string
}

// Builtins returns the built-in theme names in cycle order.
func Builtins() []string {
	return []string{
		"default", "dracula", "gruvbox", "monokai",
		"catppuccin", "solarized-dark", "solarized-light", "light",
	}
}

func setField(t *Theme, field, value string) {
	switch field {
	case "bg":
		t.Bg = value
	case "fg":
		t.Fg = value
	case "accent":
		t.Accent = value
	case "selected":
		t.Selected = value
	case "cursor_bg":
		t.CursorBg = value
	case "cursor_fg":
		t.CursorFg = value
	case "border":
		t.Border = value
	case "border_active":
		t.BorderActive = value
	case "title":
		t.Title = value
	case "muted":
		t.Muted = value
	case "status":
		t.Status = value
	case "help":
		t.Help = value
	case "error":
		t.Error = value
	case "success":
		t.Success = value
	}
}

func T(name string, kv ...string) Theme {
	t := Theme{Name: name}
	for i := 0; i+1 < len(kv); i += 2 {
		setField(&t, kv[i], kv[i+1])
	}
	return t
}

var builtinThemes = map[string]Theme{
	"default": T("default",
		"bg", "#1a1b26", "fg", "#c0caf5", "accent", "#7aa2f7", "selected", "#9ece6a",
		"cursor_bg", "#24283b", "cursor_fg", "#c0caf5",
		"border", "#3b4261", "border_active", "#7aa2f7", "title", "#7aa2f7",
		"muted", "#565f89", "status", "#7aa2f7", "help", "#565f89",
		"error", "#f7768e", "success", "#9ece6a",
	),
	"dracula": T("dracula",
		"bg", "#282a36", "fg", "#f8f8f2", "accent", "#bd93f9", "selected", "#50fa7b",
		"cursor_bg", "#44475a", "cursor_fg", "#f8f8f2",
		"border", "#6272a4", "border_active", "#bd93f9", "title", "#bd93f9",
		"muted", "#6272a4", "status", "#bd93f9", "help", "#6272a4",
		"error", "#ff5555", "success", "#50fa7b",
	),
	"gruvbox": T("gruvbox",
		"bg", "#282828", "fg", "#ebdbb2", "accent", "#fabd2f", "selected", "#b8bb26",
		"cursor_bg", "#3c3836", "cursor_fg", "#ebdbb2",
		"border", "#928374", "border_active", "#fabd2f", "title", "#fabd2f",
		"muted", "#928374", "status", "#fabd2f", "help", "#928374",
		"error", "#cc241d", "success", "#b8bb26",
	),
	"monokai": T("monokai",
		"bg", "#272822", "fg", "#f8f8f2", "accent", "#a6e22e", "selected", "#a6e22e",
		"cursor_bg", "#3e3d32", "cursor_fg", "#f8f8f2",
		"border", "#75715e", "border_active", "#a6e22e", "title", "#a6e22e",
		"muted", "#75715e", "status", "#a6e22e", "help", "#75715e",
		"error", "#f92672", "success", "#a6e22e",
	),
	"catppuccin": T("catppuccin",
		"bg", "#1e1e2e", "fg", "#cdd6f4", "accent", "#89b4fa", "selected", "#a6e3a1",
		"cursor_bg", "#313244", "cursor_fg", "#cdd6f4",
		"border", "#45475a", "border_active", "#89b4fa", "title", "#89b4fa",
		"muted", "#6c7086", "status", "#89b4fa", "help", "#6c7086",
		"error", "#f38ba8", "success", "#a6e3a1",
	),
	"solarized-dark": T("solarized-dark",
		"bg", "#002b36", "fg", "#839496", "accent", "#268bd2", "selected", "#859900",
		"cursor_bg", "#073642", "cursor_fg", "#93a1a1",
		"border", "#586e75", "border_active", "#268bd2", "title", "#268bd2",
		"muted", "#586e75", "status", "#268bd2", "help", "#586e75",
		"error", "#dc322f", "success", "#859900",
	),
	"solarized-light": T("solarized-light",
		"bg", "#fdf6e3", "fg", "#657b83", "accent", "#268bd2", "selected", "#859900",
		"cursor_bg", "#eee8d5", "cursor_fg", "#586e75",
		"border", "#93a1a1", "border_active", "#268bd2", "title", "#268bd2",
		"muted", "#93a1a1", "status", "#268bd2", "help", "#93a1a1",
		"error", "#dc322f", "success", "#859900",
	),
	"light": T("light",
		"bg", "#ffffff", "fg", "#1f2330", "accent", "#2563eb", "selected", "#16a34a",
		"cursor_bg", "#e5e9f2", "cursor_fg", "#1f2330",
		"border", "#c9d1e0", "border_active", "#2563eb", "title", "#2563eb",
		"muted", "#6b7280", "status", "#2563eb", "help", "#6b7280",
		"error", "#dc2626", "success", "#16a34a",
	),
}

// Get resolves a theme by name. Custom definitions (from the config "themes:"
// section) take precedence over built-ins; a custom with the same name as a
// built-in is merged over that built-in, otherwise over "default".
func Get(name string, custom map[string]map[string]string) (Theme, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if def, ok := custom[name]; ok {
		var t Theme
		if base, bok := builtinThemes[name]; bok {
			t = base
		} else {
			t = builtinThemes["default"]
		}
		t.Name = name
		for k, v := range def {
			setField(&t, strings.ToLower(strings.TrimSpace(k)), v)
		}
		return t, nil
	}
	if t, ok := builtinThemes[name]; ok {
		return t, nil
	}
	return Theme{}, fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(Builtins(), ", "))
}

// Styles is a set of precomputed lipgloss styles for a theme. The *Cursor
// variants carry the cursor row's background in the style itself: a cursor
// row is assembled from these spans, and because a nested style's escape
// codes terminate with a full reset, the background must be part of every
// span or the reset would cut the row's highlight short.
type Styles struct {
	Title          lipgloss.Style
	Box            lipgloss.Style
	BoxActive      lipgloss.Style
	ColTitle       lipgloss.Style
	ColTitleAct    lipgloss.Style
	Row            lipgloss.Style
	RowCursor      lipgloss.Style
	Marker         lipgloss.Style
	MarkerCursor   lipgloss.Style
	MarkerMuted    lipgloss.Style
	MarkerMutedCur lipgloss.Style
	Key            lipgloss.Style
	KeyCursor      lipgloss.Style
	Value          lipgloss.Style
	ValueCursor    lipgloss.Style
	Status         lipgloss.Style
	Help           lipgloss.Style
	HelpKey        lipgloss.Style
	Error          lipgloss.Style
	Success        lipgloss.Style
	Muted          lipgloss.Style
}

// BuildStyles renders a Theme into lipgloss styles.
func BuildStyles(t Theme) Styles {
	fg := color(t.Fg)
	cursorBg := color(t.CursorBg)
	return Styles{
		Title:          lipgloss.NewStyle().Foreground(color(t.Title)),
		Box:            lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(color(t.Border)),
		BoxActive:      lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(color(t.BorderActive)),
		ColTitle:       lipgloss.NewStyle().Foreground(color(t.Muted)),
		ColTitleAct:    lipgloss.NewStyle().Foreground(color(t.Title)).Bold(true),
		Row:            lipgloss.NewStyle().Foreground(fg),
		RowCursor:      lipgloss.NewStyle().Foreground(colorOr(t.CursorFg, fg)).Background(cursorBg).Bold(true),
		Marker:         lipgloss.NewStyle().Foreground(color(t.Selected)).Bold(true),
		MarkerCursor:   lipgloss.NewStyle().Foreground(color(t.Selected)).Background(cursorBg).Bold(true),
		MarkerMuted:    lipgloss.NewStyle().Foreground(color(t.Muted)).Bold(true),
		MarkerMutedCur: lipgloss.NewStyle().Foreground(color(t.Muted)).Background(cursorBg).Bold(true),
		Key:            lipgloss.NewStyle().Foreground(fg),
		KeyCursor:      lipgloss.NewStyle().Foreground(fg).Background(cursorBg).Bold(true),
		Value:          lipgloss.NewStyle().Foreground(color(t.Muted)),
		ValueCursor:    lipgloss.NewStyle().Foreground(color(t.Muted)).Background(cursorBg).Bold(true),
		Status:         lipgloss.NewStyle().Foreground(color(t.Status)),
		Help:           lipgloss.NewStyle().Foreground(color(t.Help)),
		HelpKey:        lipgloss.NewStyle().Foreground(color(t.Accent)).Bold(true),
		Error:          lipgloss.NewStyle().Foreground(color(t.Error)),
		Success:        lipgloss.NewStyle().Foreground(color(t.Success)),
		Muted:          lipgloss.NewStyle().Foreground(color(t.Muted)),
	}
}

// ColoredValueStyle is the style for text carrying a !color tag (an
// option's key or an icon): the tag's color as foreground instead of the
// usual color. On the cursor row it also carries the row's background and
// bold, exactly like the *Cursor styles do, so the row highlight stays
// intact.
func ColoredValueStyle(t Theme, fg string, cursor bool) lipgloss.Style {
	c := NormalizeColor(fg)
	if c == "" {
		c = color(t.Muted)
	}
	if cursor {
		return lipgloss.NewStyle().Foreground(c).Background(color(t.CursorBg)).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(c)
}

func colorOr(c string, fallback lipgloss.Color) lipgloss.Color {
	c = strings.TrimSpace(c)
	if c == "" || strings.EqualFold(c, "default") || strings.EqualFold(c, "none") {
		return fallback
	}
	return color(c)
}

var namedColors = map[string]string{
	"black":          "#000000",
	"red":            "#cc0000",
	"green":          "#00aa00",
	"yellow":         "#ccaa00",
	"blue":           "#0000ee",
	"magenta":        "#aa00aa",
	"cyan":           "#00aaaa",
	"white":          "#dddddd",
	"bright-black":   "#767676",
	"bright-red":     "#ff0000",
	"bright-green":   "#00ff00",
	"bright-yellow":  "#ffff00",
	"bright-blue":    "#5c5cff",
	"bright-magenta": "#ff00ff",
	"bright-cyan":    "#00ffff",
	"bright-white":   "#ffffff",
}

var base16 = []string{
	"#000000", "#800000", "#008000", "#808000", "#000080", "#800080", "#008080", "#c0c0c0",
	"#808080", "#ff0000", "#00ff00", "#ffff00", "#0000ff", "#ff00ff", "#00ffff", "#ffffff",
}

// NormalizeColor converts a theme color spec to a lipgloss.Color:
// "" or "default" stays the terminal default, #rrggbb passes through,
// named colors and 0-255 indexes are converted to hex.
func NormalizeColor(c string) lipgloss.Color {
	c = strings.TrimSpace(c)
	switch {
	case c == "" || strings.EqualFold(c, "default") || strings.EqualFold(c, "none"):
		return ""
	case len(c) == 7 && c[0] == '#':
		if _, err := strconv.ParseUint(c[1:], 16, 32); err != nil {
			return ""
		}
		return lipgloss.Color(strings.ToLower(c))
	}
	if hex, ok := namedColors[strings.ToLower(c)]; ok {
		return lipgloss.Color(hex)
	}
	if n, err := strconv.Atoi(c); err == nil && n >= 0 && n <= 255 {
		return lipgloss.Color(hexFrom256(n))
	}
	return ""
}

func color(c string) lipgloss.Color { return NormalizeColor(c) }

func hexFrom256(n int) string {
	if n < 16 {
		return base16[n]
	}
	if n < 232 {
		i := n - 16
		return "#" + cubeHex(i/36) + cubeHex((i/6)%6) + cubeHex(i%6)
	}
	v := 8 + (n-232)*10
	h := fmt.Sprintf("%02x", v)
	return "#" + h + h + h
}

func cubeHex(c int) string {
	v := 0
	if c > 0 {
		v = 55 + c*40
	}
	return fmt.Sprintf("%02x", v)
}
