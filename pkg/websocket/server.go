package websocket

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// Server coordinates HTTP listeners and handles Gorilla upgrades.
type Server struct {
	logger             *slog.Logger
	server             *http.Server
	pool               *ConnectionPool
	host               string
	port               int
	registerConnectRPC func(mux *http.ServeMux)
}

// NewServer initializes a strictly localized loopback WebSocket server.
func NewServer(host string, port int, pool *ConnectionPool, registerConnectRPC func(mux *http.ServeMux), logger *slog.Logger) *Server {
	// Security: Enforce strict local binding boundary.
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		logger.Warn("Requested bind address overridden to loopback for safety", "requested_host", host, "fallback_host", "127.0.0.1")
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 12345
	}
	return &Server{
		logger:             logger,
		host:               host,
		port:               port,
		pool:               pool,
		registerConnectRPC: registerConnectRPC,
	}
}

// Start runs the HTTP listener on the configured local socket.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		CheckOrigin: func(r *http.Request) bool {
			// Permissive for loopback testing since traffic is already isolated within local machine boundaries.
			return true
		},
	}

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			s.logger.Error("Upgrade failure", "error", err)
			return
		}
		go s.pool.HandleClient(conn)
	})

	// Register ConnectRPC services on ServeMux if callback is defined
	var handler http.Handler = mux
	if s.registerConnectRPC != nil {
		s.registerConnectRPC(mux)
		// Enforce HTTP/2 cleartext (h2c) wrapping for ConnectRPC streaming route safety
		handler = h2c.NewHandler(mux, &http2.Server{})
	}

	addr := net.JoinHostPort(s.host, strconv.Itoa(s.port))
	s.server = &http.Server{
		Addr:    addr,
		Handler: handler,
		// Enforce standard timeouts to protect against connection exhaustion.
		ReadHeaderTimeout: 3 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	// Run listener asynchronously
	errChan := make(chan error, 1)
	go func() {
		s.logger.Info("Listening strictly", "url", "http://"+addr)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	// Monitor context for graceful shutdown
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		s.logger.Info("Shutting down WebSocket HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return s.server.Shutdown(shutdownCtx)
	}
}
