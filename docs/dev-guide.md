<!-- markdownlint-disable MD013 -->
# matlatl developer guide

How `matlatl` is built, how to work on it, and the rules that keep it coherent.
Read this alongside [`architecture.md`](architecture.md) and the [ADRs](adr/).

## Layout

```
cmd/matlatl/            entrypoint + Cobra commands (wires infra → application)
internal/
  domain/                pure: no I/O, no goldmark, no third-party graph/MCP libs
    identity/            DocumentID (canonical-path identity) + path-containment helpers
    corpus/              Document, Section, FrontMatter, Corpus (+ Freeze), indices
    reference/           LinkType/LinkHealth, the pure-domain link Resolver
    graphmodel/          reference graph, hierarchy, reachability, orphans,
                         components (WCC union-find + iterative Tarjan SCC +
                         condensation), HITS, PageRank, betweenness, gaps, trails,
                         scent — all hand-rolled & deterministic
    analysis/            Finding, Severity, AnalysisReport (immutable, sorted)
  application/           Config, ports, the scan→parse→resolve→build→analyze pipeline
  infrastructure/        all I/O + third-party:
    fsscanner/           secure recursive markdown discovery
    mdparser/            goldmark (quarantined here) → domain Documents
    emit/                report / diagram / index / graphjson / trails / llmstxt emitters
    linkcheck/           opt-in external HTTP checker + SSRF guard
    mcpserver/           the MCP server (mark3labs/mcp-go, quarantined here)
  platform/              version, exit codes
docs/                    architecture, ADRs, user/dev guides, schemas
testdata/                fixture corpora + golden artifacts
```

## The rules (non-negotiable)

1. **Dependency direction.** `domain` imports nothing outward — no `application`,
   no `infrastructure`, no goldmark/cobra/graph/MCP/`net/http`. `application`
   imports `domain`. `infrastructure` imports `domain` (and third-party). `cmd`
   wires it together. Enforced in CI by a grep over `go list -deps` (see below).
2. **Determinism.** Every collection is iterated in sorted order; every artifact
   is byte-stable. Go map iteration order must never leak into output. Floats
   (HITS, navigability, betweenness) are emitted at fixed precision. The **Float
   determinism rule:** plain `float64` lives in the domain; the fixed-precision
   `Float` wire type lives ONLY in the `graphjson` layer (`newFloat` rounds before
   storing, so equal inputs render to equal bytes). Sum floats in sorted order and
   read medians from a histogram, never a float sort. The Brandes betweenness pass
   (`centrality.go`) is a worked example: it iterates sources in sorted order and
   accumulates dependencies over **sorted predecessor lists** (built by the
   sorted-neighbour expansion in `ForEachSourceBFS`), so every float division/sum
   runs in a fixed order. PageRank (`pagerank.go`) is the same discipline: the
   dangling-mass sum iterates the **sorted** document set and each node's inbound
   sum iterates the already-sorted `projRev`, so the float addition order is fixed
   (no L2 normalization — total mass is conserved by uniform dangling
   redistribution). Trails (`trails.go`) re-sort the Kahn frontier as a slice
   each pop (never range a map for output); scent (`scent.go`) computes Jaccard
   via a sorted merge-walk. Golden tests assert byte-stability.
3. **Security is not a phase.** Root containment, resource caps, output-path
   sanitization, label escaping, and the SSRF guard are tested with adversarial
   fixtures. See [ADR 0003](adr/0003-security-model.md).
4. **Interfaces at real seams only.** The ports (`FileScanner`, `DocumentParser`
   + `DocumentParserFactory`, `ExternalLinkChecker`, `ArtifactWriter`) exist
   because they're test seams / swap points — not as ceremony.
5. **Identity is the canonical relative path** ([ADR 0001](adr/0001-document-identity.md)) —
   never the basename.

## Build, test, lint

```console
$ task build              # ./bin/matlatl
$ task test               # unit + smoke, under -race
$ task test-integration   # golden + integration (-tags=integration)
$ task cover              # coverage (includes integration-tagged code)
$ task lint               # golangci-lint
$ task check              # fmt + vet + lint + test (run before pushing)
```

### The checks every change must pass

```console
go build ./... && go vet ./... && \
go test -race -count=1 ./... && go test -tags=integration -race -count=1 ./... && \
gofmt -l . && golangci-lint run
```

Plus the **domain-purity** guard (must print nothing):

```console
go list -deps ./internal/domain/... | \
  grep -E 'goldmark|spf13|sabhiram|dominikbraun|mark3labs|net/http|internal/(application|infrastructure)'
```

And the **application stays emitter-agnostic** guard (must print nothing):

```console
go list -deps ./internal/application/... | grep 'infrastructure/emit'
```

## Testing layers

- **Unit** (the bulk) — table-driven, pure-domain where possible. The highest-risk
  surface is link resolution (`reference.Resolver`): every `LinkHealth` branch,
  out-of-root rejection, ambiguity, and **anchor-slug parity** with the parser.
- **Smoke / golden** — emitters are compared against committed goldens under
  `internal/infrastructure/emit/testdata/golden/`. Regenerate intentionally:

  ```console
  MATLATL_UPDATE_GOLDEN=1 go test ./internal/infrastructure/emit/...
  # then review the diff before committing
  ```

