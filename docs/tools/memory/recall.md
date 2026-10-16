# Recall and search

Use `Store.Search` for the common path:

```go
entries := store.Search("deployment rule")
```

For score explanations, call `Activate(query, memory.ActivationOptions{Explain: true})`. Default search combines lexical terms, aliases, tags, importance, tier, recency, access, and one hop of wikilink/backlink/tag graph propagation. `SearchParallel` provides the same activation semantics for bounded parallel workloads.

The returned `Entry.Path` is relative to the caller supplied vault directory. Aestus reads Markdown notes directly; it does not require or include a RAG index.

`Search` is unchanged. To compare a recalled pool before cutting it, set both limits on `SearchWithOptions`:

```go
results := store.SearchWithOptions(query, memory.SearchOptions{
    Limit:        5,
    IncludeGraph: true,
    GraphDepth:   1,
    RerankPool:   20,
    RerankLimit:  5,
})
```

The first stage still scores each note alone and keeps `RerankPool` hits. The second stage only compares those hits, using shared state keys, links between them, shared aliases, and located block excerpts. It does not score the rest of the vault. `RerankPool` of zero skips the comparison and returns the first-stage order.
