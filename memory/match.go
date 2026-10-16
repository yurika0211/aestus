// Lexical match score shared by search and temporal candidate selection.
package memory

import (
	"math"
	"strings"
	"time"
)

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// termStats is counted from the notes being searched. It is derived data:
// losing it only means the next search counts again.
type termStats struct {
	docs      int
	avgLength float64
	df        map[string]int
}

func (s *Store) termStatsLocked(now time.Time) termStats {
	stats := termStats{df: map[string]int{}}
	var totalLength int
	for _, entry := range s.entries {
		if entry == nil || isConceptEntry(entry) || !entryIsActive(entry, now) {
			continue
		}
		stats.docs++
		length := len([]rune(entry.Content))
		if length == 0 {
			length = 1
		}
		totalLength += length
		for _, term := range extractQueryTerms(entry.Content) {
			stats.df[term]++
		}
	}
	if stats.docs > 0 {
		stats.avgLength = float64(totalLength) / float64(stats.docs)
	}
	if stats.avgLength <= 0 {
		stats.avgLength = 1
	}
	return stats
}

// rarity is high for a term that appears in few notes and zero for a term
// that appears in most of them. A missing term contributes nothing.
func (s termStats) rarity(term string) float64 {
	if s.docs <= 0 {
		return 0
	}
	docsWithTerm := s.df[term]
	if docsWithTerm <= 0 {
		return 0
	}
	return math.Log(1 + (float64(s.docs)-float64(docsWithTerm)+0.5)/(float64(docsWithTerm)+0.5))
}

// termCoverage is the share of the query's term weight found in one note.
// Repeated occurrences saturate, and a long note gets less credit than a
// short note for the same occurrence count.
func termCoverage(content string, queryTerms []string, stats termStats) float64 {
	length := len([]rune(content))
	if length <= 0 {
		length = 1
	}
	normalizer := 1 - bm25B + bm25B*float64(length)/stats.avgLength
	if normalizer <= 0 {
		normalizer = 1
	}
	var weighted, total float64
	seen := map[string]bool{}
	for _, term := range queryTerms {
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		weight := stats.rarity(term)
		if weight <= 0 {
			continue
		}
		total += weight
		occurrences := strings.Count(strings.ToLower(content), term)
		if occurrences <= 0 {
			continue
		}
		saturated := float64(occurrences) * (bm25K1 + 1) / (float64(occurrences) + bm25K1*normalizer)
		weighted += weight * saturated
	}
	if total <= 0 {
		return 0
	}
	return weighted / total
}

func memoryMatchScore(e *Entry, queryLower string, queryTerms []string) float64 {
	if e == nil {
		return 0
	}
	contentLower := strings.ToLower(e.Content)
	categoryLower := strings.ToLower(e.Category)

	matchScore := 0.0
	if queryLower != "" && strings.Contains(contentLower, queryLower) {
		matchScore = 1.0
		if contentLower == queryLower {
			matchScore = 2.0
		}
	}
	if queryLower != "" && strings.Contains(categoryLower, queryLower) {
		matchScore += 0.5
	}

	termHits := 0
	for _, term := range queryTerms {
		if term == "" {
			continue
		}
		if strings.Contains(contentLower, term) {
			matchScore += 0.22
			termHits++
			continue
		}
		if strings.Contains(categoryLower, term) {
			matchScore += 0.12
			termHits++
		}
	}
	if termHits >= 2 {
		matchScore += 0.25
	}

	for _, tag := range e.Tags {
		tagLower := strings.ToLower(tag)
		if queryLower != "" && strings.Contains(tagLower, queryLower) {
			matchScore += 0.3
			break
		}
		for _, term := range queryTerms {
			if strings.Contains(tagLower, term) {
				matchScore += 0.12
				break
			}
		}
	}
	for _, alias := range e.Aliases {
		aliasLower := strings.ToLower(alias)
		if queryLower != "" && (strings.Contains(aliasLower, queryLower) || strings.Contains(queryLower, aliasLower)) {
			matchScore += 0.5
			break
		}
		for _, term := range queryTerms {
			if strings.Contains(aliasLower, term) || strings.Contains(term, aliasLower) {
				matchScore += 0.16
				break
			}
		}
	}
	for _, link := range e.Links {
		linkLower := strings.ToLower(link)
		if queryLower != "" && (strings.Contains(linkLower, queryLower) || strings.Contains(queryLower, linkLower)) {
			matchScore += 0.6
			break
		}
		for _, term := range queryTerms {
			if strings.Contains(linkLower, term) || strings.Contains(term, linkLower) {
				matchScore += 0.18
				break
			}
		}
	}
	return matchScore
}
