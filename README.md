# Aestus

Aestus is a file first memory library for Go agents. It stores durable memories as Obsidian compatible Markdown, so the vault remains portable, inspectable, and useful without a database or a hosted service.

Aestus was extracted from [LuckyAgent](https://github.com/yurika0211/luckyagent). The package name remains `memory` to keep the extracted API small and familiar; LuckyAgent may later consume Aestus as a normal Go module.

## Layers

- **Short term:** an in process sliding conversation buffer with deterministic overflow summaries.
- **Mid term:** session summaries persisted as Markdown for later topic recall.
- **Durable:** a three tier Markdown vault for facts, preferences, rules, projects, decisions, and graph links.
- **Optional tidal reranker:** a conservative feedback driven reranker with optional SQLite telemetry.

The durable vault is the source of truth. Tidal SQLite data only contains runtime telemetry and learned ranking kernels.

## Quickstart

```go
package main

import (
    "fmt"
    "log"

    "github.com/yurika0211/aestus/memory"
)

func main() {
    store, err := memory.NewStore("./.aestus/memory")
    if err != nil {
        log.Fatal(err)
    }

    if err := store.SaveWithTier(
        "Prefer concise Go examples with tests.",
        "preference",
        memory.TierLong,
        0.9,
    ); err != nil {
        log.Fatal(err)
    }

    for _, entry := range store.Search("Go examples") {
        fmt.Printf("[%s] %s\n", entry.Category, entry.Content)
    }
}
```

`NewStore` takes the vault directory explicitly. It creates the directory and the standard vault folders on first use.

## Vault layout

Aestus keeps the extracted Obsidian layout and Markdown frontmatter format:

```text
.aestus/memory/
├── 00_Index/
├── 10_Profile/
├── 20_Projects/
├── 30_Sessions/
├── 40_Decisions/
├── 50_Facts/
├── 60_Rules/
├── 70_Concepts/
├── 70_Trajectories/
├── 90_Archive/
└── .lh-index/
```

Notes contain YAML frontmatter, a Markdown body, Obsidian wikilinks, aliases, tags, temporal state, and stable block IDs. Existing LuckyAgent vaults can be opened by passing their directory to `NewStore`; the reader preserves the existing note format and legacy index note.

## Package surface

The public import path is:

```text
github.com/yurika0211/aestus/memory
```

The package includes durable save/search/recall, graph activation and topology, temporal state routing, hygiene scans and quarantine, note migration/rename helpers, short term buffers, mid term summaries, and the optional tidal reranker/store. `memory.Message` is a small LLM agnostic `{Role, Content}` type returned by short term context helpers; convert it to the message type used by your provider.

See [`docs/`](docs/) for the memory model, vault format, hygiene guidance, recall patterns, and tidal design notes. A minimal runnable example is in [`examples/basic`](examples/basic).

## License

MIT © 2026 yurika0211. Aestus is extracted from LuckyAgent, which is also MIT licensed.
