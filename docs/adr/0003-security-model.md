# 3. Security model for untrusted repositories

Date: 2026-06-05
Status: Accepted

## Context

`doctopus` ingests arbitrary, potentially hostile repositories (CI on forked PRs,
scanning third-party docs). Security is therefore a **Phase 1 correctness
requirement**, not a later "hardening" step. A pre-code review identified five
distinct attack surfaces that must be addressed before the scanner ships.

## Decision

Enforce these invariants from the first scanning code:

1. **No symlink escape.** The filesystem walk does not follow symlinks by default.
   Every walked path is canonicalized and verified to remain under the scan root;
   a symlink resolving outside the root is skipped and reported, never traversed.

2. **Resolution stays in-root.** Any link/anchor target that, after path cleaning,
   resolves outside the corpus root (`../../../../etc/passwd`, absolute paths) is
   recorded as a finding — it is **never** turned into a filesystem read.

3. **Resource caps.** Enforce a max per-file size, a max file count, and bounded
   front-matter decoding (guard against YAML "billion laughs" / deep-alias bombs).
   Full in-memory parsing without caps is an OOM/DoS vector on hostile input.

4. **Output-path sanitization.** Every artifact path derived from a `DocumentID` or
   front-matter field (titles, slugs) is sanitized and verified to stay under the
   `--out` directory (reverse zip-slip). No emitter writes outside `--out`.

5. **Label escaping.** Node labels in DOT and Mermaid output are escaped per target
   syntax; titles containing quotes/newlines/special chars cannot break or inject
   into the diagram.

6. **SSRF guard for opt-in external checks.** When `--check-external` is enabled,
   block link-local/internal/metadata hosts (`169.254.169.254`, `file://`, private
   ranges) by default, bound redirects, and apply per-host rate limiting and timeouts.

Each invariant ships with an adversarial test fixture (symlink escape, `../`
traversal, oversized file, YAML bomb, hostile title) — see ADR 0005 and the test
strategy.

## Consequences

- The scanner and resolver carry an explicit, tested root-containment boundary.
- A small, deliberate set of limits is configurable but safe by default.
- Reviews at every phase gate include a security pass against this list.
