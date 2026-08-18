// Package mcpserver exposes a matlatl analysis as a read-only MCP server over
// streamable HTTP, for an agent to query a markdown corpus' link graph. It is
// the ONLY package that imports the MCP library (github.com/mark3labs/mcp-go, ADR 0002):
// the dependency is quarantined here so the core tool never depends on MCP and a
// build that does not invoke `serve` pays nothing for it at runtime (the import
// is reachable only from `matlatl serve`).
//
// The server runs the matlatl pipeline ONCE over the path at construction time
// to build the frozen analysis (corpus, graph, metrics), then serves seven
// read-only tools that return the SAME structured data as the file artifacts by
// reusing the emit.View + emit/graphjson layers — nothing is reinvented:
//
//   - what-links-to(doc)    inbound references / backlinks for a document
//   - list-orphans          isolated + unreachable docs (intentional orphans
//     suppressed, per ADR 0007)
//   - path-between(a,b)     a navigational path a→b over the document projection
//   - get-section(doc#slug) section info (level, title, doc) for an anchor
//   - corpus-summary        the graph.json manifest (nodes/edges/components/HITS/…)
//   - suggest-links([doc])  topology-based suggested links (ADR 0013): unlinked
//     but structurally-close pairs, doc-scoped or global top-N
//   - critical-docs         critical-path structure (ADR 0015): top load-bearing
//     docs by betweenness centrality + articulation points + bridges
//
// Inputs are DocumentIDs; every tool validates them against the corpus and never
// reads outside the scan root (the pipeline already enforces root containment;
// the tools only read the in-memory frozen model).
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

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

	// EndpointPath is the HTTP path the streamable-HTTP MCP endpoint is served
	// on. It matches mcp-go's default and the convention MCP clients expect.
	EndpointPath = "/mcp"

	// shutdownTimeout bounds the graceful drain of in-flight requests when the
	// serving context is canceled (e.g. SIGINT).
	shutdownTimeout = 5 * time.Second
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
// the given analysis. It does not start any transport; call Serve (or wrap it
// in a server.StreamableHTTPServer) to run it.
func NewServer(a *Analysis) *server.MCPServer {
	s := server.NewMCPServer(serverName, serverVersion,
		server.WithToolCapabilities(false),
	)
	for _, t := range a.Tools() {
		s.AddTool(t.Tool, t.Handler)
	}
	return s
}

func handler(ctx context.Context, rootPath string) (http.Handler, error) {
	a, err := BuildAnalysis(ctx, rootPath)
	if err != nil {
		return nil, err
	}
	stream := server.NewStreamableHTTPServer(NewServer(a),
		server.WithEndpointPath(EndpointPath),
	)
	mux := http.NewServeMux()
	mux.Handle(EndpointPath, stream)
	return mux, nil
}

// ServeListener serves MCP on an already-owned listener until ctx is canceled.
// Ownership of listener transfers to this function.
func ServeListener(ctx context.Context, rootPath string, listener net.Listener) error {
	httpHandler, err := handler(ctx, rootPath)
	if err != nil {
		_ = listener.Close()
		return err
	}
	httpSrv := &http.Server{Handler: httpHandler, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpSrv.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			_ = httpSrv.Close()
			<-errCh
			return fmt.Errorf("mcpserver: graceful shutdown: %w", err)
		}
		serveErr := <-errCh
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("mcpserver: serve streamable http on %s: %w", listener.Addr(), serveErr)
		}
		return nil
	case serveErr := <-errCh:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("mcpserver: serve streamable http on %s: %w", listener.Addr(), serveErr)
		}
		return nil
	}
}

// Serve builds the analysis over rootPath and serves the MCP tools over
// streamable HTTP on addr (host:port), at the EndpointPath ("/mcp"), until the
// context is canceled. On cancellation it drains in-flight requests within
// shutdownTimeout. It is the entry point `matlatl serve` calls.
func Serve(ctx context.Context, rootPath, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("mcpserver: listen on %s: %w", addr, err)
	}
	return ServeListener(ctx, rootPath, listener)
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
		{Tool: suggestLinksTool(), Handler: a.handleSuggestLinks},
		{Tool: criticalDocsTool(), Handler: a.handleCriticalDocs},
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
		mcp.WithDescription("List structurally weak documents: isolated (no inbound or outbound navigational links), unreachable (not reachable from the root set), under-linked (fewer inbound links than the discoverability threshold), dead-end (inbound links but nothing onward), and far-from-root (reachable but at or beyond the hop-distance threshold from every root, so hard to discover by link traversal). Intentional orphans are suppressed."),
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
		mcp.WithDescription("Return the full graph.json manifest of the corpus: nodes (each with per-node hopsFromRoot, the shortest hop distance from the nearest root, or -1 if unreachable or when reachability.indeterminate is true), edges, sections, components, HITS hub/authority rankings, betweenness centrality, PageRank, articulation points, bridges, orphans, unreachable, far-from-root (docs beyond the hop-distance threshold), broken links, knowledge gaps, topology-based suggested links, and summary navigability scalars (compactness, stratum, characteristic/median path length, clustering coefficient, diameter)."),
	)
}

// criticalDocsTopN bounds the number of load-bearing documents the critical-docs
// tool returns, so an agent gets the highest-betweenness connectors without the
// full ranking.
const criticalDocsTopN = 10

