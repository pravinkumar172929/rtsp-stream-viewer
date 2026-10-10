// Package server exposes the HTTP API and the WebSocket video endpoint.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/your-username/rtsp-stream-viewer/backend/internal/config"
	"github.com/your-username/rtsp-stream-viewer/backend/internal/stream"
)

// Server wires the stream manager to HTTP handlers.
type Server struct {
	cfg     *config.Config
	streams *stream.Manager
	log     *slog.Logger
	clients atomic.Int64
	started time.Time
}

// New creates a Server.
func New(cfg *config.Config, streams *stream.Manager, log *slog.Logger) *Server {
	return &Server{cfg: cfg, streams: streams, log: log, started: time.Now()}
}

// Handler returns the root HTTP handler.
//
//	GET /healthz              liveness probe
//	GET /api/presets          demo stream URLs configured on the server
//	GET /api/streams          running streams, viewer counts, bytes
//	GET /ws/stream?url=rtsp:// WebSocket: JSON status (text) + MPEG-TS video (binary)
//	GET /                     the built frontend, when STATIC_DIR is set
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/presets", s.handlePresets)
	mux.HandleFunc("GET /api/streams", s.handleStreams)
	mux.HandleFunc("GET /ws/stream", s.handleStreamWS)
	if s.cfg.StaticDir != "" {
		mux.Handle("GET /", spaHandler(s.cfg.StaticDir))
	}
	return s.withCORS(s.withLogging(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"uptimeSec": time.Since(s.started).Round(time.Second).Seconds(),
	})
}

func (s *Server) handlePresets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.Presets)
}

func (s *Server) handleStreams(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"clients":    s.clients.Load(),
		"maxClients": s.cfg.MaxClients,
		"maxStreams": s.cfg.MaxStreams,
		"streams":    s.streams.Stats(),
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// withCORS allows the frontend (hosted on a different origin, e.g. Vercel)
// to call the JSON API. WebSocket origins are checked separately on accept.
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && s.originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	for _, o := range s.cfg.AllowedOrigins {
		if o == "*" || strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/healthz" && r.URL.Path != "/ws/stream" {
			s.log.Debug("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
		}
	})
}

// spaHandler serves a built single-page app: existing files are served as is,
// anything else falls back to index.html.
func spaHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			if r.URL.Path != "/" {
				r.URL.Path = "/"
			}
		} else if strings.HasPrefix(r.URL.Path, "/assets/") {
			// Vite fingerprints asset file names, so they can be cached forever.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}
