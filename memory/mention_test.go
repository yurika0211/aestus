package memory

import (
	"strings"
	"testing"
)

func TestUnlinkedMentionIsListedButDoesNotChangeGraphOrActivation(t *testing.T) {
	store := seedVault(t)
	if err := store.SaveWithOptions("登录失败笔记", "fact", TierLong, 0.9, SaveOptions{}); err != nil {
		t.Fatalf("save target: %v", err)
	}
	var targetID string
	for _, entry := range store.entries {
		if strings.Contains(entry.Content, "登录失败笔记") {
			targetID = entry.ID
		}
	}
	if err := store.SaveWithOptions("正文提到了登录失败笔记，但没有写成链接。", "fact", TierLong, 0.7, SaveOptions{}); err != nil {
		t.Fatalf("save source: %v", err)
	}

	before := store.Activate("正文提到了", ActivationOptions{IncludeGraph: true, UpdateAccessStats: false})
	mentions := store.UnlinkedMentions()
	after := store.Activate("正文提到了", ActivationOptions{IncludeGraph: true, UpdateAccessStats: false})
	if len(mentions) != 1 || mentions[0].TargetID != targetID || !strings.Contains(mentions[0].Excerpt, "登录失败笔记") {
		t.Fatalf("mentions = %+v", mentions)
	}

	topology := store.GraphTopology(GraphTopologyOptions{IncludeIsolated: true})
	for _, edge := range topology.Edges {
		if edge.Target == targetID {
			t.Fatalf("unlinked mention became an edge: %+v", edge)
		}
	}
	if activationSignature(before) != activationSignature(after) {
		t.Fatalf("mention lookup changed activation")
	}
	for _, score := range after {
		if score.EntryID == targetID && score.Components.GraphBoost > 0 {
			t.Fatalf("unlinked mention boosted the target by %f", score.Components.GraphBoost)
		}
	}
}

func TestUnlinkedMentionSkipsShortPartialAndNumericNames(t *testing.T) {
	store := seedVault(t)
	if err := store.SaveWithOptions("login note", "fact", TierLong, 0.9, SaveOptions{Aliases: []string{"ab", "1234", "log"}}); err != nil {
		t.Fatalf("save target: %v", err)
	}
	if err := store.SaveWithOptions("这里有 ab、1234，也有 login 这个更长的词。", "fact", TierLong, 0.7, SaveOptions{}); err != nil {
		t.Fatalf("save source: %v", err)
	}
	mentions := store.UnlinkedMentions()
	for _, mention := range mentions {
		if mention.Name == "ab" || mention.Name == "1234" || mention.Name == "log" {
			t.Fatalf("partial, short, or numeric name listed: %+v", mention)
		}
	}
}
