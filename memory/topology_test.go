package memory

import (
	"strings"
	"testing"
)

func seedVault(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func topologyByTitle(topology GraphTopology) map[string]GraphTopologyNode {
	out := make(map[string]GraphTopologyNode, len(topology.Nodes))
	for _, node := range topology.Nodes {
		out[node.Title] = node
	}
	return out
}

func TestGraphTopologyBuildsEdgesFromWikilinks(t *testing.T) {
	store := seedVault(t)
	if err := store.SaveWithMetadata("Hub note about [[Target]]", "concept", TierLong, 0.8, nil, nil, nil); err != nil {
		t.Fatalf("save hub: %v", err)
	}
	if err := store.SaveWithMetadata("Target note body", "concept", TierLong, 0.5, nil, nil, []string{"Target"}); err != nil {
		t.Fatalf("save target: %v", err)
	}

	topology := store.GraphTopology(GraphTopologyOptions{})
	if len(topology.Edges) != 1 {
		t.Fatalf("edges = %d, want 1: %+v", len(topology.Edges), topology.Edges)
	}
	if len(topology.Nodes) != 2 {
		t.Fatalf("nodes = %d, want the two linked notes", len(topology.Nodes))
	}
	for _, node := range topology.Nodes {
		if node.Degree != 1 {
			t.Fatalf("node %q degree = %d, want 1", node.Title, node.Degree)
		}
		if !node.Resolved {
			t.Fatalf("node %q marked unresolved, want a real note", node.Title)
		}
	}
}

// Ordinary wikilink targets are materialized as concept notes on save, so a
// dangling link is only possible for the targets concept creation skips —
// references to a memory ID (`mem_…`) that no longer exists.
func TestGraphTopologySurfacesUnresolvedLinks(t *testing.T) {
	store := seedVault(t)
	if err := store.SaveWithMetadata("Points at [[mem_9999999999]]", "fact", TierLong, 0.6, nil, nil, nil); err != nil {
		t.Fatalf("save: %v", err)
	}

	topology := store.GraphTopology(GraphTopologyOptions{})
	if topology.Unresolved != 1 {
		t.Fatalf("unresolved = %d, want 1: %+v", topology.Unresolved, topology.Nodes)
	}

	byTitle := topologyByTitle(topology)
	dangling, ok := byTitle["mem_9999999999"]
	if !ok {
		t.Fatalf("dangling link is missing from the graph: %+v", topology.Nodes)
	}
	if dangling.Resolved {
		t.Fatal("dangling link reported as resolved; the gap would be invisible")
	}
	if len(topology.Edges) != 1 {
		t.Fatalf("edges = %d, want the link to the missing note kept", len(topology.Edges))
	}
}

func TestGraphTopologyExcludesIsolatedNotesByDefault(t *testing.T) {
	store := seedVault(t)
	if err := store.SaveWithMetadata("Linked A points to [[Linked B]]", "concept", TierLong, 0.7, nil, nil, nil); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if err := store.SaveWithMetadata("Lonely note with no links", "fact", TierLong, 0.4, nil, nil, nil); err != nil {
		t.Fatalf("save lonely: %v", err)
	}

	excluded := store.GraphTopology(GraphTopologyOptions{})
	if excluded.IsolatedCount == 0 {
		t.Fatal("isolated_count = 0, want the link-less note counted")
	}
	for _, node := range excluded.Nodes {
		if node.Degree == 0 {
			t.Fatalf("node %q has no links but survived the default view", node.Title)
		}
	}

	included := store.GraphTopology(GraphTopologyOptions{IncludeIsolated: true})
	if len(included.Nodes) != len(excluded.Nodes)+excluded.IsolatedCount {
		t.Fatalf("nodes with isolated = %d, want %d + %d isolated",
			len(included.Nodes), len(excluded.Nodes), excluded.IsolatedCount)
	}
	if included.TotalNotes != excluded.TotalNotes {
		t.Fatalf("total_notes differs between views: %d vs %d", included.TotalNotes, excluded.TotalNotes)
	}
	if !hasNodeTitled(included.Nodes, "Lonely note with no links") && !hasIsolated(included.Nodes) {
		t.Fatal("the isolated note is absent even when explicitly included")
	}
}

func hasIsolated(nodes []GraphTopologyNode) bool {
	for _, node := range nodes {
		if node.Degree == 0 {
			return true
		}
	}
	return false
}

func hasNodeTitled(nodes []GraphTopologyNode, title string) bool {
	for _, node := range nodes {
		if node.Title == title {
			return true
		}
	}
	return false
}

func TestGraphTopologyLimitKeepsTheMostConnectedAndPrunesDanglingEdges(t *testing.T) {
	store := seedVault(t)
	// Hub links to three leaves; each leaf has degree 1, the hub degree 3.
	if err := store.SaveWithMetadata("Hub links [[Leaf A]] [[Leaf B]] [[Leaf C]]", "concept", TierLong, 0.9, nil, nil, nil); err != nil {
		t.Fatalf("save hub: %v", err)
	}
	for _, name := range []string{"Leaf A", "Leaf B", "Leaf C"} {
		if err := store.SaveWithMetadata(name+" body", "concept", TierLong, 0.5, nil, nil, []string{name}); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}

	topology := store.GraphTopology(GraphTopologyOptions{Limit: 2})
	if !topology.Truncated {
		t.Fatal("truncated = false, want true when the cap drops nodes")
	}
	if len(topology.Nodes) != 2 {
		t.Fatalf("nodes = %d, want the cap honoured", len(topology.Nodes))
	}
	if topology.Nodes[0].Degree != 3 {
		t.Fatalf("first node degree = %d, want the hub kept first", topology.Nodes[0].Degree)
	}
	for _, edge := range topology.Edges {
		if edge.Source == "" || edge.Target == "" {
			t.Fatal("edge with an empty endpoint survived truncation")
		}
	}
	// Every surviving edge must connect two surviving nodes.
	visible := make(map[string]bool, len(topology.Nodes))
	for _, node := range topology.Nodes {
		visible[node.ID] = true
	}
	for _, edge := range topology.Edges {
		if !visible[edge.Source] || !visible[edge.Target] {
			t.Fatalf("edge %s->%s points outside the truncated node set", edge.Source, edge.Target)
		}
	}
}

func TestGraphTopologyIgnoresSelfLinks(t *testing.T) {
	store := seedVault(t)
	if err := store.SaveWithMetadata("Self referential [[Selfie]] note", "concept", TierLong, 0.5, nil, nil, []string{"Selfie"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	topology := store.GraphTopology(GraphTopologyOptions{IncludeIsolated: true})
	for _, edge := range topology.Edges {
		if edge.Source == edge.Target {
			t.Fatalf("self-loop survived: %+v", edge)
		}
	}
}

func TestGraphTopologyKeepsHeadingBlockDisplayAndEmbedOnOneEdge(t *testing.T) {
	store := seedVault(t)
	if err := store.SaveWithMetadata("登录笔记", "concept", TierLong, 0.5, nil, nil, []string{"登录"}); err != nil {
		t.Fatalf("save login: %v", err)
	}
	body := "见 [[mem_9999999999]]、[[登录#失败原因]]、[[登录#^block-9]]、[[登录|登录流程]]。\n嵌入 ![[登录]]。"
	if err := store.SaveWithMetadata(body, "fact", TierLong, 0.7, nil, nil, nil); err != nil {
		t.Fatalf("save source: %v", err)
	}

	topology := store.GraphTopology(GraphTopologyOptions{IncludeIsolated: true})
	loginEdges := edgesToTitle(t, topology, "登录笔记")
	if len(loginEdges) != 1 {
		t.Fatalf("edges to 登录 = %d, want one note edge: %+v", len(loginEdges), loginEdges)
	}
	edge := loginEdges[0]
	if len(edge.Refs) != 4 {
		t.Fatalf("refs = %d, want heading, block, display, embed: %+v", len(edge.Refs), edge.Refs)
	}
	seen := map[string]bool{}
	for _, ref := range edge.Refs {
		switch {
		case ref.Heading == "失败原因" && ref.Block == "" && ref.Display == "" && !ref.Embed:
			seen["heading"] = true
		case ref.Block == "block-9" && ref.Heading == "" && !ref.Embed:
			seen["block"] = true
		case ref.Display == "登录流程" && !ref.Embed:
			seen["display"] = true
		case ref.Embed && ref.Heading == "" && ref.Block == "" && ref.Display == "":
			seen["embed"] = true
		}
	}
	for _, kind := range []string{"heading", "block", "display", "embed"} {
		if !seen[kind] {
			t.Fatalf("missing %s ref: %+v", kind, edge.Refs)
		}
	}

	byTitle := topologyByTitle(topology)
	missing, ok := byTitle["mem_9999999999"]
	if !ok || missing.Resolved {
		t.Fatalf("missing memory link = %+v, present %v", missing, ok)
	}
	if topology.Unresolved != 1 {
		t.Fatalf("unresolved = %d, want the missing memory id only", topology.Unresolved)
	}
}

func TestGraphTopologyAmbiguousAliasDoesNotPickTheFirstNote(t *testing.T) {
	store := seedVault(t)
	if err := store.SaveWithMetadata("first login note", "concept", TierLong, 0.9, nil, nil, []string{"SharedName"}); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if err := store.SaveWithMetadata("second login note", "concept", TierLong, 0.4, nil, nil, []string{"SharedName"}); err != nil {
		t.Fatalf("save second: %v", err)
	}
	if err := store.SaveWithMetadata("see [[SharedName]]", "fact", TierLong, 0.6, nil, nil, nil); err != nil {
		t.Fatalf("save source: %v", err)
	}

	topology := store.GraphTopology(GraphTopologyOptions{IncludeIsolated: true})
	var ambiguous *GraphTopologyNode
	for i := range topology.Nodes {
		if topology.Nodes[i].Ambiguous {
			ambiguous = &topology.Nodes[i]
			break
		}
	}
	if ambiguous == nil {
		t.Fatalf("ambiguous name was resolved to one note: %+v", topology.Nodes)
	}
	if ambiguous.Resolved || len(ambiguous.Candidates) != 2 {
		t.Fatalf("ambiguous node = %+v, want two unresolved candidates", *ambiguous)
	}
	for _, node := range topology.Nodes {
		if node.Title == "first login note" || node.Title == "second login note" {
			if node.Degree != 0 {
				t.Fatalf("%q degree = %d, ambiguous link must not add degree", node.Title, node.Degree)
			}
		}
	}

	scores := store.Activate("see", ActivationOptions{IncludeGraph: true, UpdateAccessStats: false, Explain: true})
	for _, score := range scores {
		if strings.Contains(score.Entry.Content, "login note") && score.Components.GraphBoost > 0 {
			t.Fatalf("ambiguous alias boosted %q by %f", score.Entry.Content, score.Components.GraphBoost)
		}
	}
}

func TestGraphTopologyUniqueAliasStillResolves(t *testing.T) {
	store := seedVault(t)
	if err := store.SaveWithMetadata("see [[UniqueName]]", "fact", TierLong, 0.6, nil, nil, nil); err != nil {
		t.Fatalf("save source: %v", err)
	}
	topology := store.GraphTopology(GraphTopologyOptions{})
	if len(edgesToTitle(t, topology, "UniqueName")) != 1 {
		t.Fatal("unique alias did not resolve to its note")
	}
	for _, node := range topology.Nodes {
		if node.Ambiguous {
			t.Fatalf("unique alias marked ambiguous: %+v", node)
		}
	}
}

func TestGraphTopologyHeadingOnSelfLinkDoesNotMakeAnEdge(t *testing.T) {
	store := seedVault(t)
	if err := store.SaveWithMetadata("自己 [[Selfie#标题]]", "concept", TierLong, 0.5, nil, nil, []string{"Selfie"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	topology := store.GraphTopology(GraphTopologyOptions{IncludeIsolated: true})
	for _, edge := range topology.Edges {
		if edge.Source == edge.Target {
			t.Fatalf("self-link with a heading became an edge: %+v", edge)
		}
	}
}

func edgesToTitle(t *testing.T, topology GraphTopology, title string) []GraphTopologyEdge {
	t.Helper()
	var targetID string
	for _, node := range topology.Nodes {
		if node.Title == title {
			targetID = node.ID
			break
		}
	}
	if targetID == "" {
		t.Fatalf("no node titled %q: %+v", title, topology.Nodes)
	}
	var edges []GraphTopologyEdge
	for _, edge := range topology.Edges {
		if edge.Target == targetID {
			edges = append(edges, edge)
		}
	}
	return edges
}
