package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestStoreRecordTurnSupportsZeroValue(t *testing.T) {
	var store Store
	event := store.RecordTurn("session-zero")
	if event.RuntimeCount != 1 || event.SessionCount != 1 {
		t.Fatalf("unexpected zero-value store event: %#v", event)
	}
	if got := store.MaintenanceCoordinator().RuntimeCount(); got != 1 {
		t.Fatalf("store runtime count = %d, want 1", got)
	}
}

func TestSDKRememberConcurrentStateReplacement(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const writers = 12
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.Remember(ctx, RememberRequest{
				Content: fmt.Sprintf("Project location is folder %d", i), Category: "project",
				Tier: TierLong, Importance: .9, Mode: WriteUpsertState,
				Options: SaveOptions{StateKey: "project.location"},
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if ids := store.ActiveStateIDs("project.location"); len(ids) != 1 {
		t.Fatalf("active states = %v; want exactly one", ids)
	}
	reopened, err := NewStore(store.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if ids := reopened.ActiveStateIDs("project.location"); len(ids) != 1 {
		t.Fatalf("persisted active states = %v; want exactly one", ids)
	}
}

func TestSDKRepeatedStateWriteKeepsCurrentNote(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	req := RememberRequest{Content: "Use /workspace/project", Category: "project", Tier: TierLong,
		Importance: .9, Mode: WriteUpsertState, Options: SaveOptions{StateKey: "project.location"}}
	first, err := store.Remember(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Remember(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != again.ID || !again.UpdatedExisting {
		t.Fatalf("not deduplicated: %+v %+v", first, again)
	}
	got, err := store.Recall(context.Background(), RecallRequest{Query: "workspace", Options: SearchOptions{SkipAccessStats: true}})
	if err != nil || len(got) != 1 || got[0].Entry.ID != first.ID {
		t.Fatalf("recall = %+v, %v", got, err)
	}
}

func TestSDKStateCanReturnToAnEarlierValue(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(value string) RememberResult {
		t.Helper()
		result, err := store.Remember(context.Background(), RememberRequest{Content: value, Category: "project", Tier: TierLong,
			Importance: .9, Mode: WriteUpsertState, Options: SaveOptions{StateKey: "directory", StateValue: value}})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := write("folder A")
	write("folder B")
	again := write("folder A")
	if again.ID == first.ID {
		t.Fatalf("returning to an earlier value overwrote its historical version: %+v %+v", first, again)
	}
	ids := store.ActiveStateIDs("directory")
	if len(ids) != 1 || ids[0] != again.ID {
		t.Fatalf("returning to an earlier value left incorrect active states: %v", ids)
	}
	got := store.ByTierWithOptions(TierLong, RouteOptions{})
	if len(got) != 1 || got[0].StateValue != "folder A" {
		t.Fatalf("current value = %+v", got)
	}
}

func TestSDKScopeAppliesBeforeTopKAndGraphWalk(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A high-scoring hidden seed links to a visible fact. Filtering only the
	// output would still allow that hidden seed to activate the unrelated fact.
	target, err := store.SaveWithOptionsResult("public beta", "fact", TierLong, .1,
		SaveOptions{Tags: []string{"public"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveWithOptionsResult("private alpha alpha", "fact", TierLong, 1,
		SaveOptions{Tags: []string{"private"}, Links: []string{target.ID}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveWithOptionsResult("alpha public allowed", "fact", TierLong, .1,
		SaveOptions{Tags: []string{"public"}})
	if err != nil {
		t.Fatal(err)
	}
	visible := func(e Entry) bool {
		for _, tag := range e.Tags {
			if tag == "public" {
				return true
			}
		}
		return false
	}
	got, err := store.Recall(context.Background(), RecallRequest{Query: "alpha", Options: SearchOptions{
		Limit: 1, IncludeGraph: true, GraphDepth: 3, Explain: true, SkipAccessStats: true, EntryFilter: visible,
	}})
	if err != nil || len(got) != 1 || got[0].Entry.Content != "alpha public allowed" {
		t.Fatalf("scope/top-K = %+v, %v", got, err)
	}
	for _, path := range got[0].Paths {
		if source, _ := store.Get(path.FromID); source != nil && !visible(*source) {
			t.Fatalf("invisible graph source: %+v", path)
		}
	}
	withoutMatch := store.SearchWithOptions("private", SearchOptions{Limit: 1, IncludeGraph: true, GraphDepth: 3, EntryFilter: visible, SkipAccessStats: true})
	if len(withoutMatch) != 0 {
		t.Fatalf("hidden seed activated visible note: %+v", withoutMatch)
	}
	unfiltered := store.SearchWithOptions("private", SearchOptions{IncludeGraph: true, GraphDepth: 3, SkipAccessStats: true})
	found := false
	for _, result := range unfiltered {
		found = found || result.Entry.ID == target.ID
	}
	if !found {
		t.Fatal("invalid fixture: hidden seed did not activate the linked public note without a filter")
	}
}

func TestSDKCoreFactsResolveWithinScopeBeforeSelectingTier(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.SaveWithOptionsResult("directory A", "project", TierLong, .9, SaveOptions{
		StateKey: "directory", Tags: []string{"a"}, ValidFrom: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveWithOptionsResult("directory B", "project", TierLong, .9, SaveOptions{StateKey: "directory", Tags: []string{"b"}}); err != nil {
		t.Fatal(err)
	}
	visible := RouteOptions{EntryFilter: func(e Entry) bool { return len(e.Tags) > 0 && e.Tags[0] == "a" }}
	if got := store.ByTierWithOptions(TierLong, visible); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("another scope hid actor A: %+v", got)
	}
	if _, err := store.SaveWithOptionsResult("directory A updated", "project", TierMedium, .9, SaveOptions{StateKey: "directory", Tags: []string{"a"}, Supersedes: []string{a.ID}}); err != nil {
		t.Fatal(err)
	}
	if got := store.ByTierWithOptions(TierLong, visible); len(got) != 0 {
		t.Fatalf("tier filtering resurrected old core fact: %+v", got)
	}
}

func TestSDKWriteScopePreventsCrossScopeDedupAndReplacement(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(scope string, supersedes []string) (RememberResult, error) {
		return store.Remember(context.Background(), RememberRequest{Content: "same project preference", Category: "preference",
			Tier: TierLong, Importance: .9, Mode: WriteUpsertState,
			Options:     SaveOptions{Tags: []string{scope}, StateKey: "preference", Supersedes: supersedes},
			EntryFilter: func(e Entry) bool { return len(e.Tags) > 0 && e.Tags[0] == scope },
		})
	}
	a, err := write("a", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := write("b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("deduplicated across scopes")
	}
	if _, err := write("b", []string{a.ID}); err == nil {
		t.Fatal("cross-scope supersede accepted")
	}
	entry, err := store.Get(a.ID)
	if err != nil || entry.Status != "active" {
		t.Fatalf("scope a was modified: %+v, %v", entry, err)
	}
}

func TestSDKCancellationAndInvalidRequestHaveNoWrite(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Remember(ctx, RememberRequest{Content: "do not save", Tier: TierLong, Importance: .9})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("write error = %v", err)
	}
	_, err = store.Recall(ctx, RecallRequest{Query: "anything"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("recall error = %v", err)
	}
	_, err = store.Remember(context.Background(), RememberRequest{Content: "invalid mode", Tier: TierLong, Importance: .9, Mode: "unknown"})
	if err == nil || store.Count() != 0 {
		t.Fatalf("invalid write mutated store: count=%d error=%v", store.Count(), err)
	}
}
