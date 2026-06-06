package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
)

// rootWith stages a temp scan root containing a .matlatl.yml whose contents are
// the named testdata fixture, and returns the root. Load reads exactly
// <root>/.matlatl.yml, so each test gets an isolated directory.
func rootWith(t *testing.T, fixture string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	return rootWithBytes(t, b)
}

// rootWithBytes stages a temp scan root whose .matlatl.yml holds the given bytes.
func rootWithBytes(t *testing.T, b []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), b, 0o600); err != nil {
		t.Fatalf("write %s: %v", fileName, err)
	}
	return dir
}

// hasNoticeContaining reports whether any notice's Detail contains sub.
func hasNoticeContaining(notices []application.Notice, sub string) bool {
	for _, n := range notices {
		if strings.Contains(n.Detail, sub) {
			return true
		}
	}
	return false
}

// --- Contract row: missing file → silent no-op, zero config ---

func TestLoad_MissingFile(t *testing.T) {
	file, notices, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("missing file should be a no-op, got error: %v", err)
	}
	if notices != nil {
		t.Errorf("missing file should emit no notices, got %v", notices)
	}
	if file.Version != 0 || file.Roots != nil {
		t.Errorf("missing file should yield zero File, got %+v", file)
	}
}

// A non-regular file (a directory named .matlatl.yml) is treated as missing.
func TestLoad_NotRegularFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, fileName), 0o750); err != nil {
		t.Fatal(err)
	}
	file, notices, err := Load(dir)
	if err != nil || notices != nil || file.Roots != nil {
		t.Errorf("a directory named %s should be a no-op; got file=%+v notices=%v err=%v",
			fileName, file, notices, err)
	}
}

// --- Contract row: empty file → silent no-op ---

func TestLoad_EmptyFile(t *testing.T) {
	dir := rootWithBytes(t, []byte{})
	file, notices, err := Load(dir)
	if err != nil {
		t.Fatalf("empty file should be a no-op, got error: %v", err)
	}
	if notices != nil || file.Version != 0 || file.Roots != nil {
		t.Errorf("empty file should yield zero File, no notices; got file=%+v notices=%v", file, notices)
	}
}

// A comments/whitespace-only file is also a no-op (decodes to no mapping).
func TestLoad_CommentsOnly(t *testing.T) {
	dir := rootWithBytes(t, []byte("# just a comment\n\n  \n"))
	file, notices, err := Load(dir)
	if err != nil {
		t.Fatalf("comments-only file should be a no-op, got error: %v", err)
	}
	if notices != nil || file.Roots != nil {
		t.Errorf("comments-only should yield zero File, no notices; got file=%+v notices=%v", file, notices)
	}
}

// --- Contract row: valid (version 1 + roots parsed) ---

func TestLoad_Valid(t *testing.T) {
	file, notices, err := Load(rootWith(t, "valid.yml"))
	if err != nil {
		t.Fatalf("valid config errored: %v", err)
	}
	if len(notices) != 0 {
		t.Errorf("valid config should emit no notices, got %v", notices)
	}
	if file.Version != 1 {
		t.Errorf("version = %d, want 1", file.Version)
	}
	want := []string{".claude/agents/*.md", "docs/*.md"}
	if strings.Join(file.Roots, ",") != strings.Join(want, ",") {
		t.Errorf("roots = %v, want %v", file.Roots, want)
	}
}

// --- Contract row: oversized file → skip + notice, do NOT read ---

func TestLoad_Oversized(t *testing.T) {
	// Generate a >1 MiB file at runtime rather than committing a large fixture.
	big := make([]byte, maxConfigBytes+1)
	for i := range big {
		big[i] = '#' // a giant comment line; never decoded because we skip it.
	}
	dir := rootWithBytes(t, big)
	file, notices, err := Load(dir)
	if err != nil {
		t.Fatalf("oversized file should be skipped (not an error), got: %v", err)
	}
	if file.Roots != nil || file.Version != 0 {
		t.Errorf("oversized file must yield zero config (not read), got %+v", file)
	}
	if !hasNoticeContaining(notices, "skipped") {
		t.Errorf("oversized file should emit a skip notice, got %v", notices)
	}
	if len(notices) != 1 || notices[0].Kind != application.NoticeOversized {
		t.Errorf("oversized notice kind = %v, want one NoticeOversized", notices)
	}
}

