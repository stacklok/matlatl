# Guide

Per `.claude/rules/metrics.md`: dotted names, unit on the instrument.
See [the reference](reference.md), also written as docs/reference.md.
The ./sibling.md page is only ever mentioned, never linked.
See `frontdoor.md § V1 hardcoded` for the history.
The testing.md notes are ambiguous: two documents share that name.
Skills live under `.claude/skills/panel-review/`.

## Workflow

Run /panel-review before pushing.
Use the `triage-cve` skill (`/triage-cve CVE-YYYY-NNNNN`).
Not https://example.com/panel-review, and not docs/x/panel-review.
Nothing resolves for docs/nope.md, ghost.md, or /no-such-skill.

```
docs/fenced.md is inside a fenced block
```

A [labelled reference][ref] stays a link.

[ref]: design/frontdoor.md

<!-- maintainers: keep docs/sibling.md in sync -->
