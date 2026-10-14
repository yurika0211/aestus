package policy

import "github.com/yurika0211/aestus/memory/note"

// Excerpt names the heading or block a wikilink pointed at. The stored note
// stays whole; Context is the section a caller injects instead.
type Excerpt struct {
	Heading string `json:"heading,omitempty"`
	Block   string `json:"block,omitempty"`
	Context string `json:"context,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

// Analysis turns retrieved memories into action-facing routing signals.
type Analysis struct {
	Query             string                      `json:"query"`
	Entries           []note.Entry                `json:"entries"`
	Excerpts          map[string][]Excerpt        `json:"excerpts,omitempty"`
	ToolRequirements  []note.RouteToolRequirement `json:"tool_requirements,omitempty"`
	Risks             []note.RouteRisk            `json:"risks,omitempty"`
	AppliedPolicies   []note.AppliedRoutePolicy   `json:"applied_policies,omitempty"`
	RequiredTools     []string                    `json:"required_tools,omitempty"`
	SuggestedSearches []string                    `json:"suggested_searches,omitempty"`
	RiskFlags         []string                    `json:"risk_flags,omitempty"`
	Constraints       []string                    `json:"constraints,omitempty"`
	Clarifications    []string                    `json:"clarifications,omitempty"`
	TemporalNotes     []string                    `json:"temporal_notes,omitempty"`
	EvidenceRefs      []string                    `json:"evidence_refs,omitempty"`
	SupersededRefs    []string                    `json:"superseded_refs,omitempty"`
	ConflictRefs      []string                    `json:"conflict_refs,omitempty"`
	ExpiredRefs       []string                    `json:"expired_refs,omitempty"`
	FutureRefs        []string                    `json:"future_refs,omitempty"`
}