// --- Contract row: malformed YAML (syntax) → HARD error ---

func TestLoad_MalformedYAML(t *testing.T) {
	_, _, err := Load(rootWith(t, "malformed.yml"))
	if err == nil {
		t.Fatal("malformed YAML should be a HARD error")
	}
	// Pin it to the malformed-YAML path specifically: every hard error (version,
	// roots, shape) returns non-nil, so assert the error carries the fileName
	// prefix the malformed-YAML branch wraps with. Otherwise this test cannot
	// distinguish a regression that, say, started rejecting valid YAML for the
	// wrong reason.
	if !strings.Contains(err.Error(), ".matlatl.yml") {
		t.Errorf("error = %q, want it to carry the %q prefix", err, fileName)
	}
}

// --- Contract row: version missing (file present) → assume 1 + notice ---

func TestLoad_NoVersion(t *testing.T) {
	file, notices, err := Load(rootWith(t, "no-version.yml"))
	if err != nil {
		t.Fatalf("no-version config should not error, got: %v", err)
	}
	if file.Version != 1 {
		t.Errorf("version = %d, want assumed 1", file.Version)
	}
	if len(file.Roots) != 1 || file.Roots[0] != ".claude/agents/*.md" {
		t.Errorf("roots = %v, want [.claude/agents/*.md]", file.Roots)
	}
	if !hasNoticeContaining(notices, "assuming 1") {
		t.Errorf("no-version should emit an 'assuming 1' notice, got %v", notices)
	}
}

// --- Contract row: version > supported → HARD error ---

func TestLoad_VersionTooNew(t *testing.T) {
	_, _, err := Load(rootWith(t, "version-2.yml"))
	if err == nil {
		t.Fatal("version 2 should be a HARD error")
	}
	if !strings.Contains(err.Error(), "newer than this matlatl supports") {
		t.Errorf("error = %q, want the upgrade message", err)
	}
}

// --- Contract row: version < 1 → HARD error (distinct "minimum 1" message) ---

func TestLoad_VersionTooOld(t *testing.T) {
	_, _, err := Load(rootWith(t, "version-0.yml"))
	if err == nil {
		t.Fatal("version 0 should be a HARD error")
	}
	if !strings.Contains(err.Error(), "minimum 1") {
		t.Errorf("error = %q, want the minimum-version message", err)
	}
}

// --- Contract row: version wrong type → HARD error ---

func TestLoad_VersionWrongType(t *testing.T) {
	_, _, err := Load(rootWith(t, "version-wrong-type.yml"))
	if err == nil {
		t.Fatal("string version should be a HARD error")
	}
	if !strings.Contains(err.Error(), "must be an integer") {
		t.Errorf("error = %q, want the integer-type message", err)
	}
}

// --- Contract row: roots wrong type → HARD error ---

func TestLoad_RootsWrongType(t *testing.T) {
	_, _, err := Load(rootWith(t, "roots-wrong-type.yml"))
	if err == nil {
		t.Fatal("a string `roots` should be a HARD error")
	}
	if !strings.Contains(err.Error(), "must be a list of strings") {
		t.Errorf("error = %q, want the list-type message", err)
	}
}

// A roots list containing a non-string element is also a HARD error.
func TestLoad_RootsNonStringElement(t *testing.T) {
	dir := rootWithBytes(t, []byte("version: 1\nroots:\n  - \"ok.md\"\n  - 42\n"))
	_, _, err := Load(dir)
	if err == nil {
		t.Fatal("a non-string roots element should be a HARD error")
	}
	if !strings.Contains(err.Error(), "must be a string") {
		t.Errorf("error = %q, want the element-type message", err)
	}
}

// --- Contract row: unknown non-version key → notice + tolerated; roots parse ---

