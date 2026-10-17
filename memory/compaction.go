package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CompactionOptions bounds lossless concatenation of older short-tier facts.
// KeepRecent applies independently to each compatible metadata group.
type CompactionOptions struct {
	KeepRecent      int
	MaxSummaryRunes int
}

// CompactShortTerm selects and replaces facts under one Store lock. Facts from
// different scopes, categories or expiry windows never share a summary. State
// and route-policy records are not eligible. This is durable-note maintenance,
// not conversation/session summarization.
func (s *Store) CompactShortTerm(ctx context.Context, opts CompactionOptions) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if s == nil {
		return 0, ErrStoreUnavailable
	}
	if opts.KeepRecent < 0 || opts.MaxSummaryRunes < 1 {
		return 0, fmt.Errorf("invalid compaction bounds")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	groups := make(map[string][]*Entry)
	now := time.Now()
	for _, entry := range s.entries {
		if entry.Tier != TierShort || !compactionEligible(entry, now) {
			continue
		}
		key := compactionKey(entry)
		groups[key] = append(groups[key], entry)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	completed := 0
	for _, key := range keys {
		entries := groups[key]
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
				return entries[i].ID < entries[j].ID
			}
			return entries[i].CreatedAt.Before(entries[j].CreatedAt)
		})
		if len(entries) <= opts.KeepRecent {
			continue
		}
		var ids, contents []string
		length := 0
		for _, entry := range entries[:len(entries)-opts.KeepRecent] {
			cost := len([]rune(entry.Content))
			if len(ids) > 0 {
				cost += 3
			}
			// Never truncate a fact and then delete its complete source.
			if length+cost > opts.MaxSummaryRunes {
				continue
			}
			ids = append(ids, entry.ID)
			contents = append(contents, entry.Content)
			length += cost
		}
		if len(ids) == 0 {
			continue
		}
		if err := s.summarizeLocked(ids, strings.Join(contents, " | "), entries[0].Category); err != nil {
			return completed, err
		}
		completed++
	}
	return completed, nil
}

func compactionEligible(entry *Entry, now time.Time) bool {
	return entryIsActive(entry, now) && !isConceptEntry(entry) &&
		entry.StateKey == "" && entry.StateValue == "" && len(entry.Supersedes) == 0 && len(entry.RoutePolicies) == 0
}

func compactionKey(entry *Entry) string {
	tags := make([]string, 0, len(entry.Tags))
	for _, tag := range entry.Tags {
		tag = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(tag)), "#")
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	tags = dedupSlice(tags)
	sort.Strings(tags)
	// ValidFrom may differ for already-active facts; the result uses the latest
	// start. End bounds must match so no constituent fact is retained longer or
	// discarded earlier than requested.
	key, _ := json.Marshal(struct {
		Category              string
		Tier                  Tier
		Tags                  []string
		ExpiresAt, ValidUntil *time.Time
	}{entry.Category, entry.Tier, tags, entry.ExpiresAt, entry.ValidUntil})
	return string(key)
}

// The caller owns s.mu. All validation precedes mutation; the summary is
// persisted before source deletion. Filesystem failures are returned and can
// leave a partial commit, never a claim of a cross-file atomic transaction.
func (s *Store) summarizeLocked(ids []string, content, category string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("summary content is required")
	}
	ids = dedupSlice(ids)
	if len(ids) == 0 {
		return fmt.Errorf("no source entries to summarize")
	}
	now := time.Now()
	var sources []*Entry
	for _, id := range ids {
		entry := s.entries[id]
		if entry == nil || !compactionEligible(entry, now) {
			return fmt.Errorf("source %s is unavailable or structured", id)
		}
		if len(sources) > 0 && compactionKey(entry) != compactionKey(sources[0]) {
			return fmt.Errorf("summary sources have incompatible scope or lifecycle metadata")
		}
		sources = append(sources, entry)
	}
	first := sources[0]
	entry := &Entry{ID: s.generateID(), Content: content, Category: category, Tier: TierMedium,
		Importance: first.Importance, Confidence: first.Confidence, Status: "active",
		CreatedAt: now, AccessedAt: now, ValidFrom: first.ValidFrom,
		Tags: append([]string(nil), first.Tags...), SummaryOf: append([]string(nil), ids...)}
	if first.ExpiresAt != nil {
		value := *first.ExpiresAt
		entry.ExpiresAt = &value
	}
	if first.ValidUntil != nil {
		value := *first.ValidUntil
		entry.ValidUntil = &value
	}
	for _, source := range sources {
		if source.ValidFrom.After(entry.ValidFrom) {
			entry.ValidFrom = source.ValidFrom
		}
		if source.Importance > entry.Importance {
			entry.Importance = source.Importance
		}
		if source.Confidence < entry.Confidence {
			entry.Confidence = source.Confidence
		}
		entry.Links = append(entry.Links, source.Links...)
		entry.Aliases = append(entry.Aliases, source.Aliases...)
	}
	entry.Links = normalizeMemoryLinks(entry.Links)
	entry.Aliases = dedupSlice(entry.Aliases)
	s.entries[entry.ID] = entry
	defer s.rebuildGraphLocked()
	if err := s.persist(); err != nil {
		// Keep sources available even when writing the summary fails.
		delete(s.entries, entry.ID)
		delete(s.paths, entry.ID)
		return err
	}
	for _, source := range sources {
		if source.Path != "" {
			if err := os.Remove(filepath.Join(s.dir, filepath.FromSlash(source.Path))); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove summarized source %s: %w", source.ID, err)
			}
		}
		delete(s.entries, source.ID)
		delete(s.paths, source.ID)
	}
	return nil
}
