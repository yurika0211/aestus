package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSummarizeRejectsIncompatibleMetadata(t *testing.T) {
	for _, kind := range []string{"scope", "expiry", "state", "missing"} {
		t.Run(kind, func(t *testing.T) {
			s, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			expiry := time.Now().Add(time.Hour)
			a := SaveOptions{Tags: []string{"scope:personal", "scope:user:test:a"}, ExpiresAt: &expiry}
			b := a
			switch kind {
			case "scope":
				b.Tags = []string{"scope:personal", "scope:user:test:b"}
			case "expiry":
				other := expiry.Add(time.Hour)
				b.ExpiresAt = &other
			case "state":
				b.StateKey = "project.path"
			}
			first, err := s.SaveWithOptionsResult("Orion launch requires a staging checklist.", "project", TierShort, .5, a)
			if err != nil {
				t.Fatal(err)
			}
			second, err := s.SaveWithOptionsResult("Borealis deployment uses the violet cluster.", "project", TierShort, .5, b)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{first.ID, second.ID}
			if kind == "missing" {
				ids = append(ids, "missing")
			}
			before := s.Count()
			if err := s.Summarize(ids, "Combined project notes", "project"); err == nil {
				t.Fatal("incompatible sources were summarized")
			}
			if s.Count() != before {
				t.Fatal("rejected summary mutated the vault")
			}
			for _, id := range ids[:2] {
				if _, err := s.Get(id); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCompactShortTermPreservesScopeExpiryAndNewest(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(time.Hour)
	until := expiry.Add(-time.Minute)
	newest := map[string]string{}
	for _, owner := range []string{"alice", "bob"} {
		for i := 0; i < 7; i++ {
			result, err := s.SaveWithOptionsResult(fmt.Sprintf("%s project record number %d.", owner, i), "project", TierShort, .7,
				SaveOptions{Tags: []string{"scope:personal", "scope:user:test:" + owner}, ExpiresAt: &expiry, ValidUntil: &until, Confidence: .8})
			if err != nil {
				t.Fatal(err)
			}
			// Deterministic creation order, independently of filesystem clock resolution.
			s.entries[result.ID].CreatedAt = time.Now().Add(time.Duration(i-7) * time.Minute)
			newest[owner] = result.ID
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CompactShortTerm(context.Background(), CompactionOptions{KeepRecent: 5, MaxSummaryRunes: 500})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(s.ByTier(TierShort)); got != 10 {
		t.Fatalf("short facts = %d", got)
	}
	if got := len(s.ByTier(TierMedium)); got != 2 {
		t.Fatalf("summaries = %d", got)
	}
	for owner, id := range newest {
		if _, err := s.Get(id); err != nil {
			t.Fatalf("newest %s was compacted: %v", owner, err)
		}
	}
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range reopened.ByTier(TierMedium) {
		if entry.ExpiresAt == nil || !entry.ExpiresAt.Equal(expiry) || entry.ValidUntil == nil || !entry.ValidUntil.Equal(until) {
			t.Fatalf("lifecycle lost: %+v", entry)
		}
		if len(entry.SummaryOf) != 2 || len(entry.Tags) != 2 || entry.Confidence != .8 {
			t.Fatalf("metadata lost: %+v", entry)
		}
		if strings.Contains(entry.Content, "alice") && strings.Contains(entry.Content, "bob") {
			t.Fatal("scopes mixed")
		}
		if strings.Contains(entry.Content, "number 6") {
			t.Fatal("newest fact summarized")
		}
	}
}

func TestCompactShortTermDoesNotTruncateOrMutateAfterCancellation(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("完整事实", 200)
	r, err := s.SaveWithOptionsResult(long, "project", TierShort, .5, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	count, err := s.CompactShortTerm(context.Background(), CompactionOptions{KeepRecent: 0, MaxSummaryRunes: 50})
	if err != nil || count != 0 {
		t.Fatalf("oversize compaction = %d, %v", count, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.CompactShortTerm(ctx, CompactionOptions{KeepRecent: 0, MaxSummaryRunes: 4000}); err != context.Canceled {
		t.Fatalf("cancel = %v", err)
	}
	entry, err := s.Get(r.ID)
	if err != nil || entry.Content != long {
		t.Fatal("source lost")
	}
}

func TestCompactShortTermWriteFailureKeepsSources(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.SaveWithOptionsResult("Orion project uses the amber cluster.", "project", TierShort, .5, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, dir+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("block writes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompactShortTerm(context.Background(), CompactionOptions{KeepRecent: 0, MaxSummaryRunes: 500}); err == nil {
		t.Fatal("expected persistence error")
	}
	if _, err := s.Get(r.ID); err != nil {
		t.Fatal("source removed after failed summary write")
	}
	if _, err := os.Stat(filepath.Join(dir+".saved", filepath.FromSlash(r.Path))); err != nil {
		t.Fatal("source file lost")
	}
}
