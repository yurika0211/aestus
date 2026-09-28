# Aestus contributor notes

- Keep `memory` independent of LuckyAgent and provider SDKs.
- Preserve the Obsidian folder names, YAML frontmatter, wikilinks, and block ID format.
- Run `GOCACHE=/tmp/aestus-go-cache go test ./...` before submitting changes.
- Keep examples and docs pointed at explicit Aestus vault paths such as `./.aestus/memory`.
