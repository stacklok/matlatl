# 13. Topology-based link prediction (suggested links)

Date: 2026-06-06
Status: Accepted

## Context

The knowledge-gap signal (ADR 0007, refined by ADR 0012) frames "missing
connections" at the granularity of **weakly-connected components**: a gap is a
pair of distinct WCCs that have, by construction, **zero** navigational links
between them. That signal is:

- **Coarse.** It only fires for wholly-disconnected clusters. Two documents that
  already sit in the same component — both linked from the same index, both
  citing the same reference — but that never link to *each other* are invisible
  to it, even though that is exactly the missing-link an author most often wants.
- **O(k²) over kept components.** It enumerates every component pair, so on a
  fragmented corpus it can explode (hence `MaxGaps` truncation).

Link prediction over a graph's *topology* is a well-studied way to surface
likely-missing edges: two nodes that share many neighbours are structurally
similar and are good candidates to be connected. We want this signal for
documentation: "these two pages clearly belong together but nobody linked them."

## Decision

Add a **`suggested-link`** signal that **AUGMENTS** (does not replace) the
WCC-pair knowledge-gap signal. Both ship; they answer different questions.
Knowledge gaps flag wholly-disconnected clusters; suggested links flag concrete
**unlinked but structurally-close pairs**.

### Scoring

Over the document projection (`out(x)`, `in(x)`, both sorted, deduped,
self-loop-free), for two documents A and B with the undirected neighbour closure
`N(x) = out(x) ∪ in(x)`:

- **Bibliographic coupling** `coupling(A,B) = |out(A) ∩ out(B)|` — they link to
  the same docs.
- **Co-citation** `cocitation(A,B) = |in(A) ∩ in(B)|` — the same docs link to
  both.
- **Adamic/Adar** (the **primary** ranking score): `Σ_{c ∈ N(A)∩N(B), |N(c)|>1}
  1/log(|N(c)|)`. A rare shared neighbour (small degree) is strong evidence; a
  hub everything links to is weak evidence (its `1/log(deg)` weight is tiny).
- `sharedNeighbours = |N(A) ∩ N(B)|`.

Coupling and co-citation are reported as components/details; Adamic/Adar is the
rank key.

### Scope and gating

- **Unlinked pairs only:** a pair is dropped if `B ∈ out(A)` or `A ∈ out(B)`
  (already connected either way).
- **`MinSharedNeighbours` default 2** (config-only knob `linkSuggestionMinShared`,
  no CLI flag): a single shared neighbour is weak evidence.
- **Info severity, never gates `check`** — not even under `--strict`. It is an
  experimental discoverability hint, not a defect, so it is excluded from the
  exit-code contract (ADR 0005) the same way `knowledge-gap` is.
- **Document-level only.** Section-granularity (`why-related`) is deferred.

### Determinism and bounds

The candidate space is generated **by shared neighbours**, not by all pairs:
precompute `N(x)` and `deg(x)` once, then for each common neighbour `c` accumulate
over every unordered pair within `N(c)`. This makes the cost
**O(Σ_c deg(c)²)** over the common neighbours used as generators, rather than
O(V²).

Two guards bound it and keep output deterministic:

- **Hub fan-out guard** `MaxNeighbourFanout = 256`: a common neighbour whose
  undirected degree exceeds this is **skipped as a pair-generator** (an index page
  linking hundreds of docs would otherwise dominate the candidate space while
  contributing almost no Adamic/Adar weight). Skipping sets a `HubsSkipped` /
  truncation flag.
- **Hard cap** `MaxSuggestedLinks = 1000`, mirroring `MaxGaps`; on hitting it the
  list is truncated and a notice is surfaced (no silent cap).

Determinism (ADR 0004): the Adamic/Adar float sum is accumulated by iterating
common neighbours in **sorted** order, so the float addition order is fixed and
the score is byte-stable. Suggestions are ranked by Adamic/Adar DESC, then
sharedNeighbours DESC, then DocA ASC, then DocB ASC (the same direct float-compare
pattern as HITS `rankDesc`, no epsilon). The algorithm uses stdlib + `math` only.

### Surfaces

- **graph.json** (schema **v3**): a top-level `suggestedLinks` array (full capped
  list) + a `summary.suggestedLinks` count. The Adamic/Adar score reuses the
  fixed-precision `Float` type.
- **findings.json** (schema **v4**): a `suggested-link` finding kind (Info), with
  `details.suggestedTarget` / `sharedNeighbours` / `coupling` / `coCitation` /
  `adamicAdar`, plus a `remediationGuide` entry and a `summary.suggestedLink`
  count.
- **Human reports** (markdown/terminal): a top-5 `Suggested links` section /
  closing note, marked experimental.
- **MCP**: a `suggest-links` tool — doc-scoped (the partners of one document) or
  global top-N.

## Consequences

- A new, additive signal catches the common "these two clearly belong together"
  case the WCC-pair gap signal cannot.
- It never breaks a build, so it is safe to ship on by default.
- The hub fan-out guard means some structurally-close pairs that only co-occur
  under a very high-degree hub are not generated; this is reported via truncation
  and is an acceptable precision/cost trade (those pairs carry little signal).
- `why-related` (section-level reasoning about *why* two docs are related) is
  deferred to a later ADR.
