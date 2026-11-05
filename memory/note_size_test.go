package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveRejectsNoteOverSingleFileCap(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.MaxSingleFileBytes = 2048
	if err := s.Save(strings.Repeat("a", 100), "50_Facts"); err != nil {
		t.Fatal(err)
	}
	before := noteFileSizes(t, dir)
	err = s.Save(strings.Repeat("b", 4000), "50_Facts")
	if err == nil || !strings.Contains(err.Error(), ErrNoteTooLarge.Error()) {
		t.Fatalf("oversized save error = %v", err)
	}
	after := noteFileSizes(t, dir)
	for path, size := range after {
		if size > s.MaxSingleFileBytes {
			t.Fatalf("%s is %d bytes, cap %d", path, size, s.MaxSingleFileBytes)
		}
	}
	if len(after) != len(before) {
		t.Fatalf("vault files changed on refused write: before %d after %d", len(before), len(after))
	}
	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Search("a"); len(got) != 1 {
		t.Fatalf("reloaded notes = %+v, want the original small note", got)
	}
}

func TestNewStoreSkipsOversizedNoteWithoutReadingIt(t *testing.T) {
	dir := t.TempDir()
	conceptDir := filepath.Join(dir, "70_Concepts")
	if err := os.MkdirAll(conceptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	huge := filepath.Join(conceptDir, "junk.md")
	f, err := os.Create(huge)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(8 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()

	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if len(s.entries) != 0 {
		t.Fatalf("oversized note was loaded: %d entries", len(s.entries))
	}
	if len(s.SkippedNotes) != 1 || !strings.HasSuffix(s.SkippedNotes[0], "70_Concepts/junk.md") {
		t.Fatalf("skipped = %#v", s.SkippedNotes)
	}
	if err := s.Save("small fact", "50_Facts"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.MaxSingleFileBytes = DefaultMaxSingleFileBytes
	found := reloaded.Search("small fact")
	if len(found) != 1 || found[0].Content != "small fact" {
		t.Fatalf("small note = %+v", found)
	}
	if len(reloaded.SkippedNotes) != 1 {
		t.Fatalf("reload skipped = %#v", reloaded.SkippedNotes)
	}
}

func noteFileSizes(t *testing.T, dir string) map[string]int64 {
	t.Helper()
	sizes := map[string]int64{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		sizes[path] = info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sizes
}
