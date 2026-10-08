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

// MentionResolver turns RawMentions into Mention references. It reuses
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

// ResolveAll resolves every mention, in input order. A mention that names one
// in-corpus document other than its origin is Valid. A markdown-named path or
// file name that names no document is Broken (a stale reference), and one that
// still names several after the nearest-scope tie-break is Ambiguous. Other
// unresolved tokens are dropped: text that merely looks like a path is too
// common to keep (ADR 0026). Duplicates (the same target, kind, line, text and
// health) are collapsed. Broken and Ambiguous mentions are never findings and
// never graph edges; they surface only in graph.json's mention edges.
func (m *MentionResolver) ResolveAll(raws []RawMention) []Reference {
	type key struct {
		target identity.DocumentID
		kind   MentionKind
		line   int
		text   string
		health LinkHealth
	}
	seen := make(map[key]struct{})
	var out []Reference
	for _, raw := range raws {
		ref, ok := m.Resolve(raw)
		if !ok {
			continue
		}
		k := key{target: ref.Target.DocumentID, kind: raw.Kind, line: raw.Line, text: raw.Text, health: ref.Health}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, ref)
	}
	return out
}

// Resolve classifies a single mention. ok is false when the mention is dropped:
// it names its own origin, or is a shared name one of whose candidates is the
// origin (a self-mention carries no navigational information, mirroring the
// projection's self-loop rule), or it names nothing and is not markdown-named,
// or it is an invocation that matches no target.
func (m *MentionResolver) Resolve(raw RawMention) (Reference, bool) {
	var res resolution
	switch raw.Kind {
	case MentionPath:
		res = m.resolvePath(raw.Origin, raw.Target)
	case MentionFilename:
		res = m.nearest(raw.Origin, m.basenames[raw.Target])
		if len(res.ids) == 0 {
			res.missing = raw.Target
		}
	case MentionInvocation:
		res = m.nearest(raw.Origin, m.invocationCandidates(raw.Prefix, raw.Target))
	}
	rr := RawReference{
		Origin:      raw.Origin,
		RawTarget:   raw.Target,
		Type:        Mention,
		Line:        raw.Line,
		AnchorText:  raw.Text,
		MentionKind: raw.Kind,
	}
	switch {
	case len(res.ids) == 1:
		if res.ids[0] == raw.Origin {
			return Reference{}, false
		}
		return ref(rr, ResolvedTarget{Kind: TargetDocument, DocumentID: res.ids[0]}, Valid), true
	case len(res.ids) > 1:
		if slices.Contains(res.ids, raw.Origin) {
			// A shared name that may be the origin itself most likely is.
			return Reference{}, false
		}
		// The target is the name as written; Candidates lists what it may mean.
		r := ref(rr, ResolvedTarget{Kind: TargetDocument, DocumentID: identity.DocumentID(raw.Target)}, Ambiguous)
		r.Candidates = res.ids
		return r, true
	case res.missing != "" && raw.Kind != MentionInvocation && identity.IsMarkdownPath(res.missing):
		return ref(rr, ResolvedTarget{Kind: TargetDocument, DocumentID: identity.DocumentID(res.missing)}, Broken), true
	}
	return Reference{}, false
}

// resolution is a mention's candidate documents (sorted, unique) and, when
// there are none, the cleaned in-root path it was looking for.
type resolution struct {
	ids     []identity.DocumentID
	missing string
}

// resolvePath resolves a path mention the way a link resolves (relative to the
// origin, or from the scan/content root for a single leading "/"), then
// relative to each ancestor directory of the origin, nearest first, ending at
// the repo root. Prose often writes a path relative to the enclosing project
// (`.claude/rules/x.md` in a subproject's docs) or to the repository. Every
// attempt runs through the ADR 0003 root-containment guard. When nothing
// resolves, missing is the repo-root reading of the path.
func (m *MentionResolver) resolvePath(origin identity.DocumentID, target string) resolution {
	if cleaned, ok := resolveInRoot(origin, target, m.resolver.contentRoots); ok {
		if id, ok := m.documentAt(cleaned); ok {
			return resolution{ids: []identity.DocumentID{id}}
		}
		if IsRootAbsolute(target) {
			return resolution{missing: cleaned}
		}
	}
	if IsRootAbsolute(target) {
		return resolution{}
	}
	cleaned := path.Clean(target)
	if identity.EscapesRoot(cleaned) {
		return resolution{}
	}
	for dir := path.Dir(origin.String()); ; dir = path.Dir(dir) {
		if dir == "." || dir == "/" {
			dir = ""
		}
		candidate := path.Join(dir, cleaned)
		if !identity.EscapesRoot(candidate) {
			if id, ok := m.documentAt(candidate); ok {
				return resolution{ids: []identity.DocumentID{id}}
			}
		}
		if dir == "" {
			break
		}
	}
	return resolution{missing: cleaned}
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

// invocationCandidates returns the documents a prefixed name refers to through
// the front-matter name/aliases index (the one wikilinks use), keeping only
// documents the prefix's target globs admit.
func (m *MentionResolver) invocationCandidates(prefix, name string) []identity.DocumentID {
	globs := m.globs[prefix]
	if len(globs) == 0 || name == "" {
		return nil
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
	return sortedUnique(match)
}

// nearest narrows several same-named candidates to the one closest to the
// origin, when every candidate is a project-scoped tool file: one that lives
// under a dot-directory. Its scope is the directory holding that dot-directory
// (`enterprise/app` for `enterprise/app/.claude/skills/x/SKILL.md`, the repo
// root for `.claude/skills/x/SKILL.md`). Candidates whose scope does not
// enclose the origin are discarded, and the deepest enclosing scope wins. A
// plain document among the candidates (`docs/architecture.md` beside
// `.claude/skills/x/architecture.md`) has no such scope, so nothing is picked.
// When no single candidate wins, every candidate is kept and the mention is
// Ambiguous: a shared name is never guessed at.
func (m *MentionResolver) nearest(origin identity.DocumentID, ids []identity.DocumentID) resolution {
	if len(ids) <= 1 {
		return resolution{ids: ids}
	}
	scopes := make([]string, len(ids))
	for i, id := range ids {
		scope, ok := mentionScope(id.String())
		if !ok {
			return resolution{ids: ids}
		}
		scopes[i] = scope
	}
	originDir := path.Dir(origin.String())
	best, bestDepth, tie := identity.DocumentID(""), -1, false
	for i, id := range ids {
		scope := scopes[i]
		if !encloses(scope, originDir) {
			continue
		}
		depth := 0
		if scope != "" {
			depth = strings.Count(scope, "/") + 1
		}
		switch {
		case depth > bestDepth:
			best, bestDepth, tie = id, depth, false
		case depth == bestDepth:
			tie = true
		}
	}
	if bestDepth < 0 || tie {
		return resolution{ids: ids}
	}
	return resolution{ids: []identity.DocumentID{best}}
}

// mentionScope returns the directory holding p's first dot-directory segment
// ("" is the repo root). ok is false when p has no dot-directory.
func mentionScope(p string) (string, bool) {
	segs := strings.Split(p, "/")
	for i, seg := range segs[:len(segs)-1] {
		if strings.HasPrefix(seg, ".") {
			return strings.Join(segs[:i], "/"), true
		}
	}
	return "", false
}

// encloses reports whether dir is scope or lies below it; "" is the repo root.
func encloses(scope, dir string) bool {
	if dir == "." {
		dir = ""
	}
	return scope == "" || dir == scope || strings.HasPrefix(dir, scope+"/")
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
