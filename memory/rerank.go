// Optional second-stage ranking over a small recalled pool.
//
// The first stage still scores each note on its own. This stage only compares
// notes that stage already kept, then cuts the pool down. It is off unless a
// search asks for a rerank pool, and it never reads notes outside that pool.
package memory

import "strings"

// rerankRecalledPairs compares notes inside pool and returns them in the new
// order, cut to limit. A pool of fewer than two notes is already ordered.
func rerankRecalledPairs(pool []ActivationScore, limit int) []ActivationScore {
	if len(pool) < 2 {
		return pool
	}
	n := len(pool)
	aggregate := make([]float64, n)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if !relatedPair(pool[i], pool[j]) {
				continue
			}
			// Related notes are one piece of evidence. Each one is lifted past the
			// unrelated notes between them, and neither is lifted past the other:
				// subtracting the two preferences would cancel the pair out.
				aggregate[i] += pairwiseLift(pool, i, j)
				aggregate[j] += pairwiseLift(pool, j, i)
		}
	}
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	for a := 0; a < n; a++ {
		for b := a + 1; b < n; b++ {
			if betterPairwiseRank(pool, aggregate, order[b], order[a]) {
				order[a], order[b] = order[b], order[a]
			}
		}
	}
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]ActivationScore, limit)
	for i := 0; i < limit; i++ {
		score := pool[order[i]]
		score.Score += aggregate[order[i]] / float64(n-1)
		out[i] = score
	}
	return out
}

// relatedPair reports notes that name each other or record the same fact.
// A shared tag is not enough: it also connects unrelated chatter.
func relatedPair(left, right ActivationScore) bool {
	return sameState(left.Entry, right.Entry) || linkedPair(left, right) || aliasOverlap(left.Entry, right.Entry)
}

// pairwiseLift is how far note i has to move to stay with note j. Notes already
// ahead of their related note, and notes that only repeat common query words,
// stay where the first stage put them.
func pairwiseLift(pool []ActivationScore, i, j int) float64 {
	if pool[i].Score >= pool[j].Score {
		return 0
	}
	return pool[j].Score - pool[i].Score + 0.001
}

func betterPairwiseRank(pool []ActivationScore, aggregate []float64, left, right int) bool {
	if aggregate[left] != aggregate[right] {
		return aggregate[left] > aggregate[right]
	}
	if pool[left].Score != pool[right].Score {
		return pool[left].Score > pool[right].Score
	}
	return pool[left].Entry.CreatedAt.After(pool[right].Entry.CreatedAt)
}

func sameState(left, right Entry) bool {
	key := strings.TrimSpace(left.StateKey)
	return key != "" && strings.EqualFold(key, strings.TrimSpace(right.StateKey))
}

func linkedPair(left, right ActivationScore) bool {
	return linksTo(left, right) || linksTo(right, left)
}

func linksTo(from, to ActivationScore) bool {
	for _, path := range from.Paths {
		if path.FromID == to.EntryID && path.Kind != "shared_tag" {
			return true
		}
	}
	return false
}

func aliasOverlap(left, right Entry) bool {
	seen := make(map[string]bool, len(left.Aliases))
	for _, alias := range left.Aliases {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias != "" {
			seen[alias] = true
		}
	}
	for _, alias := range right.Aliases {
		if seen[strings.ToLower(strings.TrimSpace(alias))] {
			return true
		}
	}
	return false
}

func hasLocatedExcerpt(score ActivationScore) bool {
	for _, excerpt := range score.Excerpts {
		if !excerpt.Missing && (excerpt.Heading != "" || excerpt.Block != "") {
			return true
		}
	}
	return false
}
