package config

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

type Config struct {
	Listen   string         `json:"listen"`
	Blocky   BlockyConfig   `json:"blocky"`
	UI       UIConfig       `json:"ui"`
	Auth     AuthConfig     `json:"auth"`
	QueryLog QueryLogConfig `json:"queryLog"`
}

type BlockyConfig struct {
	URL     string            `json:"url"`
	Token   string            `json:"token"`
	Headers map[string]string `json:"headers"`
}

type UIConfig struct {
	Title              string `json:"title"`
	Instance           string `json:"instance"`
	BasePath           string `json:"basePath"`
	AutoRefreshSeconds int    `json:"autoRefreshSeconds"`
}

type AuthConfig struct {
	Enabled  bool   `json:"enabled"`
	Username string `json:"username"`
	Password string `json:"password"`
	// HeaderToken allows a reverse proxy to authenticate requests by supplying
	// X-Blocky-Dashboard-Token. Keep it empty unless a trusted proxy strips it.
	HeaderToken string `json:"headerToken"`
}

type QueryLogConfig struct {
	Type   string `json:"type"`   // csv for Blocky's csv/csv-client file logs
	Target string `json:"target"` // log directory
	Days   int    `json:"days"`   // number of daily files to search, including today
}

func Load(path string) (Config, error) {
	cfg := Config{
		Listen: "127.0.0.1:3001",
		Blocky: BlockyConfig{URL: "http://127.0.0.1:4000"},
		UI: UIConfig{
			Title:              "Blocky Dashboard",
			AutoRefreshSeconds: 30,
		},
		QueryLog: QueryLogConfig{Type: "", Days: 1},
	}
	if path == "" {
		return cfg, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate rejects configurations that would run with unsafe or unusable settings.
func (c Config) Validate() error {
	if c.Auth.Enabled && c.Auth.HeaderToken == "" {
		if c.Auth.Username == "" || c.Auth.Password == "" {
			return errors.New("auth is enabled but username or password is empty")
		}
		if c.Auth.Password == "change-me" {
			return errors.New("auth is enabled with the example password; set auth.password")
		}
	}
	if b := c.UI.BasePath; b != "" && !strings.HasPrefix(b, "/") {
		return errors.New("ui.basePath must start with /")
	}
	return nil
}
