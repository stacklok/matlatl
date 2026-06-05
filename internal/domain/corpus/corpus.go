package corpus

import (
	"cmp"
	"fmt"
	"slices"
)

// Corpus is the in-memory collection of parsed documents plus the indices that
// resolution and analysis read from. It is built up during the pipeline and
// then treated as read-only. It is not safe for concurrent mutation.
//
// The indices (HeadingInventory, AliasTable) are kept unexported and are never
// handed out by reference: callers populate them through Add* methods and query
// them through read-only accessors. This preserves the "built once, read-only
// thereafter" invariant and avoids baking in a data-race shape for later phases.
type Corpus struct {
	docs     map[DocumentID]*Document
	headings HeadingInventory
	aliases  AliasTable
}

// NewCorpus returns an empty Corpus with initialized indices.
func NewCorpus() *Corpus {
	return &Corpus{
		docs:     make(map[DocumentID]*Document),
		headings: NewHeadingInventory(),
		aliases:  NewAliasTable(),
	}
}

// Add inserts a document. It returns an error if a document with the same
// DocumentID is already present (identities are unique, ADR 0001) or if doc is
// nil or has an empty ID.
func (c *Corpus) Add(doc *Document) error {
	if doc == nil {
		return fmt.Errorf("corpus: cannot add nil document")
	}
	if doc.ID == "" {
		return fmt.Errorf("corpus: cannot add document with empty ID")
	}
	if _, exists := c.docs[doc.ID]; exists {
		return fmt.Errorf("corpus: duplicate document ID %q", doc.ID)
	}
	c.docs[doc.ID] = doc
	return nil
}

// Get returns the document with the given ID and whether it was found.
func (c *Corpus) Get(id DocumentID) (*Document, bool) {
	doc, ok := c.docs[id]
	return doc, ok
}

// Documents returns all documents sorted by DocumentID. Sorting makes
// downstream iteration deterministic regardless of map insertion order.
func (c *Corpus) Documents() []*Document {
	out := make([]*Document, 0, len(c.docs))
	for _, doc := range c.docs {
		out = append(out, doc)
	}
	slices.SortFunc(out, func(a, b *Document) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return out
}

// Len returns the number of documents in the corpus.
func (c *Corpus) Len() int { return len(c.docs) }

// AddHeading records that document id contains a heading with the given canonical
// slug (ADR 0006). This is a build-phase mutation routed through the Corpus so
// the underlying index is never exposed.
func (c *Corpus) AddHeading(id DocumentID, slug string) {
	c.headings.Add(id, slug)
}

// HasHeading reports whether document id contains a heading with the given slug.
// It is the read-only query used by anchor resolution.
func (c *Corpus) HasHeading(id DocumentID, slug string) bool {
	return c.headings.Has(id, slug)
}

// AddAlias records that document id is reachable via the given alias. This is a
// build-phase mutation routed through the Corpus so the underlying index is
// never exposed.
func (c *Corpus) AddAlias(alias string, id DocumentID) {
	c.aliases.Add(alias, id)
}

// LookupAlias returns the candidate documents for an alias, sorted by
// DocumentID for deterministic iteration. The result is a freshly allocated
// slice (mutating it does not affect the corpus) and is empty if the alias is
// unknown.
func (c *Corpus) LookupAlias(alias string) []DocumentID {
	return c.aliases.Lookup(alias)
}
