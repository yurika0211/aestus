package memory

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yurika0211/aestus/memory/policy"
)

func TestActivateReturnsExplainableComponents(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.SaveWithOptions(
		"User prefers [[Telegram Gateway]] for message delivery.",
		"preference",
		TierLong,
		0.9,
		SaveOptions{
			Tags:    []string{"gateway"},
			Aliases: []string{"telegram channel"},
		},
	); err != nil {
		t.Fatalf("SaveWithOptions: %v", err)
	}

	scores := store.Activate("telegram channel", ActivationOptions{
		IncludeGraph:      true,
		UpdateAccessStats: false,
	})
	if len(scores) != 1 {
		t.Fatalf("expected 1 activation, got %#v", scores)
	}
	components := scores[0].Components
	if components.MatchScore() <= 0 {
		t.Fatalf("expected positive match score: %#v", components)
	}
	if components.Aliases <= 0 {
		t.Fatalf("expected alias component, got %#v", components)
	}
	if components.Links <= 0 {
		t.Fatalf("expected wikilink component, got %#v", components)
	}
	if components.Importance != 0.9 {
		t.Fatalf("expected importance component 0.9, got %f", components.Importance)
	}
	if components.Recency <= 0 || components.Access <= 0 {
		t.Fatalf("expected positive recency/access components, got %#v", components)
	}
}

func TestActivateRecordsGraphPaths(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.SaveWithTierAndTags("Outdoor walks often include [[Daughter]].", "plan", TierMedium, 0.6, []string{"family"}); err != nil {
		t.Fatalf("save plan: %v", err)
	}
	if err := store.SaveWithTierAndTags("[[Daughter]] has [[Pollen Allergy]].", "health", TierLong, 0.95, []string{"health"}); err != nil {
		t.Fatalf("save health: %v", err)
	}

	scores := store.Activate("Outdoor walks", ActivationOptions{
		IncludeGraph:      true,
		UpdateAccessStats: false,
		Explain:           true,
	})
	if len(scores) < 2 {
		t.Fatalf("expected graph activation to add related memory, got %#v", scores)
	}
	for _, score := range scores {
		if strings.Contains(score.Entry.Content, "Pollen Allergy") {
			if score.Components.GraphBoost <= 0 {
				t.Fatalf("expected graph boost for propagated memory, got %#v", score.Components)
			}
			if len(score.Paths) == 0 {
				t.Fatalf("expected activation path for propagated memory")
			}
			return
		}
	}
	t.Fatalf("expected propagated allergy memory, got %#v", scores)
}

func TestActivateSpreadsMultipleHops(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.SaveWithTierAndTags("Sprint planning notes mention [[Falcon Initiative]].", "planning", TierMedium, 0.6, []string{"planning"}); err != nil {
		t.Fatalf("save seed: %v", err)
	}
	if err := store.SaveWithTierAndTags("[[Falcon Initiative]] depends on [[Nimbus Cluster]] for compute.", "engineering", TierMedium, 0.6, []string{"engineering"}); err != nil {
		t.Fatalf("save hop1: %v", err)
	}
	if err := store.SaveWithTierAndTags("[[Nimbus Cluster]] outages are tracked in the ops channel.", "ops", TierMedium, 0.6, []string{"ops"}); err != nil {
		t.Fatalf("save hop2: %v", err)
	}

	findClusterScore := func(scores []ActivationScore) (ActivationScore, bool) {
		for _, score := range scores {
			if strings.Contains(score.Entry.Content, "ops channel") {
				return score, true
			}
		}
		return ActivationScore{}, false
	}

	shallow := store.Activate("Sprint planning", ActivationOptions{
		IncludeGraph:      true,
		MaxGraphDepth:     1,
		UpdateAccessStats: false,
		Explain:           true,
	})
	if _, ok := findClusterScore(shallow); ok {
		t.Fatalf("expected depth-1 spread to NOT reach the second hop, got %#v", shallow)
	}

	deep := store.Activate("Sprint planning", ActivationOptions{
		IncludeGraph:      true,
		MaxGraphDepth:     2,
		UpdateAccessStats: false,
		Explain:           true,
	})
	score, ok := findClusterScore(deep)
	if !ok {
		t.Fatalf("expected depth-2 spread to reach the second hop, got %#v", deep)
	}
	if score.Components.GraphBoost <= 0 {
		t.Fatalf("expected graph boost for two-hop memory, got %#v", score.Components)
	}
	foundDepthTwo := false
	for _, path := range score.Paths {
		if path.Depth == 2 {
			foundDepthTwo = true
			break
		}
	}
	if !foundDepthTwo {
		t.Fatalf("expected an activation path recorded at depth 2, got %#v", score.Paths)
	}
}

