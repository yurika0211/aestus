package memory

import (
	"time"

	"github.com/yurika0211/aestus/memory/midterm"
	"github.com/yurika0211/aestus/memory/shortterm"
	"github.com/yurika0211/aestus/memory/tidal"
)

// Short-term conversation buffers. The implementation lives in package
// shortterm; these names keep the original memory API.

type ConversationTurn = shortterm.ConversationTurn
type Message = shortterm.Message
type ShortTermBuffer = shortterm.ShortTermBuffer
type SessionShortTermStore = shortterm.SessionShortTermStore

func NewSessionShortTermStore(maxTurns int) *SessionShortTermStore {
	return shortterm.NewSessionShortTermStore(maxTurns)
}

func NewShortTermBuffer(maxTurns int) *ShortTermBuffer {
	return shortterm.NewShortTermBuffer(maxTurns)
}

// Mid-term session summaries. The implementation lives in package midterm.

type SessionSummary = midterm.SessionSummary
type MidTermStore = midterm.MidTermStore

func NewMidTermStore(dir string, maxSummaries int) (*MidTermStore, error) {
	return midterm.NewMidTermStore(dir, maxSummaries)
}

func GenerateSessionSummary(sessionID, userID string, messages []ConversationTurn) *SessionSummary {
	return midterm.GenerateSessionSummary(sessionID, userID, messages)
}

// Optional tidal reranker. Ranking lives in package tidal. The constructors
// below return an adapter that still satisfies ActivationReranker.

type TidalRerankerConfig = tidal.TidalRerankerConfig
type TidalFeedback struct {
	Query   string
	QueryID string
	Entry   Entry
	Signal  string
	Value   float64
	At      time.Time
	Keys    []string
}
type TidalKernelSnapshot = tidal.TidalKernelSnapshot
type TidalStore = tidal.TidalStore
type TidalStoreStats = tidal.TidalStoreStats

func DefaultTidalRerankerConfig() TidalRerankerConfig {
	return tidal.DefaultTidalRerankerConfig()
}

func NewTidalMemoryReranker(config TidalRerankerConfig) *TidalReranker {
	return newTidalReranker(tidal.NewTidalMemoryReranker(config))
}

func NewPersistentTidalMemoryReranker(config TidalRerankerConfig, store *TidalStore) (*TidalReranker, error) {
	inner, err := tidal.NewPersistentTidalMemoryReranker(config, store)
	if err != nil {
		return nil, err
	}
	return newTidalReranker(inner), nil
}

func OpenTidalStore(path string) (*TidalStore, error) {
	return tidal.OpenTidalStore(path)
}
