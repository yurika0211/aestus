// In-memory wikilink graph rebuilt from notes. Notes stay the source of truth.
package memory

import (
	"path/filepath"
	"strings"
)

func newGraphIndex() *GraphIndex {
	return &GraphIndex{
		Forward:   make(map[string][]string),
		Backlinks: make(map[string][]string),
		Tags:      make(map[string][]string),
		Names:     make(map[string][]string),
	}
}

func graphKey(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, ".md")
	raw = strings.ReplaceAll(raw, "\\", "/")
	raw = strings.Trim(raw, "/")
	return strings.ToLower(raw)
}

func graphKeysForLink(link string) []string {
	link = strings.TrimSpace(link)
	if link == "" {
		return nil
	}
	keys := []string{graphKey(link)}
	base := strings.TrimSuffix(filepath.Base(strings.ReplaceAll(link, "\\", "/")), ".md")
	if base != "" {
		keys = append(keys, graphKey(base))
	}
	return dedupSlice(keys)
}

func graphAliasesForEntry(e *Entry) []string {
	if e == nil {
		return nil
	}
	aliases := []string{e.ID, e.BlockID}
	aliases = append(aliases, e.Aliases...)
	if e.Path != "" {
		pathNoExt := strings.TrimSuffix(filepath.ToSlash(e.Path), ".md")
		aliases = append(aliases, pathNoExt, filepath.Base(pathNoExt))
	}
	return dedupSlice(aliases)
}

func (s *Store) rebuildGraphLocked() {
	graph := newGraphIndex()
	for id, entry := range s.entries {
		links := normalizeLinks(append(entry.Links, extractWikiLinks(entry.Content)...))
		graph.Forward[id] = links
		for _, link := range links {
			for _, key := range graphKeysForLink(link) {
				graph.Backlinks[key] = append(graph.Backlinks[key], id)
			}
		}
		for _, alias := range graphAliasesForEntry(entry) {
			key := graphKey(alias)
			if key != "" {
				graph.Names[key] = append(graph.Names[key], id)
			}
		}
		for _, tag := range entry.Tags {
			tag = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(tag)), "#")
			if tag == "" {
				continue
			}
			graph.Tags[tag] = append(graph.Tags[tag], id)
		}
	}
	s.graph = graph
}
