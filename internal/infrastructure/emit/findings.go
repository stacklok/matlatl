// Package emit renders analysis results into machine- and CI-consumable
// artifacts (findings.json, JUnit XML) and writes them safely under an output
// directory. It is infrastructure: it may import the domain and third-party
// libraries, but the domain never imports it (ADR 0004). All emitted bytes are
// deterministic (the AnalysisReport is pre-sorted) so artifacts are byte-stable.
package emit

import (
	"encoding/json"
	"fmt"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/analysis"
)

// Artifact filenames (stable; CI integrations key on these).
const (
	FindingsJSONName = "findings.json"
	JUnitXMLName     = "junit.xml"
)

// FindingsSchemaVersion is the findings.json schema version. Adding optional
// fields is backward-compatible; renaming/removing a field or a Details key is a
// breaking change that bumps this. v2 added per-finding "details"
// (structured, machine-actionable context) and a top-level "remediationGuide"
// mapping each finding kind to a standalone how-to, so every finding is
// self-contained for an agent. v3 (ADR 0012) adds the graduated structure
// findings "under-linked" and "dead-end" (new kind values + summary counts).
// v4 (ADR 0013) adds the topology-based "suggested-link" finding (a new kind
// value + a suggestedLink summary count).
// v5 (ADR 0015) adds the critical-path "articulation-point" and "bridge"
// findings (two new kind values + articulationPoint/bridge summary counts).
// v6 (ADR 0016) adds the information-scent "low-scent-anchor" finding (a new
// kind value + a lowScentAnchor summary count).
// v7 (ADR 0021) adds the hops-from-root "far-from-root" finding (a new kind
// value + a farFromRoot summary count).
// v8 (ADR 0023) adds the OKF v0.1 conformance-mode findings
// "okf-missing-frontmatter", "okf-missing-type", "okf-reserved-file-structure"
// (three new kind values + three summary counts) and the always-present
// top-level "okfConformance" object (checked:false when the mode is off).
const FindingsSchemaVersion = 8

// findingsDocument is the stable findings.json schema. Adding fields is
// backward-compatible; renaming/removing is a breaking change and must bump
// FindingsSchemaVersion.
type findingsDocument struct {
	SchemaVersion int         `json:"schemaVersion"`
	Tool          string      `json:"tool"`
	Summary       findingsSum `json:"summary"`
	// RemediationGuide maps each finding kind that appears in this report to a
	// one-paragraph, standalone how-to, so an agent can act on a finding using
	// only this document. Keyed by the finding kind string (e.g. "broken-link").
	RemediationGuide map[string]string `json:"remediationGuide"`
	// OKFConformance is the OKF v0.1 conformance verdict (ADR 0023). It is ALWAYS
	// present: when the mode is off, Checked is false and the rest are zero values.
	OKFConformance okfConformanceJSON `json:"okfConformance"`
	Findings       []findingJSON      `json:"findings"`
}

// okfConformanceJSON is the top-level OKF verdict block. Always emitted so the
// shape is stable regardless of mode; consumers key on Checked first.
type okfConformanceJSON struct {
	Checked               bool   `json:"checked"`
	Conformant            bool   `json:"conformant"`
	Version               string `json:"version"`
	MissingFrontmatter    int    `json:"missingFrontmatter"`
	MissingType           int    `json:"missingType"`
	ReservedFileStructure int    `json:"reservedFileStructure"`
}

// OKFVerdict carries the OKF conformance verdict from the pipeline Result to the
// findings.json emitter (ADR 0023). The zero value (Checked:false) is the
// mode-off shape. Use OKFVerdictFromResult to build one from an application.Result.
type OKFVerdict struct {
	Checked               bool
	Conformant            bool
	Version               string
	MissingFrontmatter    int
	MissingType           int
	ReservedFileStructure int
}

// Line renders the one-line OKF conformance verdict (ADR 0023). It is the single
// home for the verdict wording, shared by the `check` summary
// (cmd/matlatl via OKFVerdictFromResult) and the human reports (terminal +
// markdown via View.OKF), so the two are byte-identical by construction. It is
// only meaningful when Checked is true; callers gate on that.
func (v OKFVerdict) Line() string {
	if v.Conformant {
		return "OKF v0.1: CONFORMANT"
	}
	total := v.MissingFrontmatter + v.MissingType + v.ReservedFileStructure
	return fmt.Sprintf(
		"OKF v0.1: NOT CONFORMANT — %d violation(s) (%d missing-frontmatter, %d missing-type, %d reserved-file)",
		total, v.MissingFrontmatter, v.MissingType, v.ReservedFileStructure)
}

