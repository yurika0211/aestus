package memory

import (
	"strings"
	"unicode/utf8"
)

// BacklinkContext is one written wikilink that points at a note, with the
// sentence around it. It is for inspection. It does not change activation.
type BacklinkContext struct {
	SourceID string `json:"source_id"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	Heading  string `json:"heading,omitempty"`
	Block    string `json:"block,omitempty"`
	Display  string `json:"display,omitempty"`
	Embed    bool   `json:"embed,omitempty"`
	Line     int    `json:"line"`
	Excerpt  string `json:"excerpt"`
}

// BacklinksFor returns every wikilink that names the note, in source order.
// Two mentions in one note are two contexts. The score of Activate is not read
// or changed here.
func (s *Store) BacklinksFor(id string) []BacklinkContext {
	if s == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	target := s.entries[id]
	if target == nil {
		return nil
	}
	names := make(map[string]bool)
	for _, name := range graphPublicNamesForEntry(target) {
		if key := graphKey(name); key != "" {
			names[key] = true
		}
	}
	if key := graphKey(target.ID); key != "" {
		names[key] = true
	}
	var out []BacklinkContext
	for _, source := range s.entries {
		if source == nil || source.ID == id {
			continue
		}
		for _, found := range backlinkContexts(source, names) {
			out = append(out, found)
		}
	}
	return out
}

func backlinkContexts(source *Entry, names map[string]bool) []BacklinkContext {
	var out []BacklinkContext
	for _, span := range wikiLinkSpan.FindAllStringSubmatchIndex(source.Content, -1) {
		raw := source.Content[span[0]:span[1]]
		link, ok := parseWikiLink(raw, source.Content[span[2]:span[3]])
		if !ok || !names[graphKey(link.Target)] {
			continue
		}
		out = append(out, BacklinkContext{
			SourceID: source.ID,
			Source:   entryDisplayTitle(source),
			Target:   link.Target,
			Heading:  link.Heading,
			Block:    link.Block,
			Display:  link.Display,
			Embed:    link.Embed,
			Line:     1 + strings.Count(source.Content[:span[0]], "\n"),
			Excerpt:  lineExcerpt(source.Content, span[0]),
		})
	}
	return out
}

func lineExcerpt(content string, bytePos int) string {
	if bytePos < 0 || bytePos > len(content) {
		return ""
	}
	start := strings.LastIndex(content[:bytePos], "\n") + 1
	end := strings.Index(content[bytePos:], "\n")
	if end < 0 {
		end = len(content)
	} else {
		end += bytePos
	}
	line := strings.TrimSpace(content[start:end])
	if utf8.RuneCountInString(line) > 240 {
		line = string([]rune(line)[:240]) + "..."
	}
	return line
}