func TestSearchParallelUsesUnifiedActivation(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.SaveWithTierAndTags("Outdoor walks often include [[Daughter]].", "plan", TierMedium, 0.6, []string{"family"}); err != nil {
		t.Fatalf("save plan: %v", err)
	}
	if err := store.SaveWithTierAndTags("[[Daughter]] has [[Pollen Allergy]].", "health", TierLong, 0.95, []string{"health"}); err != nil {
		t.Fatalf("save health: %v", err)
	}

	results := store.SearchParallel("Outdoor walks", 3)
	if len(results) < 2 {
		t.Fatalf("expected SearchParallel to include graph-propagated memory, got %#v", results)
	}
	for _, result := range results {
		if strings.Contains(result.Content, "Pollen Allergy") {
			return
		}
	}
	t.Fatalf("expected propagated allergy memory, got %#v", results)
}

func TestActivateLimitReturnsOrderedTopK(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	cases := []struct {
		content    string
		importance float64
	}{
		{content: "ranked memory low", importance: 0.2},
		{content: "ranked memory high", importance: 0.9},
		{content: "ranked memory mid", importance: 0.6},
	}
	for _, tc := range cases {
		if err := store.SaveWithTierAndTags(tc.content, "ranking", TierLong, tc.importance, []string{"ranked"}); err != nil {
			t.Fatalf("save %q: %v", tc.content, err)
		}
	}

	scores := store.Activate("ranked memory", ActivationOptions{
		Limit:             2,
		IncludeGraph:      false,
		UpdateAccessStats: false,
	})
	if len(scores) != 2 {
		t.Fatalf("expected top 2 scores, got %#v", scores)
	}
	if !strings.Contains(scores[0].Entry.Content, "high") || !strings.Contains(scores[1].Entry.Content, "mid") {
		t.Fatalf("expected high then mid, got %#v", scores)
	}
	if scores[0].Score < scores[1].Score {
		t.Fatalf("expected descending scores, got %#v", scores)
	}
}

type testActivationReranker struct {
	needle string
}

func (r testActivationReranker) RerankMemoryActivations(query string, scores []ActivationScore, now time.Time) []ActivationScore {
	out := make([]ActivationScore, len(scores))
	copy(out, scores)
	for i := range out {
		if strings.Contains(out[i].Entry.Content, r.needle) {
			out[i].Score *= 10
			out[i].Components.TidalBoost = 1
		}
	}
	return out
}

type testFeedbackReranker struct {
	feedback []ActivationFeedback
}

func (r *testFeedbackReranker) RerankMemoryActivations(query string, scores []ActivationScore, now time.Time) []ActivationScore {
	return scores
}

func (r *testFeedbackReranker) ObserveActivationFeedback(feedback ActivationFeedback) {
	r.feedback = append(r.feedback, feedback)
}

func TestActivationRerankerIsPluggable(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.SaveWithTierAndTags("ranked memory alpha", "ranking", TierLong, 0.8, []string{"ranked"}); err != nil {
		t.Fatalf("save alpha: %v", err)
	}
	if err := store.SaveWithTierAndTags("ranked memory beta", "ranking", TierLong, 0.2, []string{"ranked"}); err != nil {
		t.Fatalf("save beta: %v", err)
	}

	store.SetActivationReranker(testActivationReranker{needle: "beta"})
	scores := store.Activate("ranked memory", ActivationOptions{
		Limit:             2,
		IncludeGraph:      false,
		UpdateAccessStats: false,
	})
	if len(scores) != 2 {
		t.Fatalf("expected two scores, got %#v", scores)
	}
	if !strings.Contains(scores[0].Entry.Content, "beta") {
		t.Fatalf("expected reranker to move beta first, got %#v", scores)
	}
	if scores[0].Components.TidalBoost <= 0 {
		t.Fatalf("expected reranker component to be visible, got %#v", scores[0].Components)
	}

	store.SetActivationReranker(nil)
	if store.ActivationReranker() != nil {
		t.Fatal("expected nil reranker after reset")
	}
}

func TestActivationFeedbackObserverIsPluggable(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	entry := Entry{
		ID:        "feedback-entry",
		Content:   "context selected memory",
		Category:  "test",
		Tier:      TierLong,
		CreatedAt: time.Now(),
	}
	reranker := &testFeedbackReranker{}
	store.SetActivationReranker(reranker)

	store.RecordActivationFeedback("context query", []Entry{entry}, "context_selected", 0.15, entry.CreatedAt)
	if len(reranker.feedback) != 1 {
		t.Fatalf("expected one feedback event, got %#v", reranker.feedback)
	}
	got := reranker.feedback[0]
	if got.Query != "context query" || got.Entry.ID != entry.ID || got.Signal != "context_selected" || got.Value != 0.15 {
		t.Fatalf("unexpected feedback: %#v", got)
	}
}

