package mdparser

import (
	"context"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"go.abhg.dev/goldmark/frontmatter"

	"github.com/stacklok/doctopus/internal/domain/corpus"
)

// TestYAMLAliasBomb is the ADR 0003 adversarial test: a sub-64KiB YAML front
// matter block whose anchors/aliases would expand exponentially if naively
// dereferenced. ParseBytes (default Config) must complete without error or
// panic and without exploding memory.
func TestYAMLAliasBomb(t *testing.T) {
	// 9-level alias pyramid: each level references the previous one several
	// times, so a full expansion would be ~ fanout^levels nodes. The serialized
	// document itself stays tiny (well under the 64 KiB cap), which is exactly
	// the "billion laughs" shape we must survive.
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("a0: &a0 [\"lol\",\"lol\",\"lol\",\"lol\",\"lol\"]\n")
	const levels = 9
	for i := 1; i <= levels; i++ {
		// aN: &aN [*a(N-1), *a(N-1), *a(N-1), *a(N-1), *a(N-1)]
		prev := "*a" + strconv.Itoa(i-1)
		b.WriteString("a")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(": &a")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(" [")
		for j := 0; j < 5; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString(prev)
		}
		b.WriteString("]\n")
	}
	b.WriteString("title: Bomb\n")
	b.WriteString("---\n\n# Body\n")
	src := []byte(b.String())

	if len(src) >= DefaultMaxFrontMatterBytes {
		t.Fatalf("test fixture %d bytes is not sub-cap (%d); adjust the pyramid", len(src), DefaultMaxFrontMatterBytes)
	}

	// First defense (the 64 KiB cap) does NOT trip here — the bomb is sub-cap by
	// construction. The SECOND defense is gopkg.in/yaml.v3 >= 3.0.0's built-in
	// alias-expansion limiter, which rejects the pyramid at Decode time. Assert
	// that directly so the bomb cannot silently expand if the cap is raised.
	if err := decodeFrontMatterErr(t, src); err == nil {
		t.Error("expected yaml.v3 alias-ratio limiter to reject the bomb at Decode time")
	}

	// And the full parse degrades gracefully (no error/panic, bounded memory):
	// the malformed front matter is dropped, so Title falls back to the H1.
	assertBoundedParse(t, src)
	doc := parse(t, string(src))
	if doc.FrontMatter.Title == "Bomb" {
		t.Error("bomb front matter was decoded; it should have degraded to no front matter")
	}
}

// decodeFrontMatterErr runs goldmark + frontmatter over src and returns the
// error from Data.Decode, exposing the second-layer defense for assertion.
func decodeFrontMatterErr(t *testing.T, src []byte) error {
	t.Helper()
	p := New(Config{})
	pctx := parser.NewContext()
	p.md.Parser().Parse(text.NewReader(src), parser.WithContext(pctx))
	data := frontmatter.Get(pctx)
	if data == nil {
		t.Fatal("no front matter parsed from bomb fixture")
	}
	var out map[string]any
	return data.Decode(&out)
}

// TestTOMLDeepNestBomb is the TOML analogue: a deeply nested inline structure.
// TOML has no aliases, so this exercises deep nesting / large structure decode
// staying bounded and non-panicking under the size cap.
func TestTOMLDeepNestBomb(t *testing.T) {
	// Deeply nested inline arrays: [[[[ ... ]]]].
	const depth = 5000
	var b strings.Builder
	b.WriteString("+++\n")
	b.WriteString("title = \"Deep\"\n")
	b.WriteString("deep = ")
	b.WriteString(strings.Repeat("[", depth))
	b.WriteString(strings.Repeat("]", depth))
	b.WriteString("\n+++\n\n# Body\n")
	src := []byte(b.String())

	if len(src) >= DefaultMaxFrontMatterBytes {
		t.Fatalf("test fixture %d bytes is not sub-cap (%d)", len(src), DefaultMaxFrontMatterBytes)
	}

	assertBoundedParse(t, src)
}

// assertBoundedParse parses src with default Config and asserts it returns
// without error/panic and without an unreasonable heap growth.
func assertBoundedParse(t *testing.T, src []byte) {
	t.Helper()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	p := New(Config{})
	doc, err := p.ParseBytes(context.Background(), "bomb.md", src)
	if err != nil {
		t.Fatalf("ParseBytes returned error on bomb input (should degrade gracefully): %v", err)
	}
	if doc == nil {
		t.Fatal("ParseBytes returned nil document")
	}

	runtime.ReadMemStats(&after)
	// TotalAlloc only grows; guard against a multi-hundred-MB expansion. A sane
	// decode of these tiny inputs allocates well under this threshold.
	const maxGrowthBytes = 256 << 20 // 256 MiB
	if grew := after.TotalAlloc - before.TotalAlloc; grew > maxGrowthBytes {
		t.Fatalf("parsing the bomb allocated %d bytes (> %d cap): expansion not bounded", grew, maxGrowthBytes)
	}
}

// TestParse_HostileTitle asserts the INPUT side of the ADR-0003 escaping
// contract: titles containing quotes, angle brackets, backslashes and (escaped)
// newlines are stored VERBATIM by the parser. The parser must not mangle or
// pre-escape them; emitter-side escaping (DOT/Mermaid/HTML) is a P4/P5 concern
// applied at render time, not at parse time.
func TestParse_HostileTitle(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"quotes", `title: 'He said "hi"'`, `He said "hi"`},
		{"angle-brackets", `title: "<script>alert(1)</script>"`, `<script>alert(1)</script>`},
		{"backslash", `title: 'C:\path\to\file'`, `C:\path\to\file`},
		{"newline-escape", "title: \"line1\\nline2\"", "line1\nline2"},
		{"pipe-and-amp", `title: "a | b & c"`, `a | b & c`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parse(t, "---\n"+tt.yaml+"\n---\n\n# Body\n")
			if doc.FrontMatter.Title != tt.want {
				t.Errorf("title stored as %q, want verbatim %q", doc.FrontMatter.Title, tt.want)
			}
		})
	}
}

// TestKnownFMKeysMatchTags asserts the knownFMKeys set exactly matches the
// FrontMatter struct's typed fields (minus Extra), so a tag/name typo cannot
// silently route a known field into Extra. It uses reflection over the domain
// FrontMatter type and the lowercased field names the decoder keys on.
func TestKnownFMKeysMatchTags(t *testing.T) {
	rt := reflect.TypeOf(corpus.FrontMatter{})
	want := make(map[string]struct{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if name == "Extra" {
			continue
		}
		want[strings.ToLower(name)] = struct{}{}
	}

	if len(want) != len(knownFMKeys) {
		t.Fatalf("knownFMKeys has %d entries, FrontMatter has %d typed fields; they must match",
			len(knownFMKeys), len(want))
	}
	for k := range want {
		if _, ok := knownFMKeys[k]; !ok {
			t.Errorf("FrontMatter field %q is missing from knownFMKeys (would leak to Extra)", k)
		}
	}
	for k := range knownFMKeys {
		if _, ok := want[k]; !ok {
			t.Errorf("knownFMKeys has %q with no matching FrontMatter field (stale/typo)", k)
		}
	}
}
