package blocky

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trappiz/blocky-dashboard/internal/config"
)

func TestClientRefusesRedirects(t *testing.T) {
	contacted := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted = true }))
	t.Cleanup(other.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	_, _, err := New(config.BlockyConfig{URL: srv.URL, Token: "t"}).Do(context.Background(), http.MethodGet, "/api/blocking/status", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "redirected") {
		t.Fatalf("want a redirect error, got %v", err)
	}
	if contacted {
		t.Fatal("redirect target was contacted")
	}
}
