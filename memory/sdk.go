package memory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// WriteMode selects a durable write operation. The zero value appends a fact.
type WriteMode string

const (
	WriteAppend      WriteMode = "append"
	WriteUpsertState WriteMode = "upsert_state"
	WriteSupersede   WriteMode = "supersede"
)

// RememberRequest is independent of agents, tools, providers and transports.
// Content admission and deriving a trusted scope are the host's responsibility.
type RememberRequest struct {
	Content    string
	Category   string
	Tier       Tier
	Importance float64
	Options    SaveOptions
	Mode       WriteMode
	// EntryFilter binds the host-authorized write scope. It constrains
	// deduplication, supersedes references and state replacement. A nil filter
	// selects the whole vault. The predicate must be pure and must accept the
	// proposed note; it is never an authorization decision made by Aestus.
	EntryFilter func(Entry) bool
}

type RememberResult struct {
	SaveResult
	Superseded []string
}

type RecallRequest struct {
	Query   string
	Options SearchOptions
}

var ErrStoreUnavailable = errors.New("memory store unavailable")

// Remember serializes deduplication, state replacement and one persistence
// pass under this Store's mutex. Readers of the same Store cannot observe an
// intermediate state. A vault must have a single writer Store/process.
// Markdown is NOT a multi-file crash-atomic transaction: an I/O error can
// leave a partial disk commit, so callers must not automatically retry it.
// Cancellation is checked before mutation; once mutation starts the write
// finishes synchronously and returns its actual outcome, even if ctx expires.
func (s *Store) Remember(ctx context.Context, request RememberRequest) (RememberResult, error) {
	if err := ctx.Err(); err != nil {
		return RememberResult{}, err
	}
	if s == nil {
		return RememberResult{}, ErrStoreUnavailable
	}
	request.Content = sanitizeDurableMemoryContent(request.Content)
	if request.Content == "" {
		return RememberResult{}, fmt.Errorf("content is required")
	}
	if request.Tier < TierShort || request.Tier > TierLong {
		return RememberResult{}, fmt.Errorf("invalid memory tier")
	}
	if math.IsNaN(request.Importance) || math.IsInf(request.Importance, 0) || request.Importance < 0 || request.Importance > 1 {
		return RememberResult{}, fmt.Errorf("importance must be between 0 and 1")
	}
	opts := request.Options
	opts.StateKey = strings.TrimSpace(opts.StateKey)
	opts.Supersedes = append([]string(nil), opts.Supersedes...)
	mode := request.Mode
	if mode == "" {
		mode = WriteAppend
	}
	switch mode {
	case WriteAppend:
	case WriteUpsertState:
		if opts.StateKey == "" {
			return RememberResult{}, fmt.Errorf("upsert_state requires state_key")
		}
		if opts.Status != "" && !strings.EqualFold(opts.Status, "active") {
			return RememberResult{}, fmt.Errorf("upsert_state requires an active state")
		}
		now := time.Now()
		if opts.ValidFrom.After(now) || (opts.ValidUntil != nil && !opts.ValidUntil.After(now)) ||
			(opts.ExpiresAt != nil && !opts.ExpiresAt.After(now)) {
			return RememberResult{}, fmt.Errorf("upsert_state requires a currently valid state; use append for scheduled or historical notes")
		}
		opts.Status = "active"
		if opts.ValidFrom.IsZero() {
			opts.ValidFrom = now
		}
	case WriteSupersede:
		if len(opts.Supersedes) == 0 {
			return RememberResult{}, fmt.Errorf("supersede requires supersedes")
		}
	default:
		return RememberResult{}, fmt.Errorf("invalid write mode %q", mode)
	}
	proposed := Entry{
		Content: request.Content, Category: request.Category, Tier: request.Tier,
		Importance: request.Importance, Tags: opts.Tags, StateKey: opts.StateKey, StateValue: opts.StateValue,
		Status: opts.Status, ValidFrom: opts.ValidFrom, ValidUntil: opts.ValidUntil, ExpiresAt: opts.ExpiresAt,
		Links: opts.Links, Aliases: opts.Aliases, Confidence: opts.Confidence, RoutePolicies: opts.RoutePolicies,
	}
	if proposed.Status == "" {
		proposed.Status = "active"
	}
	if request.EntryFilter != nil && !request.EntryFilter(proposed) {
		return RememberResult{}, fmt.Errorf("proposed memory is outside the write scope")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return RememberResult{}, err
	}
	writeTime := time.Now()
	if mode == WriteUpsertState && (opts.ValidFrom.After(writeTime) ||
		(opts.ValidUntil != nil && !opts.ValidUntil.After(writeTime)) ||
		(opts.ExpiresAt != nil && !opts.ExpiresAt.After(writeTime))) {
		return RememberResult{}, fmt.Errorf("upsert_state validity expired before the write started")
	}
	for _, id := range opts.Supersedes {
		entry := s.entries[strings.TrimSpace(id)]
		if entry == nil || (request.EntryFilter != nil && !request.EntryFilter(*entry)) {
			return RememberResult{}, fmt.Errorf("superseded memory is unavailable in the write scope")
		}
	}
	if mode == WriteUpsertState {
		now := time.Now()
		for id, entry := range s.entries {
			if entryIsActive(entry, now) && strings.EqualFold(entry.StateKey, opts.StateKey) &&
				(request.EntryFilter == nil || request.EntryFilter(*entry)) {
				opts.Supersedes = append(opts.Supersedes, id)
			}
		}
	}
	filter := func(entry Entry) bool {
		return (request.EntryFilter == nil || request.EntryFilter(entry)) &&
			(opts.StateKey == "" || strings.EqualFold(entry.StateKey, opts.StateKey)) &&
			(mode != WriteUpsertState || entryIsActive(&entry, writeTime))
	}
	saved, err := s.saveWithOptionsResultLocked(request.Content, request.Category, request.Tier, request.Importance, opts, filter)
	if err != nil {
		return RememberResult{}, err
	}
	result := RememberResult{SaveResult: saved}
	// Repeating an identical state update can deduplicate to its current note.
	// Such a note must never record itself as superseded.
	entry := s.entries[saved.ID]
	if mode == WriteUpsertState {
		// Refresh validity on duplicate updates of the current state. Historical
		// notes are excluded from upsert deduplication, preserving version history.
		entry.Status = "active"
		entry.ValidFrom = opts.ValidFrom
		entry.ValidUntil = opts.ValidUntil
		entry.ExpiresAt = opts.ExpiresAt
	}
	kept := make([]string, 0, len(entry.Supersedes))
	for _, id := range entry.Supersedes {
		if id != saved.ID {
			kept = append(kept, id)
		}
	}
	entry.Supersedes = kept
	for _, id := range dedupSlice(opts.Supersedes) {
		if id == saved.ID {
			continue
		}
		if entry := s.entries[id]; entry != nil && entry.Status != "superseded" {
			entry.Status = "superseded"
			result.Superseded = append(result.Superseded, id)
		}
	}
	if err := s.persist(); err != nil {
		return RememberResult{}, fmt.Errorf("persist memory (commit may be partial; do not automatically retry): %w", err)
	}
	result.Path = s.entries[saved.ID].Path
	return result, nil
}

// Recall checks cancellation before work and after the synchronous search.
// It cannot interrupt a filesystem operation or a custom reranker in progress.
func (s *Store) Recall(ctx context.Context, request RecallRequest) ([]SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrStoreUnavailable
	}
	results := s.SearchWithOptions(request.Query, request.Options)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
