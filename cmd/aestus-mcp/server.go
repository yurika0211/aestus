package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yurika0211/aestus/memory"
)

const (
	defaultRecallLimit = 8
	maxRecallLimit     = 20
	maxExcerptRunes    = 500
	maxNoteBytes       = 256 << 10
	noteURIPrefix      = "aestus://note/"
)

// NewServer binds one vault Store to the MCP tool and resource surface.
// The Store must be the only writer process for that vault.
func NewServer(store *memory.Store) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "aestus",
		Title:   "Aestus memory",
		Version: "0.1.0",
	}, nil)
	h := &handler{store: store}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "aestus_remember",
		Description: "Save one durable fact into the Aestus vault. Use append for a new fact, upsert_state to replace the current value of a state_key, or supersede to retire specific note ids.",
	}, h.remember)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "aestus_recall",
		Description: "Search the Aestus vault. Returns ranked notes with ids, relative paths, scores, and short excerpts. Read aestus://note/{id} for the full Markdown note.",
	}, h.recall)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "aestus_get",
		Description: "Read one Aestus note by its memory id.",
	}, h.get)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "aestus_forget",
		Description: "Delete one Aestus note by its memory id. This does not search or delete by query.",
	}, h.forget)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "aestus_stats",
		Description: "Count active durable notes by tier. Concept notes are excluded, matching Store.Stats.",
	}, h.stats)
	server.AddResource(&mcp.Resource{
		URI:         "aestus://vault",
		Name:        "vault",
		Title:       "Aestus vault",
		Description: "Index of the open Aestus vault: tier counts and note ids.",
		MIMEType:    "application/json",
	}, h.readVault)
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: noteURIPrefix + "{id}",
		Name:        "note",
		Title:       "Aestus note",
		Description: "Full Markdown note for one memory id.",
		MIMEType:    "text/markdown",
	}, h.readNote)
	return server
}

type handler struct {
	store *memory.Store
}

type rememberInput struct {
	Content    string   `json:"content" jsonschema:"durable fact to store"`
	Category   string   `json:"category,omitempty" jsonschema:"note category such as fact, preference, rule, project, or decision"`
	Tier       string   `json:"tier,omitempty" jsonschema:"short, medium, or long; default long"`
	Importance float64  `json:"importance,omitempty" jsonschema:"importance from 0 to 1; default 0.7"`
	Mode       string   `json:"mode,omitempty" jsonschema:"append, upsert_state, or supersede; default append"`
	Tags       []string `json:"tags,omitempty" jsonschema:"optional tags"`
	Links      []string `json:"links,omitempty" jsonschema:"optional Obsidian wikilink targets"`
	Aliases    []string `json:"aliases,omitempty" jsonschema:"optional aliases"`
	StateKey   string   `json:"state_key,omitempty" jsonschema:"required for upsert_state"`
	StateValue string   `json:"state_value,omitempty" jsonschema:"current value for a state note"`
	Supersedes []string `json:"supersedes,omitempty" jsonschema:"note ids to retire; required for supersede"`
}

type rememberOutput struct {
	ID              string   `json:"id"`
	Path            string   `json:"path,omitempty"`
	Created         bool     `json:"created"`
	UpdatedExisting bool     `json:"updated_existing"`
	DuplicateOf     string   `json:"duplicate_of,omitempty"`
	Superseded      []string `json:"superseded,omitempty"`
}

func (h *handler) remember(ctx context.Context, _ *mcp.CallToolRequest, in rememberInput) (*mcp.CallToolResult, rememberOutput, error) {
	tier := memory.TierLong
	if strings.TrimSpace(in.Tier) != "" {
		parsed, err := parseTier(in.Tier)
		if err != nil {
			return nil, rememberOutput{}, err
		}
		tier = parsed
	}
	importance := in.Importance
	if importance == 0 {
		importance = 0.7
	}
	mode := memory.WriteMode(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = memory.WriteAppend
	}
	result, err := h.store.Remember(ctx, memory.RememberRequest{
		Content:    in.Content,
		Category:   defaultString(in.Category, "fact"),
		Tier:       tier,
		Importance: importance,
		Mode:       mode,
		Options: memory.SaveOptions{
			Tags:       in.Tags,
			Links:      in.Links,
			Aliases:    in.Aliases,
			StateKey:   in.StateKey,
			StateValue: in.StateValue,
			Supersedes: in.Supersedes,
		},
	})
	if err != nil {
		return nil, rememberOutput{}, err
	}
	return nil, rememberOutput{
		ID:              result.ID,
		Path:            result.Path,
		Created:         result.Created,
		UpdatedExisting: result.UpdatedExisting,
		DuplicateOf:     result.DuplicateOf,
		Superseded:      result.Superseded,
	}, nil
}

type recallInput struct {
	Query    string `json:"query" jsonschema:"search query"`
	Category string `json:"category,omitempty" jsonschema:"optional category filter"`
	Tier     string `json:"tier,omitempty" jsonschema:"optional short, medium, or long filter"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum results, default 8, hard cap 20"`
	Graph    *bool  `json:"include_graph,omitempty" jsonschema:"include one-hop graph activation; default true"`
}

