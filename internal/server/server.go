package server

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/trappiz/blocky-dashboard/internal/blocky"
	"github.com/trappiz/blocky-dashboard/internal/config"
	"github.com/trappiz/blocky-dashboard/internal/querylog"
)

type Server struct {
	cfg       config.Config
	instances []*instance
	web       fs.FS
}

func New(cfg config.Config, embedded embed.FS) (*Server, error) {
	web, err := fs.Sub(embedded, "web")
	if err != nil {
		return nil, err
	}
	return newServer(cfg, web), nil
}

func newServer(cfg config.Config, web fs.FS) *Server {
	s := &Server{cfg: cfg, web: web}
	for _, in := range cfg.ResolvedInstances() {
		s.instances = append(s.instances, &instance{
			ID:     config.Slug(in.Name),
			Name:   in.Name,
			blocky: blocky.New(in.BlockyConfig),
			logs:   querylog.New(in.QueryLog),
		})
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/api/config", s.configAPI)
	mux.HandleFunc("/api/overview", s.overview)
	mux.HandleFunc("/api/status", s.status)
	mux.HandleFunc("/api/stats", s.stats)
	mux.HandleFunc("/api/query", s.query)
	mux.HandleFunc("/api/query-log", s.queryLog)
	mux.HandleFunc("/api/blocking/enable", s.enableBlocking)
	mux.HandleFunc("/api/blocking/disable", s.disableBlocking)
	mux.HandleFunc("/api/cache/flush", s.cacheFlush)
	mux.HandleFunc("/api/lists/refresh", s.listsRefresh)
	mux.HandleFunc("/", s.static)

	var h http.Handler = sameOrigin(mux)
	if base := strings.TrimRight(s.cfg.UI.BasePath, "/"); base != "" {
		inner := http.StripPrefix(base, h)
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == base {
				http.Redirect(w, r, base+"/", http.StatusMovedPermanently)
				return
			}
			inner.ServeHTTP(w, r)
		})
	}
	return s.auth(logging(h))
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

// sameOrigin rejects state-changing requests sent from other sites. Browsers
// attach Basic Auth credentials automatically, so without this any page could
// POST to the dashboard on the user's behalf.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "cross-site request rejected", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(w, "cross-origin request rejected", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "time": time.Now().UTC()})
}

func (s *Server) configAPI(w http.ResponseWriter, r *http.Request) {
	type inst struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		QueryLogs bool   `json:"queryLogs"`
	}
	list := make([]inst, 0, len(s.instances))
	for _, in := range s.instances {
		list = append(list, inst{ID: in.ID, Name: in.Name, QueryLogs: in.logs.Enabled()})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"title":              s.cfg.UI.Title,
		"autoRefreshSeconds": s.cfg.UI.AutoRefreshSeconds,
		"instances":          list,
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	in, ok := s.pick(w, r)
	if !ok {
		return
	}
	data, code, err := in.blocky.Do(r.Context(), http.MethodGet, "/api/blocking/status", nil, nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error(), "blockyStatus": code})
		return
	}
	var raw json.RawMessage = data
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "blocking": raw})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	in, ok := s.pick(w, r)
	if !ok {
		return
	}
	data, code, err := in.blocky.Do(r.Context(), http.MethodGet, "/api/stats", nil, nil)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	in, ok := s.pick(w, r)
	if !ok {
		return
	}
	var q queryRequest
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil || strings.TrimSpace(q.Query) == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}
	q.Query = strings.TrimSpace(q.Query)
	if q.Type == "" {
		q.Type = "A"
	}
	data, _, err := in.blocky.Do(r.Context(), http.MethodPost, "/api/query", nil, q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (s *Server) queryLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	in, ok := s.pick(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	entries, err := in.logs.Read(q.Get("q"), q.Get("client"), q.Get("type"), limit)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"enabled": in.logs.Enabled(), "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "entries": entries})
}

// Blocky exposes enable/disable as GET, but the dashboard only accepts POST so
// that state changes are covered by the same-origin check.
func (s *Server) enableBlocking(w http.ResponseWriter, r *http.Request) {
	s.action(w, r, http.MethodGet, "/api/blocking/enable", nil)
}

func (s *Server) disableBlocking(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	for _, k := range []string{"duration", "groups"} {
		if v := r.URL.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	s.action(w, r, http.MethodGet, "/api/blocking/disable", q)
}

func (s *Server) cacheFlush(w http.ResponseWriter, r *http.Request) {
	s.action(w, r, http.MethodPost, "/api/cache/flush", nil)
}

func (s *Server) listsRefresh(w http.ResponseWriter, r *http.Request) {
	s.action(w, r, http.MethodPost, "/api/lists/refresh", nil)
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" || path == "index.html" {
		s.index(w, r)
		return
	}
	if _, err := fs.Stat(s.web, path); err != nil {
		if strings.HasPrefix(path, "api/") {
			http.NotFound(w, r)
			return
		}
		s.index(w, r)
		return
	}
	http.FileServerFS(s.web).ServeHTTP(w, r)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(s.web, "index.html")
	if err != nil {
		http.Error(w, "index.html missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
	_, _ = w.Write(b)
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
