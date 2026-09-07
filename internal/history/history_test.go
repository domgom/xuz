package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodeDecode(t *testing.T) {
	e := Entry{
		Time:  time.Date(2026, 2, 7, 12, 34, 56, 0, time.UTC),
		Alias: "llama",
		Sels:  []Selection{{Group: "model", Key: "qwen-3.8-27B"}, {Group: "context", Key: "64k"}},
	}
	line := Encode(e)
	back, err := Decode(line)
	if err != nil {
		t.Fatal(err)
	}
	if back.Alias != e.Alias || len(back.Sels) != 2 || back.Sels[0] != e.Sels[0] || back.Sels[1] != e.Sels[1] {
		t.Errorf("round trip = %+v", back)
	}
	if !back.Time.Equal(e.Time) {
		t.Errorf("time = %v", back.Time)
	}
}

func TestAppendTrims(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history")
	for i := 0; i < 15; i++ {
		e := Entry{Time: time.Now(), Alias: "a", Sels: []Selection{{Group: "g", Key: "k"}}}
		if err := Append(p, e, 10); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 10 {
		t.Fatalf("entries = %d", len(entries))
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, c := range string(data) {
		if c == '\n' {
			lines++
		}
	}
	if lines != 10 {
		t.Errorf("lines in file = %d", lines)
	}
}

func TestLatestFor(t *testing.T) {
	entries := []Entry{
		{Alias: "a"},
		{Alias: "b"},
		{Alias: "a"},
	}
	e := LatestFor(entries, "a")
	if e == nil || e.Alias != "a" {
		t.Errorf("latest a = %+v", e)
	}
	if LatestFor(entries, "zzz") != nil {
		t.Error("expected nil")
	}
}
