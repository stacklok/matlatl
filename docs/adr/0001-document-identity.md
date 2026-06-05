# 1. Document identity is the canonical repository-relative path

Date: 2026-06-05
Status: Accepted

## Context

A markdown corpus is a graph whose vertices are documents. Every downstream
concern — link resolution, backlinks, orphan detection, deduplication — depends
on a *stable, unambiguous identifier* for each document.

The dominant correctness hazard in prior art (Obsidian, Foam) is **basename
identity**: treating `README.md` as a single node when a repo contains dozens of
them across directories. Wikilink resolvers that match on basename then silently
link to the wrong target, and orphan detection collapses distinct files into one.

## Decision

A document's identity (`DocumentID`) is its **canonical repository-relative path**
(cleaned, slash-separated, relative to the scan root) — never its basename.

- The basename is, at most, a *resolution hint* used inside the reference-resolution
  logic to find candidate targets; it is never an identity.
- Wikilink / relative-link resolution maps a raw target to a `DocumentID` via an
  explicit `ResolutionPolicy` (default: longest-suffix match), and surfaces
  genuine ambiguity as a first-class `Ambiguous` finding rather than guessing.
- All artifacts (graph.json, reports, diagrams, llms.txt) key documents by
  `DocumentID`.

## Consequences

- Duplicate basenames across directories are correctly distinct nodes.
- Identity is independent of the filesystem's absolute location, so artifacts are
  reproducible across machines/checkouts.
- Resolution must do real path arithmetic (and enforce the root boundary — see
  ADR 0003); it cannot shortcut to basename equality.
- Renaming/moving a file changes its identity; backlink stability across moves is
  explicitly out of scope for v1.
