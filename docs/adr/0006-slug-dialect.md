# 6. Canonical anchor-slug dialect

Date: 2026-06-05
Status: Accepted

## Context

Cross-file anchor validation (`[doc](other.md#some-heading)`) only works if the slug
`matlatl` computes for a heading equals the slug the *consumer* will dereference.
GitHub, CommonMark/goldmark, and Obsidian slugify headings differently (punctuation
stripping, Unicode handling, duplicate-heading suffixing). Without a pinned dialect,
cross-file anchor checks emit false-positive "broken anchor" findings on real repos.

## Decision

- The **validated, canonical dialect is goldmark's `parser.WithAutoHeadingID`
  algorithm** (GitHub-compatible: lowercase, spaces→`-`, strip disallowed punctuation,
  numeric suffix `-1`, `-2`, … on duplicates). This is the slug stored in the
  `HeadingInventory` and the one anchor resolution checks against.
- Other dialects (Obsidian's space-preserving style, raw `name=` anchors) are
  **best-effort** and explicitly documented as such.
- The slugger is behind a small interface so an alternative dialect can be supplied,
  but only one dialect is *validated* per run.
- The test suite pins per-dialect fixtures (punctuation, duplicate headings, Unicode,
  emoji) and asserts slug parity between parser output and resolver expectation. A
  divergence is a test failure, not a silent false positive.

## Consequences

- Anchor findings are trustworthy on GitHub-rendered repos (the common case).
- Non-GitHub anchor conventions may under-report; this is a documented limitation,
  not a bug.
- Slug generation and resolution share one implementation, eliminating drift.