func TestActivateLimitsGraphSeedsToStrongestDirectMatches(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for i := 0; i < 8; i++ {
		if err := store.SaveWithTierAndTags(
			fmt.Sprintf("low priority benchmark note %d", i),
			"benchmark",
			TierShort,
			0.1,
			[]string{"benchmark"},
		); err != nil {
			t.Fatalf("save noise: %v", err)
		}
	}
	if err := store.SaveWithTierAndTags("priority benchmark entry links [[Critical Node]].", "benchmark", TierLong, 0.95, []string{"benchmark"}); err != nil {
		t.Fatalf("save priority: %v", err)
	}
	if err := store.SaveWithTierAndTags("[[Critical Node]] carries high-value routed evidence.", "evidence", TierLong, 0.95, []string{"critical"}); err != nil {
		t.Fatalf("save evidence: %v", err)
	}

	scores := store.Activate("benchmark", ActivationOptions{
		Limit:             4,
		IncludeGraph:      true,
		MaxGraphSeeds:     1,
		UpdateAccessStats: false,
	})
	for _, score := range scores {
		if strings.Contains(score.Entry.Content, "high-value routed evidence") {
			if score.Components.GraphBoost <= 0 {
				t.Fatalf("expected graph boost from strongest seed, got %#v", score.Components)
			}
			return
		}
	}
	t.Fatalf("expected strongest seed to propagate critical evidence, got %#v", scores)
}

func TestActivateExcerptFollowsBlockLink(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	target := "## 失败原因\n凭据过期\n^block-9\n\n## 无关配置\n不要把这一节注入"
	if err := store.SaveWithOptions(target, "fact", TierLong, 0.9, SaveOptions{}); err != nil {
		t.Fatalf("save target: %v", err)
	}
	var targetID string
	for _, entry := range store.entries {
		if strings.Contains(entry.Content, "凭据过期") {
			targetID = entry.ID
		}
	}
	if targetID == "" {
		t.Fatal("target id missing")
	}
	if err := store.SaveWithOptions("排障指向 [["+targetID+"#^block-9]]", "fact", TierLong, 0.8, SaveOptions{}); err != nil {
		t.Fatalf("save source: %v", err)
	}

	scores := store.Activate("排障指向", ActivationOptions{IncludeGraph: true, UpdateAccessStats: false, Explain: true})
	var found bool
	for _, score := range scores {
		if !strings.Contains(score.Entry.Content, "凭据过期") {
			continue
		}
		found = true
		if len(score.Excerpts) != 1 {
			t.Fatalf("excerpts = %+v, want the linked block", score.Excerpts)
		}
		excerpt := score.Excerpts[0]
		if excerpt.Block != "block-9" || excerpt.Missing || !strings.Contains(excerpt.Context, "凭据过期") || strings.Contains(excerpt.Context, "无关配置") {
			t.Fatalf("excerpt = %+v, want only the block", excerpt)
		}
	}
	if !found {
		t.Fatalf("block target was not activated: %+v", scores)
	}

	route := store.Route("排障指向")
	var routed bool
	for _, entry := range route.Entries {
		if !strings.Contains(entry.Content, "凭据过期") {
			continue
		}
		routed = true
		if !strings.Contains(entry.Content, "无关配置") {
			t.Fatalf("stored note should keep the whole body, got %q", entry.Content)
		}
		selected := SelectContextText(entry.Content, activationExcerptsFromRoute(route.Excerpts[entry.ID]))
		if !strings.Contains(selected, "凭据过期") || strings.Contains(selected, "无关配置") {
			t.Fatalf("selected context = %q, want only the linked block", selected)
		}
	}
	if !routed {
		t.Fatalf("route dropped the linked note: %+v", route.Entries)
	}
}

func TestActivateMissingBlockFallsBackToWholeNote(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.SaveWithOptions("整篇登录笔记", "fact", TierLong, 0.9, SaveOptions{}); err != nil {
		t.Fatalf("save target: %v", err)
	}
	var targetID string
	for _, entry := range store.entries {
		if strings.Contains(entry.Content, "整篇登录笔记") {
			targetID = entry.ID
		}
	}
	if err := store.SaveWithOptions("排障指向 [["+targetID+"#^missing]]", "fact", TierLong, 0.8, SaveOptions{}); err != nil {
		t.Fatalf("save source: %v", err)
	}
	scores := store.Activate("排障指向", ActivationOptions{IncludeGraph: true, UpdateAccessStats: false})
	for _, score := range scores {
		if !strings.Contains(score.Entry.Content, "整篇登录笔记") {
			continue
		}
		if len(score.Excerpts) != 1 || !score.Excerpts[0].Missing || score.Excerpts[0].Context != "整篇登录笔记" {
			t.Fatalf("missing block excerpt = %+v", score.Excerpts)
		}
		selected := score.Excerpts[0].ContextText()
		if !strings.Contains(selected, "整篇登录笔记") || !strings.Contains(selected, "section not found: ^missing") {
			t.Fatalf("missing locator should keep the note and say so, got %q", selected)
		}
		return
	}
	t.Fatal("target note was not activated")
}

func activationExcerptsFromRoute(excerpts []policy.Excerpt) []ActivationExcerpt {
	out := make([]ActivationExcerpt, len(excerpts))
	for i, excerpt := range excerpts {
		out[i] = ActivationExcerpt{
			Heading: excerpt.Heading,
			Block:   excerpt.Block,
			Context: excerpt.Context,
			Missing: excerpt.Missing,
		}
	}
	return out
}
