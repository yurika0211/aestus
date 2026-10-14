package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// NoteRenameOptions controls human-readable Obsidian note renaming.
type NoteRenameOptions struct {
	Apply bool
	Limit int
}

// NoteRenameReport describes planned or applied memory note renames.
type NoteRenameReport struct {
	Scanned               int                 `json:"scanned"`
	WouldRename           int                 `json:"would_rename"`
	Renamed               int                 `json:"renamed,omitempty"`
	WouldPruneDuplicates  int                 `json:"would_prune_duplicates"`
	PrunedDuplicates      int                 `json:"pruned_duplicates,omitempty"`
	Entries               []NoteRenamePlan    `json:"entries,omitempty"`
	DuplicatePruneEntries []NoteDuplicatePlan `json:"duplicate_prune_entries,omitempty"`
	Apply                 bool                `json:"apply"`
}

// NoteRenamePlan is one memory note path change.
type NoteRenamePlan struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Category string `json:"category,omitempty"`
	Tier     string `json:"tier,omitempty"`
	Preview  string `json:"preview,omitempty"`
}

// NoteDuplicatePlan is one duplicate Markdown note moved out of the Obsidian graph.
type NoteDuplicatePlan struct {
	ID     string `json:"id"`
	Keep   string `json:"keep"`
	Remove string `json:"remove"`
	Backup string `json:"backup,omitempty"`
}

