package memory

import (
	"strings"
	"testing"
	"unicode"
)

func termSet(terms []string) map[string]bool {
	set := make(map[string]bool, len(terms))
	for _, term := range terms {
		set[term] = true
	}
	return set
}

// hanTerms keeps only the terms that are pure Han, so assertions about Chinese
// segmentation are not disturbed by alias expansion (女儿 also yields daughter,
// child and family; 过敏 yields allergy, pollen and hay fever).
func hanTerms(terms []string) []string {
	var out []string
	for _, term := range terms {
		if term == "" {
			continue
		}
		allHan := true
		for _, r := range term {
			if !unicode.Is(unicode.Han, r) {
				allHan = false
				break
			}
		}
		if allHan {
			out = append(out, term)
		}
	}
	return out
}

func runeLen(s string) int { return len([]rune(s)) }

func assertHasTerms(t *testing.T, terms []string, want ...string) {
	t.Helper()
	set := termSet(terms)
	for _, term := range want {
		if !set[term] {
			t.Errorf("missing term %q in %v", term, terms)
		}
	}
}

func assertNoTerms(t *testing.T, terms []string, unwanted ...string) {
	t.Helper()
	set := termSet(terms)
	for _, term := range unwanted {
		if set[term] {
			t.Errorf("unexpected term %q in %v", term, terms)
		}
	}
}

// An 8-character Han run used to expand into every 2/3/4-gram, yielding 18
// terms of which 11 were artifacts. Long runs now contribute bigrams only, so
// no 3- or 4-gram survives and the word-level bigrams are still present.
//
// Cross-boundary bigrams (儿花, 粉过, 敏出) are deliberately kept: without a
// dictionary there is no way to tell them from real words, and Chinese
// retrieval conventionally indexes bigrams. They are harmless because they do
// not occur in real content, so they never contribute a term hit; what actually
// cost precision was the sheer term count, which tripped the termHits bonus.
func TestHanQueryTermsDropsLongNgrams(t *testing.T) {
	terms := hanTerms(extractQueryTerms("女儿花粉过敏出门"))

	assertHasTerms(t, terms, "女儿", "花粉", "过敏", "出门")
	assertNoTerms(t, terms, "女花粉", "儿花粉", "花粉过", "女儿花粉", "花粉过敏", "儿花粉过")

	for _, term := range terms {
		if runeLen(term) > 2 {
			t.Errorf("expected bigrams only for a long run, got %q", term)
		}
	}
	if len(terms) > 7 {
		t.Errorf("expected at most 7 bigrams for an 8-char run, got %d: %v", len(terms), terms)
	}
}

// 和 and 吗 are grammatical and mark where one real segment ends and the next
// begins, so they must not appear inside any emitted term.
func TestHanQueryTermsSplitsOnParticles(t *testing.T) {
	terms := hanTerms(extractQueryTerms("明天下午适合和女儿出门吗"))

	assertHasTerms(t, terms, "女儿", "出门")
	assertNoTerms(t, terms, "合和", "和女", "门吗", "出门吗")

	for _, term := range terms {
		if strings.ContainsAny(term, "和吗") {
			t.Errorf("term %q still contains a particle", term)
		}
	}
}

// Runs of 2 to 4 characters keep the whole run so an exact concept still
// matches, in addition to its bigrams.
func TestHanQueryTermsKeepsShortRunWhole(t *testing.T) {
	terms := extractQueryTerms("当前状态")

	assertHasTerms(t, terms, "当前状态", "当前", "状态")
}

// A lone Han character carries no retrievable meaning on its own.
func TestHanQueryTermsDropsSingleCharacterRuns(t *testing.T) {
	if terms := hanQueryTerms([]rune("猫")); len(terms) != 0 {
		t.Errorf("expected no terms for a single character, got %v", terms)
	}
}

// 过 is excluded from the stop set on purpose: pulling it out would split
// 过敏 (allergy), which is one of the most load-bearing terms in the corpus.
// This is the regression guard for that decision.
func TestHanQueryTermsKeepsAllergyTermIntact(t *testing.T) {
	terms := hanTerms(extractQueryTerms("过敏"))

	assertHasTerms(t, terms, "过敏")
	for _, term := range terms {
		if runeLen(term) < 2 {
			t.Errorf("expected no single-character fragments, got %q", term)
		}
	}
}

// Latin tokenisation must be untouched by the Han changes.
func TestLatinQueryTermsUnchanged(t *testing.T) {
	terms := extractQueryTerms("Memory Benchmark")

	assertHasTerms(t, terms, "memory", "benchmark")
	assertNoTerms(t, terms, "memory benchmark")
}

// Mixed queries must still yield both scripts, and the Latin part must not be
// glued to the Han part.
func TestHanQueryTermsMixedScript(t *testing.T) {
	terms := extractQueryTerms("transcription 转录 状态")

	assertHasTerms(t, terms, "transcription", "转录", "状态")
}