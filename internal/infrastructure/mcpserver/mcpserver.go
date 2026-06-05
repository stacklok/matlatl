// Package mcpserver exposes a matlatl analysis as a read-only MCP server over
// stdio, for an agent to query a markdown corpus' link graph. It is the ONLY
// package that imports the MCP library (github.com/mark3labs/mcp-go, ADR 0002):
// the dependency is quarantined here so the core tool never depends on MCP and a
// build that does not invoke `serve` pays nothing for it at runtime (the import
// is reachable only from `matlatl serve`).
//
// The server runs the matlatl pipeline ONCE over the path at construction time
// to build the frozen analysis (corpus, graph, metrics), then serves five
// read-only tools that return the SAME structured data as the file artifacts by
// reusing the emit.View + emit/graphjson layers — nothing is reinvented:
//
//   - what-links-to(doc)    inbound references / backlinks for a document
//   - list-orphans          isolated + unreachable docs (intentional orphans
//     suppressed, per ADR 0007)
//   - path-between(a,b)     a navigational path a→b over the document projection
//   - get-section(doc#slug) section info (level, title, doc) for an anchor
//   - corpus-summary        the graph.json manifest (nodes/edges/components/HITS/…)
//
// Inputs are DocumentIDs; every tool validates them against the corpus and never
// reads outside the scan root (the pipeline already enforces root containment;
// the tools only read the in-memory frozen model).
package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
)

const (
	serverName    = "matlatl"
	serverVersion = "1"
)

// Analysis is the frozen, read-only snapshot the tools query. It is built once
// (BuildAnalysis) and never mutated, so the tool handlers are safe for
// concurrent calls.
type Analysis struct {
	view    emit.View
	metrics *graphmodel.GraphMetrics
}

// BuildAnalysis runs the matlatl pipeline over rootPath and returns the frozen
// analysis the MCP tools serve. It uses the production scanner + parser factory;
// external link checking is OFF (the MCP surface is read-only and deterministic).
func BuildAnalysis(ctx context.Context, rootPath string) (*Analysis, error) {
	cfg := application.DefaultConfig()
	cfg.RootPath = rootPath
	p := application.NewPipeline(cfg,
		fsscanner.New(fsscanner.Config{}),
		mdparser.NewFactory(mdparser.Config{}),
		nil)
	_, res, err := p.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: build analysis: %w", err)
	}
	return &Analysis{view: emit.BuildView(res), metrics: res.Metrics}, nil
}

// NewServer builds the MCP server with the read-only tools registered against
// the given analysis. It does not start any transport; call Serve (or
// server.ServeStdio) to run it.
func NewServer(a *Analysis) *server.MCPServer {
	s := server.NewMCPServer(serverName, serverVersion,
		server.WithToolCapabilities(false),
	)
	for _, t := range a.Tools() {
		s.AddTool(t.Tool, t.Handler)
	}
	return s
}

// Serve builds the analysis over rootPath and serves the MCP tools over stdio
// until the context is canceled or stdin closes. It is the entry point
// `matlatl serve` calls.
func Serve(ctx context.Context, rootPath string) error {
	a, err := BuildAnalysis(ctx, rootPath)
	if err != nil {
		return err
	}
	s := NewServer(a)
	if err := server.ServeStdio(s, server.WithStdioContextFunc(func(context.Context) context.Context {
		return ctx
	})); err != nil {
		return fmt.Errorf("mcpserver: serve stdio: %w", err)
	}
	return nil
}

// Tools returns the read-only tool set bound to this analysis. Exposed so tests
// can invoke handlers in-process without a live client or transport.
func (a *Analysis) Tools() []server.ServerTool {
	return []server.ServerTool{
		{Tool: whatLinksToTool(), Handler: a.handleWhatLinksTo},
		{Tool: listOrphansTool(), Handler: a.handleListOrphans},
		{Tool: pathBetweenTool(), Handler: a.handlePathBetween},
		{Tool: getSectionTool(), Handler: a.handleGetSection},
		{Tool: corpusSummaryTool(), Handler: a.handleCorpusSummary},
	}
}

// --- tool schemas ---

func whatLinksToTool() mcp.Tool {
	return mcp.NewTool("what-links-to",
		mcp.WithDescription("List the documents that link TO the given document (inbound references / backlinks), over the navigational document projection."),
		mcp.WithString("doc", mcp.Required(),
			mcp.Description("DocumentID (canonical repo-relative path, e.g. docs/guide.md) to find backlinks for.")),
	)
}

func listOrphansTool() mcp.Tool {
	return mcp.NewTool("list-orphans",
		mcp.WithDescription("List orphan documents: isolated (no inbound or outbound navigational links) and unreachable (not reachable from the root set). Intentional orphans are suppressed."),
	)
}

func pathBetweenTool() mcp.Tool {
	return mcp.NewTool("path-between",
		mcp.WithDescription("Return a navigational path (sequence of DocumentIDs) from document A to document B over the document projection, or report that none exists."),
		mcp.WithString("from", mcp.Required(), mcp.Description("Source DocumentID.")),
		mcp.WithString("to", mcp.Required(), mcp.Description("Target DocumentID.")),
	)
}

