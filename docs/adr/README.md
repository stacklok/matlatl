# Architecture Decision Records

These ADRs capture the significant, hard-to-reverse decisions behind `matlatl`.
Each record is short, dated, and immutable once accepted — supersede rather than edit.

Format: [Michael Nygard's ADR template](https://github.com/joelparkerhenderson/architecture-decision-record).

| #    | Title                                   | Status   |
| ---- | --------------------------------------- | -------- |
| 0001 | Document identity is the relative path  | Accepted |
| 0002 | Library choices                         | Accepted |
| 0003 | Security model for untrusted repos      | Accepted |
| 0004 | DDD layering and full-fat scope         | Accepted |
| 0005 | `check` exit-code contract              | Accepted |
| 0006 | Canonical anchor-slug dialect           | Accepted |
| 0007 | Graph node semantics and the document projection | Accepted (superseded in part by 0012, 0013) |
| 0008 | Directory links resolve and confer navigational reachability | Accepted |
| 0009 | `fix-prompt` serves acting agents with an embedded prompt | Accepted |
| 0010 | How matlatl treats agent-tooling scaffolding | Accepted (superseded in part by 0018) |
| 0011 | Per-repo configuration file (`.matlatl.yml`) | Accepted |
| 0012 | Graduated structure findings and bow-tie classification | Accepted |
| 0013 | Topology-based link prediction (suggested links)        | Accepted |
| 0014 | Corpus navigability metrics                             | Accepted |
| 0015 | Critical-path analysis (betweenness, articulation points, bridges) | Accepted |
| 0016 | Agent experience (PageRank, reading-order trails, backlinks, information scent) | Accepted |
| 0017 | Nested git repositories are out of scan scope          | Accepted |
| 0018 | Default-ignore `.claude/agent-memory`                  | Accepted |
| 0019 | `emitExclude`: in the corpus, off the navigation surfaces | Accepted |
| 0020 | `fix-prompt` scope: curated default, kind selection, and emitExclude | Accepted |
| 0021 | Hops-from-root: distance from the entry points as the discoverability metric | Accepted |
| 0022 | Root-absolute links: a single leading `/` resolves from the scan root | Accepted |
| 0023 | OKF v0.1 conformance mode | Accepted |