func TestLoad_UnknownKey(t *testing.T) {
	file, notices, err := Load(rootWith(t, "unknown-key.yml"))
	if err != nil {
		t.Fatalf("unknown key should be tolerated, got error: %v", err)
	}
	if len(file.Roots) != 1 || file.Roots[0] != "docs/*.md" {
		t.Errorf("recognized roots should still parse, got %v", file.Roots)
	}
	if !hasNoticeContaining(notices, "rootz") {
		t.Errorf("unknown key `rootz` should be surfaced in a notice, got %v", notices)
	}
	if !hasNoticeContaining(notices, "unknown config key") {
		t.Errorf("notice should explain the key is unknown, got %v", notices)
	}
}

// --- ADR 0012: inboundThreshold + structureFindingsSeverity ---

func TestLoad_InboundThreshold(t *testing.T) {
	file, _, err := Load(rootWithBytes(t, []byte("version: 1\ninboundThreshold: 5\n")))
	if err != nil {
		t.Fatalf("valid inboundThreshold should load, got %v", err)
	}
	if file.InboundThreshold == nil || *file.InboundThreshold != 5 {
		t.Errorf("inboundThreshold = %v, want pointer to 5", file.InboundThreshold)
	}
}

func TestLoad_InboundThresholdZeroAllowed(t *testing.T) {
	file, _, err := Load(rootWithBytes(t, []byte("inboundThreshold: 0\n")))
	if err != nil {
		t.Fatalf("inboundThreshold 0 should load (normalized to default in domain), got %v", err)
	}
	if file.InboundThreshold == nil || *file.InboundThreshold != 0 {
		t.Errorf("inboundThreshold = %v, want pointer to 0", file.InboundThreshold)
	}
}

func TestLoad_InboundThresholdNegative(t *testing.T) {
	if _, _, err := Load(rootWithBytes(t, []byte("inboundThreshold: -1\n"))); err == nil {
		t.Fatal("negative inboundThreshold should be a HARD error")
	}
}

func TestLoad_InboundThresholdWrongType(t *testing.T) {
	if _, _, err := Load(rootWithBytes(t, []byte("inboundThreshold: \"three\"\n"))); err == nil {
		t.Fatal("non-integer inboundThreshold should be a HARD error")
	}
}

func TestLoad_StructureFindingsSeverity(t *testing.T) {
	for _, v := range []string{"info", "warning"} {
		file, _, err := Load(rootWithBytes(t, []byte("structureFindingsSeverity: "+v+"\n")))
		if err != nil {
			t.Fatalf("severity %q should load, got %v", v, err)
		}
		if file.StructureFindingsSeverity == nil || *file.StructureFindingsSeverity != v {
			t.Errorf("structureFindingsSeverity = %v, want pointer to %q", file.StructureFindingsSeverity, v)
		}
	}
}

func TestLoad_StructureFindingsSeverityInvalid(t *testing.T) {
	if _, _, err := Load(rootWithBytes(t, []byte("structureFindingsSeverity: loud\n"))); err == nil {
		t.Fatal("an invalid severity should be a HARD error")
	}
}

func TestLoad_NewKeysAbsentAreNil(t *testing.T) {
	file, _, err := Load(rootWithBytes(t, []byte("version: 1\nroots: [\"docs/*.md\"]\n")))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if file.InboundThreshold != nil {
		t.Errorf("absent inboundThreshold should be nil, got %v", *file.InboundThreshold)
	}
	if file.StructureFindingsSeverity != nil {
		t.Errorf("absent structureFindingsSeverity should be nil, got %v", *file.StructureFindingsSeverity)
	}
}

func TestLoad_NewKeysNotFlaggedUnknown(t *testing.T) {
	_, notices, err := Load(rootWithBytes(t,
		[]byte("inboundThreshold: 4\nstructureFindingsSeverity: warning\n")))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if hasNoticeContaining(notices, "inboundThreshold") || hasNoticeContaining(notices, "structureFindingsSeverity") {
		t.Errorf("known keys must not be flagged as unknown, got %v", notices)
	}
}

// --- ADR 0013: linkSuggestionMinShared ---

func TestLoad_LinkSuggestionMinShared(t *testing.T) {
	file, _, err := Load(rootWithBytes(t, []byte("version: 1\nlinkSuggestionMinShared: 3\n")))
	if err != nil {
		t.Fatalf("valid linkSuggestionMinShared should load, got %v", err)
	}
	if file.LinkSuggestionMinShared == nil || *file.LinkSuggestionMinShared != 3 {
		t.Errorf("linkSuggestionMinShared = %v, want pointer to 3", file.LinkSuggestionMinShared)
	}
}

