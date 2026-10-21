package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/trappiz/blocky-dashboard/internal/blocky"
	"github.com/trappiz/blocky-dashboard/internal/querylog"
)

type instance struct {
	ID     string
	Name   string
	blocky *blocky.Client
	logs   *querylog.Reader
}

// pick resolves ?instance=<id> for endpoints that work on one instance.
// It defaults to the first instance and writes a 400 for unknown ids.
func (s *Server) pick(w http.ResponseWriter, r *http.Request) (*instance, bool) {
	id := r.URL.Query().Get("instance")
	if id == "" {
		return s.instances[0], true
	}
	for _, in := range s.instances {
		if in.ID == id {
			return in, true
		}
	}
	http.Error(w, "unknown instance "+id, http.StatusBadRequest)
	return nil, false
}

// targets resolves ?instance= for actions: empty or "all" means every instance.
func (s *Server) targets(r *http.Request) []*instance {
	id := r.URL.Query().Get("instance")
	if id == "" || id == "all" {
		return s.instances
	}
	for _, in := range s.instances {
		if in.ID == id {
			return []*instance{in}
		}
	}
	return nil
}

// each runs fn for every instance in parallel and returns results in input order.
func each[T any](list []*instance, fn func(*instance) T) []T {
	out := make([]T, len(list))
	var wg sync.WaitGroup
	for i, in := range list {
		wg.Go(func() { out[i] = fn(in) })
	}
	wg.Wait()
	return out
}

type actionResult struct {
	Instance string `json:"instance"`
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

// action forwards a state change to the targeted instances. It answers 200
// when at least one instance succeeded, with per-instance results, so the UI
// can report partial failures.
func (s *Server) action(w http.ResponseWriter, r *http.Request, blockyMethod, path string, query url.Values) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list := s.targets(r)
	if len(list) == 0 {
		http.Error(w, "unknown instance "+r.URL.Query().Get("instance"), http.StatusBadRequest)
		return
	}
	results := each(list, func(in *instance) actionResult {
		res := actionResult{Instance: in.ID, Name: in.Name, OK: true}
		if _, _, err := in.blocky.Do(r.Context(), blockyMethod, path, query, nil); err != nil {
			res.OK, res.Error = false, err.Error()
		}
		return res
	})

	var failed []string
	for _, res := range results {
		if !res.OK {
			failed = append(failed, res.Name+": "+res.Error)
		}
	}
	if len(failed) == len(results) {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": strings.Join(failed, "; "), "results": results})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": len(failed) == 0, "results": results})
}

type overviewEntry struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Blocking      json.RawMessage `json:"blocking,omitempty"`
	BlockingError string          `json:"blockingError,omitempty"`
	Stats         json.RawMessage `json:"stats,omitempty"`
	StatsError    string          `json:"statsError,omitempty"`
}

// overview fetches blocking status and stats from every instance in one call,
// so the page refreshes with a single request however many instances exist.
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	entries := each(s.instances, func(in *instance) overviewEntry {
		e := overviewEntry{ID: in.ID, Name: in.Name}
		var wg sync.WaitGroup
		wg.Go(func() {
			if data, _, err := in.blocky.Do(ctx, http.MethodGet, "/api/blocking/status", nil, nil); err != nil {
				e.BlockingError = err.Error()
			} else if json.Valid(data) {
				e.Blocking = data
			} else {
				e.BlockingError = "invalid JSON from Blocky"
			}
		})
		wg.Go(func() {
			if data, _, err := in.blocky.Do(ctx, http.MethodGet, "/api/stats", nil, nil); err != nil {
				e.StatsError = err.Error()
			} else if json.Valid(data) {
				e.Stats = data
			} else {
				e.StatsError = "invalid JSON from Blocky"
			}
		})
		wg.Wait()
		return e
	})
	writeJSON(w, http.StatusOK, map[string]any{"instances": entries})
}
