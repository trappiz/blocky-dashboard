# Blocky Dashboard

A web dashboard for one or more [Blocky](https://github.com/0xERR0R/blocky) DNS servers, shipped as a
single Go binary with the UI embedded.

- Blocking status, timed pause and enable/disable for all instances at once
- Query stats for the last 24 hours: chart, outcome and query-type breakdown, top domains, top
  blocked domains and top clients
- DNS query tester, cache flush and list refresh
- Optional search in Blocky's query log: `csv` / `csv-client` files, a MySQL, PostgreSQL or
  TimescaleDB database, or a small JSON log server on the Blocky host (for `csv` or `sqlite` logs
  on another machine)
- Optional Basic Auth or a trusted reverse-proxy header

## Blocky requirements

The dashboard talks to Blocky's HTTP API. In Blocky's config, enable the HTTP listener and statistics
(without `statistics`, everything except the stats works):

```yaml
ports:
  http: 4000
statistics:
  enable: true
```

Keep that port private; only the dashboard needs to reach it.

## Run

```bash
./blocky-dashboard --blocky http://127.0.0.1:4000          # one Blocky, no config file
./blocky-dashboard --config /etc/blocky-dashboard/config.json
```

The default listen address is `127.0.0.1:3001`. Flags can also be set as environment variables:

| Flag | Environment variable |
| --- | --- |
| `--config` | `BLOCKY_DASHBOARD_CONFIG` |
| `--listen` | `BLOCKY_DASHBOARD_LISTEN` |
| `--blocky` | `BLOCKY_DASHBOARD_BLOCKY_URL` (overrides the config and uses this single instance) |

`GET /healthz` answers without auth and outside `ui.basePath`; `--healthcheck` probes it.

### Docker

Images for `linux/amd64` and `linux/arm64` are published to `ghcr.io/<owner>/<repository>` (and Docker Hub,
if configured). The image is distroless, runs as uid 65532 and listens on `:3001`.

```bash
docker run -p 3001:3001 -e BLOCKY_DASHBOARD_BLOCKY_URL=http://10.0.0.53:4000 ghcr.io/<owner>/<repository>

docker run -p 3001:3001 \
  -v ./config.json:/etc/blocky-dashboard/config.json:ro \
  -e BLOCKY_DASHBOARD_CONFIG=/etc/blocky-dashboard/config.json \
  ghcr.io/<owner>/<repository>
```

`make docker` builds `blocky-dashboard:dev` locally.

### Kubernetes

Use the Helm chart in [`charts/blocky-dashboard`](charts/blocky-dashboard/README.md):

```bash
helm install blocky-dashboard oci://ghcr.io/<owner>/charts/blocky-dashboard-chart --version <version without v> -f my-values.yaml
```

### systemd

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin blocky-dashboard
sudo install -d -o blocky-dashboard -g blocky-dashboard /var/lib/blocky-dashboard
sudo install -Dm755 blocky-dashboard /usr/local/bin/blocky-dashboard
sudo install -Dm644 blocky-dashboard.service /etc/systemd/system/blocky-dashboard.service
sudo install -Dm640 -g blocky-dashboard config.example.json /etc/blocky-dashboard/config.json
sudo systemctl daemon-reload
sudo systemctl enable --now blocky-dashboard
```

## Configuration

Start from [`config.example.json`](config.example.json). Main settings:

| Key | Meaning |
| --- | --- |
| `listen` | listen address |
| `blocky.url`, `blocky.token`, `blocky.headers` | Blocky API URL, optional bearer token and extra headers |
| `instances` | several Blocky servers (replaces `blocky` and `queryLog`) |
| `ui.title`, `ui.instance` | page title, name of the single instance |
| `ui.basePath` | serve under a sub-path, e.g. `/dns` |
| `ui.autoRefreshSeconds` | refresh interval |
| `auth` | Basic Auth or proxy header token |
| `queryLog` | query-log files or database |

### Multiple instances

```json
"instances": [
  { "name": "dns1", "url": "http://10.0.0.53:4000",
    "queryLog": { "type": "csv", "target": "/var/lib/blocky/logs", "days": 2 } },
  { "name": "dns2", "url": "http://10.0.0.54:4000", "token": "", "headers": {} }
]
```

The page shows one card per instance; click one to see its details (the choice is kept in the URL,
e.g. `#dns2`). The blocking and maintenance menus in the header act on all instances, the pill on a
card on just that one.

### Query logs

The query log section appears for each instance whose `queryLog` is set up; `"enabled": false` hides
it without removing the settings. `type` is the same as Blocky's `queryLog.type`, and `days` is how
many days to search, including today.

**Database** (`mysql`, `postgresql` or `timescale`): the dashboard connects with the same connection
string Blocky uses as its `target`, so it can run anywhere that reaches the database. Use a
read-only user, and pass the string through `BLOCKY_DASHBOARD_QUERYLOG_TARGET` (or
`BLOCKY_DASHBOARD_INSTANCE_<NAME>_QUERYLOG_TARGET`) rather than the file, since it holds a
password. When several Blocky servers share one database, `hostname` limits an instance to the
entries its server wrote (an SQL `LIKE` pattern such as `blocky-primary-%`; Blocky records its
hostname, which in Kubernetes is the pod name).

```json
"queryLog": { "type": "postgresql", "days": 7, "hostname": "blocky-primary-%" }
```

**Files** (`csv` or `csv-client`): `target` is the log directory, which must be on the same host as
the dashboard or on a mounted share.

```json
"queryLog": { "type": "csv", "target": "/var/lib/blocky/logs", "days": 2 }
```

**Log server** (`http`): for `csv` or `sqlite` logs on another host, run a small HTTP service next to
Blocky that returns recent entries as JSON, and set `target` to its URL. The dashboard expects
`GET <target>` to answer with an array of objects, newest first:

```json
[{"timestamp": "2026-10-30 11:13:56.2+01:00", "client_ip": "10.1.1.65", "client_name": "laptop",
  "reply_type": "CACHED", "domain": "example.com", "status": "NOERROR", "type": "A"}]
```

`reason`, `answer` and `duration_ms` are used when present. The dashboard sends its filters as
`?q=…&client=…&type=…&limit=…` and filters again itself, so a server that ignores the parameters
still works; it only searches what the server returns, though, so a server that applies them can
search further back. `days` doesn't apply.

```json
"queryLog": { "type": "http", "target": "http://10.1.1.60:4001/" }
```

`sqlite` can't be read directly, since only Blocky's host can open the file; use a log server.
Query logs show your network's activity, so enable auth before exposing the dashboard, and keep
the log server reachable only from the dashboard.

### Authentication

```json
"auth": { "enabled": true, "username": "admin", "password": "use-a-long-random-password" }
```

Alternatively, set `auth.headerToken` and have a trusted reverse proxy send it as
`X-Blocky-Dashboard-Token`. Only use this when the dashboard isn't reachable except through that proxy.

The dashboard refuses to start with auth enabled and the example password `change-me`.

Secrets can come from environment variables instead of the file:

| Variable | Overrides |
| --- | --- |
| `BLOCKY_DASHBOARD_AUTH_PASSWORD` | `auth.password` |
| `BLOCKY_DASHBOARD_AUTH_HEADER_TOKEN` | `auth.headerToken` |
| `BLOCKY_DASHBOARD_BLOCKY_TOKEN` | `blocky.token` |
| `BLOCKY_DASHBOARD_QUERYLOG_TARGET` | `queryLog.target` |
| `BLOCKY_DASHBOARD_INSTANCE_<NAME>_TOKEN` | the token of instance `<NAME>`, upper-cased with other characters as `_` (`Office DNS` → `OFFICE_DNS`) |
| `BLOCKY_DASHBOARD_INSTANCE_<NAME>_QUERYLOG_TARGET` | the query log target of instance `<NAME>` |

### Reverse proxy

```text
dns.example.com {
    reverse_proxy 127.0.0.1:3001
}
```

For a sub-path such as `https://example.com/dns/`, set `ui.basePath` to `/dns` and proxy that path
without stripping it.

## Build

Requires Go 1.25+.

```bash
make build
make test
```

## Releases and CI

Push a version tag to release:

```bash
git tag v1.0.0 && git push origin v1.0.0
```

The GitHub Actions workflows then:

- `build.yml`: build binaries for Linux, macOS and Windows (amd64 and arm64) on every push to
  `main` and every PR, downloadable from the run's *Artifacts* for 14 days.
- `docker.yml`: test, then build images on every push and PR. Pushes to `main` publish `edge`; tags
  publish `v1.0.0` and `latest`.
- `release.yml`: create a GitHub Release with the same binaries, named with the tag
  (`blocky-dashboard-v1.0.0-linux-amd64`), and checksums. Tags like `v1.0.0-rc.1` become
  pre-releases.
- `helm.yml`: lint the chart, and on tags publish it to `oci://ghcr.io/<owner>/charts` (and Docker
  Hub) and attach it to the release. The chart version has no `v` (`1.0.0`), because Helm ignores
  `v`-prefixed chart versions unless asked for exactly; its `appVersion` and default image tag
  keep it (`v1.0.0`). Afterwards it commits the new version to `Chart.yaml` on `main`, so don't
  bump it by hand (if `main` is protected, allow GitHub Actions to push to it).

GHCR works with no setup, though new packages start private. Docker Hub is optional; configure it
under **Settings → Secrets and variables → Actions**:

| Name | Kind | Value |
| --- | --- | --- |
| `DOCKERHUB_USERNAME` | variable | Docker Hub account or organisation |
| `DOCKERHUB_TOKEN` | secret | Docker Hub access token with Read & Write scope |
| `IMAGE_NAME` | variable, optional | Docker Hub image name, default `<DOCKERHUB_USERNAME>/<repository>` |
| `CHART_NAME` | variable, optional | chart name, default `<image name>-chart` |