- **Integration** (`//go:build integration`) — run the built pipeline over a
  fixture corpus and assert exit codes + artifact contents.
- **Adversarial** — symlink escape, `../` traversal, oversized files, YAML
  alias bombs, hostile titles in diagram/table labels, SSRF to internal hosts,
  nested-repo `.git` markers (file + dir; ADR 0017).
- **Determinism** — concurrent vs single-threaded parse must be byte-identical
  (`TestPipeline_Determinism_AcrossWorkerCounts`).
- **Benchmark** — `go test -bench BenchmarkPipeline_5kDocs -benchmem -run x ./...`
  plus a memory-ceiling assertion guards the O(V+E) expectation.

## Concurrency model

Only the **parse** stage fans out: a bounded worker pool (default `GOMAXPROCS`,
`Config.ParseWorkers` to override; `1` = sequential), each worker with its **own**
parser from `DocumentParserFactory.Clone()` (goldmark instances are not safe to
share). Results are written to disjoint slice indices, then **sorted by
`DocumentID` and merged on a single goroutine**. `Corpus.Freeze()` is called
after the merge; mutating a frozen corpus errors. Everything downstream
(resolve / graph / analyze) runs single-threaded over the frozen corpus. This is
why output is byte-identical at any worker count. See [ADR 0004](adr/0004-ddd-layering-and-scope.md).

## Adding things

- **A new finding kind** — append to the `analysis.FindingKind` iota (keep the
  newest kind last and update `Valid()`'s upper bound to it — currently
  `FarFromRoot`),
  update its `String()`, produce it in `application` with a concrete
  `SuggestedFix` and structured `Details`, add a `remediationGuide` entry **and** a
  `kindPresentationOrder` entry in the findings emitter (a test asserts every kind
  has remediation), bump `FindingsSchemaVersion` + the schema if the artifact
  shape changes, and cover it in `TestCheckExitCode` + a golden. If the kind is a
  non-gating Info hint (like `suggested-link`, `articulation-point`, `bridge`),
  leave `CheckExitCode` untouched so it never gates — even under `--strict`.
- **A new emitter** — put it in `internal/infrastructure/emit/...`, render from
  the frozen `emit.View`/`GraphMetrics` (never mutate the domain), write through
  the `FSWriter`/`safeJoin` guard, escape labels per format, and add a golden +
  byte-stability test.
- **A new graph metric over distances** — reuse the streaming APSP family in
  `graphmodel/apsp.go`: `ForEachSourceDistances` (one BFS per source, a single
  reused distance map, no V² matrix, `O(V)` transient memory) for distance-only
  consumers, or its sibling `ForEachSourceBFS` when you also need the BFS
  discovery order, shortest-path predecessors and path counts (σ) — as Brandes'
  betweenness does (`centrality.go`). Both share the same per-source streaming
  shape (sorted sources, sorted neighbours, reused maps the callback must not
  retain); add another sibling rather than bolting out-parameters onto an existing
  one. Compute betweenness over the **directed** projection and cut structure
  (articulation/bridges, `articulation.go`) over the **undirected** closure — the
  same split navigability uses. See [ADR 0014](adr/0014-navigability-metrics.md)
  and [ADR 0015](adr/0015-critical-path-analysis.md).
- **Anything that needs a link's display text** — the parser threads the anchor
  text (inline-link label, image alt, wikilink alias/target) onto
  `reference.RawReference.AnchorText` → `reference.Reference` (via embedding) →
  `graphmodel.Edge.AnchorText` (with the source `Line`), meaningful only for
  `EdgeReference` edges. It is pure data the resolver ignores (identity stays
  keyed on the target, [ADR 0001](adr/0001-document-identity.md)). The
  information-scent analysis (`graphmodel/scent.go`) is the consumer; its
  **scent-free phrase set and stopword set are in-source constants in
  `scent.go`** (`scentFreePhrases` / `scentStopwords`) — edit them there, they are
  documented inline. See [ADR 0016](adr/0016-agent-experience.md).
- **A new typed front-matter field** — add it to `corpus.FrontMatter`, parse it
  in `mdparser.decodeFrontMatter`, and add the lowercased field name to
  `knownFMKeys` (else it leaks into `Extra`). `TestKnownFMKeysMatchTags` enforces
  the field-set ↔ `knownFMKeys` correspondence. Wikilink aliases are indexed in
  `corpus.indexAliases`: both the `aliases:` list and the single-valued `name:`
  field are added to the `AliasTable`, so `[[name]]` resolves to the document
  (clashes are reported Ambiguous by the resolver, unchanged).
- **A new library** — record the decision (or the choice to hand-roll) in
  [ADR 0002](adr/0002-library-choices.md). Keep it out of `domain`.
- **A new ADR** — copy the format of an existing one, add it to
  [`docs/adr/README.md`](adr/README.md), and supersede rather than edit accepted ones.

## How the build was made

matlatl was built in phases P0–P6, each: implement → a six-lens expert panel
review (security, concurrency, QA, duplication, library-vs-handroll, idiomacy) →
address findings → small commits. The phasing and the panel's load-bearing
corrections are recorded in the ADRs and the commit history.
