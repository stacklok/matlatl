package diagram

import (
	"fmt"
	"strings"

	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
)

// mermaidComponentClasses is the number of distinct component fill classes the
// Mermaid output cycles through (kept small so the legend stays readable).
const mermaidComponentClasses = 6

// Mermaid renders the document-projection reference graph as a hand-rolled
// Mermaid flowchart inside a fenced ```mermaid block (so it renders inline in
// GitHub/Obsidian). Nodes are classed by weakly-connected component; orphans and
// broken-link targets are visually distinct (red stroke). When the graph exceeds
// LargeGraphThreshold nodes it focuses on orphans/broken + their neighborhood
// and emits an explicit truncation note — no silent truncation. Every label is
// sanitized (escapeMermaidLabel) so a hostile title/path cannot break or inject
// into the diagram (ADR 0003). Output is deterministic (sorted iteration).
func Mermaid(v emit.View) []byte {
	var b strings.Builder
	b.WriteString("```mermaid\n")
	b.WriteString("flowchart LR\n")

	nodes, focused := focusSet(v)
	if focused {
		fmt.Fprintf(&b, "  %%%% NOTE: graph has %d documents (> %d); showing a focused subgraph of orphans, broken-link sources and their neighborhood.\n",
			len(v.Docs), LargeGraphThreshold)
	}

	colorIdx := componentColorIndex(v)
	cls := docClasses(v)

	// Document nodes, in sorted order, only those in the focus set.
	for _, d := range v.Docs {
		if _, ok := nodes[d.ID]; !ok {
			continue
		}
		nid := nodeIDFor(d.ID)
		label := mermaidLabel(v, d.ID)
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", nid, label)
		fmt.Fprintf(&b, "  class %s %s\n", nid, mermaidClassName(d.ID, cls, colorIdx, v))
	}

	// Broken-link placeholder target nodes (red), in sorted order. Only when the
	// source document is in the focus set.
	emittedBroken := make(map[string]struct{})
	for _, e := range v.BrokenEdges {
		if _, ok := nodes[e.Origin]; !ok {
			continue
		}
		bid := brokenNodeIDFor(e.Target)
		if _, done := emittedBroken[bid]; !done {
			fmt.Fprintf(&b, "  %s[\"%s\"]\n", bid, escapeMermaidLabel(e.Target+" (missing)"))
			fmt.Fprintf(&b, "  class %s broken\n", bid)
			emittedBroken[bid] = struct{}{}
		}
		fmt.Fprintf(&b, "  %s -.-> %s\n", nodeIDFor(e.Origin), bid)
	}

	// Projection edges, both endpoints in the focus set.
	for _, e := range projectionEdges(v) {
		_, fromOK := nodes[e.From]
		_, toOK := nodes[e.To]
		if !fromOK || !toOK {
			continue
		}
		fmt.Fprintf(&b, "  %s --> %s\n", nodeIDFor(e.From), nodeIDFor(e.To))
	}

	writeMermaidClassDefs(&b, colorIdx)
	b.WriteString("```\n")
	return []byte(b.String())
}

// MermaidHierarchy renders the folder/front-matter hierarchy tree as a Mermaid
// flowchart (the tree variant). Edges are parent → child. Labels are sanitized
// and output is deterministic (sorted roots and children).
func MermaidHierarchy(v emit.View) []byte {
	var b strings.Builder
	b.WriteString("```mermaid\n")
	b.WriteString("flowchart TB\n")
	if v.Metrics == nil || v.Metrics.Hierarchy == nil {
		b.WriteString("```\n")
		return []byte(b.String())
	}
	h := v.Metrics.Hierarchy

	// Emit one node per document, sorted.
	for _, d := range v.Docs {
		nid := nodeIDFor(d.ID)
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", nid, mermaidLabel(v, d.ID))
	}
	// Emit parent→child edges by walking from roots (deterministic order).
	seen := make(map[identity.DocumentID]bool)
	var walk func(id identity.DocumentID)
	walk = func(id identity.DocumentID) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, child := range h.Children(id) {
			fmt.Fprintf(&b, "  %s --> %s\n", nodeIDFor(id), nodeIDFor(child))
			walk(child)
		}
	}
	for _, r := range h.Roots() {
		walk(r)
	}
	b.WriteString("```\n")
	return []byte(b.String())
}

