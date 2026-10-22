package querylog

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/trappiz/blocky-dashboard/internal/config"
)

func TestBuildQueryPostgres(t *testing.T) {
	since := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	q, args := buildQuery(dialectPostgres, filter{needle: "ads_1%", client: "Phone", rtype: "blocked"}, "dns1-%", since, 50)
	for _, want := range []string{
		"FROM log_entries WHERE request_ts >= $1",
		"LOWER(question_name) LIKE $2 ESCAPE '!'",
		"client_ip LIKE $4 ESCAPE '!'",
		"(LOWER(client_name) = $5 OR client_ip = $6)",
		"UPPER(response_type) = $7",
		"hostname LIKE $8",
		"ORDER BY request_ts DESC LIMIT 50",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query lacks %q:\n%s", want, q)
		}
	}
	want := []any{since, "%ads!_1!%%", "%ads!_1!%%", "%ads!_1!%%", "phone", "Phone", "BLOCKED", "dns1-%"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestBuildQueryMySQLPlaceholders(t *testing.T) {
	q, args := buildQuery(dialectMySQL, filter{needle: "x"}, "", time.Now(), 10)
	if strings.Contains(q, "$") || strings.Count(q, "?") != len(args) || len(args) != 4 {
		t.Fatalf("placeholders do not match args (%d):\n%s", len(args), q)
	}
	if strings.Contains(q, "hostname") {
		t.Fatalf("hostname filter without hostname:\n%s", q)
	}
}

func TestEnabledToggle(t *testing.T) {
	off := false
	cases := []struct {
		cfg  config.QueryLogConfig
		want bool
	}{
		{config.QueryLogConfig{Type: "csv", Target: "/logs"}, true},
		{config.QueryLogConfig{Type: "csv", Target: "/logs", Enabled: &off}, false},
		{config.QueryLogConfig{Type: "postgresql", Target: "postgres://u:p@db/blocky"}, true},
		{config.QueryLogConfig{Type: "sqlite", Target: "/data/blocky.db"}, false},
		{config.QueryLogConfig{Type: "mysql"}, false},
		{config.QueryLogConfig{}, false},
	}
	for _, c := range cases {
		r := New(c.cfg)
		if got := r.Enabled(); got != c.want {
			t.Errorf("Enabled(%+v) = %v, want %v", c.cfg, got, c.want)
		}
		r.Close()
	}
	if _, err := New(config.QueryLogConfig{Type: "csv", Target: "/logs", Enabled: &off}).Read(context.Background(), "", "", "", 0); err == nil {
		t.Fatal("Read on a disabled query log should fail")
	}
}

func TestBadMySQLTarget(t *testing.T) {
	r := New(config.QueryLogConfig{Type: "mysql", Target: "not a dsn"})
	if _, err := r.Read(context.Background(), "", "", "", 0); err == nil || !strings.Contains(err.Error(), "query log target") {
		t.Fatalf("err = %v, want a query log target error", err)
	}
}