type noteView struct {
	ID         string   `json:"id"`
	Path       string   `json:"path,omitempty"`
	URI        string   `json:"uri"`
	Category   string   `json:"category,omitempty"`
	Tier       string   `json:"tier"`
	Status     string   `json:"status,omitempty"`
	Importance float64  `json:"importance,omitempty"`
	Score      float64  `json:"score,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	StateKey   string   `json:"state_key,omitempty"`
	Excerpt    string   `json:"excerpt,omitempty"`
	Content    string   `json:"content,omitempty"`
}

type recallOutput struct {
	Notes []noteView `json:"notes"`
}

func (h *handler) recall(ctx context.Context, _ *mcp.CallToolRequest, in recallInput) (*mcp.CallToolResult, recallOutput, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, recallOutput{}, errors.New("query is required")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	if limit > maxRecallLimit {
		limit = maxRecallLimit
	}
	opts := memory.SearchOptions{
		Limit:           limit,
		Category:        in.Category,
		IncludeGraph:    in.Graph == nil || *in.Graph,
		GraphDepth:      1,
		SkipAccessStats: true,
	}
	if strings.TrimSpace(in.Tier) != "" {
		tier, err := parseTier(in.Tier)
		if err != nil {
			return nil, recallOutput{}, err
		}
		opts.Tier = &tier
	}
	results, err := h.store.Recall(ctx, memory.RecallRequest{Query: in.Query, Options: opts})
	if err != nil {
		return nil, recallOutput{}, err
	}
	out := recallOutput{Notes: make([]noteView, 0, len(results))}
	for _, result := range results {
		view := noteFromEntry(result.Entry)
		view.Score = result.Score
		view.Excerpt = excerpt(result.Entry.Content)
		out.Notes = append(out.Notes, view)
	}
	return nil, out, nil
}

type idInput struct {
	ID string `json:"id" jsonschema:"memory note id"`
}

type getOutput struct {
	Note noteView `json:"note"`
}

func (h *handler) get(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, getOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, getOutput{}, err
	}
	entry, err := h.store.Get(strings.TrimSpace(in.ID))
	if err != nil {
		return nil, getOutput{}, err
	}
	view := noteFromEntry(*entry)
	view.Content = entry.Content
	return nil, getOutput{Note: view}, nil
}

type forgetOutput struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

func (h *handler) forget(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, forgetOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, forgetOutput{}, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return nil, forgetOutput{}, errors.New("id is required")
	}
	if err := h.store.Delete(id); err != nil {
		return nil, forgetOutput{}, err
	}
	return nil, forgetOutput{ID: id, Deleted: true}, nil
}

type statsOutput struct {
	Short  int `json:"short"`
	Medium int `json:"medium"`
	Long   int `json:"long"`
	Total  int `json:"total"`
}

func (h *handler) stats(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, statsOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, statsOutput{}, err
	}
	counts := h.store.Stats()
	out := statsOutput{
		Short:  counts[memory.TierShort],
		Medium: counts[memory.TierMedium],
		Long:   counts[memory.TierLong],
	}
	out.Total = out.Short + out.Medium + out.Long
	return nil, out, nil
}

type vaultIndex struct {
	Notes []noteView  `json:"notes"`
	Stats statsOutput `json:"stats"`
}

func (h *handler) readVault(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.Params.URI != "aestus://vault" {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	counts := h.store.Stats()
	index := vaultIndex{
		Stats: statsOutput{
			Short:  counts[memory.TierShort],
			Medium: counts[memory.TierMedium],
			Long:   counts[memory.TierLong],
		},
	}
	index.Stats.Total = index.Stats.Short + index.Stats.Medium + index.Stats.Long
	entries := h.store.Recent(maxRecallLimit)
	index.Notes = make([]noteView, 0, len(entries))
	for _, entry := range entries {
		view := noteFromEntry(entry)
		view.Excerpt = excerpt(entry.Content)
		index.Notes = append(index.Notes, view)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI:      "aestus://vault",
		MIMEType: "application/json",
		Text:     mustJSON(index),
	}}}, nil
}

func (h *handler) readNote(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := noteIDFromURI(req.Params.URI)
	if err != nil {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	entry, err := h.store.Get(id)
	if err != nil {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	body, err := h.noteMarkdown(entry)
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI:      req.Params.URI,
		MIMEType: "text/markdown",
		Text:     body,
	}}}, nil
}

func (h *handler) noteMarkdown(entry *memory.Entry) (string, error) {
	if entry == nil || strings.TrimSpace(entry.Path) == "" {
		return "", errors.New("note path is unavailable")
	}
	rel := filepath.Clean(entry.Path)
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", errors.New("note path escapes the vault")
	}
	path := filepath.Join(h.store.Dir(), rel)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxNoteBytes {
		return "", fmt.Errorf("note exceeds %d bytes", maxNoteBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func noteFromEntry(entry memory.Entry) noteView {
	return noteView{
		ID:         entry.ID,
		Path:       entry.Path,
		URI:        noteURIPrefix + url.PathEscape(entry.ID),
		Category:   entry.Category,
		Tier:       entry.Tier.String(),
		Status:     entry.Status,
		Importance: entry.Importance,
		Tags:       entry.Tags,
		StateKey:   entry.StateKey,
	}
}

func noteIDFromURI(raw string) (string, error) {
	if !strings.HasPrefix(raw, noteURIPrefix) {
		return "", errors.New("not a note uri")
	}
	id, err := url.PathUnescape(strings.TrimPrefix(raw, noteURIPrefix))
	if err != nil || strings.TrimSpace(id) == "" || strings.Contains(id, "/") {
		return "", errors.New("invalid note uri")
	}
	return id, nil
}

func parseTier(raw string) (memory.Tier, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "short":
		return memory.TierShort, nil
	case "medium":
		return memory.TierMedium, nil
	case "long":
		return memory.TierLong, nil
	default:
		return 0, fmt.Errorf("tier must be short, medium, or long")
	}
}

func excerpt(content string) string {
	content = strings.TrimSpace(content)
	if utf8.RuneCountInString(content) <= maxExcerptRunes {
		return content
	}
	return string([]rune(content)[:maxExcerptRunes]) + "..."
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return `{"error":"encode"}`
	}
	return string(raw)
}
