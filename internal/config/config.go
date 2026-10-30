package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type Config struct {
	Listen string       `json:"listen"`
	Blocky BlockyConfig `json:"blocky"`
	// Instances lists several Blocky servers. When empty, Blocky and QueryLog
	// describe a single instance named after UI.Instance.
	Instances []InstanceConfig `json:"instances"`
	UI        UIConfig         `json:"ui"`
	Auth      AuthConfig       `json:"auth"`
	QueryLog  QueryLogConfig   `json:"queryLog"`
}

type InstanceConfig struct {
	Name string `json:"name"`
	BlockyConfig
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
	// Enabled switches the query log off without removing its settings.
	// Unset means on when type and target are set.
	Enabled *bool `json:"enabled,omitempty"`
	// Type is Blocky's queryLog.type (csv, csv-client, mysql, postgresql or
	// timescale), or http for a JSON log server running next to Blocky.
	Type string `json:"type"`
	// Target is the log directory for csv, the connection string (DSN) for a
	// database, or the log server's URL for http.
	Target string `json:"target"`
	Days   int    `json:"days"` // days to search, including today
	// Hostname limits a database shared by several Blocky servers to the
	// entries written by this one: an SQL LIKE pattern on its hostname column.
	Hostname string `json:"hostname"`
}

// IsEnabled reports whether the query log is switched on (it may still be
// unconfigured).
func (q QueryLogConfig) IsEnabled() bool { return q.Enabled == nil || *q.Enabled }

// KnownQueryLogType reports whether the dashboard can read this Blocky
// query log type.
func KnownQueryLogType(t string) bool {
	switch strings.ToLower(t) {
	case "csv", "csv-client", "http":
		return true
	}
	return IsDatabaseQueryLog(t)
}

// IsHTTPQueryLog reports whether entries come from a JSON log server.
func IsHTTPQueryLog(t string) bool { return strings.EqualFold(t, "http") }

// IsDatabaseQueryLog reports whether the type stores entries in a database
// reached over the network, rather than in local files.
func IsDatabaseQueryLog(t string) bool {
	switch strings.ToLower(t) {
	case "mysql", "postgresql", "postgres", "timescale":
		return true
	}
	return false
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
	for _, in := range c.ResolvedInstances() {
		if q := in.QueryLog; q.IsEnabled() && q.Type != "" && !KnownQueryLogType(q.Type) {
			return fmt.Errorf("instance %q: unsupported queryLog.type %q (use csv, csv-client, mysql, postgresql or timescale)", in.Name, q.Type)
		}
	}
	seen := map[string]string{}
	for i, in := range c.Instances {
		if strings.TrimSpace(in.Name) == "" {
			return fmt.Errorf("instances[%d]: name is required", i)
		}
		if in.URL == "" {
			return fmt.Errorf("instance %q: url is required", in.Name)
		}
		id := Slug(in.Name)
		if prev, ok := seen[id]; ok {
			return fmt.Errorf("instance names %q and %q are too similar", prev, in.Name)
		}
		seen[id] = in.Name
	}
	return nil
}

// ResolvedInstances returns the configured instances, falling back to the
// single top-level blocky/queryLog settings.
func (c Config) ResolvedInstances() []InstanceConfig {
	if len(c.Instances) > 0 {
		return c.Instances
	}
	name := c.UI.Instance
	if name == "" {
		name = "blocky"
	}
	return []InstanceConfig{{Name: name, BlockyConfig: c.Blocky, QueryLog: c.QueryLog}}
}

// Environment variables that override secrets in the config file, so they can
// be injected from a secret store instead of written into config.json.
const (
	EnvAuthPassword    = "BLOCKY_DASHBOARD_AUTH_PASSWORD"
	EnvAuthHeaderToken = "BLOCKY_DASHBOARD_AUTH_HEADER_TOKEN"
	EnvBlockyToken     = "BLOCKY_DASHBOARD_BLOCKY_TOKEN"
	EnvQueryLogTarget  = "BLOCKY_DASHBOARD_QUERYLOG_TARGET"
)

// ApplyEnv overrides secrets with non-empty environment variables: the auth
// password and header token, the single-instance Blocky token and query log
// target, and each instance's token and query log target from
// InstanceTokenEnv(name) and InstanceQueryLogTargetEnv(name). The query log
// target is a secret when it is a database connection string.
func (c *Config) ApplyEnv(getenv func(string) string) {
	set := func(dst *string, key string) {
		if v := getenv(key); v != "" {
			*dst = v
		}
	}
	set(&c.Auth.Password, EnvAuthPassword)
	set(&c.Auth.HeaderToken, EnvAuthHeaderToken)
	set(&c.Blocky.Token, EnvBlockyToken)
	set(&c.QueryLog.Target, EnvQueryLogTarget)
	for i := range c.Instances {
		set(&c.Instances[i].Token, InstanceTokenEnv(c.Instances[i].Name))
		set(&c.Instances[i].QueryLog.Target, InstanceQueryLogTargetEnv(c.Instances[i].Name))
	}
}

// InstanceTokenEnv names the variable holding an instance's Blocky token:
// "Home DNS" → BLOCKY_DASHBOARD_INSTANCE_HOME_DNS_TOKEN.
func InstanceTokenEnv(name string) string { return instanceEnv(name, "TOKEN") }

// InstanceQueryLogTargetEnv names the variable holding an instance's query
// log target: "Home DNS" → BLOCKY_DASHBOARD_INSTANCE_HOME_DNS_QUERYLOG_TARGET.
func InstanceQueryLogTargetEnv(name string) string { return instanceEnv(name, "QUERYLOG_TARGET") }

func instanceEnv(name, suffix string) string {
	return "BLOCKY_DASHBOARD_INSTANCE_" + strings.ToUpper(strings.ReplaceAll(Slug(name), "-", "_")) + "_" + suffix
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns an instance name into the stable id used in URLs.
func Slug(name string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if s == "" {
		s = "instance"
	}
	return s
}