// OKFVerdictFromResult projects the OKF fields of a frozen pipeline Result into
// an OKFVerdict for the findings emitter and the verdict Line. When OKF mode was
// off, the result is the zero verdict (Checked:false), which renders
// okfConformance.checked=false.
func OKFVerdictFromResult(res application.Result) OKFVerdict {
	return OKFVerdict{
		Checked:               res.OKFMode,
		Conformant:            res.OKFConformant,
		Version:               res.OKFVersion,
		MissingFrontmatter:    res.OKFMissingFrontmatterCount,
		MissingType:           res.OKFMissingTypeCount,
		ReservedFileStructure: res.OKFReservedFileStructureCount,
	}
}

type findingsSum struct {
	Total             int `json:"total"`
	BrokenLink        int `json:"brokenLink"`
	BrokenAnchor      int `json:"brokenAnchor"`
	Ambiguous         int `json:"ambiguous"`
	Orphan            int `json:"orphan"`
	Unreachable       int `json:"unreachable"`
	KnowledgeGap      int `json:"knowledgeGap"`
	UnderLinked       int `json:"underLinked"`
	DeadEnd           int `json:"deadEnd"`
	SuggestedLink     int `json:"suggestedLink"`
	ArticulationPoint int `json:"articulationPoint"`
	Bridge            int `json:"bridge"`
	LowScentAnchor    int `json:"lowScentAnchor"`
	FarFromRoot       int `json:"farFromRoot"`
	// OKF conformance-mode counts (ADR 0023). Zero when the mode is off.
	OKFMissingFrontmatter    int `json:"okfMissingFrontmatter"`
	OKFMissingType           int `json:"okfMissingType"`
	OKFReservedFileStructure int `json:"okfReservedFileStructure"`
}

type findingJSON struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Severity     string `json:"severity"`
	Document     string `json:"document"`
	Line         int    `json:"line"`
	Message      string `json:"message"`
	SuggestedFix string `json:"suggestedFix,omitempty"`
	// Details is the finding's structured, machine-actionable context (e.g. the
	// ambiguous candidates, the expected anchor slug). Omitted when empty. The
	// map is rendered with sorted keys (encoding/json sorts map keys), so output
	// is deterministic.
	Details map[string]string `json:"details,omitempty"`
}

