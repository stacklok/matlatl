package okf_test

import (
	"reflect"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/okf"
)

// concept builds a non-reserved concept document with the given frontmatter
// presence/parsed state and Extra map.
func concept(id string, present, parsed bool, extra map[string]any) *corpus.Document {
	return &corpus.Document{
		ID:                 identity.DocumentID(id),
		FrontMatterPresent: present,
		FrontMatterParsed:  parsed,
		FrontMatter:        corpus.FrontMatter{Extra: extra},
	}
}

// logDoc builds a log.md with the given level-2 heading texts (in order).
func logDoc(id string, headings ...string) *corpus.Document {
	root := &corpus.Section{Level: 0}
	for i, h := range headings {
		root.Children = append(root.Children, &corpus.Section{Level: 2, Text: h, StartLine: i + 3})
	}
	return &corpus.Document{ID: identity.DocumentID(id), Root: root}
}

func mkCorpus(t *testing.T, docs ...*corpus.Document) *corpus.Corpus {
	t.Helper()
	c := corpus.NewCorpus()
	for _, d := range docs {
		if err := c.Add(d); err != nil {
			t.Fatalf("add %s: %v", d.ID, err)
		}
	}
	return c
}

// --- Rule R1: parseable frontmatter on every concept document ---

func TestR1_Frontmatter(t *testing.T) {
	cases := []struct {
		name            string
		present, parsed bool
		wantViolation   bool
		wantState       string
	}{
		{"absent", false, false, true, okf.FrontMatterAbsent},
		// present+parsed with a type is conformant (see valid case below); an
		// oversized-guarded or unparseable block is present-but-unparsed.
		{"unparseable", true, false, true, okf.FrontMatterUnparseable},
		{"oversized-guarded", true, false, true, okf.FrontMatterUnparseable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mkCorpus(t, concept("doc.md", tc.present, tc.parsed, nil))
			rep := okf.Check(c)
			if got := len(rep.MissingFrontmatter) == 1; got != tc.wantViolation {
				t.Fatalf("MissingFrontmatter = %+v, wantViolation=%v", rep.MissingFrontmatter, tc.wantViolation)
			}
			if tc.wantViolation && rep.MissingFrontmatter[0].State != tc.wantState {
				t.Errorf("state = %q, want %q", rep.MissingFrontmatter[0].State, tc.wantState)
			}
			if rep.Conformant == tc.wantViolation {
				t.Errorf("Conformant = %v, want %v", rep.Conformant, !tc.wantViolation)
			}
		})
	}
}

func TestR1_ValidFrontmatterWithType_Conformant(t *testing.T) {
	c := mkCorpus(t, concept("doc.md", true, true, map[string]any{"type": "Reference"}))
	rep := okf.Check(c)
	if !rep.Conformant {
		t.Fatalf("want conformant, got %+v", rep)
	}
}

// --- Rule R2: non-empty `type` ---

func TestR2_Type(t *testing.T) {
	cases := []struct {
		name          string
		extra         map[string]any
		wantViolation bool
	}{
		{"absent", map[string]any{}, true},
		{"empty", map[string]any{"type": ""}, true},
		{"whitespace", map[string]any{"type": "   "}, true},
		{"non-string-list", map[string]any{"type": []any{"a", "b"}}, true},
		{"non-string-int", map[string]any{"type": 42}, true},
		{"valid", map[string]any{"type": "Reference"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mkCorpus(t, concept("doc.md", true, true, tc.extra))
			rep := okf.Check(c)
			if got := len(rep.MissingType) == 1; got != tc.wantViolation {
				t.Fatalf("MissingType = %+v, wantViolation=%v", rep.MissingType, tc.wantViolation)
			}
			if len(rep.MissingFrontmatter) != 0 {
				t.Errorf("R2 case must not produce an R1 violation: %+v", rep.MissingFrontmatter)
			}
		})
	}
}

// TestR2_NeverValidatesTypeValue asserts matlatl accepts ANY non-empty type
// string — OKF §4.1 forbids a central registry, so no allow-list.
func TestR2_NeverValidatesTypeValue(t *testing.T) {
	for _, v := range []string{"Reference", "totally-made-up-type", "BigQuery Table", "x"} {
		c := mkCorpus(t, concept("doc.md", true, true, map[string]any{"type": v}))
		if rep := okf.Check(c); !rep.Conformant {
			t.Errorf("type %q should be accepted, got %+v", v, rep)
		}
	}
}

// --- Rule R3: reserved-file structure ---

func TestR3_LogDates(t *testing.T) {
	cases := []struct {
		name          string
		headings      []string
		wantViolCount int
	}{
		{"good", []string{"2026-05-22", "2026-05-21"}, 0},
		{"bad", []string{"May 22"}, 1},
		{"mixed", []string{"2026-05-22", "not-a-date", "2026-05-20"}, 1},
		{"no-h2", nil, 0},
		// format-only: an impossible calendar date is well-FORMED and passes.
		{"format-only-not-calendar", []string{"2026-13-45"}, 0},
		// Anchored regex: a date with trailing text is NOT a bare date heading.
		// (Pins the ^...$ anchors — without them this case would wrongly pass.)
		{"date-with-trailing-text", []string{"2026-05-22 release notes"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mkCorpus(t, logDoc("log.md", tc.headings...))
			rep := okf.Check(c)
			if len(rep.ReservedStructure) != tc.wantViolCount {
				t.Fatalf("ReservedStructure = %+v, want %d", rep.ReservedStructure, tc.wantViolCount)
			}
		})
	}
}

