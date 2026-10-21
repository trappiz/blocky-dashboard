package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/trappiz/blocky-dashboard/internal/config"
)

// upstream is a fake Blocky. Handlers run concurrently (the overview fetches
// status and stats in parallel), so all access goes through the mutex.
type upstream struct {
	mu    sync.Mutex
	calls []string
	fail  bool
}

func (u *upstream) Calls() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.calls...)
}

func (u *upstream) Reset() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls = nil
}

func (u *upstream) SetFail(fail bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.fail = fail
}

func newUpstream(t *testing.T, u *upstream) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.calls = append(u.calls, r.Method+" "+r.URL.RequestURI())
		fail := u.fail
		u.mu.Unlock()
		if fail {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enabled":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

var testWeb = fstest.MapFS{
	"index.html": {Data: []byte("<!doctype html>index")},
	"app.js":     {Data: []byte("// app")},
}

func newTestServer(t *testing.T, basePath string) (*Server, *upstream) {
	t.Helper()
	u := &upstream{}
	cfg := config.Config{Blocky: config.BlockyConfig{URL: newUpstream(t, u)}, UI: config.UIConfig{BasePath: basePath}}
	return newServer(cfg, testWeb), u
}

func newMultiServer(t *testing.T, ups ...*upstream) *Server {
	t.Helper()
	cfg := config.Config{}
	for i, u := range ups {
		cfg.Instances = append(cfg.Instances, config.InstanceConfig{
			Name:         []string{"DNS One", "DNS Two", "DNS Three"}[i],
			BlockyConfig: config.BlockyConfig{URL: newUpstream(t, u)},
		})
	}
	return newServer(cfg, testWeb)
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
	s, up := newTestServer(t, "/")
	h := s.Handler()

	if rec := do(h, http.MethodGet, "/api/blocking/disable?duration=5m", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET should be rejected, got %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/api/blocking/disable?duration=5m&evil=1", nil); rec.Code != http.StatusOK {
		t.Fatalf("POST failed: %d %s", rec.Code, rec.Body)
	}
	if calls := up.Calls(); len(calls) != 1 || calls[0] != "GET /api/blocking/disable?duration=5m" {
		t.Fatalf("unexpected upstream calls: %v", calls)
	}
}

func TestCrossSiteRequestsRejected(t *testing.T) {
	s, up := newTestServer(t, "/")
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
	if calls := up.Calls(); len(calls) != 1 {
		t.Fatalf("want exactly one upstream call, got %v", calls)
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

func TestActionsFanOutToAllInstances(t *testing.T) {
	a, b := &upstream{}, &upstream{}
	h := newMultiServer(t, a, b).Handler()

	rec := do(h, http.MethodPost, "/api/blocking/disable?duration=5m", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable all failed: %d %s", rec.Code, rec.Body)
	}
	if len(a.Calls()) != 1 || len(b.Calls()) != 1 {
		t.Fatalf("want one call per instance, got %v / %v", a.Calls(), b.Calls())
	}

	rec = do(h, http.MethodPost, "/api/blocking/enable?instance=dns-two", nil)
	if rec.Code != http.StatusOK || len(a.Calls()) != 1 || len(b.Calls()) != 2 {
		t.Fatalf("single-instance action hit wrong upstream: %d %v / %v", rec.Code, a.Calls(), b.Calls())
	}

	if rec := do(h, http.MethodPost, "/api/cache/flush?instance=nope", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown instance should be 400, got %d", rec.Code)
	}
}

func TestActionReportsPartialFailure(t *testing.T) {
	a, b := &upstream{}, &upstream{fail: true}
	h := newMultiServer(t, a, b).Handler()

	rec := do(h, http.MethodPost, "/api/cache/flush", nil)
	var body struct {
		OK      bool           `json:"ok"`
		Results []actionResult `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusOK || body.OK || len(body.Results) != 2 || !body.Results[0].OK || body.Results[1].OK {
		t.Fatalf("want partial success, got %d %s", rec.Code, rec.Body)
	}

	a.SetFail(true)
	if rec := do(h, http.MethodPost, "/api/cache/flush", nil); rec.Code != http.StatusBadGateway {
		t.Fatalf("all failed should be 502, got %d", rec.Code)
	}
}

func TestOverviewAndPerInstanceRouting(t *testing.T) {
	a, b := &upstream{}, &upstream{fail: true}
	h := newMultiServer(t, a, b).Handler()

	rec := do(h, http.MethodGet, "/api/overview", nil)
	var body struct {
		Instances []overviewEntry `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Instances) != 2 || body.Instances[0].ID != "dns-one" || body.Instances[0].Blocking == nil ||
		body.Instances[1].BlockingError == "" || body.Instances[1].StatsError == "" {
		t.Fatalf("unexpected overview: %s", rec.Body)
	}

	a.Reset()
	b.Reset()
	do(h, http.MethodGet, "/api/stats?instance=dns-two", nil)
	if len(a.Calls()) != 0 || len(b.Calls()) != 1 {
		t.Fatalf("stats routed wrong: %v / %v", a.Calls(), b.Calls())
	}
	if rec := do(h, http.MethodGet, "/api/stats?instance=nope", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown instance should be 400, got %d", rec.Code)
	}
}

func TestHealthzBypassesAuthAndBasePath(t *testing.T) {
	s, _ := newTestServer(t, "/dns/")
	s.cfg.Auth = config.AuthConfig{Enabled: true, Username: "admin", Password: "secret"}
	h := s.Handler()

	if rec := do(h, http.MethodGet, "/healthz", nil); rec.Code != http.StatusOK {
		t.Fatalf("healthz should not need auth, got %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/dns/api/config", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("api should still need auth, got %d", rec.Code)
	}
}
