package main

import (
	"embed"
	"errors"
	"flag"
	"log"
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

func main() {
	configPath := flag.String("config", "", "path to JSON configuration file")
	listen := flag.String("listen", "", "HTTP listen address")
	blocky := flag.String("blocky", "", "Blocky HTTP base URL")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *blocky != "" {
		cfg.Blocky.URL = strings.TrimRight(*blocky, "/")
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
		log.Printf("blocky-dashboard listening on %s; Blocky=%s", cfg.Listen, cfg.Blocky.URL)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	_ = httpServer.Close()
}
