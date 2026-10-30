{{/*
Resource names use the app name rather than .Chart.Name, which the release
workflow may rename to avoid clashing with the image repository.
*/}}
{{- define "blocky-dashboard.name" -}}
{{- default "blocky-dashboard" .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "blocky-dashboard.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := include "blocky-dashboard.name" . }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "blocky-dashboard.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "blocky-dashboard.labels" -}}
helm.sh/chart: {{ include "blocky-dashboard.chart" . }}
{{ include "blocky-dashboard.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "blocky-dashboard.selectorLabels" -}}
app.kubernetes.io/name: {{ include "blocky-dashboard.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "blocky-dashboard.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "blocky-dashboard.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "blocky-dashboard.image" -}}
{{- $repo := required "image.repository is required, e.g. docker.io/<account>/blocky-dashboard" .Values.image.repository }}
{{- if .Values.image.digest }}
{{- printf "%s@%s" $repo .Values.image.digest }}
{{- else }}
{{- printf "%s:%s" $repo (default .Chart.AppVersion .Values.image.tag | toString) }}
{{- end }}
{{- end }}

{{/* Path served by the app; used for Ingress and HTTPRoute. */}}
{{- define "blocky-dashboard.basePath" -}}
{{- $p := trimSuffix "/" (default "" .Values.config.ui.basePath) }}
{{- default "/" $p }}
{{- end }}

{{/* Environment variable holding an instance's Blocky token; mirrors config.InstanceTokenEnv. */}}
{{- define "blocky-dashboard.instanceTokenEnv" -}}
{{- $s := regexReplaceAll "[^a-z0-9]+" (lower .) "-" | trimAll "-" }}
{{- printf "BLOCKY_DASHBOARD_INSTANCE_%s_TOKEN" (default "instance" $s | upper | replace "-" "_") }}
{{- end }}

{{/* Environment variable holding an instance's query log target; mirrors config.InstanceQueryLogTargetEnv. */}}
{{- define "blocky-dashboard.instanceQueryLogTargetEnv" -}}
{{- include "blocky-dashboard.instanceTokenEnv" . | trimSuffix "_TOKEN" }}_QUERYLOG_TARGET
{{- end }}

{{/* Environment variable holding an instance's query log server token; mirrors config.InstanceQueryLogTokenEnv. */}}
{{- define "blocky-dashboard.instanceQueryLogTokenEnv" -}}
{{- include "blocky-dashboard.instanceTokenEnv" . | trimSuffix "_TOKEN" }}_QUERYLOG_TOKEN
{{- end }}

{{/* Whether a queryLog block points at a database; its target (DSN) is then a secret. */}}
{{- define "blocky-dashboard.queryLogIsDatabase" -}}
{{- if has (lower (default "" .type)) (list "mysql" "postgresql" "postgres" "timescale") }}true{{ end }}
{{- end }}

{{/* queryLog block without secrets: the token, and a database connection string. */}}
{{- define "blocky-dashboard.queryLogJson" -}}
{{- if include "blocky-dashboard.queryLogIsDatabase" . }}
{{- toJson (omit . "target" "token") }}
{{- else }}
{{- toJson (omit . "token") }}
{{- end }}
{{- end }}

{{/* config.json without secrets. */}}
{{- define "blocky-dashboard.configJson" -}}
{{- $c := .Values.config }}
{{- $instances := list }}
{{- range $c.instances }}
{{- $i := omit . "token" }}
{{- with .queryLog }}{{ $_ := set $i "queryLog" (include "blocky-dashboard.queryLogJson" . | fromJson) }}{{ end }}
{{- $instances = append $instances $i }}
{{- end }}
{{- $out := dict
  "ui" $c.ui
  "auth" (omit $c.auth "password" "headerToken")
  "blocky" (omit $c.blocky "token")
  "queryLog" (include "blocky-dashboard.queryLogJson" (default dict $c.queryLog) | fromJson)
  "instances" $instances }}
{{- toPrettyJson $out }}
{{- end }}

{{/* Secret values from config, keyed by environment variable name. */}}
{{- define "blocky-dashboard.secretData" -}}
{{- $c := .Values.config }}
{{- $d := dict }}
{{- with $c.auth.password }}{{ $_ := set $d "BLOCKY_DASHBOARD_AUTH_PASSWORD" . }}{{ end }}
{{- with $c.auth.headerToken }}{{ $_ := set $d "BLOCKY_DASHBOARD_AUTH_HEADER_TOKEN" . }}{{ end }}
{{- with $c.blocky.token }}{{ $_ := set $d "BLOCKY_DASHBOARD_BLOCKY_TOKEN" . }}{{ end }}
{{- with $c.queryLog }}
{{- if and .target (include "blocky-dashboard.queryLogIsDatabase" .) }}{{ $_ := set $d "BLOCKY_DASHBOARD_QUERYLOG_TARGET" .target }}{{ end }}
{{- with .token }}{{ $_ := set $d "BLOCKY_DASHBOARD_QUERYLOG_TOKEN" . }}{{ end }}
{{- end }}
{{- range $c.instances }}
{{- if .token }}{{ $_ := set $d (include "blocky-dashboard.instanceTokenEnv" .name) .token }}{{ end }}
{{- $name := .name }}
{{- with .queryLog }}
{{- if and .target (include "blocky-dashboard.queryLogIsDatabase" .) }}{{ $_ := set $d (include "blocky-dashboard.instanceQueryLogTargetEnv" $name) .target }}{{ end }}
{{- with .token }}{{ $_ := set $d (include "blocky-dashboard.instanceQueryLogTokenEnv" $name) . }}{{ end }}
{{- end }}
{{- end }}
{{- toYaml $d }}
{{- end }}

{{- define "blocky-dashboard.secretName" -}}
{{- default (include "blocky-dashboard.fullname" .) .Values.secrets.existingSecret }}
{{- end }}

{{/* Whether the pod reads secrets from a Secret at all. */}}
{{- define "blocky-dashboard.hasSecret" -}}
{{- if or .Values.secrets.existingSecret (fromYaml (include "blocky-dashboard.secretData" .)) }}true{{ end }}
{{- end }}
