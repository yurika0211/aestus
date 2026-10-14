package memory

import (
	"path/filepath"
	"sort"
	"strings"
)

// GraphTopologyNode is one note in the memory vault, or one wikilink target
// that no note answers to yet.
type GraphTopologyNode struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Category   string   `json:"category,omitempty"`
	Tier       string   `json:"tier,omitempty"`
	Path       string   `json:"path,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Importance float64  `json:"importance"`
	Degree     int      `json:"degree"`
	// Resolved is false for a link target with no note behind it — Obsidian's
	// "unresolved link". Keeping them visible is the point: they show where the
	// vault refers to something it has not written down.
	Resolved bool `json:"resolved"`
	// Ambiguous is true when a link name matches more than one note. The graph
	// lists the candidates and does not pick the first registered note.
	Ambiguous  bool     `json:"ambiguous,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

// GraphLinkRef is one written wikilink on a note-to-note edge. Heading, block,
// display text, and embed do not create extra edges.
type GraphLinkRef struct {
	Heading string `json:"heading,omitempty"`
	Block   string `json:"block,omitempty"`
	Display string `json:"display,omitempty"`
	Embed   bool   `json:"embed,omitempty"`
	// Ambiguous means the written name matches more than one note. The edge
	// stays visible, but it is not a resolved relation and does not add degree.
	Ambiguous  bool     `json:"ambiguous,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

// GraphTopologyEdge is a wikilink from one note to another. Refs records how
// that link was written. Old clients can ignore it; the edge is still one
// note-to-note link.
type GraphTopologyEdge struct {
	Source string         `json:"source"`
	Target string         `json:"target"`
	Weight int            `json:"weight"`
	Refs   []GraphLinkRef `json:"refs,omitempty"`
}

// GraphTopology is a read-only snapshot of the wikilink graph.
type GraphTopology struct {
	Nodes         []GraphTopologyNode `json:"nodes"`
	Edges         []GraphTopologyEdge `json:"edges"`
	TotalNotes    int                 `json:"total_notes"`
	TotalEdges    int                 `json:"total_edges"`
	IsolatedCount int                 `json:"isolated_count"`
	Unresolved    int                 `json:"unresolved"`
	Truncated     bool                `json:"truncated"`
	Categories    []string            `json:"categories,omitempty"`
}

// GraphTopologyOptions controls what a snapshot includes.
type GraphTopologyOptions struct {
	// Limit caps the node count, keeping the most connected nodes. Zero means
	// no cap.
	Limit int
	// IncludeIsolated keeps notes that neither link out nor are linked to.
	// They dominate the node count in a typical vault while carrying no
	// topology, so they are excluded by default.
	IncludeIsolated bool
}

// GraphTopology returns the note graph derived from Obsidian wikilinks. The
// markdown notes remain the source of truth; this is a derived view for
// display, computed from the same link normalization the recall path uses.
func (s *Store) GraphTopology(opts GraphTopologyOptions) GraphTopology {
	if s == nil {
		return GraphTopology{Nodes: []GraphTopologyNode{}, Edges: []GraphTopologyEdge{}}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// name key -> entry IDs, rebuilt here rather than read off s.graph so a
	// stale index can never produce edges that disagree with the notes.
	// More than one ID means the written name is ambiguous.
	nameToIDs := graphNameIndex(s.entries)

	nodes := make(map[string]*GraphTopologyNode, len(s.entries))
	for id, entry := range s.entries {
		nodes[id] = &GraphTopologyNode{
			ID:         id,
			Title:      entryDisplayTitle(entry),
			Category:   entry.Category,
			Tier:       entry.Tier.String(),
			Path:       entry.Path,
			Tags:       append([]string(nil), entry.Tags...),
			Importance: entry.Importance,
			Resolved:   true,
		}
	}

	weights := make(map[string]int)
	refs := make(map[string][]GraphLinkRef)
	order := make([]string, 0, len(s.entries))
	unresolved := 0

	for id, entry := range s.entries {
		for _, written := range noteWikiLinks(entry) {
			resolved := resolveGraphLink(written.Target, nameToIDs)
			if !resolved.OK {
				continue
			}
			targetID := resolved.ID
			if targetID == "" {
				// Surface the dangling link as its own node instead of
				// dropping the edge and hiding the gap. Heading and block
				// stay on the edge; they are not extra nodes.
				targetID = "unresolved:" + graphKey(written.Target)
				if _, seen := nodes[targetID]; !seen {
					nodes[targetID] = &GraphTopologyNode{
						ID:       targetID,
						Title:    resolved.Title,
						Resolved: false,
					}
					unresolved++
				}
			}
			if resolved.Ambiguous {
				targetID = "ambiguous:" + graphKey(written.Target)
				if _, seen := nodes[targetID]; !seen {
					nodes[targetID] = &GraphTopologyNode{
						ID:         targetID,
						Title:      strings.TrimSpace(written.Target),
						Resolved:   false,
						Ambiguous:  true,
						Candidates: append([]string(nil), resolved.Candidates...),
					}
				}
			}
			if targetID == id {
				continue
			}
			edgeKey := id + "\x00" + targetID
			if _, seen := weights[edgeKey]; !seen {
				order = append(order, edgeKey)
			}
			weights[edgeKey]++
			ref := written.Ref
			ref.Ambiguous = resolved.Ambiguous
			ref.Candidates = append([]string(nil), resolved.Candidates...)
			refs[edgeKey] = append(refs[edgeKey], ref)
		}
	}

	for _, edgeKey := range order {
		parts := strings.SplitN(edgeKey, "\x00", 2)
		// An ambiguous name is not a relation to either candidate, so it
		// does not make those notes look more connected.
		if nodes[parts[1]] != nil && nodes[parts[1]].Ambiguous {
			continue
		}
		nodes[parts[0]].Degree++
		nodes[parts[1]].Degree++
	}

	totalNotes := len(s.entries)
	isolated := 0
	kept := make([]GraphTopologyNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Degree == 0 {
			isolated++
			if !opts.IncludeIsolated {
				continue
			}
		}
		kept = append(kept, *node)
	}

	// Most connected first, so a cap keeps the informative core; ties break on
	// title to keep the response stable between calls.
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].Degree != kept[j].Degree {
			return kept[i].Degree > kept[j].Degree
		}
		return kept[i].Title < kept[j].Title
	})

	truncated := false
	if opts.Limit > 0 && len(kept) > opts.Limit {
		kept = kept[:opts.Limit]
		truncated = true
	}

	visible := make(map[string]struct{}, len(kept))
	for _, node := range kept {
		visible[node.ID] = struct{}{}
	}

	edges := make([]GraphTopologyEdge, 0, len(order))
	for _, edgeKey := range order {
		parts := strings.SplitN(edgeKey, "\x00", 2)
		if _, ok := visible[parts[0]]; !ok {
			continue
		}
		if _, ok := visible[parts[1]]; !ok {
			continue
		}
		edges = append(edges, GraphTopologyEdge{
			Source: parts[0],
			Target: parts[1],
			Weight: weights[edgeKey],
			Refs:   graphLinkRefs(refs[edgeKey]),
		})
	}

	categorySet := make(map[string]struct{})
	for _, node := range kept {
		if node.Category != "" {
			categorySet[node.Category] = struct{}{}
		}
	}
	categories := make([]string, 0, len(categorySet))
	for category := range categorySet {
		categories = append(categories, category)
	}
	sort.Strings(categories)

	return GraphTopology{
		Nodes:         kept,
		Edges:         edges,
		TotalNotes:    totalNotes,
		TotalEdges:    len(order),
		IsolatedCount: isolated,
		Unresolved:    unresolved,
		Truncated:     truncated,
		Categories:    categories,
	}
}

// writtenWikiLink is a wikilink reduced to the note name the graph resolves,
// plus the heading, block, display text, and embed that stay on that edge.
type writtenWikiLink struct {
	Target string
	Ref    GraphLinkRef
}

func noteWikiLinks(entry *Entry) []writtenWikiLink {
	if entry == nil {
		return nil
	}
	out := make([]writtenWikiLink, 0, len(entry.Links))
	seen := make(map[string]bool)
	add := func(target string, ref GraphLinkRef) {
		target = strings.TrimSpace(target)
		if target == "" {
			return
		}
		key := strings.ToLower(target) + "\x00" + strings.ToLower(ref.Heading) + "\x00" + strings.ToLower(ref.Block) + "\x00" + ref.Display + "\x00"
		if ref.Embed {
			key += "embed"
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, writtenWikiLink{Target: target, Ref: ref})
	}
	for _, raw := range entry.Links {
		link, ok := parseLinkLocator(raw)
		if !ok {
			add(raw, GraphLinkRef{})
			continue
		}
		add(link.Target, GraphLinkRef{Heading: link.Heading, Block: link.Block, Display: link.Display, Embed: link.Embed})
	}
	for _, link := range parseWikiLinks(entry.Content) {
		add(link.Target, GraphLinkRef{
			Heading: link.Heading,
			Block:   link.Block,
			Display: link.Display,
			Embed:   link.Embed,
		})
	}
	return out
}

// graphLinkResolution is how a written note name maps onto the vault.
// Ambiguous is set instead of silently keeping the first registered note.
type graphLinkResolution struct {
	ID         string
	Title      string
	Candidates []string
	Ambiguous  bool
	OK         bool
}

// graphNameIndex maps a written note name to the notes that answer to it.
// Internal IDs stay out of this map: a link written as [[登录]] is ambiguous
// only when two notes share that alias or filename, not because each note
// also has its own memory ID.
func graphNameIndex(entries map[string]*Entry) map[string][]string {
	names := make(map[string][]string, len(entries)*2)
	for id, entry := range entries {
		seen := make(map[string]bool)
		for _, alias := range graphPublicNamesForEntry(entry) {
			key := graphKey(alias)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			names[key] = append(names[key], id)
		}
	}
	// A built-in concept note is the vault's placeholder for a wikilink. It is not
	// a second note competing with an alias of the same name.
	for key, ids := range names {
		names[key] = dropConceptPlaceholder(entries, ids)
	}
	return names
}

func dropConceptPlaceholder(entries map[string]*Entry, ids []string) []string {
	if len(ids) < 2 {
		return ids
	}
	var notes, placeholders []string
	for _, id := range ids {
		if strings.HasPrefix(id, "concept_") {
			placeholders = append(placeholders, id)
			continue
		}
		notes = append(notes, id)
	}
	if len(notes) == 0 {
		return placeholders
	}
	return notes
}

func graphPublicNamesForEntry(e *Entry) []string {
	if e == nil {
		return nil
	}
	// A concept note created from a wikilink answers to that name. Counting its
	// filename as well would make [[Target]] ambiguous against a note that also
	// lists Target as an alias.
	if isConceptEntry(e) {
		names := append([]string{}, e.Aliases...)
		if title := strings.TrimSpace(e.Content); title != "" {
			names = append(names, title)
		}
		return dedupSlice(names)
	}
	names := append([]string{}, e.Aliases...)
	if e.Path != "" {
		pathNoExt := strings.TrimSuffix(filepath.ToSlash(e.Path), ".md")
		names = append(names, pathNoExt, filepath.Base(pathNoExt))
	}
	return dedupSlice(names)
}

// resolveGraphLink returns the note a link names. An unresolved note name
// returns OK with an empty ID so the caller can keep the dangling node.
// A name owned by more than one note returns the candidates and no ID.
func resolveGraphLink(link string, nameToIDs map[string][]string) graphLinkResolution {
	link = strings.TrimSpace(link)
	if link == "" {
		return graphLinkResolution{}
	}
	for _, key := range graphKeysForLink(link) {
		ids := uniqueIDs(nameToIDs[key])
		switch len(ids) {
		case 0:
			continue
		case 1:
			return graphLinkResolution{ID: ids[0], OK: true}
		default:
			return graphLinkResolution{Candidates: ids, Ambiguous: true, OK: true}
		}
	}
	if graphKey(link) == "" {
		return graphLinkResolution{}
	}
	return graphLinkResolution{Title: link, OK: true}
}

func uniqueIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func graphLinkRefs(refs []GraphLinkRef) []GraphLinkRef {
	kept := make([]GraphLinkRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Heading == "" && ref.Block == "" && ref.Display == "" && !ref.Embed && !ref.Ambiguous {
			continue
		}
		kept = append(kept, ref)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// entryDisplayTitle prefers the note filename, which is what wikilinks point
// at, and falls back to the first line of the body.
func entryDisplayTitle(e *Entry) string {
	if e == nil {
		return ""
	}
	if path := strings.TrimSpace(e.Path); path != "" {
		base := strings.TrimSuffix(filepath.Base(filepath.ToSlash(path)), ".md")
		if base != "" {
			return base
		}
	}
	for _, line := range strings.Split(e.Content, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "# "))
		if line == "" {
			continue
		}
		if len([]rune(line)) > 60 {
			return string([]rune(line)[:60]) + "…"
		}
		return line
	}
	return e.ID
}
