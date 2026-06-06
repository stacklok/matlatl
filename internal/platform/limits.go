package platform

// PreWalkReadCap is the size cap (in bytes) for files matlatl reads at the scan
// root BEFORE/OUTSIDE the per-file walk cap — currently `.matlatlignore`
// (fsscanner) and `.matlatl.yml` (config). Those reads happen before the walk's
// per-file MaxFileSizeBytes guard applies, so a hostile repo could otherwise
// hand us a multi-GB file and OOM the scan. This is the single audit point for
// that cap (ADR 0003 invariant 3): 1 MiB is far more than any real ignore/config
// file needs, and an oversized file is skipped (treated like a missing file)
// rather than read into memory. Combined with the YAML decoder's built-in
// alias-expansion budget, it also bounds the .matlatl.yml decode against
// alias/billion-laughs bombs.
const PreWalkReadCap int64 = 1 << 20 // 1 MiB
