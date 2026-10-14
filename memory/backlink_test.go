package memory

import (
	"strings"
	"testing"
)

func TestBacklinksForKeepsEachMentionAndDoesNotChangeActivation(t *testing.T) {
	store := seedVault(t)
	if err := store.SaveWithOptions("目标笔记", "fact", TierLong, 0.9, SaveOptions{Aliases: []string{"目标"}}); err != nil {
		t.Fatalf("save target: %v", err)
	}
	var targetID string
	for _, entry := range store.entries {
		if strings.Contains(entry.Content, "目标笔记") {
			targetID = entry.ID
		}
	}
	source := "第一句提到 [[" + targetID + "]].\n第二句也提到 [[" + targetID + "#标题]]."
	if err := store.SaveWithOptions(source, "fact", TierLong, 0.7, SaveOptions{}); err != nil {
		t.Fatalf("save source: %v", err)
	}

	before := store.Activate("第一句", ActivationOptions{IncludeGraph: true, UpdateAccessStats: false})
	contexts := store.BacklinksFor(targetID)
	after := store.Activate("第一句", ActivationOptions{IncludeGraph: true, UpdateAccessStats: false})
	if len(contexts) != 2 {
		t.Fatalf("contexts = %+v, want two mentions", contexts)
	}
	if contexts[0].Line == contexts[1].Line || contexts[0].Excerpt == contexts[1].Excerpt {
		t.Fatalf("mentions were merged: %+v", contexts)
	}
	if !strings.Contains(contexts[0].Excerpt, "[[") || contexts[1].Heading != "标题" {
		t.Fatalf("context lost the written link: %+v", contexts)
	}
	if activationSignature(before) != activationSignature(after) {
		t.Fatalf("backlink lookup changed activation\nbefore %s\nafter %s", activationSignature(before), activationSignature(after))
	}
}

func activationSignature(scores []ActivationScore) string {
	var b strings.Builder
	for _, score := range scores {
		b.WriteString(score.EntryID)
		b.WriteByte(':')
		b.WriteString(strings.TrimSpace(strings.ReplaceAll(score.Entry.Content, "\n", " ")))
		b.WriteByte(';')
	}
	return b.String()
}
