package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryNoteRoundTripDoesNotDuplicateLinksSection(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	result, err := store.SaveWithOptionsResult("A durable fact.", "knowledge", TierLong, 0.9, SaveOptions{Links: []string{"RAG"}})
	if err != nil {
		t.Fatalf("save linked memory: %v", err)
	}
	notePath := filepath.Join(dir, filepath.FromSlash(result.Path))

	for i := 0; i < 3; i++ {
		store, err = NewStore(dir)
		if err != nil {
			t.Fatalf("reload %d: %v", i, err)
		}
		entry, err := store.Get(result.ID)
		if err != nil {
			t.Fatalf("get linked memory after reload %d: %v", i, err)
		}
		if strings.Contains(entry.Content, "## Links") {
			t.Fatalf("generated links section became durable content on reload %d: %q", i, entry.Content)
		}
		if _, err := store.SaveWithOptionsResult("Unrelated fact.", "knowledge", TierLong, 0.4, SaveOptions{}); err != nil {
			t.Fatalf("save unrelated memory %d: %v", i, err)
		}
		raw, err := os.ReadFile(notePath)
		if err != nil {
			t.Fatalf("read note after save %d: %v", i, err)
		}
		if got := strings.Count(string(raw), "\n## Links\n"); got != 1 {
			t.Fatalf("links section count after save %d = %d, want 1", i, got)
		}
	}
}

func TestMemoryNoteLoadDropsRepeatedGeneratedSections(t *testing.T) {
	dir := t.TempDir()
	noteDir := filepath.Join(dir, "50_Facts")
	if err := os.MkdirAll(noteDir, 0o700); err != nil {
		t.Fatalf("mkdir note dir: %v", err)
	}
	raw := `---
id: mem_1
type: memory
tier: long
category: knowledge
importance: 0.9
created_at: 2026-10-31T00:00:00Z
accessed_at: 2026-10-31T00:00:00Z
links:
  - RAG
block_id: mem-1
---

# ## Memory ## Memory ## Memory

## Memory

## Memory

## Memory

^mem-1

## Links

- [[RAG]]

^mem-1
`
	path := filepath.Join(noteDir, "Memory.md")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	entry, err := store.Get("mem_1")
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if strings.Contains(entry.Content, "## Links") || strings.Contains(entry.Content, "## Memory") {
		t.Fatalf("generated sections retained as memory content: %q", entry.Content)
	}
	if len(entry.Links) != 1 || entry.Links[0] != "RAG" {
		t.Fatalf("frontmatter link was not preserved: %#v", entry.Links)
	}
}
