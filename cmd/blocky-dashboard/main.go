package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/trappiz/blocky-dashboard/internal/config"
	"github.com/trappiz/blocky-dashboard/internal/server"
)

//go:embed web
var webFS embed.FS

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// Every flag can also be set through an environment variable, which is
	// the usual way to configure the container image.
	configPath := flag.String("config", os.Getenv("BLOCKY_DASHBOARD_CONFIG"), "path to JSON configuration file (env BLOCKY_DASHBOARD_CONFIG)")
	listen := flag.String("listen", os.Getenv("BLOCKY_DASHBOARD_LISTEN"), "HTTP listen address (env BLOCKY_DASHBOARD_LISTEN)")
	blocky := flag.String("blocky", os.Getenv("BLOCKY_DASHBOARD_BLOCKY_URL"), "Blocky HTTP base URL (env BLOCKY_DASHBOARD_BLOCKY_URL)")
	healthcheck := flag.Bool("healthcheck", false, "probe the running server's /healthz and exit 0 if healthy")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	// Secrets (auth password, Blocky tokens) may come from the environment.
	cfg.ApplyEnv(os.Getenv)
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *healthcheck {
		os.Exit(probe(cfg.Listen))
	}
	if *blocky != "" {
		// A URL on the command line means "talk to this one Blocky".
		cfg.Blocky.URL = strings.TrimRight(*blocky, "/")
		cfg.Instances = nil
	}
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	srv, err := server.New(cfg, webFS)
	if err != nil {
		log.Fatal(err)
	}

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("blocky-dashboard %s", version)
		for _, in := range cfg.ResolvedInstances() {
			log.Printf("instance %s: %s", in.Name, in.URL)
		}
		log.Printf("listening on %s", cfg.Listen)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// probe calls /healthz on the local listener. The distroless image has no
// shell or curl, so the container HEALTHCHECK runs the binary itself.
func probe(listen string) int {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}
