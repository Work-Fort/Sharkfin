// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/charmbracelet/log"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// Server is the sharkfind HTTP server.
type Server struct {
	addr       string
	store      domain.Store
	sessions   *SessionManager
	httpServer *http.Server
}

// NewServer creates a new sharkfind server.
func NewServer(addr string, store domain.Store, pongTimeout time.Duration, webhookURL string) (*Server, error) {
	// Set webhook_url if provided via flag (always overwrite).
	if webhookURL != "" {
		store.SetSetting("webhook_url", webhookURL)
	}

	sm := NewSessionManager(store)
	hub := NewHub()
	presenceHandler := NewPresenceHandler(sm, hub, pongTimeout)
	wsHandler := NewWSHandler(sm, store, hub, pongTimeout)

	sharkfinMCP := NewSharkfinMCP(sm, store, hub)
	mcpTransport := mcpserver.NewStreamableHTTPServer(sharkfinMCP.Server(),
		mcpserver.WithStateful(true),
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpTransport)
	mux.Handle("GET /presence", presenceHandler)
	mux.Handle("GET /ws", wsHandler)

	return &Server{
		addr:     addr,
		store:    store,
		sessions: sm,
		httpServer: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}, nil
}

// Store returns the server's store. Intended for test access.
func (s *Server) Store() domain.Store { return s.store }

// Start begins listening for connections.
func (s *Server) Start() error {
	// TODO: change back to "tcp" when Nexus supports IPv6
	ln, err := net.Listen("tcp4", s.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	fmt.Fprintf(os.Stderr, "sharkfind listening on %s\n", ln.Addr())
	return s.httpServer.Serve(ln)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info("shutting down sharkfind")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}
	return s.store.Close()
}
