package mdparser

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"

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
		prev := "*a" + itoa(i-1)
		b.WriteString("a")
		b.WriteString(itoa(i))
		b.WriteString(": &a")
		b.WriteString(itoa(i))
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

	assertBoundedParse(t, src)
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

// itoa is a tiny dependency-free int→string for building fixtures.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
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
