# MCP memory server

`aestus-mcp` exposes one Aestus vault to any MCP client. It is a transport
adapter over `Store.Remember`, `Store.Recall`, `Store.Get`, `Store.Delete`,
and `Store.Stats`. Markdown notes remain the source of truth.

The process that starts the server chooses the vault. Tool arguments cannot
select another directory, a caller namespace, or an authorization filter.
One process owns one vault. A partial disk commit is returned as an error and
is not retried.

## Run

Stdio, for clients that launch the process:

```bash
go run ./cmd/aestus-mcp --vault ./.aestus/memory
```

Streamable HTTP, for clients that connect to a URL:

```bash
go run ./cmd/aestus-mcp --vault ./.aestus/memory --http 127.0.0.1:8765
```

`--vault` falls back to `AESTUS_VAULT`. HTTP listens only on the address you
pass. Logs go to stderr so they do not mix with stdio JSON-RPC.

## Client config

Stdio:

```json
{
  "mcpServers": {
    "aestus": {
      "command": "aestus-mcp",
      "args": ["--vault", "/absolute/path/to/.aestus/memory"]
    }
  }
}
```

HTTP:

```json
{
  "mcpServers": {
    "aestus": {
      "url": "http://127.0.0.1:8765"
    }
  }
}
```

Build the binary with `go build -o aestus-mcp ./cmd/aestus-mcp` and put that
path in `command` when the client does not share this module.

## Tools

| Tool | Use |
| --- | --- |
| `aestus_remember` | Save one fact. `mode` is `append`, `upsert_state`, or `supersede`. |
| `aestus_recall` | Search. Default limit 8, hard cap 20. Results include id, relative path, score, and a short excerpt. |
| `aestus_get` | Read one note by id, including its content. |
| `aestus_forget` | Delete one note by id. There is no delete-by-query. |
| `aestus_stats` | Count active notes by tier. |

`aestus_recall` does not update access counts. `upsert_state` requires
`state_key`. `supersede` requires `supersedes`.

## Resources

- `aestus://vault` is a JSON index of recent notes and tier counts.
- `aestus://note/{id}` is the full Markdown note.

## Out of scope

Session summaries, hygiene, maintenance, tidal reranking, and per-caller
authorization stay in the library or in the host. This server does not import
LuckyAgent or a provider SDK.
