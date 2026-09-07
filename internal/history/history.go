// Package history records and reads launched configurations.
package history

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Selection is one chosen option: option group and selected key.
type Selection struct {
	Group string
	Key   string
}

// Entry is one launch.
type Entry struct {
	Time  time.Time
	Alias string
	Sels  []Selection
}

// Path returns the default history file path ($XUZ_HISTORY or ~/.xuz/history).
func Path() string {
	if p := os.Getenv("XUZ_HISTORY"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".xuz/history"
	}
	return home + "/.xuz/history"
}

// Encode renders an entry as one tab-separated line.
func Encode(e Entry) string {
	parts := []string{e.Time.UTC().Format(time.RFC3339), e.Alias}
	for _, s := range e.Sels {
		parts = append(parts, s.Group+"="+s.Key)
	}
	return strings.Join(parts, "\t")
}

// Decode parses a line written by Encode.
func Decode(line string) (Entry, error) {
	var e Entry
	parts := strings.Split(line, "\t")
	if len(parts) < 2 {
		return e, fmt.Errorf("malformed history line: %q", line)
	}
	t, err := time.Parse(time.RFC3339, parts[0])
	if err != nil {
		return e, fmt.Errorf("bad timestamp %q: %w", parts[0], err)
	}
	e.Time, e.Alias = t, parts[1]
	for _, p := range parts[2:] {
		i := strings.IndexByte(p, '=')
		if i <= 0 {
			return e, fmt.Errorf("malformed selection %q", p)
		}
		e.Sels = append(e.Sels, Selection{Group: p[:i], Key: p[i+1:]})
	}
	return e, nil
}

// Load reads all entries from the history file (missing file is not an error).
func Load(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		e, err := Decode(line)
		if err != nil {
			continue // skip bad lines
		}
		out = append(out, e)
	}
	return out, nil
}

// Append appends an entry to the history file, keeping at most rememberLast
// entries (the oldest are dropped).
func Append(path string, e Entry, rememberLast int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	existing, err := Load(path)
	if err != nil {
		return err
	}
	existing = append(existing, e)
	if rememberLast > 0 && len(existing) > rememberLast {
		existing = existing[len(existing)-rememberLast:]
	}
	var sb strings.Builder
	for _, en := range existing {
		sb.WriteString(Encode(en))
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// LatestFor returns the most recent entry for alias, or nil.
func LatestFor(entries []Entry, alias string) *Entry {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Alias == alias {
			return &entries[i]
		}
	}
	return nil
}
