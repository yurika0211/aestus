package memory

import (
	"errors"
	"strings"
)

// repeatedBlockThreshold is how many identical non-trivial blocks in a row
// refuse a save. Normal notes and a single Links section stay under it.
const repeatedBlockThreshold = 8

// ErrRepeatedMemoryBlock is returned when a save would persist the same
// Memory/Links block many times. The vault file is left unchanged.
var ErrRepeatedMemoryBlock = errors.New("memory note repeats the same Memory/Links block")

func repeatedMemoryBlock(content string) bool {
	return maxConsecutiveBlocks(splitRepeatBlocks(content)) >= repeatedBlockThreshold
}

func splitRepeatBlocks(content string) []string {
	text := strings.ReplaceAll(content, "\r\n", "\n")
	var blocks []string
	var current []string
	flush := func() {
		block := strings.TrimSpace(strings.Join(current, "\n"))
		current = nil
		if block != "" {
			blocks = append(blocks, block)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), "## memory") && len(current) > 0 {
			flush()
		}
		current = append(current, line)
	}
	flush()
	if len(blocks) >= repeatedBlockThreshold {
		return blocks
	}
	return splitParagraphs(text)
}

func splitParagraphs(content string) []string {
	parts := strings.Split(content, "\n\n")
	blocks := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			blocks = append(blocks, part)
		}
	}
	return blocks
}

func maxConsecutiveBlocks(blocks []string) int {
	best := 1
	run := 1
	for i := 1; i < len(blocks); i++ {
		if normalizeRepeatBlock(blocks[i]) == normalizeRepeatBlock(blocks[i-1]) && substantialRepeatBlock(blocks[i]) {
			run++
			if run > best {
				best = run
			}
			continue
		}
		run = 1
	}
	if best < repeatedBlockThreshold {
		return 0
	}
	return best
}

func normalizeRepeatBlock(block string) string {
	lines := strings.Split(strings.ReplaceAll(block, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept = append(kept, strings.ToLower(line))
	}
	return strings.Join(kept, "\n")
}

func substantialRepeatBlock(block string) bool {
	text := strings.TrimSpace(block)
	if strings.Contains(strings.ToLower(text), "## memory") && strings.Contains(strings.ToLower(text), "## links") {
		return true
	}
	return len(text) >= 24
}