// TestR3_LogChecksOnlyLevel2 asserts only `##` (level-2) headings are date-checked
// (OKF §7): a level-3 `###` non-date heading in a log.md does not violate.
func TestR3_LogChecksOnlyLevel2(t *testing.T) {
	root := &corpus.Section{Level: 0, Children: []*corpus.Section{
		{Level: 2, Text: "2026-05-22", StartLine: 3},
		{Level: 3, Text: "not-a-date", StartLine: 5}, // level-3: not checked
	}}
	doc := &corpus.Document{ID: "log.md", Root: root}
	if rep := okf.Check(mkCorpus(t, doc)); !rep.Conformant {
		t.Errorf("a level-3 non-date heading must not violate (level-2-only): %+v", rep.ReservedStructure)
	}
}

func TestR3_NonRootIndexFrontmatter(t *testing.T) {
	// A non-root index.md WITH frontmatter is a violation; WITHOUT is conformant.
	with := &corpus.Document{ID: "sub/index.md", FrontMatterPresent: true, FrontMatterParsed: true}
	without := &corpus.Document{ID: "sub2/index.md", FrontMatterPresent: false}
	rep := okf.Check(mkCorpus(t, with, without))
	if len(rep.ReservedStructure) != 1 {
		t.Fatalf("want exactly 1 reserved-structure violation, got %+v", rep.ReservedStructure)
	}
	if rep.ReservedStructure[0].Doc != "sub/index.md" {
		t.Errorf("violation on %q, want sub/index.md", rep.ReservedStructure[0].Doc)
	}
}

func TestR3_RootIndex(t *testing.T) {
	t.Run("only-okf_version passes", func(t *testing.T) {
		doc := &corpus.Document{
			ID: "index.md", FrontMatterPresent: true, FrontMatterParsed: true,
			// Title simulates the parser's H1 fallback — it must NOT count as a key.
			FrontMatter: corpus.FrontMatter{Title: "Home", Extra: map[string]any{"okf_version": "0.1"}},
		}
		rep := okf.Check(mkCorpus(t, doc))
		if !rep.Conformant {
			t.Fatalf("want conformant, got %+v", rep)
		}
		if rep.DetectedVersion != "0.1" {
			t.Errorf("DetectedVersion = %q, want 0.1", rep.DetectedVersion)
		}
	})
	t.Run("extra key fails", func(t *testing.T) {
		doc := &corpus.Document{
			ID: "index.md", FrontMatterPresent: true, FrontMatterParsed: true,
			FrontMatter: corpus.FrontMatter{Extra: map[string]any{"okf_version": "0.1", "type": "Overview"}},
		}
		rep := okf.Check(mkCorpus(t, doc))
		if rep.Conformant || len(rep.ReservedStructure) != 1 {
			t.Fatalf("want 1 reserved-structure violation, got %+v", rep)
		}
		// The version is still surfaced even though the index is non-conformant.
		if rep.DetectedVersion != "0.1" {
			t.Errorf("DetectedVersion = %q, want 0.1", rep.DetectedVersion)
		}
	})
	t.Run("typed key fails", func(t *testing.T) {
		// A real typed frontmatter key (not Title) also fails.
		doc := &corpus.Document{
			ID: "index.md", FrontMatterPresent: true, FrontMatterParsed: true,
			FrontMatter: corpus.FrontMatter{Description: "the bundle", Extra: map[string]any{"okf_version": "0.1"}},
		}
		if rep := okf.Check(mkCorpus(t, doc)); rep.Conformant {
			t.Fatalf("a description key should make the root index non-conformant: %+v", rep)
		}
	})
	t.Run("no frontmatter passes", func(t *testing.T) {
		doc := &corpus.Document{ID: "index.md", FrontMatterPresent: false}
		if rep := okf.Check(mkCorpus(t, doc)); !rep.Conformant {
			t.Fatalf("root index without frontmatter should be conformant, got %+v", rep)
		}
	})
	t.Run("present-but-unparseable fails", func(t *testing.T) {
		// An undecodable block cannot be shown to contain only okf_version → R3.
		doc := &corpus.Document{ID: "index.md", FrontMatterPresent: true, FrontMatterParsed: false}
		rep := okf.Check(mkCorpus(t, doc))
		if rep.Conformant || len(rep.ReservedStructure) != 1 {
			t.Fatalf("root index with unparseable frontmatter should be a reserved-structure violation, got %+v", rep)
		}
	})
	t.Run("case-insensitive root INDEX.MD only-okf_version passes", func(t *testing.T) {
		// A bundle-root INDEX.MD is still the root index (case-insensitive), so its
		// okf_version-only frontmatter is conformant and its version is detected.
		doc := &corpus.Document{
			ID: "INDEX.MD", FrontMatterPresent: true, FrontMatterParsed: true,
			FrontMatter: corpus.FrontMatter{Extra: map[string]any{"okf_version": "0.1"}},
		}
		rep := okf.Check(mkCorpus(t, doc))
		if !rep.Conformant {
			t.Fatalf("bundle-root INDEX.MD with only okf_version should be conformant, got %+v", rep)
		}
		if rep.DetectedVersion != "0.1" {
			t.Errorf("DetectedVersion = %q, want 0.1 (case-insensitive root detection)", rep.DetectedVersion)
		}
	})
}

