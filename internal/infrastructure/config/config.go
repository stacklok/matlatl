// Package config loads the optional per-repo `.matlatl.yml` configuration file
// that lives at the scan root (a sibling of `.matlatlignore`). v1 carries only
// additional reachability roots, which the CLI UNIONs into
// application.Config.Roots before the domain's source-agnostic
// graphmodel.ResolveRootSet consumes them. matlatl ships zero
// tool-specific knowledge; the repo's `.matlatl.yml` carries it (ADR 0011).
//
// The loader is the durable forward-compat seam: a mistake in something matlatl
// UNDERSTANDS (malformed YAML, wrong types, unsupported version) is a HARD error
// the CLI maps to ExitUsage; a thing it does not understand yet (an unknown
// non-version key) is TOLERATED with a notice. See ADR 0011 for the full
// contract table.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/platform"
)

const (
	// fileName is the per-repo config file, read ONLY at the scan root. It is a
	// sibling of `.matlatlignore` and is NOT discovered up the directory tree
	// (ADR 0011).
	fileName = ".matlatl.yml"

	// supportedVersion is the highest config schema version this matlatl
	// understands. A file declaring a higher integer version is a HARD error
	// (the repo expects a newer tool). v1 = roots only.
	supportedVersion = 1

	// maxConfigBytes caps the config file we will read into memory. It is the
	// shared pre-walk read cap (platform.PreWalkReadCap, the single audit point
	// for this ADR 0003 invariant-3 limit, shared with fsscanner's .matlatlignore
	// read): the config is read OUTSIDE the per-file scan cap, so we Lstat it
	// first and refuse to read past this cap. Combined with yaml.v3's built-in
	// alias-expansion budget, this bounds the decode against alias/billion-laughs
	// bombs.
	maxConfigBytes = platform.PreWalkReadCap
)

// File is the parsed, validated v1 configuration. The zero value is the
// no-config default (no extra roots) and is what Load returns for a missing or
// empty file.
type File struct {
	// Version is the declared schema version (assumed 1 when the field is absent
	// from a present file).
	Version int
	// Roots holds additional reachability root globs, matched against document
	// IDs (repo-root-relative, slash-separated) with the same path.Match
	// semantics as the --root flag. UNIONED with conventions and --root.
	Roots []string
}

// rawFile is the permissive decode target. We decode into a generic map first
// so we can (a) detect unknown keys for the typo notice and (b) type-check
// `version`/`roots` ourselves rather than letting yaml's struct binding silently
// coerce or ignore.
type rawFile map[string]any

// Load reads <scanRoot>/.matlatl.yml and returns the parsed File plus any
// tolerated-condition notices. It returns a real error ONLY for the HARD-error
// rows of the ADR 0011 contract (malformed YAML, wrong types, unsupported
// version); the CLI maps that error to ExitUsage. A missing or empty file is a
// silent no-op (zero File, no notices, nil error).
//
// Security (ADR 0003): reads exactly <scanRoot>/.matlatl.yml — nothing outside
// the scan root. The globs it carries are only string-matched against in-corpus
// DocumentIDs by ResolveRootSet (never a filesystem read), so a hostile
// `roots: ["/etc/**"]` is inert.
func Load(scanRoot string) (File, []application.Notice, error) {
	p := filepath.Join(scanRoot, fileName)

	// Lstat first (mirror fsscanner.loadIgnore): a missing file is the
	// zero-config no-op. Lstat (not Stat) so we observe a symlink without
	// following it.
	li, err := os.Lstat(p)
	if err != nil {
		return File{}, nil, nil
	}
	// No-symlink-escape (ADR 0003 invariant 1): a repo-supplied .matlatl.yml that
	// is a symlink could point OUTSIDE the scan root. This is a pre-walk read
	// surface, so the scanner's no-follow stance must apply here too. Lstat-skip
	// matches the walk's posture (it skips symlinks rather than resolving +
	// containment-checking them); emit the skipped-symlink notice and treat it as
	// zero config.
	if li.Mode()&os.ModeSymlink != 0 {
		return File{}, []application.Notice{{
			Kind:   application.NoticeSkippedSymlink,
			Path:   p,
			Detail: "config file is a symlink; not followed (no-follow policy, ADR 0003)",
		}}, nil
	}
	// A non-regular file (dir, device, fifo) is likewise the zero-config no-op.
	if !li.Mode().IsRegular() {
		return File{}, nil, nil
	}
	fi := li

	// The file is read BEFORE/OUTSIDE the per-file scan cap, so refuse to read
	// past maxConfigBytes (ADR 0003 invariant 3): skip + notice, do NOT read.
	if fi.Size() > maxConfigBytes {
		return File{}, []application.Notice{{
			Kind:   application.NoticeOversized,
			Path:   p,
			Detail: fmt.Sprintf("config file is %d bytes (cap %d); skipped", fi.Size(), maxConfigBytes),
		}}, nil
	}

	b, err := os.ReadFile(p) //nolint:gosec // size-capped above (ADR 0003 invariant 3)
	if err != nil {
		// Statted as a regular readable file but the read failed (race / perms):
		// treat as a hard error so the user is not silently running with a config
		// they intended to apply.
		return File{}, nil, fmt.Errorf("read %s: %w", fileName, err)
	}

	// Empty file: silent no-op (an empty .matlatl.yml is the same as none).
	if len(b) == 0 {
		return File{}, nil, nil
	}

	return decode(p, b)
}