// RenameNotes plans or applies human-readable Markdown filenames while keeping
// memory IDs in frontmatter and block IDs stable.
func (s *Store) RenameNotes(opts NoteRenameOptions) (NoteRenameReport, error) {
	report := NoteRenameReport{Apply: opts.Apply}
	if s == nil {
		return report, nil
	}
	if opts.Limit <= 0 {
		opts.Limit = 1000
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	duplicatePlans, canonical, err := s.planDuplicateNotePrunesLocked()
	if err != nil {
		return report, err
	}
	report.WouldPruneDuplicates = len(duplicatePlans)
	report.DuplicatePruneEntries = duplicatePlans
	if opts.Apply {
		for id, file := range canonical {
			s.entries[id] = file.Entry
			s.paths[id] = file.Rel
		}
		s.ensureConceptEntriesLocked(renameNotesEntryLinks(s.entries))
	}

	ids := sortedMemoryEntryIDs(s.entries)
	used := make(map[string]string, len(ids))
	for _, id := range ids {
		entry := s.entries[id]
		if entry == nil {
			continue
		}
		if rel := filepath.ToSlash(strings.TrimSpace(entry.Path)); rel != "" {
			used[rel] = id
		}
	}

	var plans []NoteRenamePlan
	for _, id := range ids {
		if len(plans) >= opts.Limit {
			break
		}
		entry := s.entries[id]
		if entry == nil || renameNotesSkipEntry(entry) {
			continue
		}
		report.Scanned++
		oldRel := filepath.ToSlash(strings.TrimSpace(entry.Path))
		if oldRel == "" {
			continue
		}
		delete(used, oldRel)
		newRel := s.uniqueNotePathForEntry(entry, used, oldRel)
		if newRel == oldRel {
			used[oldRel] = id
			continue
		}
		used[newRel] = id
		plan := NoteRenamePlan{
			ID:       entry.ID,
			From:     oldRel,
			To:       newRel,
			Category: entry.Category,
			Tier:     entry.Tier.String(),
			Preview:  truncateRunes(strings.ReplaceAll(strings.TrimSpace(entry.Content), "\n", " "), 120),
		}
		plans = append(plans, plan)
	}
	report.WouldRename = len(plans)
	report.Entries = plans
	if !opts.Apply {
		return report, nil
	}
	if err := s.applyDuplicatePrunePlansLocked(duplicatePlans, &report); err != nil {
		return report, err
	}
	if len(plans) == 0 {
		if err := s.persist(); err != nil {
			return report, err
		}
		if err := s.pruneDuplicateNotesAfterPersistLocked(&report); err != nil {
			return report, err
		}
		return report, nil
	}

	for _, plan := range plans {
		oldPath := filepath.Join(s.dir, filepath.FromSlash(plan.From))
		newPath := filepath.Join(s.dir, filepath.FromSlash(plan.To))
		if err := os.MkdirAll(filepath.Dir(newPath), 0o700); err != nil {
			return report, fmt.Errorf("create rename target dir: %w", err)
		}
		if err := os.Rename(oldPath, newPath); err != nil && !os.IsNotExist(err) {
			return report, fmt.Errorf("rename memory note %s to %s: %w", plan.From, plan.To, err)
		}
		if entry := s.entries[plan.ID]; entry != nil {
			entry.Path = plan.To
			s.paths[plan.ID] = plan.To
		}
		report.Renamed++
	}
	if err := s.rewriteIncomingLinksLocked(plans); err != nil {
		return report, err
	}
	if err := s.persist(); err != nil {
		return report, err
	}
	if err := s.pruneDuplicateNotesAfterPersistLocked(&report); err != nil {
		return report, err
	}
	return report, nil
}

type noteFileCandidate struct {
	Entry   *Entry
	Rel     string
	ModTime time.Time
}

func (s *Store) planDuplicateNotePrunesLocked() ([]NoteDuplicatePlan, map[string]noteFileCandidate, error) {
	byID := make(map[string][]noteFileCandidate)
	err := filepath.Walk(s.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case ".lh-index", ".obsidian":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		entry, ok, err := parseMemoryNote(path, s.dir)
		if err != nil {
			return err
		}
		if !ok || entry == nil || strings.TrimSpace(entry.ID) == "" {
			return nil
		}
		byID[entry.ID] = append(byID[entry.ID], noteFileCandidate{
			Entry:   entry,
			Rel:     filepath.ToSlash(entry.Path),
			ModTime: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	var plans []NoteDuplicatePlan
	canonical := make(map[string]noteFileCandidate)
	for id, files := range byID {
		if len(files) == 0 {
			continue
		}
		preferred := duplicateIdealNotePath(s.entries[id], files)
		if preferred == "" {
			preferred = filepath.ToSlash(strings.TrimSpace(s.paths[id]))
		}
		if preferred == "" {
			if entry := s.entries[id]; entry != nil {
				preferred = filepath.ToSlash(strings.TrimSpace(entry.Path))
			}
		}
		sort.Slice(files, func(i, j int) bool {
			return noteFileCandidateLess(files[i], files[j], preferred)
		})
		keep := files[0]
		canonical[id] = keep
		if len(files) == 1 {
			continue
		}
		for _, remove := range files[1:] {
			plans = append(plans, NoteDuplicatePlan{
				ID:     id,
				Keep:   keep.Rel,
				Remove: remove.Rel,
				Backup: duplicateNoteBackupPath(remove.Rel),
			})
		}
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].ID != plans[j].ID {
			return plans[i].ID < plans[j].ID
		}
		return plans[i].Remove < plans[j].Remove
	})
	return plans, canonical, nil
}

func noteFileCandidateLess(a, b noteFileCandidate, preferredRel string) bool {
	preferredRel = filepath.ToSlash(strings.TrimSpace(preferredRel))
	if preferredRel != "" {
		aPreferred := a.Rel == preferredRel
		bPreferred := b.Rel == preferredRel
		if aPreferred != bPreferred {
			return aPreferred
		}
	}
	aMachine := machineMemoryNotePath(a.Rel)
	bMachine := machineMemoryNotePath(b.Rel)
	if aMachine != bMachine {
		return !aMachine
	}
	if !a.ModTime.Equal(b.ModTime) {
		return a.ModTime.After(b.ModTime)
	}
	if len(a.Rel) != len(b.Rel) {
		return len(a.Rel) < len(b.Rel)
	}
	return a.Rel < b.Rel
}

func duplicateIdealNotePath(entry *Entry, files []noteFileCandidate) string {
	candidate := entry
	if candidate == nil && len(files) > 0 {
		candidate = files[0].Entry
	}
	if candidate == nil {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(candidate.Category), "concept") {
		return conceptNotePath(candidate.Content)
	}
	currentRel := filepath.ToSlash(strings.TrimSpace(candidate.Path))
	if strings.EqualFold(strings.TrimSpace(candidate.Status), "archived") || strings.HasPrefix(currentRel, "90_Archive/dirty/") || duplicateCandidatesInDirtyArchive(files) {
		return filepath.ToSlash(filepath.Join("90_Archive", "dirty", humanMemoryFileBase(candidate)+".md"))
	}
	return notePathForEntry(candidate)
}

func duplicateCandidatesInDirtyArchive(files []noteFileCandidate) bool {
	for _, file := range files {
		if strings.HasPrefix(filepath.ToSlash(strings.TrimSpace(file.Rel)), "90_Archive/dirty/") {
			return true
		}
	}
	return false
}

func machineMemoryNotePath(rel string) bool {
	base := strings.TrimSuffix(filepath.Base(filepath.FromSlash(rel)), ".md")
	return strings.Contains(base, "mem_") || strings.HasPrefix(base, "20") && strings.Contains(base, "-")
}

func duplicateNoteBackupPath(rel string) string {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.TrimLeft(rel, "/")
	if rel == "" {
		rel = "duplicate.md"
	}
	return filepath.ToSlash(filepath.Join(".lh-index", "duplicate-notes", rel+".bak"))
}

func (s *Store) applyDuplicatePrunePlansLocked(plans []NoteDuplicatePlan, report *NoteRenameReport) error {
	for _, plan := range plans {
		oldPath := filepath.Join(s.dir, filepath.FromSlash(plan.Remove))
		backupRel := plan.Backup
		if backupRel == "" {
			backupRel = duplicateNoteBackupPath(plan.Remove)
		}
		backupPath := filepath.Join(s.dir, filepath.FromSlash(backupRel))
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
			return fmt.Errorf("create duplicate backup dir: %w", err)
		}
		if err := os.Rename(oldPath, backupPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("prune duplicate memory note %s: %w", plan.Remove, err)
		}
		if report != nil {
			report.PrunedDuplicates++
		}
	}
	return nil
}

func (s *Store) pruneDuplicateNotesAfterPersistLocked(report *NoteRenameReport) error {
	plans, canonical, err := s.planDuplicateNotePrunesLocked()
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		return nil
	}
	for id, file := range canonical {
		s.entries[id] = file.Entry
		s.paths[id] = file.Rel
	}
	if report != nil {
		report.WouldPruneDuplicates += len(plans)
		report.DuplicatePruneEntries = append(report.DuplicatePruneEntries, plans...)
	}
	if err := s.applyDuplicatePrunePlansLocked(plans, report); err != nil {
		return err
	}
	return s.persist()
}

