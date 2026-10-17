package memory

import (
	"context"
	"sync"
	"testing"
)

func TestSDKConcurrentGetAndDuplicateWrite(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request := RememberRequest{Content: "project runtime directory", Category: "project", Tier: TierLong, Importance: .9,
		Options: SaveOptions{RoutePolicies: []RoutePolicy{{ID: "inspect", Match: RoutePolicyMatch{QueryAll: []RouteTermGroup{{Any: []string{"runtime"}}}}, RequiredTools: []RouteToolRequirement{{Name: "read"}}}}}}
	first, err := store.Remember(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	errs := make(chan error, 1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			if _, err := store.Remember(context.Background(), request); err != nil {
				errs <- err
				return
			}
		}
	}()
	for i := 0; i < 1000; i++ {
		entry, err := store.Get(first.ID)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Content == "" || entry.AccessedAt.IsZero() || entry.RoutePolicies[0].Match.QueryAll[0].Any[0] != "runtime" || entry.RoutePolicies[0].RequiredTools[0].Name != "read" {
			t.Error("incomplete record snapshot")
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
