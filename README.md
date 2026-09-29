# Blocky Dashboard

A self-contained, single-binary dashboard for an existing [Blocky](https://github.com/0xERR0R/blocky) DNS server.

The project takes inspiration from [BlockyUI](https://github.com/GabeDuarteM/blocky), but is deliberately designed around a Go server with an embedded frontend.

## What is included

- Overview dashboard using Blocky's native `/api/stats`
- Queries-over-time chart
- Outcome breakdown
- Top domains
- Top blocked domains
- Top clients
- List and cache gauges
- DNS query tester with result/reason/response-code presentation
- Blocking controls with timed disable
- Cache flush and list refresh
- Optional Blocky CSV query-log reader with domain/client/outcome filtering
- HTTP Basic Authentication or trusted reverse-proxy header token
- Custom Blocky request headers / bearer token
- Responsive UI
- systemd unit
- Linux amd64/arm64 release workflow
- Everything embedded into one executable

Blocky's statistics API provides a rolling 24-hour window, hourly data, top domains/blocked domains/clients, list counts, and cache entries. The dashboard consumes that directly rather than requiring Prometheus.

## Query logs

Blocky can write query logs to CSV, SQLite, MySQL/MariaDB, PostgreSQL/Timescale, and other targets. This dashboard currently reads **CSV** logs directly because that keeps the dashboard's binary and operational footprint small.

Example Blocky configuration:

```yaml
queryLog:
  type: csv
  target: /var/lib/blocky/logs
  logRetentionDays: 7
```

Then configure:

```json
"queryLog": {
  "type": "csv",
  "target": "/var/lib/blocky/logs",
  "days": 7
}
```

Query logs contain sensitive network activity. Restrict filesystem access and enable authentication before exposing the dashboard beyond a trusted network.

## Authentication

Enable Basic Auth:

```json
"auth": {
  "enabled": true,
  "username": "admin",
  "password": "use-a-long-random-password"
}
```

Or have a trusted reverse proxy inject:

```text
X-Blocky-Dashboard-Token: ...
```

and configure `headerToken`. Do not expose that header-token mode directly to an untrusted network.

## Build

Requires Go 1.24+.

```bash
go build -trimpath -ldflags="-s -w" -o blocky-dashboard ./cmd/blocky-dashboard
```

The frontend is embedded using `go:embed`.

## Run

```bash
cp config.example.json /etc/blocky-dashboard.json
./blocky-dashboard --config /etc/blocky-dashboard.json
```

Or:

```bash
./blocky-dashboard \
  --listen 127.0.0.1:3001 \
  --blocky http://127.0.0.1:4000
```

## Reverse proxy

Example Caddy:

```text
dns.example.com {
    reverse_proxy 127.0.0.1:3001
}
```

Keep Blocky's API listener private if possible and expose only the dashboard through your proxy.

## systemd

Create the service user, install the binary/config, then:

```bash
sudo install -Dm755 blocky-dashboard /usr/local/bin/blocky-dashboard
sudo install -Dm644 blocky-dashboard.service /etc/systemd/system/blocky-dashboard.service
sudo install -Dm640 config.example.json /etc/blocky-dashboard/config.json

sudo systemctl daemon-reload
sudo systemctl enable --now blocky-dashboard
```

The included unit applies a restrictive systemd sandbox.

## Roadmap

- Native SQLite query-log reader matching Blocky's schema
- PostgreSQL/MySQL query-log adapters
- query-log charts for arbitrary time ranges
- query-log pagination and richer filters
- client/domain drill-down pages
- CSRF protection for state-changing operations when cookie auth is added
- configurable groups for timed blocking
- Prometheus metrics for the dashboard itself
- optional Blivit integration
- signed multi-platform releases
