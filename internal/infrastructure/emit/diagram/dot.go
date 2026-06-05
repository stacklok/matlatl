package diagram

import (
	"fmt"
	"math"
	"strings"

	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
)

// DOT-library decision (ADR 0002 allowed github.com/dominikbraun/graph's
// draw.DOT for the P4 DOT path): we HAND-ROLL DOT instead. Rationale:
//   - draw.DOT renders labels from the graph's vertex attributes and does not
//     give us a single choke-point to enforce the ADR 0003 hostile-label
//     escaping contract; we would have to pre-escape and still trust its writer.
//   - We need vertex size ∝ in-degree, fill color ∝ component, and red broken-
//     target placeholder nodes — all custom attributes that are simpler to emit
//     directly than to coax out of a generic drawer.
//   - DOT's grammar is tiny; hand-rolling is ~80 lines, keeps the dependency out
//     of the build, and gives total control over deterministic ordering.
// The domain stays free of any graph library either way (ADR 0004 / 0007).

// dotComponentColors is a deterministic fill palette indexed by component.
var dotComponentColors = []string{
	"#e3f2fd", "#f1f8e9", "#fff3e0", "#f3e5f5", "#e0f7fa", "#fce4ec",
	"#ede7f6", "#e8f5e9",
}

// DOT renders the document-projection reference graph as Graphviz DOT. Vertex
// size scales with in-degree, fill color is the document's weakly-connected
// component, and unresolved broken-link targets are red placeholder nodes. Every
// label is escaped per DOT syntax (escapeDOTString) so a hostile title/path
// cannot break or inject into the output (ADR 0003). Output is deterministic
// (sorted iteration). It is a complete, balanced-brace DOT digraph.
func DOT(v emit.View) []byte {
	var b strings.Builder
	b.WriteString("digraph doctopus {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box, style=\"rounded,filled\", fontname=\"Helvetica\"];\n")

	nodes, focused := focusSet(v)
	if focused {
		fmt.Fprintf(&b, "  // NOTE: graph has %d documents (> %d); showing a focused subgraph of orphans, broken-link sources and their neighborhood.\n",
			len(v.Docs), LargeGraphThreshold)
	}

	colorIdx := componentColorIndex(v)
	cls := docClasses(v)

	// Document nodes.
	for _, d := range v.Docs {
		if _, ok := nodes[d.ID]; !ok {
			continue
		}
		attrs := dotNodeAttrs(v, d, cls, colorIdx)
		fmt.Fprintf(&b, "  %q [%s];\n", nodeIDFor(d.ID), attrs)
	}

	// Broken-link placeholder target nodes (red).
	emitted := make(map[string]struct{})
	for _, e := range v.BrokenEdges {
		if _, ok := nodes[e.Origin]; !ok {
			continue
		}
		bid := brokenNodeIDFor(e.Target)
		if _, done := emitted[bid]; !done {
			fmt.Fprintf(&b, "  %q [label=\"%s\", fillcolor=\"#ffcdd2\", color=\"#b71c1c\", penwidth=2];\n",
				bid, escapeDOTString(e.Target+" (missing)"))
			emitted[bid] = struct{}{}
		}
		fmt.Fprintf(&b, "  %q -> %q [style=dashed, color=\"#b71c1c\"];\n", nodeIDFor(e.Origin), bid)
	}

	// Projection edges.
	for _, e := range projectionEdges(v) {
		_, fromOK := nodes[e.From]
		_, toOK := nodes[e.To]
		if !fromOK || !toOK {
			continue
		}
		fmt.Fprintf(&b, "  %q -> %q;\n", nodeIDFor(e.From), nodeIDFor(e.To))
	}

	b.WriteString("}\n")
	return []byte(b.String())
}

// dotNodeAttrs builds the attribute list for a document node: escaped label,
// fill color by component (or red/amber for orphan/unreachable), and width that
// scales with in-degree.
func dotNodeAttrs(v emit.View, d emit.DocView, cls map[identity.DocumentID]docClass, colorIdx map[identity.DocumentID]int) string {
	label := v.TitleOf(d.ID)
	if label != d.ID.String() {
		label = label + "\n" + d.ID.String()
	}
	fill := dotComponentColors[colorIdx[d.Component]%len(dotComponentColors)]
	stroke := "#90a4ae"
	penwidth := 1.0
	switch cls[d.ID] {
	case classOrphan:
		fill, stroke, penwidth = "#ffebee", "#c62828", 2
	case classUnreachable:
		fill, stroke, penwidth = "#fff8e1", "#ff8f00", 2
	case classNormal:
	}
	// Width scales gently with in-degree so hubs/authorities are visually larger
	// without exploding on a high-degree node.
	width := 0.9 + 0.25*math.Log1p(float64(d.InDegree))
	// escapeDOTString already produces a DOT quoted-string body (it escapes " and
	// \), so we wrap it in literal quotes rather than %q (which would double-escape).
	return fmt.Sprintf("label=\"%s\", fillcolor=\"%s\", color=\"%s\", penwidth=%g, width=%.3f",
		escapeDOTString(label), fill, stroke, penwidth, width)
}

// escapeDOTString escapes a string for a DOT double-quoted ID/label. Per the
// DOT grammar, only the double-quote and backslash are special inside a quoted
// string; a literal "\n" is interpreted as a line break (which we want for the
// title/path split). We therefore escape backslashes and double-quotes, and
// convert raw control characters (CR, embedded NUL, etc.) to spaces so a hostile
// title cannot break the token or inject attributes (ADR 0003).
func escapeDOTString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`) // explicit line break in the rendered label
		case '\r', '\t', 0x00:
			b.WriteByte(' ')
		default:
			if r < 0x20 {
				b.WriteByte(' ')
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}
