package corpus

import (
	"cmp"
	"slices"
)

// HeadingInventory records, per document, the set of canonical anchor slugs
// available in that document (ADR 0006). It drives cross-file anchor
// validation: a reference to other.md#some-heading is a broken anchor unless
// the inventory has the slug for that document.
//
// It is an implementation detail of Corpus: instances are owned by a Corpus and
// reached only through Corpus.AddHeading / Corpus.HasHeading; the live map is
// never handed to callers.
type HeadingInventory map[DocumentID]map[string]struct{}

// NewHeadingInventory returns an empty inventory.
func NewHeadingInventory() HeadingInventory {
	return make(HeadingInventory)
}

// Add records that document id contains a heading with the given slug.
func (h HeadingInventory) Add(id DocumentID, slug string) {
	slugs, ok := h[id]
	if !ok {
		slugs = make(map[string]struct{})
		h[id] = slugs
	}
	slugs[slug] = struct{}{}
}

// Has reports whether document id contains a heading with the given slug.
func (h HeadingInventory) Has(id DocumentID, slug string) bool {
	slugs, ok := h[id]
	if !ok {
		return false
	}
	_, ok = slugs[slug]
	return ok
}

// AliasTable maps a front-matter alias (or other lookup key) to the set of
// candidate documents that declare it. A single alias may map to several
// documents; that ambiguity is surfaced as a finding during resolution rather
// than guessed at (ADR 0001).
//
// Like HeadingInventory it is an implementation detail of Corpus: reached only
// through Corpus.AddAlias / Corpus.LookupAlias; the live map is never exposed.
type AliasTable map[string]map[DocumentID]struct{}

// NewAliasTable returns an empty alias table.
func NewAliasTable() AliasTable {
	return make(AliasTable)
}

// Add records that document id is reachable via the given alias.
func (a AliasTable) Add(alias string, id DocumentID) {
	ids, ok := a[alias]
	if !ok {
		ids = make(map[DocumentID]struct{})
		a[alias] = ids
	}
	ids[id] = struct{}{}
}

// Lookup returns the candidate documents for an alias, sorted by DocumentID for
// deterministic iteration. The result is empty if the alias is unknown.
func (a AliasTable) Lookup(alias string) []DocumentID {
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
