<!-- markdownlint-disable MD013 -->
# doctopus developer guide

How `doctopus` is built, how to work on it, and the rules that keep it coherent.
Read this alongside [`architecture.md`](architecture.md) and the [ADRs](adr/).

## Layout

```
cmd/doctopus/            entrypoint + Cobra commands (wires infra → application)
internal/
  domain/                pure: no I/O, no goldmark, no third-party graph/MCP libs
    identity/            DocumentID (canonical-path identity) + path-containment helpers
    corpus/              Document, Section, FrontMatter, Corpus (+ Freeze), indices
    reference/           LinkType/LinkHealth, the pure-domain link Resolver
    graphmodel/          reference graph, hierarchy, reachability, orphans,
                         components (WCC union-find + iterative Tarjan SCC),
                         HITS, gaps — all hand-rolled & deterministic
    analysis/            Finding, Severity, AnalysisReport (immutable, sorted)
  application/           Config, ports, the scan→parse→resolve→build→analyze pipeline
  infrastructure/        all I/O + third-party:
    fsscanner/           secure recursive markdown discovery
    mdparser/            goldmark (quarantined here) → domain Documents
    emit/                report / diagram / index / graphjson / llmstxt emitters
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
   (HITS) are emitted at fixed precision. Golden tests assert byte-stability.
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
$ make build              # ./bin/doctopus
$ make test               # unit + smoke, under -race
$ make test-integration   # golden + integration (-tags=integration)
$ make cover              # coverage (includes integration-tagged code)
$ make lint               # golangci-lint
$ make check              # fmt + vet + lint + test (run before pushing)
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
  DOCTOPUS_UPDATE_GOLDEN=1 go test ./internal/infrastructure/emit/...
  # then review the diff before committing
  ```

- **Integration** (`//go:build integration`) — run the built pipeline over a
  fixture corpus and assert exit codes + artifact contents.
- **Adversarial** — symlink escape, `../` traversal, oversized files, YAML
  alias bombs, hostile titles in diagram/table labels, SSRF to internal hosts.
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

- **A new finding kind** — add to `analysis.FindingKind`, produce it in
  `application` with a concrete `SuggestedFix` and structured `Details`, add a
  `remediationGuide` entry in the findings emitter, and cover it in
  `TestCheckExitCode` + a golden.
- **A new emitter** — put it in `internal/infrastructure/emit/...`, render from
  the frozen `emit.View`/`GraphMetrics` (never mutate the domain), write through
  the `FSWriter`/`safeJoin` guard, escape labels per format, and add a golden +
  byte-stability test.
- **A new library** — record the decision (or the choice to hand-roll) in
  [ADR 0002](adr/0002-library-choices.md). Keep it out of `domain`.
- **A new ADR** — copy the format of an existing one, add it to
  [`docs/adr/README.md`](adr/README.md), and supersede rather than edit accepted ones.

## How the build was made

doctopus was built in phases P0–P6, each: implement → a six-lens expert panel
review (security, concurrency, QA, duplication, library-vs-handroll, idiomacy) →
address findings → small commits. The phasing and the panel's load-bearing
corrections are recorded in the ADRs and the commit history.
