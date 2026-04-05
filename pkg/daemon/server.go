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

	auth "github.com/Work-Fort/Passport/go/service-auth"
	authapikey "github.com/Work-Fort/Passport/go/service-auth/apikey"
	authjwt "github.com/Work-Fort/Passport/go/service-auth/jwt"
	"github.com/Work-Fort/sharkfin/pkg/domain"
	"github.com/Work-Fort/sharkfin/web"
)

type Server struct {
	addr       string
	store      domain.Store
	httpServer *http.Server
	closers    []interface{ Close() }
}

func NewServer(ctx context.Context, addr string, store domain.Store, pongTimeout time.Duration, webhookURL string, bus domain.EventBus, version string, passportURL string, uiDir string) (*Server, error) {
	if webhookURL != "" {
		store.SetSetting("webhook_url", webhookURL)
	}

	// Initialize Passport auth middleware.
	opts := auth.DefaultOptions(passportURL)
	jwtV, err := authjwt.New(ctx, opts.JWKSURL, opts.JWKSRefreshInterval)
	if err != nil {
		return nil, fmt.Errorf("init JWT validator: %w", err)
	}
	akV := authapikey.New(opts.VerifyAPIKeyURL, opts.APIKeyCacheTTL)
	mw := auth.NewFromValidators(jwtV, akV)

	hub := NewHub(bus)
	presenceHandler := NewPresenceHandler(pongTimeout)

	var closers []interface{ Close() }
	closers = append(closers, jwtV)
	if bus != nil {
		closers = append(closers, NewWebhookSubscriber(bus, store))
		closers = append(closers, NewPresenceNotifier(bus, presenceHandler, store))
	}

	wsHandler := NewWSHandler(store, hub, presenceHandler, pongTimeout, version)

	sharkfinMCP := NewSharkfinMCP(store, hub, presenceHandler, version)
	mcpTransport := mcpserver.NewStreamableHTTPServer(sharkfinMCP.Server(),
		mcpserver.WithStateful(true),
	)

	mux := http.NewServeMux()
	registerUIRoutes(mux, uiDir, web.Dist)
	mux.Handle("/mcp", mw(mcpTransport))
	mux.Handle("GET /presence", mw(presenceHandler))
	mux.Handle("GET /ws", mw(wsHandler))
	mux.Handle("GET /notifications/subscribe", mw(http.HandlerFunc(wsHandler.handleNotificationSubscribe)))

	rest := NewRESTHandler(store, hub, bus)
	mux.Handle("POST /api/v1/auth/register", mw(http.HandlerFunc(rest.handleRegisterIdentity)))
	mux.Handle("GET /api/v1/channels", mw(http.HandlerFunc(rest.handleListChannels)))
	mux.Handle("POST /api/v1/channels", mw(http.HandlerFunc(rest.handleCreateChannel)))
	mux.Handle("POST /api/v1/channels/{channel}/join", mw(http.HandlerFunc(rest.handleJoinChannel)))
	mux.Handle("POST /api/v1/channels/{channel}/messages", mw(http.HandlerFunc(rest.handleSendMessage)))
	mux.Handle("GET /api/v1/channels/{channel}/messages", mw(http.HandlerFunc(rest.handleListMessages)))
	mux.Handle("POST /api/v1/webhooks", mw(http.HandlerFunc(rest.handleRegisterWebhook)))
	mux.Handle("GET /api/v1/webhooks", mw(http.HandlerFunc(rest.handleListWebhooks)))
	mux.Handle("DELETE /api/v1/webhooks/{id}", mw(http.HandlerFunc(rest.handleDeleteWebhook)))

	return &Server{
		addr:    addr,
		store:   store,
		closers: closers,
		httpServer: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}, nil
}

func (s *Server) Store() domain.Store { return s.store }

func (s *Server) Start() error {
	ln, err := net.Listen("tcp4", s.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	fmt.Fprintf(os.Stderr, "sharkfind listening on %s\n", ln.Addr())
	return s.httpServer.Serve(ln)
}

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
