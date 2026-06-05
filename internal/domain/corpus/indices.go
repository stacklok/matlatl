package corpus

import (
	"cmp"
	"slices"
)

// headingInventory records, per document, the set of canonical anchor slugs
// available in that document (ADR 0006). It drives cross-file anchor
// validation: a reference to other.md#some-heading is a broken anchor unless
// the inventory has the slug for that document.
//
// It is an unexported implementation detail of Corpus: instances are owned by a
// Corpus and reached only through Corpus.AddHeading / Corpus.HasHeading; the
// live map is never handed to callers (encapsulation is statically enforced).
type headingInventory map[DocumentID]map[string]struct{}

// newHeadingInventory returns an empty inventory.
func newHeadingInventory() headingInventory {
	return make(headingInventory)
}

// add records that document id contains a heading with the given slug.
func (h headingInventory) add(id DocumentID, slug string) {
	slugs, ok := h[id]
	if !ok {
		slugs = make(map[string]struct{})
		h[id] = slugs
	}
	slugs[slug] = struct{}{}
}

// count returns the total number of (document, slug) heading entries.
func (h headingInventory) count() int {
	n := 0
	for _, slugs := range h {
		n += len(slugs)
	}
	return n
}

// has reports whether document id contains a heading with the given slug.
func (h headingInventory) has(id DocumentID, slug string) bool {
	slugs, ok := h[id]
	if !ok {
		return false
	}
	_, ok = slugs[slug]
	return ok
}

// aliasTable maps a front-matter alias (or other lookup key) to the set of
// candidate documents that declare it. A single alias may map to several
// documents; that ambiguity is surfaced as a finding during resolution rather
// than guessed at (ADR 0001).
//
// Like headingInventory it is an unexported implementation detail of Corpus:
// reached only through Corpus.AddAlias / Corpus.LookupAlias; the live map is
// never exposed.
type aliasTable map[string]map[DocumentID]struct{}

// newAliasTable returns an empty alias table.
func newAliasTable() aliasTable {
	return make(aliasTable)
}

// add records that document id is reachable via the given alias.
func (a aliasTable) add(alias string, id DocumentID) {
	ids, ok := a[alias]
	if !ok {
		ids = make(map[DocumentID]struct{})
		a[alias] = ids
	}
	ids[id] = struct{}{}
}

// lookup returns the candidate documents for an alias, sorted by DocumentID for
// deterministic iteration. The result is empty if the alias is unknown.
func (a aliasTable) lookup(alias string) []DocumentID {
	ids, ok := a[alias]
	if !ok {
		return nil
	}
	out := make([]DocumentID, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	slices.SortFunc(out, cmp.Compare[DocumentID])
	return out
}
