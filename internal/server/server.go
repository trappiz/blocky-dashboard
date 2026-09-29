package server

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/trappiz/blocky-dashboard/internal/blocky"
	"github.com/trappiz/blocky-dashboard/internal/config"
	"github.com/trappiz/blocky-dashboard/internal/querylog"
)

type Server struct {
	cfg       config.Config
	blocky    *blocky.Client
	logs      *querylog.Reader
	web       fs.FS
	historyMu sync.Mutex
}

func New(cfg config.Config, embedded embed.FS) (*Server, error) {
	web, err := fs.Sub(embedded, "web")
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, blocky: blocky.New(cfg.Blocky), logs: querylog.New(cfg.QueryLog), web: web}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/api/config", s.configAPI)
	mux.HandleFunc("/api/status", s.status)
	mux.HandleFunc("/api/stats", s.stats)
	mux.HandleFunc("/api/query", s.query)
	mux.HandleFunc("/api/query-log", s.queryLog)
	mux.HandleFunc("/api/blocking/enable", s.enableBlocking)
	mux.HandleFunc("/api/blocking/disable", s.disableBlocking)
	mux.HandleFunc("/api/cache/flush", s.cacheFlush)
	mux.HandleFunc("/api/lists/refresh", s.listsRefresh)
	mux.HandleFunc("/", s.static)
	return s.auth(logging(mux))
}

func (s *Server) auth(next http.Handler) http.Handler {
	if !s.cfg.Auth.Enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Auth.HeaderToken != "" {
			got := r.Header.Get("X-Blocky-Dashboard-Token")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Auth.HeaderToken)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}
		u, p, ok := r.BasicAuth()
		if ok && subtle.ConstantTimeCompare([]byte(u), []byte(s.cfg.Auth.Username)) == 1 &&
			subtle.ConstantTimeCompare([]byte(p), []byte(s.cfg.Auth.Password)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="Blocky Dashboard"`)
		http.Error(w, "authentication required", http.StatusUnauthorized)
	})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "time": time.Now().UTC()})
}

func (s *Server) configAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"title":              s.cfg.UI.Title,
		"instance":           s.cfg.UI.Instance,
		"autoRefreshSeconds": s.cfg.UI.AutoRefreshSeconds,
		"queryLogs":          s.logs.Enabled(),
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	data, code, err := s.blocky.Do(r.Context(), http.MethodGet, "/api/blocking/status", nil, nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error(), "blockyStatus": code})
		return
	}
	var raw json.RawMessage = data
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "blocking": raw})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	data, code, err := s.blocky.Do(r.Context(), http.MethodGet, "/api/stats", nil, nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error(), "blockyStatus": code})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

type queryRequest struct {
	Query string `json:"query"`
	Type  string `json:"type"`
}

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var q queryRequest
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil || strings.TrimSpace(q.Query) == "" {
		http.Error(w, "query and type are required", 400)
		return
	}
	if q.Type == "" {
		q.Type = "A"
	}
	data, _, err := s.blocky.Do(r.Context(), http.MethodPost, "/api/query", nil, q)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (s *Server) queryLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	entries, err := s.logs.Read(r.URL.Query().Get("q"), r.URL.Query().Get("client"), r.URL.Query().Get("type"), 200)
	if err != nil {
		writeJSON(w, 503, map[string]any{"enabled": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": true, "entries": entries})
}

func (s *Server) enableBlocking(w http.ResponseWriter, r *http.Request) {
	s.action(w, r, http.MethodGet, "/api/blocking/enable", nil)
}
func (s *Server) disableBlocking(w http.ResponseWriter, r *http.Request) {
	s.action(w, r, http.MethodGet, "/api/blocking/disable", r.URL.Query())
}
func (s *Server) cacheFlush(w http.ResponseWriter, r *http.Request) {
	s.action(w, r, http.MethodPost, "/api/cache/flush", nil)
}
func (s *Server) listsRefresh(w http.ResponseWriter, r *http.Request) {
	s.action(w, r, http.MethodPost, "/api/lists/refresh", nil)
}

func (s *Server) action(w http.ResponseWriter, r *http.Request, method, path string, query map[string][]string) {
	if r.Method != method {
		http.Error(w, "method not allowed", 405)
		return
	}
	data, _, err := s.blocky.Do(r.Context(), method, path, query, nil)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if len(data) == 0 {
		_, _ = w.Write([]byte(`{"ok":true}`))
		return
	}
	_, _ = w.Write(data)
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if _, err := fs.Stat(s.web, path); err != nil {
		path = "index.html"
	}
	http.FileServer(http.FS(s.web)).ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

var _ = fmt.Sprintf
var _ = os.ErrNotExist
