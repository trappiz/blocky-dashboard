package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trappiz/blocky-dashboard/internal/config"
)

type upstream struct {
	calls []string
	fail  bool
}

func newUpstream(t *testing.T, u *upstream) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.calls = append(u.calls, r.Method+" "+r.URL.RequestURI())
		if u.fail {
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

func newTestServer(t *testing.T, basePath string) (*Server, *[]string) {
	t.Helper()
	u := &upstream{}
	cfg := config.Config{Blocky: config.BlockyConfig{URL: newUpstream(t, u)}, UI: config.UIConfig{BasePath: basePath}}
	return newServer(cfg, testWeb), &u.calls
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

func TestActionsFanOutToAllInstances(t *testing.T) {
	a, b := &upstream{}, &upstream{}
	h := newMultiServer(t, a, b).Handler()

	rec := do(h, http.MethodPost, "/api/blocking/disable?duration=5m", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable all failed: %d %s", rec.Code, rec.Body)
	}
	if len(a.calls) != 1 || len(b.calls) != 1 {
		t.Fatalf("want one call per instance, got %v / %v", a.calls, b.calls)
	}

	rec = do(h, http.MethodPost, "/api/blocking/enable?instance=dns-two", nil)
	if rec.Code != http.StatusOK || len(a.calls) != 1 || len(b.calls) != 2 {
		t.Fatalf("single-instance action hit wrong upstream: %d %v / %v", rec.Code, a.calls, b.calls)
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

	a.fail = true
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

	a.calls, b.calls = nil, nil
	do(h, http.MethodGet, "/api/stats?instance=dns-two", nil)
	if len(a.calls) != 0 || len(b.calls) != 1 {
		t.Fatalf("stats routed wrong: %v / %v", a.calls, b.calls)
	}
	if rec := do(h, http.MethodGet, "/api/stats?instance=nope", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown instance should be 400, got %d", rec.Code)
	}
}
