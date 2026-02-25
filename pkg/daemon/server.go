// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/charmbracelet/log"

	"github.com/Work-Fort/sharkfin/pkg/db"
)

// Server is the sharkfind HTTP server.
type Server struct {
	addr       string
	db         *db.DB
	sessions   *SessionManager
	httpServer *http.Server
}

// NewServer creates a new sharkfind server.
func NewServer(addr, dbPath string, allowChannelCreation bool, pongTimeout time.Duration) (*Server, error) {
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sm := NewSessionManager(database, allowChannelCreation)
	mcpHandler := NewMCPHandler(sm, database)
	presenceHandler := NewPresenceHandler(sm, pongTimeout)

	mux := http.NewServeMux()
	mux.Handle("POST /mcp", mcpHandler)
	mux.Handle("GET /presence", presenceHandler)

	return &Server{
		addr:     addr,
		db:       database,
		sessions: sm,
		httpServer: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}, nil
}

// Start begins listening for connections.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	log.Infof("sharkfind listening on %s", s.addr)
	return s.httpServer.Serve(ln)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info("shutting down sharkfind")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}
	return s.db.Close()
}
