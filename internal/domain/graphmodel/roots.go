package graphmodel

import (
	"path"
	"slices"
	"strings"

	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/identity"
)

// Front-matter keys/values that affect analysis (ADR 0007).
const (
	// FMKeyDoctopus is the front-matter key (in FrontMatter.Extra) carrying
	// doctopus directives.
	FMKeyDoctopus = "doctopus"
	// FMValOrphanIntentional marks a document as an intentional orphan, excluding
	// it from Orphan/Unreachable findings.
	FMValOrphanIntentional = "orphan-intentional"
	// FMTypeIndex is the front-matter `type: index` value that marks a root.
	FMTypeIndex = "index"
)

// RootSet is the resolved set of reachability roots plus whether it is
// indeterminate (empty). When Indeterminate is true, reachability analysis is
// skipped and a notice is emitted (ADR 0005/0007); orphan detection still runs.
type RootSet struct {
	Roots         []identity.DocumentID // sorted
	Indeterminate bool
	// BadGlobs holds configured globs that are malformed (path.Match reported
	// ErrBadPattern). They matched nothing and are surfaced as a notice by the
	// caller rather than silently ignored.
	BadGlobs []string
}

// ResolveRootSet computes the root set from configured globs plus conventions
// (ADR 0007): any README.md/index.md at any depth, and any doc with front matter
// `type: index`. configuredGlobs are matched against DocumentIDs with
// path.Match (slash paths; the single-`*` wildcard does NOT cross `/`, and
// `**` is not supported). A malformed glob is collected in BadGlobs (it matches
// nothing) rather than silently discarded. The result is sorted and
// de-duplicated.
func ResolveRootSet(c *corpus.Corpus, configuredGlobs []string) RootSet {
	set := make(map[identity.DocumentID]struct{})

	// Validate globs once up-front so a bad pattern is reported regardless of
	// whether any document would have matched it.
	var badGlobs []string
	goodGlobs := make([]string, 0, len(configuredGlobs))
	for _, g := range configuredGlobs {
		if _, err := path.Match(g, ""); err != nil {
			badGlobs = append(badGlobs, g)
			continue
		}
		goodGlobs = append(goodGlobs, g)
	}

	for _, doc := range c.Documents() {
		id := doc.ID
		if identity.IsDirectoryIndex(path.Base(id.String())) {
			set[id] = struct{}{}
			continue
		}
		if isIndexType(doc) {
			set[id] = struct{}{}
			continue
		}
		for _, g := range goodGlobs {
			if ok, _ := path.Match(g, id.String()); ok {
				set[id] = struct{}{}
				break
			}
		}
	}

	roots := make([]identity.DocumentID, 0, len(set))
	for id := range set {
		roots = append(roots, id)
	}
	slices.Sort(roots)
	slices.Sort(badGlobs)
	return RootSet{Roots: roots, Indeterminate: len(roots) == 0, BadGlobs: badGlobs}
}

// isIndexType reports whether a document declares `type: index` in front matter
// (the typed Status field is not used for this; type lives in Extra).
func isIndexType(doc *corpus.Document) bool {
	if doc.FrontMatter.Extra == nil {
		return false
	}
	v, ok := doc.FrontMatter.Extra["type"]
	if !ok {
		return false
	}
	s, ok := v.(string)
	return ok && strings.EqualFold(s, FMTypeIndex)
}

// isIntentionalOrphan reports whether a document opts out of orphan/unreachable
// findings via `doctopus: orphan-intentional` (ADR 0007).
func isIntentionalOrphan(doc *corpus.Document) bool {
	if doc.FrontMatter.Extra == nil {
		return false
	}
	v, ok := doc.FrontMatter.Extra[FMKeyDoctopus]
	if !ok {
		return false
	}
	s, ok := v.(string)
	return ok && strings.EqualFold(strings.TrimSpace(s), FMValOrphanIntentional)
}

// IntentionalOrphans returns the sorted set of documents marked
// orphan-intentional.
func IntentionalOrphans(c *corpus.Corpus) []identity.DocumentID {
	var out []identity.DocumentID
	for _, doc := range c.Documents() {
		if isIntentionalOrphan(doc) {
			out = append(out, doc.ID)
		}
	}
	slices.Sort(out)
	return out
}
