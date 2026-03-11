// SPDX-License-Identifier: AGPL-3.0-or-later
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
	closers    []interface{ Close() }
}

// NewServer creates a new sharkfind server.
func NewServer(addr string, store domain.Store, pongTimeout time.Duration, webhookURL string, bus domain.EventBus, version string) (*Server, error) {
	// Set webhook_url if provided via flag (always overwrite).
	if webhookURL != "" {
		store.SetSetting("webhook_url", webhookURL)
	}

	sm := NewSessionManager(store)
	hub := NewHub(bus)
	var closers []interface{ Close() }
	if bus != nil {
		closers = append(closers, NewWebhookSubscriber(bus, store))
		closers = append(closers, NewPresenceNotifier(bus, sm, store))
	}
	presenceHandler := NewPresenceHandler(sm, hub, pongTimeout)
	wsHandler := NewWSHandler(sm, store, hub, pongTimeout, version)

	sharkfinMCP := NewSharkfinMCP(sm, store, hub, version)
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
		closers:  closers,
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
	for _, c := range s.closers {
		c.Close()
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}
	return s.store.Close()
}
