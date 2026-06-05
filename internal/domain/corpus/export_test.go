package corpus

// Test-only mutating seams. These live in export_test.go (compiled ONLY under
// `go test`) so production code physically cannot bypass Add's consistency
// contract by poking the heading/alias indices directly: the production path
// always populates both indices from a Document's section tree + front matter
// inside Add. Tests, however, sometimes need to seed a single heading or alias
// without constructing a full Document, which is what these provide.
//
// They mirror Add's frozen contract (return ErrFrozen after Freeze) rather than
// panicking, keeping the mutator behavior uniform.

// AddHeading records that document id contains a heading with the given
// canonical slug (ADR 0006), for tests that build a heading inventory without a
// full Document. Returns ErrFrozen if the corpus is frozen.
func (c *Corpus) AddHeading(id DocumentID, slug string) error {
	if c.frozen.Load() {
		return ErrFrozen
	}
	c.headings.add(id, slug)
	return nil
}

// AddAlias records that document id is reachable via the given alias, for tests
// that seed aliases without a full Document. Returns ErrFrozen if the corpus is
// frozen.
func (c *Corpus) AddAlias(alias string, id DocumentID) error {
	if c.frozen.Load() {
		return ErrFrozen
	}
	c.aliases.add(alias, id)
	return nil
}