// TestReadmeIsConceptDoc asserts README.md is a concept document (R1/R2 apply),
// NOT a reserved file — the reserved set is index.md/log.md ONLY.
func TestReadmeIsConceptDoc(t *testing.T) {
	// README.md without frontmatter must be an R1 (missing-frontmatter) violation.
	rep := okf.Check(mkCorpus(t, concept("README.md", false, false, nil)))
	if len(rep.MissingFrontmatter) != 1 || rep.MissingFrontmatter[0].Doc != "README.md" {
		t.Fatalf("README.md should be treated as a concept doc (R1 applies), got %+v", rep)
	}
	// With a type it is conformant.
	rep = okf.Check(mkCorpus(t, concept("README.md", true, true, map[string]any{"type": "Reference"})))
	if !rep.Conformant {
		t.Errorf("README.md with a type should be conformant, got %+v", rep)
	}
}

// TestReservedCaseInsensitive asserts reserved-name matching is case-insensitive
// (INDEX.MD / Log.md are reserved, not concept docs).
func TestReservedCaseInsensitive(t *testing.T) {
	// INDEX.MD non-root with frontmatter → reserved-structure violation, NOT an
	// R1 concept violation.
	rep := okf.Check(mkCorpus(t, &corpus.Document{ID: "sub/INDEX.MD", FrontMatterPresent: true, FrontMatterParsed: true}))
	if len(rep.MissingFrontmatter) != 0 {
		t.Errorf("INDEX.MD should not be a concept doc: %+v", rep.MissingFrontmatter)
	}
	if len(rep.ReservedStructure) != 1 {
		t.Errorf("non-root INDEX.MD with frontmatter should be a reserved-structure violation: %+v", rep.ReservedStructure)
	}
}

// TestOKFVersionDetection covers stringifying a non-string okf_version and the
// absent case.
func TestOKFVersionDetection(t *testing.T) {
	num := &corpus.Document{
		ID: "index.md", FrontMatterPresent: true, FrontMatterParsed: true,
		FrontMatter: corpus.FrontMatter{Extra: map[string]any{"okf_version": 0.1}},
	}
	if got := okf.Check(mkCorpus(t, num)).DetectedVersion; got != "0.1" {
		t.Errorf("numeric okf_version detected as %q, want 0.1", got)
	}
	none := &corpus.Document{ID: "index.md", FrontMatterPresent: false}
	if got := okf.Check(mkCorpus(t, none)).DetectedVersion; got != "" {
		t.Errorf("DetectedVersion = %q, want empty", got)
	}
}

// TestDeterminismAndSorting asserts violations come out sorted by (Doc, Line)
// and the check is stable across runs.
func TestDeterminismAndSorting(t *testing.T) {
	c := mkCorpus(t,
		concept("z.md", false, false, nil),
		concept("a.md", false, false, nil),
		concept("m.md", true, true, map[string]any{}), // missing type
		logDoc("log.md", "bad-1", "bad-2"),
	)
	r1 := okf.Check(c)
	r2 := okf.Check(c)
	if !reflect.DeepEqual(r1, r2) {
		t.Fatal("okf.Check is not deterministic across runs")
	}
	// MissingFrontmatter sorted by Doc.
	if len(r1.MissingFrontmatter) != 2 ||
		r1.MissingFrontmatter[0].Doc != "a.md" || r1.MissingFrontmatter[1].Doc != "z.md" {
		t.Errorf("MissingFrontmatter not sorted by Doc: %+v", r1.MissingFrontmatter)
	}
	// log.md date violations sorted by Line.
	if len(r1.ReservedStructure) != 2 ||
		r1.ReservedStructure[0].Line >= r1.ReservedStructure[1].Line {
		t.Errorf("ReservedStructure not sorted by Line: %+v", r1.ReservedStructure)
	}
}

// TestEmptyCorpusConformant asserts a nil/empty corpus is trivially conformant.
func TestEmptyCorpusConformant(t *testing.T) {
	if rep := okf.Check(nil); !rep.Conformant {
		t.Errorf("nil corpus should be conformant, got %+v", rep)
	}
	if rep := okf.Check(corpus.NewCorpus()); !rep.Conformant {
		t.Errorf("empty corpus should be conformant, got %+v", rep)
	}
}