// remediationByKind is the standalone how-to for each finding kind. It is keyed
// by the analysis.FindingKind.String() value and is the single source of truth
// for the findings.json remediationGuide. Every kind a finding can carry MUST
// have an entry (asserted in tests) so the guide always covers the report.
var remediationByKind = map[string]string{
	analysis.BrokenLink.String(): "The reference points at a path that does not resolve to any document in the corpus. " +
		"Open the finding's `document` at `line`, then either correct the link target to a real, " +
		"existing document path (the `details.target` field holds the path as written, relative to the " +
		"origin document), move/create the intended target file, or remove the dead link.",
	analysis.BrokenAnchor.String(): "The target document exists but has no heading whose slug matches the fragment. " +
		"Either add a heading to `details.targetDocument` that slugifies to `details.expectedSlug` " +
		"(slugs are GitHub-style: lowercase, spaces→dashes, punctuation dropped), or change the link's " +
		"`#fragment` to an existing heading slug in that document.",
	analysis.Ambiguous.String(): "The target matches more than one document, so the link is non-deterministic. " +
		"Replace it with one of the unique paths in `details.candidates` (newline-separated): pick the " +
		"intended document and use a path long enough to be unambiguous.",
	analysis.Orphan.String(): "The document is isolated: nothing links to it and it links to nothing, so no reader " +
		"or agent can navigate to it. Add an inbound link from a relevant page (an index or a related doc) " +
		"and outbound links to its neighbors, or delete it if obsolete. To keep it intentionally unlinked, " +
		"add front matter `matlatl: orphan-intentional`.",
	analysis.Unreachable.String(): "The document cannot be reached by following links from any root (README.md/index.md " +
		"or a `type: index` doc). Add an inbound link from a page that is itself reachable from a root. " +
		"To keep it intentionally unlinked, add front matter `matlatl: orphan-intentional`.",
	analysis.KnowledgeGap.String(): "Two clusters of documentation (`details.componentA` and `details.componentB`) have no " +
		"navigational links between them. This is an experimental heuristic, not an error. If the two areas " +
		"are related, add a link between `details.representativeA` and `details.representativeB` to connect them.",
	analysis.UnderLinked.String(): "The document has fewer inbound navigational links than the discoverability threshold " +
		"(`details.inboundCount` holds the actual count), so readers and agents are unlikely to find it. " +
		"Add inbound links from related, more-connected pages (an index or topic hub is ideal). To keep it " +
		"intentionally sparse, add front matter `matlatl: orphan-intentional`.",
	analysis.DeadEnd.String(): "The document has inbound links but links to nothing onward, so navigation stops there. " +
		"Add onward internal links from it to related documents so readers and agents can continue. To keep " +
		"it intentionally terminal, add front matter `matlatl: orphan-intentional`.",
	analysis.SuggestedLink.String(): "Two documents (`details.targetDocument` and `details.suggestedTarget`) share " +
		"`details.sharedNeighbours` navigational neighbour(s) but do not link to each other. This is an " +
		"experimental, topology-based discoverability hint (Adamic/Adar score `details.adamicAdar`), not an " +
		"error. If the two documents are related, add a navigational link between them; otherwise ignore it.",
	analysis.DeadLink.String(): "An external (http/https) link failed an opt-in liveness check (--check-external): it was " +
		"unreachable, returned an error status (`details.statusCode`), or was refused by the SSRF guard " +
		"(`details.blocked`). Verify the URL is correct and reachable; if it moved, update it, otherwise " +
		"remove or replace the link. Dead-link findings appear only under --check-external.",
	analysis.ArticulationPoint.String(): "The document is an articulation point (cut vertex) of the link graph: it is the " +
		"only connector between two parts of the corpus, so if it is removed or unlinked the corpus fragments " +
		"(`details.betweenness` holds its betweenness centrality). This is an experimental, topology-based " +
		"resilience hint, not an error. Add a redundant link path between the parts it joins so it is no longer " +
		"a single point of failure, or treat it as deliberately load-bearing.",
	analysis.Bridge.String(): "The link between `details.targetDocument` and `details.bridgeEndpoint` is a bridge (cut " +
		"edge): it is the only connection between two parts of the corpus, so losing it disconnects them. This " +
		"is an experimental, topology-based resilience hint, not an error. Add another navigational path between " +
		"these two clusters so the single link is not a single point of failure.",
	analysis.LowScentAnchor.String(): "The link's anchor text (`details.anchorText`) shares too few meaningful words with the " +
		"destination's title or section headings to preview where it leads (Jaccard `details.scentScore`); generic labels like " +
		"\"click here\" or \"read more\" give a reader or agent weak information scent (Pirolli & Card 1999). This " +
		"is an experimental discoverability hint, not an error. Rename the anchor in `details.sourceDocument` at " +
		"`line` to describe the destination — `details.suggestedAnchor` holds the destination's title or " +
		"best-matching heading as a starting point.",
	analysis.OKFMissingFrontmatter.String(): "OKF v0.1 conformance (rule R1): every non-reserved `.md` concept document must contain a " +
		"PARSEABLE YAML frontmatter block. `details.targetDocument` is the file; `details.frontmatterState` is " +
		"\"absent\" (no block) or \"unparseable\" (a block that failed to decode or exceeded the size cap). Add " +
		"a `---`-delimited YAML block at the top of the file — or fix its syntax — and give it at least a " +
		"non-empty `type:` field. Reserved files (index.md, log.md) are exempt from this rule. Reported only in " +
		"OKF mode (--okf).",
	analysis.OKFMissingType.String(): "OKF v0.1 conformance (rule R2): every concept document's frontmatter must carry a non-empty " +
		"`type` field. `details.targetDocument` is the file and `details.reason` states what was wrong (absent, " +
		"empty, or a non-string value). Add a `type:` string; OKF does not restrict the vocabulary (consumers " +
		"must tolerate any value), so use a short descriptive type such as `Reference`, `Playbook`, or " +
		"`API Endpoint`. matlatl never validates the type value against a list. Reported only in OKF mode (--okf).",
	analysis.OKFReservedFileStructure.String(): "OKF v0.1 conformance (rule R3): reserved files must follow their structure. A `log.md`'s `##` " +
		"headings must be ISO 8601 `YYYY-MM-DD` dates (OKF §7); a non-root `index.md` must carry no " +
		"frontmatter (OKF §6); and the bundle-root `index.md` may carry only an `okf_version` key (OKF §11). " +
		"`details.reservedFile` is the reserved file (`index.md` / `log.md`), `details.reason` states which rule " +
		"was broken, and `details.okfVersion` (when present) is the bundle's declared version. Reported only in " +
		"OKF mode (--okf).",
	analysis.FarFromRoot.String(): "The document is reachable from a root but far from every entry point — `details.hopsFromRoot` " +
		"holds its shortest hop distance from the nearest root. A reader or agent following links from an entry " +
		"point (README.md/index.md) is unlikely to reach it that deep, so it is effectively undiscoverable even " +
		"though it is not orphaned. This is an experimental discoverability hint, not an error. Add an inbound link " +
		"to it from a document closer to a root (an index or hub is ideal) so it is fewer hops away. To keep it " +
		"intentionally deep, add front matter `matlatl: orphan-intentional`.",
}

