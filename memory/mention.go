package memory

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// UnlinkedMention is a public note name written as plain text, not as a
// wikilink. Listing it does not create an edge or change activation. Turning
// it into a link is a separate confirmation.
type UnlinkedMention struct {
	SourceID string `json:"source_id"`
	Source   string `json:"source"`
	Name     string `json:"name"`
	TargetID string `json:"target_id"`
	Line     int    `json:"line"`
	Excerpt  string `json:"excerpt"`
}

// UnlinkedMentions lists plain-text mentions of another note's title, alias,
// or filename. Short names, pure numbers, and partial overlaps are skipped.
// A name owned by more than one note is skipped rather than guessed.
func (s *Store) UnlinkedMentions() []UnlinkedMention {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	byName := make(map[string][]namedNote)
	for id, entry := range s.entries {
		seen := make(map[string]bool)
		for _, name := range graphPublicNamesForEntry(entry) {
			name = strings.TrimSpace(name)
			if !mentionableName(name) {
				continue
			}
			key := graphKey(name)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			byName[key] = append(byName[key], namedNote{id: id, name: name})
		}
	}

	var out []UnlinkedMention
	for _, source := range s.entries {
		if source == nil {
			continue
		}
		linked := make(map[string]bool)
		for _, link := range parseWikiLinks(source.Content) {
			linked[graphKey(link.Target)] = true
		}
		for _, found := range findUnlinkedMentions(source, byName, linked) {
			out = append(out, found)
		}
	}
	return out
}

type namedNote struct {
	id   string
	name string
}

func mentionableName(name string) bool {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) < 3 {
		return false
	}
	digits := true
	for _, r := range name {
		if !unicode.IsDigit(r) {
			digits = false
			break
		}
	}
	return !digits
}

func findUnlinkedMentions(source *Entry, byName map[string][]namedNote, linked map[string]bool) []UnlinkedMention {
	text := stripWikiSyntax(source.Content)
	var out []UnlinkedMention
	seen := make(map[string]bool)
	for key, notes := range byName {
		if linked[key] || len(notes) != 1 || notes[0].id == source.ID {
			continue
		}
		name := notes[0].name
		bytePos := indexWholeMention(text, name)
		if bytePos < 0 || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, UnlinkedMention{
			SourceID: source.ID,
			Source:   entryDisplayTitle(source),
			Name:     name,
			TargetID: notes[0].id,
			Line:     1 + strings.Count(text[:bytePos], "\n"),
			Excerpt:  lineExcerpt(text, bytePos),
		})
	}
	return out
}

// indexWholeMention matches the name only when it is not inside a longer
// token. A partial overlap such as "log" inside "login" is not a mention.
func indexWholeMention(text, name string) int {
	lowerText := strings.ToLower(text)
	lowerName := strings.ToLower(name)
	from := 0
	for from <= len(lowerText)-len(lowerName) {
		at := strings.Index(lowerText[from:], lowerName)
		if at < 0 {
			return -1
		}
		at += from
		if mentionBounded(text, at, at+len(lowerName)) {
			return at
		}
		from = at + len(lowerName)
	}
	return -1
}

func mentionBounded(text string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(text[:start])
		if isMentionToken(r) {
			return false
		}
	}
	if end < len(text) {
		r, _ := utf8.DecodeRuneInString(text[end:])
		if isMentionToken(r) {
			return false
		}
	}
	return true
}

func isMentionToken(r rune) bool {
	// CJK text has no spaces between words, so a following Han character is
	// still a bounded mention. ASCII letters and digits stay strict, which
	// keeps "log" inside "login" from matching.
	if unicode.Is(unicode.Han, r) {
		return false
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}
