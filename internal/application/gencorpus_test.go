package application_test

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// genCorpusChainLen is the length of the deterministic deep chain genCorpus
// appends (deep/chain0 … chain{N-1}) so every generated corpus has a non-empty
// far-from-root set (ADR 0021). Exposed so count-sensitive tests can account for
// the extra documents.
const genCorpusChainLen = 7

// genCorpus writes n synthetic markdown documents into a fresh temp dir laid out
// in a handful of subdirectories, each with front matter, several headings, and
// a realistic mix of cross-links (relative links to other generated docs, a few
// anchored links, and a couple of external links). A README.md root ties the
// graph together so reachability is determinate. Link targets are chosen with a
// FIXED seed so the corpus is identical across runs (the determinism test
// depends on this). It returns the root directory.
func genCorpus(tb testing.TB, n int) string {
	tb.Helper()
	root := tb.TempDir()
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic test fixture, not security

	dirs := []string{"", "guides", "guides/advanced", "reference", "tutorials", "notes"}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			tb.Fatal(err)
		}
	}

	id := func(i int) string {
		d := dirs[i%len(dirs)]
		name := fmt.Sprintf("doc%05d.md", i)
		if d == "" {
			return name
		}
		return d + "/" + name
	}

	// Root README links to the first handful of docs so the graph is connected
	// and reachability is determinate.
	var sb []byte
	rootBody := "---\ntitle: Home\ntype: index\n---\n\n# Home\n\nEntry point.\n\n"
	for i := 0; i < n && i < 20; i++ {
		rootBody += fmt.Sprintf("- [doc %d](%s)\n", i, id(i))
	}
	// Link the head of a deterministic deep chain (written below) so it is reachable
	// but far from the root — this guarantees a NON-EMPTY far-from-root set.
	rootBody += "- [deep chain](deep/chain0.md)\n"
	writeFile(tb, filepath.Join(root, "README.md"), []byte(rootBody))

	for i := 0; i < n; i++ {
		path := filepath.Join(root, filepath.FromSlash(id(i)))
		_ = sb
		var b []byte
		b = append(b, []byte(fmt.Sprintf("---\ntitle: Document %d\ndescription: synthetic doc %d\n---\n\n", i, i))...)
		b = append(b, []byte(fmt.Sprintf("# Document %d\n\nIntro for %d.\n\n", i, i))...)
		b = append(b, []byte("## Details\n\nSome details here.\n\n")...)
		b = append(b, []byte("## See also\n\n")...)
		// 3-6 cross-links to other docs as proper relative paths (resolved
		// relative to the origin's directory). A fraction are anchored.
		nlinks := 3 + rng.Intn(4)
		originDir := filepath.Dir(id(i))
		for j := 0; j < nlinks; j++ {
			tgt := rng.Intn(n)
			if tgt == i {
				tgt = (tgt + 1) % n
			}
			rel, err := filepath.Rel(originDir, id(tgt))
			if err != nil {
				tb.Fatal(err)
			}
			rel = filepath.ToSlash(rel)
			if j%3 == 0 {
				b = append(b, []byte(fmt.Sprintf("- [to %d](%s#details)\n", tgt, rel))...)
			} else {
				b = append(b, []byte(fmt.Sprintf("- [to %d](%s)\n", tgt, rel))...)
			}
		}
		// A couple of external links to exercise classification (never fetched
		// unless --check-external).
		b = append(b, []byte("\n## External\n\n- <https://example.com/page>\n- [site](http://example.org)\n")...)
		writeFile(tb, path, b)
	}

	// A deterministic linear deep chain deep/chain0 -> … -> deep/chain6. README
	// links chain0, and each link points one deeper, so chain5 is 6 hops from the
	// root and chain6 is 7 — both at/beyond the default far-from-root threshold
	// (ADR 0021). This ensures the corpus always has a NON-EMPTY farFromRoot list,
	// so the determinism test's byte-equality actually covers a populated
	// hopsFromRoot/farFromRoot surface rather than the trivially-empty case. No main
	// doc links into the chain, so the distances are fixed.
	if err := os.MkdirAll(filepath.Join(root, "deep"), 0o755); err != nil {
		tb.Fatal(err)
	}
	for k := 0; k < genCorpusChainLen; k++ {
		var b []byte
		b = append(b, []byte(fmt.Sprintf("---\ntitle: Chain %d\ndescription: deep chain link %d\n---\n\n# Chain %d\n\nDeep chain link.\n\n", k, k, k))...)
		if k+1 < genCorpusChainLen {
			b = append(b, []byte(fmt.Sprintf("## Onward\n\n- [next](chain%d.md)\n", k+1))...)
		}
		writeFile(tb, filepath.Join(root, "deep", fmt.Sprintf("chain%d.md", k)), b)
	}
	return root
}

func writeFile(tb testing.TB, path string, b []byte) {
	tb.Helper()
	if err := os.WriteFile(path, b, 0o644); err != nil { //nolint:gosec // test fixture
		tb.Fatal(err)
	}
}
