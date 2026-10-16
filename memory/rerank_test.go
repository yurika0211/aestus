package memory

import (
	"strings"
	"testing"
)

func TestPairwiseRerankKeepsLinkedEvidenceAheadOfLexicalChatter(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.SaveWithOptions("Brooks Ghost 跑鞋是这次买的。", "fact", TierLong, 0.9, SaveOptions{
		StateKey: "purchase.shoes",
		Aliases:  []string{"跑鞋"},
	}); err != nil {
		t.Fatalf("save evidence: %v", err)
	}
	var evidenceID string
	for _, entry := range store.Search("Brooks Ghost") {
		evidenceID = entry.ID
		break
	}
	if evidenceID == "" {
		t.Fatal("evidence id missing")
	}
	if err := store.SaveWithOptions("另一段记得也是这双 [["+evidenceID+"]].", "fact", TierLong, 0.4, SaveOptions{
		StateKey: "purchase.shoes",
	}); err != nil {
		t.Fatalf("save linked evidence: %v", err)
	}
	// Chatter contains the query words more times, so first-stage lexical order
	// puts it ahead of the note that only names the evidence.
	for i, chatter := range []string{
		"跑鞋 跑鞋 跑鞋，看了一整晚跑鞋广告。",
		"跑鞋评测节目提到很多跑鞋。",
		"朋友圈里都在讨论跑鞋。",
		"跑鞋店打折，顺便看看跑鞋。",
	} {
		if err := store.SaveWithOptions(chatter, "fact", TierLong, 0.9, SaveOptions{}); err != nil {
			t.Fatalf("save chatter %d: %v", i, err)
		}
	}

	firstStage := store.SearchWithOptions("跑鞋", SearchOptions{
		Limit:           5,
		IncludeGraph:    true,
		GraphDepth:      1,
		SkipAccessStats: true,
	})
	if containsContent(firstStage, "另一段记得") {
		t.Fatal("fixture no longer needs reranking; the linked note is already in the first five")
	}

	reranked := store.SearchWithOptions("跑鞋", SearchOptions{
		Limit:           5,
		IncludeGraph:    true,
		GraphDepth:      1,
		SkipAccessStats: true,
		RerankPool:      20,
		RerankLimit:     5,
	})
	if len(reranked) != 5 {
		t.Fatalf("reranked %d notes, want 5", len(reranked))
	}
	if !containsContent(reranked, "Brooks Ghost") || !containsContent(reranked, "另一段记得") {
		t.Fatalf("reranked lost linked evidence: %#v", contents(reranked))
	}
}

func TestPairwiseRerankIsOffUnlessRequested(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.SaveWithOptions("跑鞋在柜子里。", "fact", TierLong, 0.5, SaveOptions{}); err != nil {
		t.Fatalf("save: %v", err)
	}
	plain := store.SearchWithOptions("跑鞋", SearchOptions{Limit: 5, SkipAccessStats: true})
	zero := store.SearchWithOptions("跑鞋", SearchOptions{Limit: 5, SkipAccessStats: true, RerankPool: 0})
	if len(plain) != 1 || len(zero) != 1 || plain[0].Entry.ID != zero[0].Entry.ID {
		t.Fatalf("default search changed: plain=%#v zero=%#v", plain, zero)
	}
	if delta := plain[0].DirectScore - zero[0].DirectScore; delta < -1e-9 || delta > 1e-9 {
		t.Fatalf("lexical score changed: plain=%v zero=%v", plain[0].DirectScore, zero[0].DirectScore)
	}
}

func TestPairwiseLiftDoesNotDependOnPairOrder(t *testing.T) {
	pool := []ActivationScore{
		{EntryID: "a", Score: 2, Entry: Entry{ID: "a", StateKey: "purchase.shoes"}},
		{EntryID: "b", Score: 0.3, Entry: Entry{ID: "b", StateKey: "purchase.shoes"}},
	}
	if !relatedPair(pool[0], pool[1]) || !relatedPair(pool[1], pool[0]) {
		t.Fatal("the same state key must relate the pair in both directions")
	}
	if pairwiseLift(pool, 0, 1) != 0 {
		t.Fatal("the note already ahead of its related note was lifted")
	}
	if pairwiseLift(pool, 1, 0) <= 0 {
		t.Fatal("the note behind its related note was not lifted")
	}
}

func containsContent(results []SearchResult, needle string) bool {
	for _, result := range results {
		if strings.Contains(result.Entry.Content, needle) {
			return true
		}
	}
	return false
}

func contents(results []SearchResult) []string {
	out := make([]string, len(results))
	for i, result := range results {
		out[i] = result.Entry.Content
	}
	return out
}