// rewriteIncomingLinksLocked updates wikilinks that name a note by its old
// filename. Heading, block, and display text stay. A mention that is not a
// wikilink is left alone. The caller persists after the path rename.
func (s *Store) rewriteIncomingLinksLocked(plans []NoteRenamePlan) error {
	renames := make(map[string]string)
	for _, plan := range plans {
		oldName := strings.TrimSuffix(filepath.Base(filepath.FromSlash(plan.From)), ".md")
		newName := strings.TrimSuffix(filepath.Base(filepath.FromSlash(plan.To)), ".md")
		if oldName == "" || newName == "" || oldName == newName {
			continue
		}
		key := graphKey(oldName)
		if key == "" {
			continue
		}
		if previous, exists := renames[key]; exists && previous != newName {
			return fmt.Errorf("rename %q and another note to different names from %q", newName, oldName)
		}
		renames[key] = newName
	}
	if len(renames) == 0 {
		return nil
	}
	for _, entry := range s.entries {
		if entry == nil || !incomingLinksNeedRename(entry.Content, renames) {
			continue
		}
		updated := renameWikiLinkTargets(entry.Content, renames)
		if updated == entry.Content {
			continue
		}
		entry.Content = updated
		entry.Links = normalizeMemoryLinks(extractWikiLinks(updated))
	}
	return nil
}

func incomingLinksNeedRename(content string, renames map[string]string) bool {
	for _, link := range parseWikiLinks(content) {
		if _, ok := renames[graphKey(link.Target)]; ok {
			return true
		}
	}
	return false
}

func renameWikiLinkTargets(content string, renames map[string]string) string {
	return wikiLinkSpan.ReplaceAllStringFunc(content, func(match string) string {
		span := wikiLinkSpan.FindStringSubmatchIndex(match)
		if span == nil {
			return match
		}
		inner := match[span[2]:span[3]]
		link, ok := parseWikiLink(match, inner)
		if !ok {
			return match
		}
		newName, ok := renames[graphKey(link.Target)]
		if !ok {
			return match
		}
		body := newName
		if link.Heading != "" && link.Block != "" {
			body += "#" + link.Heading + "#^" + link.Block
		} else if link.Block != "" {
			body += "#^" + link.Block
		} else if link.Heading != "" {
			body += "#" + link.Heading
		}
		rewritten := "[[" + body
		if link.Display != "" {
			rewritten += "|" + link.Display
		}
		rewritten += "]]"
		if link.Embed {
			rewritten = "!" + rewritten
		}
		return rewritten
	})
}

func renameNotesSkipEntry(entry *Entry) bool {
	return entry == nil
}

func renameNotesEntryLinks(entries map[string]*Entry) []string {
	var links []string
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		links = append(links, entry.Links...)
		links = append(links, extractWikiLinks(entry.Content)...)
	}
	return normalizeMemoryLinks(links)
}