// kindPresentationOrder is the single source of truth for the order finding
// kinds are presented in across the emit package: the findings.json
// remediationGuide (remediationGuideFor) and the fix-prompt "How to fix each
// kind" section (kindsPresent) both iterate it, so a newly-added FindingKind
// cannot slip past one and be ordered inconsistently in the other.
var kindPresentationOrder = []analysis.FindingKind{
	analysis.OKFMissingFrontmatter, analysis.OKFMissingType, analysis.OKFReservedFileStructure,
	analysis.BrokenLink, analysis.BrokenAnchor, analysis.Ambiguous,
	analysis.Orphan, analysis.Unreachable, analysis.UnderLinked, analysis.DeadEnd,
	analysis.FarFromRoot,
	analysis.KnowledgeGap, analysis.SuggestedLink, analysis.DeadLink,
	analysis.ArticulationPoint, analysis.Bridge, analysis.LowScentAnchor,
}

// remediationGuideFor returns the remediation entries for exactly the kinds
// present in the report, so the guide is scoped to what was emitted. A clean
// report yields an empty (but non-nil) guide.
func remediationGuideFor(report *analysis.AnalysisReport) map[string]string {
	guide := make(map[string]string)
	for _, k := range kindPresentationOrder {
		if report.CountByKind(k) > 0 {
			guide[k.String()] = remediationByKind[k.String()]
		}
	}
	return guide
}

// FindingsJSON renders an AnalysisReport as the canonical findings.json bytes
// (pretty-printed, trailing newline). The report's findings are already sorted,
// so output is deterministic.
//
// The okf verdict is rendered into the always-present top-level okfConformance
// object (ADR 0023). Only the NON-DERIVABLE parts of the verdict are read from
// the parameter — Checked and Version; the three counts AND the conformant bit
// are DERIVED from the report's own okf-* finding counts, so findings.json can
// never emit an okfConformance inconsistent with its findings list (a hand-built
// verdict cannot lie about the counts). Pass the zero OKFVerdict (or
// OKFVerdictFromResult of a non-OKF run) for the mode-off shape (checked:false).
func FindingsJSON(report *analysis.AnalysisReport, okf OKFVerdict) ([]byte, error) {
	okfMF := report.CountByKind(analysis.OKFMissingFrontmatter)
	okfMT := report.CountByKind(analysis.OKFMissingType)
	okfRS := report.CountByKind(analysis.OKFReservedFileStructure)
	doc := findingsDocument{
		SchemaVersion:    FindingsSchemaVersion,
		Tool:             "matlatl",
		RemediationGuide: remediationGuideFor(report),
		OKFConformance: okfConformanceJSON{
			Checked: okf.Checked,
			// Conformant is derived, not taken from the verdict: in OKF mode the
			// bundle is conformant iff no okf-* finding was produced. Off-mode → false.
			Conformant:            okf.Checked && okfMF+okfMT+okfRS == 0,
			Version:               okf.Version,
			MissingFrontmatter:    okfMF,
			MissingType:           okfMT,
			ReservedFileStructure: okfRS,
		},
		Summary: findingsSum{
			Total:             report.Len(),
			BrokenLink:        report.CountByKind(analysis.BrokenLink),
			BrokenAnchor:      report.CountByKind(analysis.BrokenAnchor),
			Ambiguous:         report.CountByKind(analysis.Ambiguous),
			Orphan:            report.CountByKind(analysis.Orphan),
			Unreachable:       report.CountByKind(analysis.Unreachable),
			KnowledgeGap:      report.CountByKind(analysis.KnowledgeGap),
			UnderLinked:       report.CountByKind(analysis.UnderLinked),
			DeadEnd:           report.CountByKind(analysis.DeadEnd),
			SuggestedLink:     report.CountByKind(analysis.SuggestedLink),
			ArticulationPoint: report.CountByKind(analysis.ArticulationPoint),
			Bridge:            report.CountByKind(analysis.Bridge),
			LowScentAnchor:    report.CountByKind(analysis.LowScentAnchor),
			FarFromRoot:       report.CountByKind(analysis.FarFromRoot),

			OKFMissingFrontmatter:    okfMF,
			OKFMissingType:           okfMT,
			OKFReservedFileStructure: okfRS,
		},
		Findings: make([]findingJSON, 0, report.Len()),
	}
	for _, f := range report.Findings() {
		doc.Findings = append(doc.Findings, findingJSON{
			ID:           f.ID,
			Kind:         f.Kind.String(),
			Severity:     f.Severity.String(),
			Document:     f.Location.Document.String(),
			Line:         f.Location.Line,
			Message:      f.Message,
			SuggestedFix: f.SuggestedFix,
			Details:      f.Details,
		})
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("emit: marshal findings.json: %w", err)
	}
	return append(b, '\n'), nil
}