func getSectionTool() mcp.Tool {
	return mcp.NewTool("get-section",
		mcp.WithDescription("Return section info (document, slug, level, title) for a 'doc#slug' anchor, e.g. docs/guide.md#installation."),
		mcp.WithString("ref", mcp.Required(),
			mcp.Description("A 'DocumentID#slug' reference identifying a heading-scoped section.")),
	)
}

func corpusSummaryTool() mcp.Tool {
	return mcp.NewTool("corpus-summary",
		mcp.WithDescription("Return the full graph.json manifest of the corpus: nodes, edges, sections, components, HITS hub/authority rankings, orphans, unreachable, broken links and knowledge gaps."),
	)
}

// --- tool handlers ---

// docID validates an input string against the corpus and returns the typed
// DocumentID. It rejects unknown IDs (never reads outside the corpus model).
func (a *Analysis) docID(s string) (identity.DocumentID, bool) {
	id := identity.DocumentID(s)
	if _, ok := a.view.Doc(id); ok {
		return id, true
	}
	return "", false
}

func (a *Analysis) handleWhatLinksTo(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	doc, err := req.RequireString("doc")
	if err != nil {
		return mcp.NewToolResultError("missing required argument 'doc'"), nil
	}
	id, ok := a.docID(doc)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("unknown document %q", doc)), nil
	}
	var inbound []string
	if a.metrics != nil && a.metrics.Graph != nil {
		for _, src := range a.metrics.Graph.ProjectionIn(id) { // sorted
			inbound = append(inbound, src.String())
		}
	}
	if inbound == nil {
		inbound = []string{}
	}
	return mcp.NewToolResultStructured(map[string]any{
		"document":  id.String(),
		"backlinks": inbound,
		"count":     len(inbound),
	}, fmt.Sprintf("%d documents link to %s", len(inbound), id)), nil
}

func (a *Analysis) handleListOrphans(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	orphans := identity.IDStrings(a.view.Orphans)
	unreachable := identity.IDStrings(a.view.Unreachable)
	return mcp.NewToolResultStructured(map[string]any{
		"isolated":                  orphans,
		"unreachable":               unreachable,
		"reachabilityIndeterminate": a.view.ReachabilityIndeterminate,
	}, fmt.Sprintf("%d isolated orphan(s), %d unreachable document(s)", len(orphans), len(unreachable))), nil
}

func (a *Analysis) handlePathBetween(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	from, ferr := req.RequireString("from")
	to, terr := req.RequireString("to")
	if ferr != nil || terr != nil {
		return mcp.NewToolResultError("both 'from' and 'to' are required"), nil
	}
	src, ok1 := a.docID(from)
	dst, ok2 := a.docID(to)
	if !ok1 {
		return mcp.NewToolResultError(fmt.Sprintf("unknown document %q", from)), nil
	}
	if !ok2 {
		return mcp.NewToolResultError(fmt.Sprintf("unknown document %q", to)), nil
	}
	path, found := a.shortestPath(src, dst)
	pathStrs := make([]string, 0, len(path))
	for _, id := range path {
		pathStrs = append(pathStrs, id.String())
	}
	msg := fmt.Sprintf("no navigational path from %s to %s", src, dst)
	if found {
		msg = fmt.Sprintf("path of %d hop(s) from %s to %s", len(path)-1, src, dst)
	}
	return mcp.NewToolResultStructured(map[string]any{
		"from":  src.String(),
		"to":    dst.String(),
		"found": found,
		"path":  pathStrs,
	}, msg), nil
}

func (a *Analysis) handleGetSection(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ref, err := req.RequireString("ref")
	if err != nil {
		return mcp.NewToolResultError("missing required argument 'ref'"), nil
	}
	docPart, slug, ok := splitAnchor(ref)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("expected a 'DocumentID#slug' reference, got %q", ref)), nil
	}
	id, known := a.docID(docPart)
	if !known {
		return mcp.NewToolResultError(fmt.Sprintf("unknown document %q", docPart)), nil
	}
	doc, ok := a.view.Document(id)
	if !ok || doc.Root == nil {
		return mcp.NewToolResultError(fmt.Sprintf("document %q has no sections", id)), nil
	}
	sec := findSection(doc.Root, slug)
	if sec == nil {
		return mcp.NewToolResultError(fmt.Sprintf("no section with slug %q in %q", slug, id)), nil
	}
	return mcp.NewToolResultStructured(map[string]any{
		"document": id.String(),
		"slug":     sec.Slug,
		"level":    sec.Level,
		"title":    sec.Text,
		"nodeId":   graphmodel.NodeIDForSection(id, sec.Slug).String(),
	}, fmt.Sprintf("%s#%s (h%d): %s", id, sec.Slug, sec.Level, sec.Text)), nil
}

func (a *Analysis) handleCorpusSummary(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	manifest := graphjson.Build(a.view)
	return mcp.NewToolResultStructured(manifest,
		fmt.Sprintf("corpus: %d documents, %d edges, %d components",
			manifest.Summary.Documents, manifest.Summary.Edges, manifest.Summary.Components)), nil
}
