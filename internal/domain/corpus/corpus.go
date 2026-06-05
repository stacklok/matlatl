package corpus

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
)

// Corpus is the in-memory collection of parsed documents plus the indices that
// resolution and analysis read from. It is built up during the pipeline and
// then treated as read-only. It is not safe for concurrent mutation.
//
// Lifecycle / concurrency contract: the Corpus is mutated only during the
// single-threaded parse-and-merge stage (Add). At the phase boundary it is
// FROZEN — no further Add calls — after which the read-only accessors (Get,
// Documents, HasDocument, HasHeading, LookupAlias, …) are safe for concurrent
// readers. Resolution in P2 runs single-threaded over the frozen Corpus by
// design; any future fan-out must read, never mutate, a frozen Corpus.
//
// The indices (headingInventory, aliasTable) are unexported and never handed
// out by reference: callers populate them through Add* methods and query them
// through read-only accessors. This preserves the "built once, read-only
// thereafter" invariant and avoids baking in a data-race shape for later phases.
type Corpus struct {
	docs     map[DocumentID]*Document
	headings headingInventory
	aliases  aliasTable
	// frozen, once set by Freeze, makes every mutator (Add, AddHeading,
	// AddAlias) reject further writes. This enforces the "built once, read-only
	// thereafter" lifecycle contract as code, not just documentation: the
	// pipeline freezes the corpus after the parse-and-merge stage so the
	// concurrent fan-out path cannot mutate it during resolution/analysis
	// (ADR 0004; P6 concurrency-readiness).
	frozen bool
}

// ErrFrozen is returned by mutators called after Freeze. It signals a
// programming error (a write attempted past the corpus lifecycle boundary),
// surfaced as an error (not a panic) so the pipeline can degrade gracefully.
var ErrFrozen = errors.New("corpus: frozen (no mutations allowed after Freeze)")

// NewCorpus returns an empty Corpus with initialized indices.
func NewCorpus() *Corpus {
	return &Corpus{
		docs:     make(map[DocumentID]*Document),
		headings: newHeadingInventory(),
		aliases:  newAliasTable(),
	}
}

// Freeze marks the corpus read-only: subsequent Add/AddHeading/AddAlias calls
// return ErrFrozen. The pipeline calls Freeze once parsing and the
// single-threaded merge complete, before resolution/analysis, so the read-only
// accessors are then safe for concurrent readers (ADR 0004). Freeze is
// idempotent.
func (c *Corpus) Freeze() { c.frozen = true }

// Frozen reports whether Freeze has been called.
func (c *Corpus) Frozen() bool { return c.frozen }

// Add inserts a document. It returns an error if a document with the same
// DocumentID is already present (identities are unique, ADR 0001) or if doc is
// nil or has an empty ID.
func (c *Corpus) Add(doc *Document) error {
	if c.frozen {
		return ErrFrozen
	}
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
	c.indexHeadings(doc)
	c.indexAliases(doc)
	return nil
}

// indexHeadings records every section slug of doc into the heading inventory,
// keeping the inventory consistent with the documents in the corpus (ADR 0006).
func (c *Corpus) indexHeadings(doc *Document) {
	if doc.Root == nil {
		return
	}
	var walk func(s *Section)
	walk = func(s *Section) {
		if s.Slug != "" {
			c.headings.add(doc.ID, s.Slug)
		}
		for _, child := range s.Children {
			walk(child)
		}
	}
	walk(doc.Root)
}

// indexAliases records each front-matter alias of doc into the alias table, so
// the P2 resolver can map wikilink aliases to candidate documents (ADR 0001).
func (c *Corpus) indexAliases(doc *Document) {
	for _, alias := range doc.FrontMatter.Aliases {
		if alias == "" {
			continue
		}
		c.aliases.add(alias, doc.ID)
	}
}

// Get returns the document with the given ID and whether it was found.
func (c *Corpus) Get(id DocumentID) (*Document, bool) {
	doc, ok := c.docs[id]
	return doc, ok
}

// HasDocument reports whether id is a known markdown document in the corpus. It
// (together with DocumentIDs, HasHeading and LookupAlias) lets *Corpus satisfy
// the reference.Catalog read-only interface used by the link resolver.
func (c *Corpus) HasDocument(id DocumentID) bool {
	_, ok := c.docs[id]
	return ok
}

// DocumentIDs returns all known document identities, sorted for determinism.
func (c *Corpus) DocumentIDs() []DocumentID {
	out := make([]DocumentID, 0, len(c.docs))
	for id := range c.docs {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
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

// HeadingCount returns the total number of heading slugs indexed across all
// documents.
func (c *Corpus) HeadingCount() int { return c.headings.count() }

// AddHeading records that document id contains a heading with the given canonical
// slug (ADR 0006). This is a build-phase mutation routed through the Corpus so
// the underlying index is never exposed.
//
// Test-only seam: the production path populates the heading inventory directly
// from a Document's section tree inside Add; this exported entry point exists so
// tests can build a heading inventory without a full Document and is not called
// from non-test code. It panics if the corpus is frozen (a programming error:
// the only legitimate callers run before Freeze).
func (c *Corpus) AddHeading(id DocumentID, slug string) {
	if c.frozen {
		panic(ErrFrozen)
	}
	c.headings.add(id, slug)
}

// HasHeading reports whether document id contains a heading with the given slug.
// It is the read-only query used by anchor resolution.
func (c *Corpus) HasHeading(id DocumentID, slug string) bool {
	return c.headings.has(id, slug)
}

// AddAlias records that document id is reachable via the given alias. This is a
// build-phase mutation routed through the Corpus so the underlying index is
// never exposed.
//
// Test-only seam: the production path populates the alias table directly from a
// Document's front-matter aliases inside Add; this exported entry point exists
// so tests can seed aliases without a full Document and is not called from
// non-test code. It panics if the corpus is frozen (a programming error: the
// only legitimate callers run before Freeze).
func (c *Corpus) AddAlias(alias string, id DocumentID) {
	if c.frozen {
		panic(ErrFrozen)
	}
	c.aliases.add(alias, id)
}

// LookupAlias returns the candidate documents for an alias, sorted by
// DocumentID for deterministic iteration. The result is a freshly allocated
// slice (mutating it does not affect the corpus) and is empty if the alias is
// unknown.
func (c *Corpus) LookupAlias(alias string) []DocumentID {
	return c.aliases.lookup(alias)
}