// mermaidLabel builds the (escaped) node label: the display title plus the
// path, so a node is identifiable even when titles collide.
func mermaidLabel(v emit.View, id identity.DocumentID) string {
	title := v.TitleOf(id)
	if title == id.String() {
		return escapeMermaidLabel(id.String())
	}
	return escapeMermaidLabel(title + " — " + id.String())
}

// mermaidClassName picks the class for a document node: orphan/unreachable take
// precedence over the component-color class.
func mermaidClassName(id identity.DocumentID, cls map[identity.DocumentID]docClass, colorIdx map[identity.DocumentID]int, v emit.View) string {
	switch cls[id] {
	case classOrphan:
		return "orphan"
	case classUnreachable:
		return "unreachable"
	case classNormal:
	}
	comp := v.Metrics.ComponentOf(id)
	i := colorIdx[comp] % mermaidComponentClasses
	return fmt.Sprintf("c%d", i)
}

func writeMermaidClassDefs(b *strings.Builder, colorIdx map[identity.DocumentID]int) {
	// Component fill classes, drawn from the shared palette (Mermaid caps at
	// mermaidComponentClasses so the legend stays short).
	used := mermaidComponentClasses
	if len(colorIdx) < used {
		used = len(colorIdx)
	}
	for i := 0; i < used; i++ {
		fmt.Fprintf(b, "  classDef c%d fill:%s,stroke:%s,color:#000;\n", i, componentFillPalette[i], componentStroke)
	}
	// Distinct, accessible styles for the actionable classes (shared semantic hexes).
	fmt.Fprintf(b, "  classDef orphan fill:%s,stroke:%s,stroke-width:2px,color:#000;\n", orphanFill, orphanStroke)
	fmt.Fprintf(b, "  classDef unreachable fill:%s,stroke:%s,stroke-width:2px,color:#000;\n", unreachableFill, unreachableStroke)
	fmt.Fprintf(b, "  classDef broken fill:%s,stroke:%s,stroke-width:2px,color:#000;\n", brokenFill, brokenStroke)
}

// escapeMermaidLabel sanitizes a label for a Mermaid node. Mermaid node text is
// fragile: double quotes terminate a quoted label, newlines break the line-based
// parser, square brackets and parens are shape delimiters, and `<`/`>`/`#` can
// inject HTML or directives. We neutralize all of them so a hostile
// DocumentID/title cannot break or inject into the diagram (ADR 0003). The
// transformation is lossy-but-safe (visible substitutes), which is acceptable
// for a label.
//
// We do NOT touch ';'. A semicolon is only a statement separator in Mermaid's
// unquoted syntax; every label we emit is wrapped in double quotes ("..."), and
// the '"' that would close the quoted context is itself replaced below, so a ';'
// inside the label stays inert. (The previous code mapped ';' to itself — a
// verified no-op with a misleading comment; removed.) The hostile-label test
// pins this by asserting a ';' in the title does not produce an injection.
// mermaidLabelReplacer is a package-level singleton (a strings.Replacer is
// immutable and safe to share). Promoting it out of escapeMermaidLabel avoids
// rebuilding the replacer on every call — ~5k extra allocations at 5k docs —
// matching the pattern in escape.go.
var mermaidLabelReplacer = strings.NewReplacer(
	`\`, "∖", // set-minus look-alike; a trailing '\' could escape our closing quote
	"\r\n", " ",
	"\n", " ",
	"\r", " ",
	"\t", " ",
	`"`, "'", // a literal double-quote would close the quoted label
	"#", "＃", // '#' begins an HTML entity / can inject
	"<", "‹",
	">", "›",
	"[", "⟦",
	"]", "⟧",
	"{", "⦃",
	"}", "⦄",
	"(", "❨",
	")", "❩",
	"|", "¦",
	"`", "ʼ",
)

func escapeMermaidLabel(s string) string {
	return mermaidLabelReplacer.Replace(s)
}
