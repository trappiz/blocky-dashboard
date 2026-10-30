package querylog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/trappiz/blocky-dashboard/internal/config"
)

// sample mirrors the log server's real output (sqlite mode), plus a null
// entry and a csv-style reply_type that carries Blocky's reason.
const sample = `[
 {"timestamp": "2026-10-30 11:13:56.222514204+01:00", "client_ip": "10.1.1.65", "client_name": "teleport.int.example", "reply_type": "CACHED", "domain": "reporting.example.sh", "status": "NOERROR", "type": "A"},
 null,
 {"timestamp": "2026-10-30 11:13:53.708079886+01:00", "client_ip": "10.1.3.20", "client_name": "10.1.3.20", "reply_type": "BLOCKED (ads)", "domain": "ads.example.net", "status": "NXDOMAIN", "type": "AAAA"},
 {"timestamp": "2026-10-30 11:13:55.000000000+01:00", "client_ip": "10.1.1.65", "client_name": "teleport.int.example", "reply_type": "RESOLVED", "domain": "api.example.com", "status": "NOERROR", "type": "HTTPS"}
]`

func newLogServer(t *testing.T, status int, body string) (string, *atomic.Int32, *atomic.Value) {
	t.Helper()
	var hits atomic.Int32
	var lastQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		lastQuery.Store(r.URL.RawQuery)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/", &hits, &lastQuery
}

func TestHTTPSourceReadsLogServer(t *testing.T) {
	url, hits, lastQuery := newLogServer(t, http.StatusOK, sample)
	r := New(config.QueryLogConfig{Type: "http", Target: url})
	if !r.Enabled() {
		t.Fatal("http query log should be enabled")
	}

	got, err := r.Read(context.Background(), "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 entries (null skipped), got %d: %+v", len(got), got)
	}
	if got[0].Question != "reporting.example.sh" || got[1].Question != "api.example.com" || got[2].Question != "ads.example.net" {
		t.Fatalf("want newest first, got %v, %v, %v", got[0].Question, got[1].Question, got[2].Question)
	}
	first := got[0]
	if first.Time.IsZero() || first.Time.Nanosecond() != 222514204 || first.ResponseType != "CACHED" || first.ResponseCode != "NOERROR" || first.QuestionType != "A" || first.ClientName != "teleport.int.example" {
		t.Fatalf("fields parsed wrong: %+v", first)
	}
	blocked := got[2]
	if blocked.ResponseType != "BLOCKED" || blocked.Reason != "BLOCKED (ads)" || blocked.ClientName != "" || blocked.ClientIP != "10.1.3.20" {
		t.Fatalf("reason/client parsed wrong: %+v", blocked)
	}

	// Filters apply locally and are also sent to the server.
	got, err = r.Read(context.Background(), "Example.NET", "10.1.3.20", "blocked", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Question != "ads.example.net" {
		t.Fatalf("filters failed: %+v", got)
	}
	q := lastQuery.Load().(string)
	for _, want := range []string{"q=example.net", "client=10.1.3.20", "type=blocked", "limit=10"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q is missing %q", q, want)
		}
	}

	// Limit, and the short cache: the same request again does not hit the server.
	before := hits.Load()
	got, _ = r.Read(context.Background(), "", "", "", 1)
	if len(got) != 1 || got[0].Question != "reporting.example.sh" {
		t.Fatalf("limit failed: %+v", got)
	}
	_, _ = r.Read(context.Background(), "", "", "", 1)
	if hits.Load() != before+1 {
		t.Fatalf("want one server hit for two identical reads, got %d", hits.Load()-before)
	}
}

func TestHTTPSourceErrors(t *testing.T) {
	url, _, _ := newLogServer(t, http.StatusInternalServerError, `{"error": "no such table: log_entries"}`)
	_, err := New(config.QueryLogConfig{Type: "http", Target: url}).Read(context.Background(), "", "", "", 10)
	if err == nil || !strings.Contains(err.Error(), "no such table") {
		t.Fatalf("want server error passed through, got %v", err)
	}

	url, _, _ = newLogServer(t, http.StatusOK, `{"not": "a list"}`)
	if _, err := New(config.QueryLogConfig{Type: "http", Target: url}).Read(context.Background(), "", "", "", 10); err == nil {
		t.Fatal("want error for a non-array response")
	}

	if _, err := New(config.QueryLogConfig{Type: "http", Target: "10.1.1.60:4001"}).Read(context.Background(), "", "", "", 10); err == nil {
		t.Fatal("want error for a target without http://")
	}
}
