package websocket

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

// Server coordinates HTTP listeners and handles Gorilla upgrades.
type Server struct {
	server *http.Server
	pool   *ConnectionPool
	host   string
	port   int
}

// NewServer initializes a strictly localized loopback WebSocket server.
func NewServer(host string, port int, pool *ConnectionPool) *Server {
	// Security: Enforce strict local binding boundary.
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		log.Printf("[WebSocket] [Security Warning] Requested bind address '%s' overridden to loopback '127.0.0.1' for safety.", host)
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 12345
	}
	return &Server{
		host: host,
		port: port,
		pool: pool,
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
			log.Printf("[WebSocket] Upgrade failure: %v", err)
			return
		}
		go s.pool.HandleClient(conn)
	})

	addr := net.JoinHostPort(s.host, strconv.Itoa(s.port))
	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
		// Enforce standard timeouts to protect against connection exhaustion.
		ReadHeaderTimeout: 3 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	// Run listener asynchronously
	errChan := make(chan error, 1)
	go func() {
		log.Printf("[WebSocket] Listening strictly on ws://%s/ws", addr)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	// Monitor context for graceful shutdown
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		log.Printf("[WebSocket] Shutting down WebSocket HTTP server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return s.server.Shutdown(shutdownCtx)
	}
}
