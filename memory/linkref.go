package memory

import (
	"regexp"
	"strings"
)

// WikiLink is one Obsidian wikilink as written. Target is the note name the
// graph edge uses; heading, block, display text, and embed stay attached so
// later recall can point at a section without drawing a second edge.
type WikiLink struct {
	Target  string `json:"target"`
	Heading string `json:"heading,omitempty"`
	Block   string `json:"block,omitempty"`
	Display string `json:"display,omitempty"`
	Embed   bool   `json:"embed,omitempty"`
	Raw     string `json:"raw,omitempty"`
}

var wikiLinkSpan = regexp.MustCompile(`!?\[\[([^\]]+)\]\]`)

// parseWikiLinks keeps heading, block, display text, and embed. A heading and
// a block in the same link are both kept. The note target is what the graph
// resolves; the rest is detail on that one edge.
func parseWikiLinks(text string) []WikiLink {
	if text == "" {
		return nil
	}
	spans := wikiLinkSpan.FindAllStringSubmatchIndex(text, -1)
	out := make([]WikiLink, 0, len(spans))
	for _, span := range spans {
		raw := text[span[0]:span[1]]
		inner := text[span[2]:span[3]]
		link, ok := parseWikiLink(raw, inner)
		if !ok {
			continue
		}
		out = append(out, link)
	}
	return out
}

func parseWikiLink(raw, inner string) (WikiLink, bool) {
	link := WikiLink{Raw: raw, Embed: strings.HasPrefix(raw, "!")}
	body, display, hasDisplay := strings.Cut(inner, "|")
	if hasDisplay {
		link.Display = strings.TrimSpace(display)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return WikiLink{}, false
	}
	target, anchor, hasAnchor := strings.Cut(body, "#")
	link.Target = strings.TrimSpace(target)
	if link.Target == "" {
		return WikiLink{}, false
	}
	if hasAnchor {
		anchor = strings.TrimSpace(anchor)
		if block, ok := strings.CutPrefix(anchor, "^"); ok {
			link.Block = strings.TrimSpace(block)
		} else if heading, block, ok := strings.Cut(anchor, "#^"); ok {
			link.Heading = strings.TrimSpace(heading)
			link.Block = strings.TrimSpace(block)
		} else {
			link.Heading = anchor
		}
	}
	return link, true
}
