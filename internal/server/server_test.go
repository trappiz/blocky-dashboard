package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trappiz/blocky-dashboard/internal/blocky"
	"github.com/trappiz/blocky-dashboard/internal/config"
	"github.com/trappiz/blocky-dashboard/internal/querylog"
)

func newTestServer(t *testing.T, basePath string) (*Server, *[]string) {
	t.Helper()
	var calls []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enabled":true}`))
	}))
	t.Cleanup(upstream.Close)

	cfg := config.Config{Blocky: config.BlockyConfig{URL: upstream.URL}, UI: config.UIConfig{BasePath: basePath}}
	web := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html>index")},
		"app.js":     {Data: []byte("// app")},
	}
	return &Server{cfg: cfg, blocky: blocky.New(cfg.Blocky), logs: querylog.New(cfg.QueryLog), web: web}, &calls
}

func do(h http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDisableRequiresPostAndForwardsOnlyKnownParams(t *testing.T) {
	s, calls := newTestServer(t, "/")
	h := s.Handler()

	if rec := do(h, http.MethodGet, "/api/blocking/disable?duration=5m", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET should be rejected, got %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/api/blocking/disable?duration=5m&evil=1", nil); rec.Code != http.StatusOK {
		t.Fatalf("POST failed: %d %s", rec.Code, rec.Body)
	}
	if len(*calls) != 1 || (*calls)[0] != "GET /api/blocking/disable?duration=5m" {
		t.Fatalf("unexpected upstream calls: %v", *calls)
	}
}

func TestCrossSiteRequestsRejected(t *testing.T) {
	s, calls := newTestServer(t, "/")
	h := s.Handler()

	if rec := do(h, http.MethodPost, "/api/cache/flush", map[string]string{"Origin": "https://evil.example"}); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST should be rejected, got %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/api/cache/flush", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST should be rejected, got %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/api/cache/flush", map[string]string{"Origin": "http://example.com", "Sec-Fetch-Site": "same-origin"}); rec.Code != http.StatusOK {
		t.Fatalf("same-origin POST should pass, got %d", rec.Code)
	}
	if len(*calls) != 1 {
		t.Fatalf("want exactly one upstream call, got %v", *calls)
	}
}

func TestBasePathAndSPAFallback(t *testing.T) {
	s, _ := newTestServer(t, "/dns/")
	h := s.Handler()

	if rec := do(h, http.MethodGet, "/dns", nil); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/dns/" {
		t.Fatalf("want redirect to /dns/, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := do(h, http.MethodGet, "/dns/", nil); !strings.Contains(rec.Body.String(), "index") {
		t.Fatalf("index not served at base path: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, http.MethodGet, "/dns/app.js", nil); !strings.Contains(rec.Body.String(), "// app") {
		t.Fatalf("asset not served: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/dns/some/route", nil); !strings.Contains(rec.Body.String(), "index") {
		t.Fatalf("unknown route should fall back to index, got %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/dns/api/nope", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown api path should 404, got %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/dns/api/status", nil); rec.Code != http.StatusOK {
		t.Fatalf("api under base path failed: %d", rec.Code)
	}
}
