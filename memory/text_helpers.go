package memory

import "github.com/yurika0211/aestus/internal/textutil"

func truncateField(s string, maxLen int) string {
	return textutil.TrimToRunes(s, maxLen)
}

func dedupSlice(items []string) []string {
	return textutil.DedupNonEmptyStrings(items)
}
