package reference

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/internal/domain/identity"
)

// skillManifestBase is the agent-skills manifest file name. ADR 0010 already
// auto-detects it by FILENAME (never by directory), and the same convention
// names the entry document of a directory mention that has no README/index
// (ADR 0026): `.claude/skills/foo/` resolves to `.claude/skills/foo/SKILL.md`.
const skillManifestBase = "skill.md"

// RawMention is an unlinked mention as extracted from a document, before
// resolution (ADR 0026). The parser classifies its shape; the MentionResolver
// decides whether it names an in-corpus document.
type RawMention struct {
	// Origin is the document the mention was found in.
	Origin identity.DocumentID
	// Kind is the textual form: path, bare file name, or invocation.
	Kind MentionKind
	// Target is the resolvable part: the path, the file name, or (for an
	// invocation) the name WITHOUT its prefix.
	Target string
	// Prefix is the invocation prefix the token was matched with (e.g. "/").
	// Empty for path and file-name mentions.
	Prefix string
	// Text is the token exactly as written, including an invocation's prefix.
	Text string
	// Line is the 1-based source line the mention appears on.
	Line int
}

// InvocationRule declares one name-prefixed invocation form (ADR 0026): a token
// made of Prefix followed by a name refers to the document whose front-matter
// `name:` or `aliases:` equals that name, provided the document's ID matches at
// least one of the Targets globs. The rule is repo-config-declared; matlatl
// ships no knowledge of any tool's prefix or file layout (ADR 0011).
type InvocationRule struct {
	Prefix  string
	Targets []string
}

// MentionResolver turns RawMentions into Valid Mention references. It reuses
// the link Resolver's path arithmetic (root containment, root-absolute and
// content-root handling, directory detection) and the corpus alias index, so
// mentions and links can never disagree on what a path names. It is a pure
// domain service: catalog lookups and path arithmetic only.
type MentionResolver struct {
	resolver *Resolver
	// globs maps an invocation prefix to the union of its rules' target globs.
	globs map[string][]string
	// basenames maps a document basename to every document carrying it, sorted.
	basenames map[string][]identity.DocumentID
}

// NewMentionResolver builds a MentionResolver over the same catalog, asset
// lookup, policy and content roots as r. Rules sharing a prefix are merged.
func NewMentionResolver(r *Resolver, rules []InvocationRule) *MentionResolver {
	m := &MentionResolver{
		resolver:  r,
		globs:     make(map[string][]string, len(rules)),
		basenames: make(map[string][]identity.DocumentID),
	}
	for _, rule := range rules {
		m.globs[rule.Prefix] = append(m.globs[rule.Prefix], rule.Targets...)
	}
	ids := r.catalog.DocumentIDs()
	slices.Sort(ids)
	for _, id := range ids {
		base := id.Base()
		m.basenames[base] = append(m.basenames[base], id)
	}
	return m
}

// ResolveAll resolves every mention and returns only the ones that name an
// in-corpus document other than their origin, in input order. Duplicates (the
// same target, kind, line and text) are collapsed. An unresolved or ambiguous
// mention is dropped silently: text that merely looks like a path is too common
// to report on (ADR 0026).
func (m *MentionResolver) ResolveAll(raws []RawMention) []Reference {
	type key struct {
		target identity.DocumentID
		kind   MentionKind
		line   int
		text   string
	}
	seen := make(map[key]struct{})
	var out []Reference
	for _, raw := range raws {
		ref, ok := m.Resolve(raw)
		if !ok {
			continue
		}
		k := key{target: ref.Target.DocumentID, kind: raw.Kind, line: raw.Line, text: raw.Text}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, ref)
	}
	return out
}

// Resolve classifies a single mention. ok is false when the mention names no
// single in-corpus document, or names its own origin (a self-mention carries no
// navigational information, mirroring the projection's self-loop rule).
func (m *MentionResolver) Resolve(raw RawMention) (Reference, bool) {
	var (
		id identity.DocumentID
		ok bool
	)
	switch raw.Kind {
	case MentionPath:
		id, ok = m.resolvePath(raw.Origin, raw.Target)
	case MentionFilename:
		id, ok = m.resolveFilename(raw.Target)
	case MentionInvocation:
		id, ok = m.resolveInvocation(raw.Prefix, raw.Target)
	}
	if !ok || id == raw.Origin {
		return Reference{}, false
	}
	return ref(RawReference{
		Origin:      raw.Origin,
		RawTarget:   raw.Target,
		Type:        Mention,
		Line:        raw.Line,
		AnchorText:  raw.Text,
		MentionKind: raw.Kind,
	}, ResolvedTarget{Kind: TargetDocument, DocumentID: id}, Valid), true
}

