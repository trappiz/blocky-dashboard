# blocky-dashboard Helm chart

Deploys [blocky-dashboard](../../README.md), a web UI for one or more
[Blocky](https://0xerr0r.github.io/blocky/) DNS servers.

The chart renders the app's JSON config from `values.yaml` into a ConfigMap
mounted at `/etc/blocky-dashboard/config.json`. Secrets (the auth password,
the auth header token and Blocky API tokens) never go into the ConfigMap: they
are stored in a Secret, or read from one you manage, and passed to the app as
environment variables. Changing either rolls the pod.

The pod runs the distroless image as uid 65532 with a read-only root
filesystem, no capabilities and the `RuntimeDefault` seccomp profile. Probes
use `GET /healthz`, which bypasses auth and `config.ui.basePath`.

## Install

Each release publishes the chart as an OCI artifact to GHCR and, when Docker
Hub is configured, to Docker Hub, and attaches the `.tgz` to the GitHub
Release. `image.repository` and the default tag point at the image of the same
release (the Docker Hub image when there is one, otherwise GHCR):

```bash
helm install blocky-dashboard oci://ghcr.io/<owner>/charts/blocky-dashboard-chart --version <version without v> -f my-values.yaml
helm install blocky-dashboard oci://registry-1.docker.io/<account>/blocky-dashboard-chart --version <version without v> -f my-values.yaml
```

The chart is named `<image name>-chart` unless the `CHART_NAME` repository
variable says otherwise; on Docker Hub it must differ from the image
repository.

From a checkout, set the image yourself:

```bash
helm install blocky-dashboard ./charts/blocky-dashboard --set image.repository=docker.io/<account>/blocky-dashboard -f my-values.yaml
```

## Single instance

```yaml
config:
  ui:
    instance: home-dns
  blocky:
    url: http://blocky.blocky.svc.cluster.local:4000
  auth:
    enabled: true
    username: admin
secrets:
  existingSecret: blocky-dashboard   # holds BLOCKY_DASHBOARD_AUTH_PASSWORD
```

```bash
kubectl create secret generic blocky-dashboard --from-literal=BLOCKY_DASHBOARD_AUTH_PASSWORD='...'
```

## Query logs in a database

If Blocky writes its query log to MySQL, PostgreSQL or TimescaleDB, the
dashboard reads it over the network and needs no volumes. Use the same `type`
and connection string as Blocky's `queryLog`, preferably with a read-only
database user. The chart moves `target` into the Secret
(`BLOCKY_DASHBOARD_QUERYLOG_TARGET`, or
`BLOCKY_DASHBOARD_INSTANCE_<NAME>_QUERYLOG_TARGET` per instance); or leave it
out of values and put it in an existing Secret under that key.

```yaml
config:
  instances:
    - name: primary
      url: http://blocky-primary.blocky.svc:4000
      queryLog:
        type: postgresql
        target: postgres://dashboard:password@postgres.blocky.svc:5432/blocky
        days: 7
        hostname: blocky-primary-%   # only when several Blockys share the database
```

`queryLog.enabled: false` hides the query log without removing its settings.

## Query logs from a log server

For csv or sqlite logs on a Blocky host outside the cluster, run a small
JSON log server next to Blocky and point `queryLog.target` at it with
`type: http` (the response format is in the main README). No volumes are
needed. If `networkPolicy.egress.enabled` is on, add an egress rule for the
log server's address and port.

```yaml
config:
  instances:
    - name: home-dns
      url: http://10.1.1.60:4000
      queryLog: {type: http, target: "http://10.1.1.60:4001/", token: "..."}
```

`token` goes into the Secret as
`BLOCKY_DASHBOARD_INSTANCE_<NAME>_QUERYLOG_TOKEN` (or
`BLOCKY_DASHBOARD_QUERYLOG_TOKEN` for the single-instance `queryLog`); or
leave it out of values and put it in an existing Secret under that key.

## Multiple instances with query logs

Blocky's csv query logs are read from local directories, so mount each one
read-only with `queryLogVolumes` and point the instance's `queryLog.target`
at its `mountPath`. `source` is any Kubernetes volume source (hostPath,
persistentVolumeClaim, nfs, ...).

```yaml
config:
  ui:
    basePath: /blocky
  instances:
    - name: primary
      url: http://blocky-primary.blocky.svc:4000
      queryLog: {type: csv, target: /logs/primary, days: 2}
    - name: Office DNS
      url: https://office-dns.example.com
      queryLog: {type: csv, target: /logs/office, days: 2}

queryLogVolumes:
  - name: primary-logs
    mountPath: /logs/primary
    source:
      persistentVolumeClaim: {claimName: blocky-logs, readOnly: true}
  - name: office-logs
    mountPath: /logs/office
    source:
      nfs: {server: nas.example.com, path: /export/blocky/logs, readOnly: true}

secrets:
  existingSecret: blocky-dashboard

ingress:
  enabled: true
  className: nginx
  hosts: [dns.example.com]   # served at https://dns.example.com/blocky
  tls:
    - secretName: dns-example-com-tls
      hosts: [dns.example.com]
```

A hostPath volume only works when the pod runs on the node holding the logs;
pin it with `nodeSelector` or `affinity`.

## Secrets

Either set them in values (the chart creates a Secret):

```yaml
config:
  auth: {enabled: true, password: "..."}
  blocky: {token: "..."}            # single instance
  instances:
    - {name: primary, url: "...", token: "..."}
```

or set `secrets.existingSecret` to a Secret whose keys are environment
variable names (or `secrets.existingSecrets` to several, see below). All keys
are optional:

| Key | Overrides |
| --- | --- |
| `BLOCKY_DASHBOARD_AUTH_PASSWORD` | `auth.password` |
| `BLOCKY_DASHBOARD_AUTH_HEADER_TOKEN` | `auth.headerToken` |
| `BLOCKY_DASHBOARD_BLOCKY_TOKEN` | `blocky.token` (single instance) |
| `BLOCKY_DASHBOARD_QUERYLOG_TARGET` | `queryLog.target` (single instance, database DSN) |
| `BLOCKY_DASHBOARD_QUERYLOG_TOKEN` | `queryLog.token` (single instance, log server token) |
| `BLOCKY_DASHBOARD_INSTANCE_<NAME>_TOKEN` | `instances[].token`; `<NAME>` is the instance name upper-cased with runs of other characters replaced by `_` (`Office DNS` → `OFFICE_DNS`) |
| `BLOCKY_DASHBOARD_INSTANCE_<NAME>_QUERYLOG_TARGET` | `instances[].queryLog.target` (database DSN) |
| `BLOCKY_DASHBOARD_INSTANCE_<NAME>_QUERYLOG_TOKEN` | `instances[].queryLog.token` (log server token) |

### Several Secrets

`secrets.existingSecrets` loads several Secrets, for example to keep the auth
password apart from the query log tokens so each can be rotated or shared on
its own. They are loaded after `existingSecret`.

Secret values set in `config` still go into the Secret the chart creates
(named after the release), so you can mix the two: for example the auth
password in your values and the query log tokens in an existing Secret. The
pod loads the chart's Secret first and the existing ones after it, so when a
key is in more than one place, the existing Secret listed last wins. Don't
list a Secret with the same name as the chart's own; the chart stops with an
error if you do while also setting secret values in `config`.

```yaml
config:
  auth: {enabled: true, username: admin, password: "..."}   # → the chart's Secret
  instances: [...]
secrets:
  existingSecrets:
    - blocky-dashboard-querylog   # BLOCKY_DASHBOARD_INSTANCE_<NAME>_QUERYLOG_TOKEN keys
```

```yaml
config:
  auth: {enabled: true, username: admin}
  instances:
    - name: dns1
      url: http://10.1.1.60:4000
      queryLog: {type: http, target: "http://10.1.1.60:4001/"}
    - name: dns2
      url: http://10.1.1.61:4000
      queryLog: {type: http, target: "http://10.1.1.61:4001/"}
secrets:
  existingSecrets:
    - blocky-dashboard-auth       # BLOCKY_DASHBOARD_AUTH_PASSWORD
    - blocky-dashboard-querylog   # BLOCKY_DASHBOARD_INSTANCE_DNS1_QUERYLOG_TOKEN, ..._DNS2_...
```

Env vars from a Secret are read when the pod starts, so restart it
(`kubectl rollout restart deployment/<release>`) after changing one.

Values in `blocky.headers` / `instances[].headers` stay in the ConfigMap; do
not put credentials there.

## Exposing the dashboard

- `ingress.enabled`: an Ingress whose path defaults to `config.ui.basePath`.
- `httpRoute.enabled`: a Gateway API `HTTPRoute` (requires `parentRefs`),
  matching `PathPrefix` on `config.ui.basePath`.
- `networkPolicy.enabled`: only allow traffic to the dashboard port, from
  `networkPolicy.ingressFrom` peers (all when empty). With
  `networkPolicy.egress.enabled`, egress is limited to DNS and
  `networkPolicy.egress.to`; make sure Blocky stays reachable.

## Values

See [`values.yaml`](values.yaml) for every option with comments;
[`values.schema.json`](values.schema.json) validates them. Commonly changed:

| Value | Default | |
| --- | --- | --- |
| `image.repository` | set when published | Required when installing from source |
| `image.tag` | chart `appVersion` (`v1.2.3`) | |
| `image.digest` | `""` | Pins the image; overrides `tag` |
| `config.*` | | App config, see `config.example.json` |
| `secrets.existingSecret` | `""` | Secret with the keys above |
| `secrets.existingSecrets` | `[]` | Several such Secrets, loaded after `existingSecret`; can be mixed with secret values in `config` |
| `queryLogVolumes` | `[]` | Read-only log volumes |
| `service.port` | `80` | |
| `ingress.*`, `httpRoute.*`, `networkPolicy.*`, `podDisruptionBudget.*` | disabled | |
| `resources` | 10m / 32Mi requested, 128Mi limit | |
| `nodeSelector`, `tolerations`, `affinity`, `topologySpreadConstraints` | empty | |
| `podAnnotations`, `podLabels`, `extraEnv`, `extraEnvFrom`, `extraVolumes`, `extraVolumeMounts` | empty | |

## Development

```bash
make helm-lint
```

lints and renders the chart with every file in `ci/`.
