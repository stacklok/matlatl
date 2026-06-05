package mcpserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// freeAddr reserves an ephemeral loopback port and returns it as host:port. The
// listener is closed before returning, so there is a small reuse window; it is
// acceptable for a test and avoids hardcoding a port.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close reservation: %v", err)
	}
	return addr
}

// postInitialize sends an MCP initialize request to the endpoint and returns the
// HTTP status code, or an error if the request could not be made.
func postInitialize(ctx context.Context, url string) (int, error) {
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize",` +
		`"params":{"protocolVersion":"2024-11-05","capabilities":{},` +
		`"clientInfo":{"name":"test","version":"0"}}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close() //nolint:errcheck // status code is all we assert
	return resp.StatusCode, nil
}

// TestServe_StreamableHTTP starts the real Serve over streamable HTTP, completes
// an initialize handshake against the /mcp endpoint, then cancels the context
// and asserts Serve returns nil (graceful shutdown drained cleanly).
func TestServe_StreamableHTTP(t *testing.T) {
	addr := freeAddr(t)
	url := fmt.Sprintf("http://%s%s", addr, EndpointPath)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, fixtureRoot(t), addr) }()

	// Poll initialize until the listener is up (Serve binds asynchronously).
	var status int
	deadline := time.Now().Add(5 * time.Second)
	for {
		reqCtx, c := context.WithTimeout(context.Background(), time.Second)
		code, err := postInitialize(reqCtx, url)
		c()
		if err == nil {
			status = code
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("endpoint never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status != http.StatusOK {
		cancel()
		t.Fatalf("initialize status = %d, want 200", status)
	}

	// Cancellation must trigger a clean graceful shutdown.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error on shutdown: %v", err)
		}
	case <-time.After(shutdownTimeout + 2*time.Second):
		t.Fatal("Serve did not return after context cancellation")
	}
}