// resolvePath resolves a path mention the way a link resolves (relative to the
// origin, or from the scan/content root for a single leading "/"), then falls
// back to repo-root-relative, since prose usually writes repository paths. Both
// attempts run through the ADR 0003 root-containment guard.
func (m *MentionResolver) resolvePath(origin identity.DocumentID, target string) (identity.DocumentID, bool) {
	if cleaned, ok := resolveInRoot(origin, target, m.resolver.contentRoots); ok {
		if id, ok := m.documentAt(cleaned); ok {
			return id, true
		}
	}
	if IsRootAbsolute(target) {
		return "", false // already resolved from the root above
	}
	cleaned := path.Clean(target)
	if identity.EscapesRoot(cleaned) {
		return "", false
	}
	return m.documentAt(cleaned)
}

// documentAt maps a cleaned, in-root path to the document it names: the
// document itself, or for a directory (ADR 0008) its README/index, else its
// SKILL.md. A directory with neither has no single document to point at, so it
// does not resolve (a mention never fans out to a folder's children).
func (m *MentionResolver) documentAt(cleaned string) (identity.DocumentID, bool) {
	id := identity.DocumentID(cleaned)
	if m.resolver.catalog.HasDocument(id) {
		return id, true
	}
	children, index, ok := m.resolver.directoryContents(cleaned)
	if !ok {
		return "", false
	}
	if index != "" {
		return index, true
	}
	for _, child := range children { // sorted
		if strings.EqualFold(child.Base(), skillManifestBase) {
			return child, true
		}
	}
	return "", false
}

// resolveFilename resolves a bare file name by basename, only when exactly one
// in-corpus document carries it. A shared basename is ambiguous and is never
// guessed at.
func (m *MentionResolver) resolveFilename(name string) (identity.DocumentID, bool) {
	if ids := m.basenames[name]; len(ids) == 1 {
		return ids[0], true
	}
	return "", false
}

// resolveInvocation resolves a prefixed name through the front-matter
// name/aliases index (the one wikilinks use), keeping only documents the
// prefix's target globs admit. Exactly one survivor resolves.
func (m *MentionResolver) resolveInvocation(prefix, name string) (identity.DocumentID, bool) {
	globs := m.globs[prefix]
	if len(globs) == 0 || name == "" {
		return "", false
	}
	var match []identity.DocumentID
	for _, id := range m.resolver.catalog.LookupAlias(name) {
		for _, g := range globs {
			if MatchGlob(g, id.String()) {
				match = append(match, id)
				break
			}
		}
	}
	match = sortedUnique(match)
	if len(match) == 1 {
		return match[0], true
	}
	return "", false
}

// errEmptyGlob is returned by ValidateGlob for an empty pattern.
var errEmptyGlob = errors.New("empty pattern")

// ValidateGlob reports whether pattern is a well-formed MatchGlob pattern: a
// non-empty, repo-relative slash path whose segments are each `**` or a valid
// path.Match pattern.
func ValidateGlob(pattern string) error {
	if pattern == "" {
		return errEmptyGlob
	}
	if strings.HasPrefix(pattern, "/") {
		return fmt.Errorf("pattern %q must be repository-relative (no leading /)", pattern)
	}
	for seg := range strings.SplitSeq(pattern, "/") {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return fmt.Errorf("pattern %q: %w", pattern, err)
		}
	}
	return nil
}

// MatchGlob reports whether the slash path name matches pattern. A `**`
// segment matches zero or more whole path segments; every other segment uses
// path.Match semantics (a `*` never crosses a `/`). A malformed segment
// matches nothing. It runs in O(segments(pattern) × segments(name)).
func MatchGlob(pattern, name string) bool {
	ps := strings.Split(pattern, "/")
	ns := strings.Split(name, "/")
	// next[j] holds whether ps[i+1:] matches ns[j:] while computing row i.
	next := make([]bool, len(ns)+1)
	next[len(ns)] = true // the empty pattern matches only the empty remainder
	for i := len(ps) - 1; i >= 0; i-- {
		cur := make([]bool, len(ns)+1)
		for j := len(ns); j >= 0; j-- {
			if ps[i] == "**" {
				cur[j] = next[j] || (j < len(ns) && cur[j+1])
				continue
			}
			if j < len(ns) {
				ok, err := path.Match(ps[i], ns[j])
				cur[j] = err == nil && ok && next[j+1]
			}
		}
		next = cur
	}
	return next[0]
}