// decode parses the size-capped bytes and applies the ADR 0011 version /
// type / unknown-key contract. The 1 MiB source cap plus yaml.v3's built-in
// alias budget bound the decode against alias/billion-laughs bombs (ADR 0003),
// the same guard mdparser relies on for front matter.
func decode(path string, b []byte) (File, []application.Notice, error) {
	var raw rawFile
	if err := yaml.Unmarshal(b, &raw); err != nil {
		// Malformed YAML (syntax), an alias-budget rejection, OR a top-level
		// scalar/sequence (which cannot unmarshal into a mapping): all HARD
		// errors. The governing rule: a shape matlatl understands but that is
		// wrong is loud (ADR 0011).
		return File{}, nil, fmt.Errorf("%s: %w", fileName, err)
	}
	// A comments/whitespace-only document (no syntax error, no mapping) decodes
	// to a nil map: a legitimate no-op, like an empty file.
	if raw == nil {
		return File{}, nil, nil
	}

	var notices []application.Notice

	// --- version ---
	version, vNotices, err := resolveVersion(raw)
	if err != nil {
		return File{}, nil, err
	}
	notices = append(notices, vNotices...)

	// --- roots ---
	roots, err := resolveRoots(raw)
	if err != nil {
		return File{}, nil, err
	}

	// --- unknown keys (typo / future additive key): ignore + notice ---
	notices = append(notices, unknownKeyNotices(path, raw)...)

	return File{Version: version, Roots: roots}, notices, nil
}

// resolveVersion enforces the version rows of the contract: missing → assume 1
// with a notice; ==1 → ok; integer >1 → hard error; wrong type → hard error.
func resolveVersion(raw rawFile) (int, []application.Notice, error) {
	v, present := raw["version"]
	if !present {
		return supportedVersion, []application.Notice{{
			Kind:   application.NoticeConfig,
			Path:   fileName,
			Detail: "no version field; assuming 1; pin `version: 1`",
		}}, nil
	}
	n, ok := v.(int)
	if !ok {
		return 0, nil, fmt.Errorf(
			"%s: `version` must be an integer, got %T", fileName, v)
	}
	if n > supportedVersion {
		return 0, nil, fmt.Errorf(
			"%s: config version %d is newer than this matlatl supports (max %d); upgrade matlatl",
			fileName, n, supportedVersion)
	}
	if n < 1 {
		return 0, nil, fmt.Errorf(
			"%s: config version %d is not valid (minimum 1)", fileName, n)
	}
	return n, nil, nil
}

// resolveRoots enforces the roots-type row: absent → none; a list of strings →
// parsed; any other shape (a string, a list with a non-string element) → hard
// error.
func resolveRoots(raw rawFile) ([]string, error) {
	v, present := raw["roots"]
	if !present || v == nil {
		return nil, nil
	}
	seq, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf(
			"%s: `roots` must be a list of strings, got %T", fileName, v)
	}
	roots := make([]string, 0, len(seq))
	for i, e := range seq {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf(
				"%s: `roots[%d]` must be a string, got %T", fileName, i, e)
		}
		roots = append(roots, s)
	}
	return roots, nil
}

// unknownKeyNotices surfaces every key that is neither `version` nor `roots`,
// one notice each, sorted for determinism. This catches typos (`rootz:`) while
// tolerating future additive keys (ADR 0011 governing rule).
func unknownKeyNotices(path string, raw rawFile) []application.Notice {
	known := map[string]struct{}{"version": {}, "roots": {}}
	var unknown []string
	for k := range raw {
		if _, ok := known[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	// Deterministic order regardless of map iteration.
	slices.Sort(unknown)
	notices := make([]application.Notice, 0, len(unknown))
	for _, k := range unknown {
		notices = append(notices, application.Notice{
			Kind:   application.NoticeConfig,
			Path:   path,
			Detail: fmt.Sprintf("ignoring unknown config key %q (typo, or a key from a newer matlatl)", k),
		})
	}
	return notices
}
