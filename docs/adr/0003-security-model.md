# 3. Security model for untrusted repositories

Date: 2026-06-05
Status: Accepted

## Context

`matlatl` ingests arbitrary, potentially hostile repositories (CI on forked PRs,
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

### Known limitation: EvalSymlinks→ReadFile TOCTOU (accepted)

The scanner canonicalizes each file with `filepath.EvalSymlinks` and verifies
containment, then stores the resolved path so the parser reads exactly what was
validated. This **narrows but does not close** a time-of-check/time-of-use
window: an attacker who can swap a path between our `Lstat`/`EvalSymlinks` and
the parser's `ReadFile` (or `BodyReader`'s `os.ReadFile` for llms-full.txt) could
in principle redirect the read. Fully closing it requires handle-based,
`openat`/`O_NOFOLLOW`-style I/O, for which Go's standard library exposes **no
portable API** today. The residual window is **accepted** for a batch scanner
over a **local** tree it owns end-to-end (it does not follow symlinks and walks a
single canonicalized root); it is documented here so the trade-off is explicit
rather than implicit, and re-evaluated if a portable `openat` API lands.

### Ignore-file size cap

`.matlatlignore` is read **before** the walk, so the per-file `MaxFileSizeBytes`
guard (invariant 3) does not cover it. The scanner therefore `os.Stat`s it first
and **skips** an ignore file larger than `maxIgnoreBytes` (1 MiB), reading the
capped bytes itself rather than relying on the dependency's uncapped
`ReadFile`-based loader, so a hostile multi-GB ignore file cannot OOM the scan.
