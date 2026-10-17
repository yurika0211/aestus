# Agent integration contract

Import `github.com/yurika0211/aestus/memory` using Go 1.25 or newer. Pin a
reviewed module version or commit rather than following a moving branch. No
HTTP service, provider, agent or global default vault is required.

The following is a function body with a context.Context named ctx:

```go
store, err := memory.NewStore("./.aestus/memory")
if err != nil {
    return err
}
defer store.Close()

written, err := store.Remember(ctx, memory.RememberRequest{
    Content: "Project directory is /workspace/example",
    Category: "project",
    Tier: memory.TierLong,
    Importance: 0.9,
    Mode: memory.WriteUpsertState,
    Options: memory.SaveOptions{
        StateKey: "project.directory",
        StateValue: "/workspace/example",
    },
})
if err != nil {
    return err
}
_ = written.ID // Use the actual write ID, not a guess from Recent(1).

results, err := store.Recall(ctx, memory.RecallRequest{
    Query: "project directory",
    Options: memory.SearchOptions{
        Limit: 5, IncludeGraph: true, GraphDepth: 1, SkipAccessStats: true,
    },
})
_ = results // Render the returned structured evidence in the host.
return err
```

Full runnable setup is in `examples/basic`.

## Ownership

The host decides when to capture a fact, content admission, authenticated
identity, visibility, context budgets, provider messages and tool execution.
`AnalyzeMemoryContent` supplies hygiene findings for the host's admission
policy. `Remember` checks structural write requirements; it does not turn all
hygiene findings into a mandatory application policy.

Aestus owns note format, deduplication, graph activation, temporal resolution,
maintenance and optional reranking. Short-term buffers and mid-term summary
storage are reusable library components; the host still owns their session IDs,
summary generation triggers and cleanup. The library never runs an LLM.

## Visibility

ActivationOptions, SearchOptions and RouteOptions accept an EntryFilter
predicate. Derive it from the authenticated request, never model-supplied
namespace parameters. Nil selects the whole vault. Predicates run under the
store lock and must be pure: do not call back into the store or mutate arguments.

Filtering precedes seed selection, graph propagation, top-K and current-state
selection. Historical search also applies the filter. Invisible notes cannot
act as graph intermediaries or suppress visible aliases. For core/profile
evidence, use `ByTierWithOptions`: it resolves visible current states across
tiers before selecting a tier. Legacy `ByTier` keeps administrative listing
semantics.

RememberRequest.EntryFilter restricts duplicate matching, supersedes references
and upsert_state to the host's write scope. The proposed note must also satisfy
the filter. Supply trusted scope metadata in SaveOptions. Tags alone do not
create authentication or authorization.

## Writes, cancellation and failures

- Empty mode means append; modes are append, upsert_state and supersede. Results
  include the actual ID/path, created/updated flags and superseded IDs. Repeating
  an identical state update preserves the current ID without self-supersession.
  Upsert requires a currently valid active state; use append for scheduled or
  historical notes. Returning to an earlier value creates a new version and
  preserves the old note's history.
- State changes run under one Store mutex and one persistence pass. Concurrent
  readers/writers using that same Store cannot see an intermediate transition.
- A vault requires a single writer Store/process. This is not a multi-file
  crash-atomic transaction or a cross-process lock. I/O failure can leave a
  partial commit; surface the error and reconcile/reopen the vault rather than
  automatically retrying or switching to another engine.
- Cancellation is checked before mutation, including after waiting for the
  mutex. Once mutation starts, the synchronous write finishes and returns its
  actual outcome even if the context expires. Filesystem calls and custom
  rerankers cannot be interrupted.
- Recall checks cancellation before and after synchronous search. Set
  SkipAccessStats for diagnostic reads: normal search can update usage counts,
  and reranker telemetry hooks can have their own effects.
- Close releases owned optional resources, including the installed reranker.
  The host controls startup, shutdown and error presentation.

## Compatibility

Markdown remains authoritative. Test an existing LuckyAgent vault backup before
opening the original path. IDs, paths, frontmatter, aliases, wikilinks,
heading/block locators and history keep the existing format. Indexes and Tidal
SQLite state are derived data.

Older save/search/admin methods remain available for inspection and migration.
New application writes should use Remember rather than splitting state changes
into host-side read/save/supersede calls. Future SDK versions may change their
API: adapters should carry only consumer-required evidence and run contract
tests on upgrades.
