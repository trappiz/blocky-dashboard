# Blocky Dashboard

A self-contained, single-binary dashboard for an existing [Blocky](https://github.com/0xERR0R/blocky) DNS server.

The project takes inspiration from [BlockyUI](https://github.com/GabeDuarteM/blocky), but is deliberately designed around a Go server with an embedded frontend.

## What is included

- Single-page dashboard: everything on one screen, no navigation
- Multiple Blocky instances: a summary card per instance, details for the selected one, and blocking/maintenance actions sent to all instances at once
- Blocking status and timed pause (with live countdown) always visible in the header
- Summary cards, queries-over-time chart, outcome and query-type breakdown using Blocky's `/api/stats`
- Top domains, top blocked domains and top clients — click one to filter the query log
- DNS query tester with result/reason/response-code presentation
- Cache flush and list refresh (with confirmation)
- Optional query-log reader for Blocky's `csv`/`csv-client` file logs, with search-as-you-type and client/outcome filters
- Light and dark theme following the system setting
- HTTP Basic Authentication or trusted reverse-proxy header token
- Custom Blocky request headers / bearer token
- Responsive UI
- systemd unit
- Linux amd64/arm64 release workflow
- Everything embedded into one executable

Blocky's statistics API provides a rolling 24-hour window, hourly data, top domains/blocked domains/clients, list counts, and cache entries. The dashboard consumes that directly rather than requiring Prometheus.

## Multiple instances

List several Blocky servers under `instances` instead of using the top-level `blocky`/`queryLog` blocks:

```json
"instances": [
  { "name": "dns1", "url": "http://10.0.0.53:4000",
    "queryLog": { "type": "csv", "target": "/var/lib/blocky/logs", "days": 2 } },
  { "name": "dns2", "url": "http://10.0.0.54:4000", "token": "", "headers": {} }
]
```

- The top of the page shows one card per instance: reachability, blocking state, queries, blocked share, cache hit rate and response time, plus a combined total.
- Click a card (or use the tabs) to pick the instance for the details below: chart, outcome breakdown, top lists, domain tester and query log. The choice is kept in the URL (`#dns2`), so it can be bookmarked.
- The header blocking menu and the maintenance menu act on **all** instances; each card's own blocking pill acts on just that instance. Partial failures are reported per instance.
- Query logs are read from the dashboard's local filesystem, so each instance's `queryLog` only works if its log directory is reachable from the dashboard host (local, or a mounted share). Instances without one simply have no query-log section.
- If your instances share Redis, Blocky already syncs blocking state between them; sending the action to all instances is harmless.

`--blocky <url>` on the command line overrides the config and runs against that single instance.

## Query logs

Blocky can write query logs to CSV, SQLite, MySQL/MariaDB, PostgreSQL/Timescale, and other targets. This dashboard currently reads Blocky's **file logs** (`csv` and `csv-client`) directly because that keeps the dashboard's binary and operational footprint small. It reads Blocky's tab-separated `YYYY-MM-DD_*.log` files, and also accepts `.csv` files with a header row. `days` is how many daily files to search, newest first.

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
  "days": 2
}
```

The dashboard's service user needs read access to that directory.

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

Secrets can also come from environment variables, which override the file:
`BLOCKY_DASHBOARD_AUTH_PASSWORD`, `BLOCKY_DASHBOARD_AUTH_HEADER_TOKEN`, `BLOCKY_DASHBOARD_BLOCKY_TOKEN`
(single instance) and `BLOCKY_DASHBOARD_INSTANCE_<NAME>_TOKEN` per instance, where `<NAME>` is the
instance name upper-cased with other characters replaced by `_` (`Office DNS` → `OFFICE_DNS`).

Or have a trusted reverse proxy inject:

```text
X-Blocky-Dashboard-Token: ...
```

and configure `headerToken`. Do not expose that header-token mode directly to an untrusted network.

The dashboard refuses to start with auth enabled and the example password. State-changing endpoints (blocking, cache flush, list refresh) only accept `POST` and reject cross-site requests.

## Build

Requires Go 1.25+.

```bash
make build   # or: go build -trimpath -ldflags="-s -w" -o blocky-dashboard ./cmd/blocky-dashboard
make test
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

To serve the dashboard under a sub-path (for example `https://example.com/dns/`), set `ui.basePath` to `/dns/` and proxy that path without stripping it.

Keep Blocky's API listener private if possible and expose only the dashboard through your proxy.

## Docker

The image is a two-stage build: Go compiles a static binary, which is copied into
[distroless `static-debian12:nonroot`](https://github.com/GoogleContainerTools/distroless)
(no shell or package manager, runs as uid 65532). It is published for `linux/amd64` and `linux/arm64`.

```bash
make docker                       # builds blocky-dashboard:dev locally

# one Blocky, no config file
docker run -p 3001:3001 -e BLOCKY_DASHBOARD_BLOCKY_URL=http://10.0.0.53:4000 blocky-dashboard:dev

# with a config file (instances, auth, query logs, ...)
docker run -p 3001:3001 \
  -v ./config.json:/etc/blocky-dashboard/config.json:ro \
  -e BLOCKY_DASHBOARD_CONFIG=/etc/blocky-dashboard/config.json \
  blocky-dashboard:dev
```

The container listens on `:3001`. Settings can come from flags or environment variables:

| Flag | Environment variable |
| --- | --- |
| `--config` | `BLOCKY_DASHBOARD_CONFIG` |
| `--listen` | `BLOCKY_DASHBOARD_LISTEN` (image default `:3001`) |
| `--blocky` | `BLOCKY_DASHBOARD_BLOCKY_URL` |

`/healthz` answers without authentication and outside `ui.basePath`, so it works for
container and Kubernetes probes. The image's `HEALTHCHECK` runs `blocky-dashboard -healthcheck`.
Mount query-log directories read-only if you use them; the dashboard never writes to disk,
so it runs fine with a read-only root filesystem.

### Publishing with GitHub Actions

`.github/workflows/docker.yml` tests the code, then builds the image on every push and pull request.
Pushes to `main` publish the tags `edge` and `sha-<commit>`; version tags publish `v1.2.3` as
`1.2.3`, `1.2`, `1` and `latest`. Pull requests only build. Images go to:

- **GitHub Container Registry**, always: `ghcr.io/<owner>/<repository>`, shown under this
  repository's *Packages*. It uses the workflow's built-in `GITHUB_TOKEN`, so nothing needs setting up.
  New packages start private; make it public under the package's settings if you want anonymous pulls.
- **Docker Hub**, optional: enabled by setting the variables below.

Nothing is hard-coded. Image names are derived in one place, `.github/actions/image-names`, from
the repository name and these settings under **Settings → Secrets and variables → Actions**:

| Name | Kind | Value |
| --- | --- | --- |
| `DOCKERHUB_USERNAME` | variable, optional | Docker Hub account or organisation; leave unset for GHCR only |
| `DOCKERHUB_TOKEN` | secret | Docker Hub [access token](https://app.docker.com/settings/personal-access-tokens) with Read & Write scope |
| `IMAGE_NAME` | variable, optional | Docker Hub image name; defaults to `<DOCKERHUB_USERNAME>/<repository name>` |

### Releases

Push a version tag to cut a release:

```bash
git tag v1.0.0 && git push origin v1.0.0
```

`.github/workflows/release.yml` creates a GitHub Release with Linux binaries, `checksums.txt`,
generated notes and the `docker pull` commands for both registries; `helm.yml` attaches the
packaged Helm chart to the same release. Tags with a suffix (`v1.0.0-rc.1`) become pre-releases.

Dependabot (`.github/dependabot.yml`) opens weekly PRs to keep the base images and actions current.

## Kubernetes (Helm)

A Helm chart lives in [`charts/blocky-dashboard`](charts/blocky-dashboard/README.md). It renders
the config (single or multiple instances) into a ConfigMap, keeps the auth password and Blocky
tokens in a Secret (or an existing one), runs the pod non-root with a read-only root filesystem,
and can add an Ingress or HTTPRoute (honouring `ui.basePath`), a NetworkPolicy and read-only
query-log volumes.

```bash
helm install blocky-dashboard oci://ghcr.io/<owner>/charts/blocky-dashboard-chart --version <version> -f my-values.yaml
```

`.github/workflows/helm.yml` lints and renders the chart on pull requests. For version tags it
publishes it to `oci://ghcr.io/<owner>/charts`, to `oci://registry-1.docker.io/<DOCKERHUB_USERNAME>`
when Docker Hub is configured (skipped with a warning otherwise), and attaches the `.tgz` to the
GitHub Release. `image.repository` and the default tag point at the image of the same release: the
Docker Hub image when configured, otherwise `ghcr.io/<owner>/<repository>`. It reuses
`DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN` and `IMAGE_NAME`; the optional variable `CHART_NAME`
(default `<image name>-chart`) names the chart repository, which on Docker Hub has to differ from the
image repository.

## systemd

Create the service user, install the binary/config, then:

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin blocky-dashboard
sudo install -d -o blocky-dashboard -g blocky-dashboard /var/lib/blocky-dashboard
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
- CSRF protection for state-changing operations when cookie auth is added
- configurable groups for timed blocking
- Prometheus metrics for the dashboard itself
- optional Blivit integration
- signed multi-platform releases