func TestLoad_LinkSuggestionMinSharedNegative(t *testing.T) {
	if _, _, err := Load(rootWithBytes(t, []byte("linkSuggestionMinShared: -1\n"))); err == nil {
		t.Fatal("negative linkSuggestionMinShared should be a HARD error")
	}
}

func TestLoad_LinkSuggestionMinSharedWrongType(t *testing.T) {
	if _, _, err := Load(rootWithBytes(t, []byte("linkSuggestionMinShared: \"two\"\n"))); err == nil {
		t.Fatal("non-integer linkSuggestionMinShared should be a HARD error")
	}
}

func TestLoad_LinkSuggestionMinSharedNotFlaggedUnknown(t *testing.T) {
	_, notices, err := Load(rootWithBytes(t, []byte("linkSuggestionMinShared: 2\n")))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if hasNoticeContaining(notices, "linkSuggestionMinShared") {
		t.Errorf("known key linkSuggestionMinShared must not be flagged as unknown, got %v", notices)
	}
}

// --- Contract row: top-level scalar/sequence (shape error) → HARD error ---

func TestLoad_TopLevelScalar(t *testing.T) {
	// A top-level scalar cannot unmarshal into the mapping target: a HARD error.
	dir := rootWithBytes(t, []byte("just a string\n"))
	if _, _, err := Load(dir); err == nil {
		t.Fatal("a top-level scalar document should be a HARD error")
	}
	// A top-level sequence is likewise a shape error.
	dir2 := rootWithBytes(t, []byte("- a\n- b\n"))
	if _, _, err := Load(dir2); err == nil {
		t.Fatal("a top-level sequence document should be a HARD error")
	}
}

// --- ADR 0003 adversarial fixture: alias bomb → bounded decode (HARD error) ---

func TestLoad_AliasBomb(t *testing.T) {
	fixture := filepath.Join("testdata", "bomb.yml")
	fi, err := os.Stat(fixture)
	if err != nil {
		t.Fatal(err)
	}
	// The bomb is sub-cap by construction: the size guard is NOT what stops it;
	// yaml.v3's alias budget is. Assert the fixture is genuinely under the cap so
	// this test exercises the second defense, not the first.
	if fi.Size() >= maxConfigBytes {
		t.Fatalf("bomb fixture %d bytes is not sub-cap (%d); adjust the pyramid", fi.Size(), maxConfigBytes)
	}
	_, _, lerr := Load(rootWith(t, "bomb.yml"))
	if lerr == nil {
		t.Error("expected yaml.v3 alias-budget limiter to reject the bomb as a HARD error")
	}
}

// --- ADR 0003 adversarial fixture: symlinked config → NOT followed ---

// TestLoad_SymlinkNotFollowed pins the no-symlink-escape invariant (ADR 0003
// invariant 1): a repo-supplied .matlatl.yml that is a SYMLINK to a file OUTSIDE
// the scan root must NOT be read. The loader Lstat-skips it, returning zero
// config plus a skipped-symlink notice — the target's contents never load.
func TestLoad_SymlinkNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	// A secret config OUTSIDE the scan root that, if followed, WOULD apply.
	outside := t.TempDir()
	target := filepath.Join(outside, "evil.yml")
	if err := os.WriteFile(target, []byte("version: 1\nroots:\n  - \"pwned/*.md\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	scanRoot := t.TempDir()
	link := filepath.Join(scanRoot, fileName)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	file, notices, err := Load(scanRoot)
	if err != nil {
		t.Fatalf("symlinked config should be skipped (not an error), got: %v", err)
	}
	if file.Roots != nil || file.Version != 0 {
		t.Errorf("symlinked config must NOT be followed; got %+v (the target leaked in)", file)
	}
	if len(notices) != 1 || notices[0].Kind != application.NoticeSkippedSymlink {
		t.Errorf("symlinked config should emit one skipped-symlink notice, got %v", notices)
	}
}
