package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAllowsUniqueNote(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("LuckyAgent keeps one durable fact.", "50_Facts"); err != nil {
		t.Fatal(err)
	}
	if got := s.Search("durable fact"); len(got) != 1 {
		t.Fatalf("saved notes = %+v", got)
	}
}

func TestSaveAllowsSingleLinksSection(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	content := "## Memory\n\nOne fact.\n\n## Links\n\n- [[LuckyAgent]]\n"
	if err := s.Save(content, "70_Concepts"); err != nil {
		t.Fatal(err)
	}
	if got := s.Search("One fact"); len(got) != 1 {
		t.Fatalf("single links note = %+v", got)
	}
}

func TestSaveRejectsRepeatedMemoryLinksBlock(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("kept", "50_Facts"); err != nil {
		t.Fatal(err)
	}
	before := markdownCount(t, dir)
	blob := strings.TrimSpace(`## Memory

## Links

- [[LuckyAgent]]
- [[真实 Subagent 调用链（delegate_task 现场）]]
- [[Agent工作流完善法：规则-规划-验证-复用]]

^concept-luckyagent-long-running-tasks`)
	content := strings.Repeat(blob+"\n\n", repeatedBlockThreshold)
	err = s.Save(content, "70_Concepts")
	if !errors.Is(err, ErrRepeatedMemoryBlock) {
		t.Fatalf("error = %v", err)
	}
	if got := markdownCount(t, dir); got != before {
		t.Fatalf("vault markdown files = %d, want %d", got, before)
	}
	if found := s.Search("delegate_task"); len(found) != 0 {
		t.Fatalf("repeated note was stored: %+v", found)
	}
}

func markdownCount(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}