func criticalDocsTool() mcp.Tool {
	return mcp.NewTool("critical-docs",
		mcp.WithDescription("Return the corpus' critical-path structure (experimental, ADR 0015): the top load-bearing documents by betweenness centrality (most shortest paths flow through them), the articulation points (documents whose removal fragments the corpus), and the bridges (links that are the only connection between two clusters). These are the single points of failure in the link graph."),
	)
}

// suggestLinksGlobalTopN bounds the number of suggestions the global (no-doc)
// suggest-links view returns, so an agent gets the highest-signal candidates
// without the full capped list.
const suggestLinksGlobalTopN = 10

func suggestLinksTool() mcp.Tool {
	return mcp.NewTool("suggest-links",
		mcp.WithDescription("Return topology-based suggested links (experimental, ADR 0013): pairs of documents that share navigational neighbours but do not link to each other, ranked by Adamic/Adar. With 'doc', returns the suggested partners for that document; without it, returns the global top suggestions."),
		mcp.WithString("doc",
			mcp.Description("Optional DocumentID to scope suggestions to (its suggested link partners). Omit for the global top-N.")),
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
	underLinked := identity.IDStrings(a.view.UnderLinked)
	deadEnd := identity.IDStrings(a.view.DeadEnd)
	farFromRoot := identity.IDStrings(a.view.FarFromRoot)
	return mcp.NewToolResultStructured(map[string]any{
		"isolated":                  orphans,
		"unreachable":               unreachable,
		"underLinked":               underLinked,
		"deadEnd":                   deadEnd,
		"farFromRoot":               farFromRoot,
		"reachabilityIndeterminate": a.view.ReachabilityIndeterminate,
	}, fmt.Sprintf("%d isolated orphan(s), %d unreachable, %d under-linked, %d dead-end, %d far-from-root document(s)",
		len(orphans), len(unreachable), len(underLinked), len(deadEnd), len(farFromRoot))), nil
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

// suggestionPayload is the wire shape of one suggested link returned by
// suggest-links. It mirrors the graph.json SuggestedLink fields.
type suggestionPayload struct {
	DocA             string  `json:"docA"`
	DocB             string  `json:"docB"`
	SharedNeighbours int     `json:"sharedNeighbours"`
	Coupling         int     `json:"coupling"`
	CoCitation       int     `json:"coCitation"`
	AdamicAdar       float64 `json:"adamicAdar"`
}

func (a *Analysis) handleSuggestLinks(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	doc := req.GetString("doc", "")
	all := a.view.SuggestedLinks // already ranked (Adamic/Adar DESC, tie-broken)

	if doc != "" {
		id, ok := a.docID(doc)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("unknown document %q", doc)), nil
		}
		partners := make([]suggestionPayload, 0)
		for _, s := range all {
			if s.DocA == id || s.DocB == id {
				partners = append(partners, toSuggestionPayload(s))
			}
		}
		return mcp.NewToolResultStructured(map[string]any{
			"document":    id.String(),
			"suggestions": partners,
			"count":       len(partners),
			"truncated":   a.view.SuggestedLinksTruncated,
		}, fmt.Sprintf("%d suggested link(s) for %s", len(partners), id)), nil
	}

	// Global: the top-N highest-signal suggestions.
	shown := all
	if len(shown) > suggestLinksGlobalTopN {
		shown = shown[:suggestLinksGlobalTopN]
	}
	out := make([]suggestionPayload, 0, len(shown))
	for _, s := range shown {
		out = append(out, toSuggestionPayload(s))
	}
	return mcp.NewToolResultStructured(map[string]any{
		"suggestions": out,
		"count":       len(out),
		"total":       len(all),
		"truncated":   a.view.SuggestedLinksTruncated,
	}, fmt.Sprintf("%d of %d topology-based suggested link(s)", len(out), len(all))), nil
}

// rankedDocPayload is the wire shape of one load-bearing document returned by
// critical-docs (a document and its betweenness score).
type rankedDocPayload struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// bridgePayload is the wire shape of one bridge (cut edge) returned by
// critical-docs. from < to canonically.
type bridgePayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (a *Analysis) handleCriticalDocs(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var loadBearing []rankedDocPayload
	if a.metrics != nil {
		for _, r := range a.metrics.Betweenness.TopBetweenness(criticalDocsTopN) {
			loadBearing = append(loadBearing, rankedDocPayload{ID: r.ID.String(), Score: r.Score})
		}
	}
	if loadBearing == nil {
		loadBearing = []rankedDocPayload{}
	}

	articulation := identity.IDStrings(a.view.ArticulationPoints)
	bridges := make([]bridgePayload, 0, len(a.view.Bridges))
	for _, b := range a.view.Bridges {
		bridges = append(bridges, bridgePayload{From: b.A.String(), To: b.B.String()})
	}

	return mcp.NewToolResultStructured(map[string]any{
		"loadBearing":        loadBearing,
		"articulationPoints": articulation,
		"bridges":            bridges,
	}, fmt.Sprintf("%d load-bearing doc(s), %d articulation point(s), %d bridge(s)",
		len(loadBearing), len(articulation), len(bridges))), nil
}

func toSuggestionPayload(s graphmodel.LinkSuggestion) suggestionPayload {
	return suggestionPayload{
		DocA:             s.DocA.String(),
		DocB:             s.DocB.String(),
		SharedNeighbours: s.SharedNeighbours,
		Coupling:         s.Coupling,
		CoCitation:       s.CoCitation,
		AdamicAdar:       s.AdamicAdar,
	}
}
