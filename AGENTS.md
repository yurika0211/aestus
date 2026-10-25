# Aestus contributor notes

- Keep `memory` independent of LuckyAgent, provider SDKs, and MCP.
- `cmd/aestus-mcp` is the only MCP adapter. It calls `memory.Store` and does not grow a second store.
- Preserve the Obsidian folder names, YAML frontmatter, wikilinks, and block ID format.
- Run `GOCACHE=/tmp/aestus-go-cache go test ./...` before submitting changes.
- Keep examples and docs pointed at explicit Aestus vault paths such as `./.aestus/memory`.
