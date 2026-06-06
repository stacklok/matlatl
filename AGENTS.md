# Agent guide

matlatl maps a repo's markdown into a graph/tree and emits human + LLM
artifacts. This file points you at what you can't trivially grep for; read the
linked docs rather than re-deriving them.

## Where to look first

- **[docs/architecture.md](docs/architecture.md)** — the pipeline
  (Scan → Parse → Resolve → Build → Analyze → Emit), the three-layer DDD design,
  and the core model types.
- **[docs/adr/](docs/adr/README.md)** — the decisions that constrain changes.
  Read these before reworking anything load-bearing:
  - 0001 identity is the repo-relative path; 0003 security model (untrusted
    repos); 0004 layering + purity; 0005 `check` exit-code contract;
    0006 anchor-slug dialect; 0007 graph node semantics + the document
    projection; 0008 directory-link reachability; 0012 graduated structure
    (orphan/under-linked/dead-end) + bow-tie classification.
- **docs/schemas/** — published JSON Schemas for the two machine artifacts:
  [graph.schema.json](docs/schemas/graph.schema.json) (graph schema version 2)
  and [findings.schema.json](docs/schemas/findings.schema.json) (findings schema
  version 3). The emitter types are kept in lockstep and validated by tests; if
  you change an artifact's shape, change the schema and bump its version.
- **[docs/user-guide.md](docs/user-guide.md)** /
  **[docs/dev-guide.md](docs/dev-guide.md)** — commands/flags/CI, and the layout
  + contribution rules.

## Hard rules (enforced, not aspirational)

- **Domain purity (ADR 0004):** `internal/domain/...` imports only stdlib +
  sibling domain packages — no cobra, goldmark, net/http, or any infrastructure/
  application package. There is a grep gate for this (see CONTRIBUTING.md).
- **Determinism:** sorted iteration everywhere; artifacts are byte-stable and
  golden-tested. Never iterate a map for output without sorting.
- **Security (ADR 0003):** scanning is for untrusted repos — respect root
  containment, resource caps, output-path sanitization, and the SSRF guard.

## Running it / the MCP entrypoint

```console
matlatl .              # terminal report
matlatl check .        # CI gate (exit codes per ADR 0005); --strict to harden
matlatl emit --out ai  # full human + LLM artifact bundle
matlatl fix-prompt .   # agent-agnostic prompt to fix the findings (pipe to any agent)
matlatl serve .        # read-only MCP server over streamable HTTP (127.0.0.1:8080/mcp)
```

`matlatl serve` is the **MCP entrypoint** for agents: it speaks MCP over
streamable HTTP (at `/mcp` on `--address`, default `127.0.0.1:8080`)
and exposes read-only tools (`what-links-to`, `list-orphans`, `path-between`,
`get-section`, `corpus-summary`). Prefer it for live graph queries over parsing
artifacts yourself.

## Dogfooding

This repo eats its own dog food: `task dogfood` regenerates the repo-root
`llms.txt` and runs `matlatl check . --strict` (also a CI gate). `testdata/` is
excluded via `.matlatlignore` so the gate sees only real docs. If you add
markdown with links, keep that gate green — don't introduce doc-link rot.
