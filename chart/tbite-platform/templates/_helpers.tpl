{{/* vim: set filetype=mustache: */}}
{{/*
Common helpers for tbite-platform.
*/}}

{{- define "tbite.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "tbite.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "tbite.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "tbite.namespace" -}}
{{- default .Release.Namespace .Values.global.namespaceOverride -}}
{{- end -}}

{{/* Common labels */}}
{{- define "tbite.labels" -}}
helm.sh/chart: {{ include "tbite.chart" . }}
app.kubernetes.io/name: {{ include "tbite.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/part-of: tbite-platform
{{- end -}}

{{/* Selector labels (no version) */}}
{{- define "tbite.selectorLabels" -}}
app.kubernetes.io/name: {{ include "tbite.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Role-scoped labels: takes a dict with .ctx and .role */}}
{{- define "tbite.roleLabels" -}}
{{ include "tbite.labels" .ctx }}
app.kubernetes.io/component: {{ .role }}
{{- end -}}

{{- define "tbite.roleSelectorLabels" -}}
{{ include "tbite.selectorLabels" .ctx }}
app.kubernetes.io/component: {{ .role }}
{{- end -}}

{{/* Role fullname: "<release>-<chart>-<role>" */}}
{{- define "tbite.roleFullname" -}}
{{- printf "%s-%s" (include "tbite.fullname" .ctx) .role | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Image reference */}}
{{- define "tbite.image" -}}
{{- $repo := .repo -}}
{{- $tag := default .ctx.Chart.AppVersion .tag -}}
{{- $registry := .ctx.Values.global.imageRegistry -}}
{{- if $registry -}}
{{- printf "%s/%s:%s" $registry $repo $tag -}}
{{- else -}}
{{- printf "%s:%s" $repo $tag -}}
{{- end -}}
{{- end -}}

{{/* Platform binary image — defaults to .Values.image.repository:.Values.image.tag or AppVersion */}}
{{- define "tbite.platformImage" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- $registry := .Values.global.imageRegistry -}}
{{- if $registry -}}
{{- printf "%s/%s:%s" $registry .Values.image.repository $tag -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
{{- end -}}

{{/* ServiceAccount name for a role: <release>-<chart>-<role> */}}
{{- define "tbite.roleServiceAccountName" -}}
{{- include "tbite.roleFullname" . -}}
{{- end -}}

{{/* OTel collector endpoint based on subchart release-name conventions */}}
{{- define "tbite.otelCollectorPort" -}}
{{- if eq .Values.otel.exporterProtocol "http/protobuf" -}}
{{- int .Values.observability.otelCollector.httpPort -}}
{{- else -}}
{{- int .Values.observability.otelCollector.grpcPort -}}
{{- end -}}
{{- end -}}

{{- define "tbite.otelCollectorEndpoint" -}}
{{- if .Values.observability.otelCollector.enabled -}}
{{- printf "%s-opentelemetry-collector.%s.svc:%s" .Release.Name (include "tbite.namespace" .) (include "tbite.otelCollectorPort" .) -}}
{{- else -}}
{{- printf "otel-collector:%s" (include "tbite.otelCollectorPort" .) -}}
{{- end -}}
{{- end -}}

{{- define "tbite.otelCollectorEndpointValue" -}}
{{- if eq .Values.otel.exporterProtocol "http/protobuf" -}}
{{- printf "http://%s" (include "tbite.otelCollectorEndpoint" .) -}}
{{- else -}}
{{- include "tbite.otelCollectorEndpoint" . -}}
{{- end -}}
{{- end -}}

{{/* Common OTel env vars for app workloads */}}
{{- define "tbite.otelEnv" -}}
{{- if .Values.observability.otelCollector.enabled }}
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: {{ include "tbite.otelCollectorEndpointValue" . | quote }}
- name: OTEL_EXPORTER_OTLP_PROTOCOL
  value: {{ .Values.otel.exporterProtocol | quote }}
- name: OTEL_SERVICE_NAMESPACE
  value: {{ .Values.otel.serviceNamespace | quote }}
- name: OTEL_RESOURCE_ATTRIBUTES
  value: "service.namespace={{ .Values.otel.serviceNamespace }},deployment.environment={{ .Values.profile.size }}"
{{- end }}
{{- end -}}

{{/* Standard application env vars (URLs, OIDC, NATS, S3, etc.) — derived from .Values.global */}}
{{- define "tbite.appEnv" -}}
- name: TBITE_PROFILE
  value: {{ .Values.profile.size | quote }}
- name: TBITE_DOMAIN
  value: {{ .Values.global.domain | quote }}
- name: TBITE_BASE_URL_API
  value: {{ .Values.global.baseURL.api | quote }}
- name: TBITE_BASE_URL_EMPLOYEE
  value: {{ .Values.global.baseURL.employee | quote }}
- name: TBITE_BASE_URL_MERCHANT
  value: {{ .Values.global.baseURL.merchant | quote }}
- name: TBITE_BASE_URL_ADMIN
  value: {{ .Values.global.baseURL.admin | quote }}
- name: TBITE_OIDC_ISSUER_URL
  value: {{ .Values.global.oidcIssuerURL | quote }}
- name: TBITE_NATS_URL
  value: {{ .Values.global.nats.url | quote }}
- name: TBITE_S3_ENDPOINT
  value: {{ .Values.global.s3.endpoint | quote }}
- name: TBITE_S3_REGION
  value: {{ .Values.global.s3.region | quote }}
- name: TBITE_S3_BUCKET
  value: {{ .Values.global.s3.bucket | quote }}
- name: TBITE_S3_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.global.s3.credentialsSecretRef.name | quote }}
      key: {{ .Values.global.s3.credentialsSecretRef.accessKeyKey | quote }}
- name: TBITE_S3_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.global.s3.credentialsSecretRef.name | quote }}
      key: {{ .Values.global.s3.credentialsSecretRef.secretKeyKey | quote }}
{{- if and .Values.global.nats.credentialsSecretRef .Values.global.nats.credentialsSecretRef.name }}
- name: TBITE_NATS_CREDENTIALS
  valueFrom:
    secretKeyRef:
      name: {{ .Values.global.nats.credentialsSecretRef.name | quote }}
      key: {{ .Values.global.nats.credentialsSecretRef.key | quote }}
{{- end }}
{{- end -}}

{{/*
DB connection-pool tuning env for app/worker roles. The DATABASE_RW_URL
and DATABASE_RO_URL secret-backed env vars are provided by
`tbite.appSecretEnv`; this helper carries only the non-secret tuning
knobs that live in values.yaml and would otherwise need to be repeated
across every Deployment. provision-streams Job still includes this for
its --role=provision-streams binary which honours the same env shape.
*/}}
{{- define "tbite.dbEnv" -}}
- name: DATABASE_MAX_CONNS
  value: {{ .Values.api.database.maxConns | quote }}
- name: DATABASE_MAX_CONNS_RO
  value: {{ .Values.api.database.maxConnsRO | quote }}
{{- end -}}

{{/*
DB env for the provision-streams Job which does not include
`tbite.appSecretEnv`. Mirrors the pre-split combined helper: URL +
tuning in one block.
*/}}
{{- define "tbite.dbEnvForJob" -}}
- name: DATABASE_RW_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.api.database.rwUrlSecretRef.name | quote }}
      key: {{ .Values.api.database.rwUrlSecretRef.key | quote }}
{{- if and .Values.api.database.roUrlSecretRef .Values.api.database.roUrlSecretRef.name }}
- name: DATABASE_RO_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.api.database.roUrlSecretRef.name | quote }}
      key: {{ .Values.api.database.roUrlSecretRef.key | quote }}
{{- end }}
- name: DATABASE_MAX_CONNS
  value: {{ .Values.api.database.maxConns | quote }}
- name: DATABASE_MAX_CONNS_RO
  value: {{ .Values.api.database.maxConnsRO | quote }}
{{- end -}}

{{/* Pod-level security context */}}
{{- define "tbite.podSecurityContext" -}}
{{- toYaml .Values.securityContext -}}
{{- end -}}

{{- define "tbite.containerSecurityContext" -}}
{{- toYaml .Values.containerSecurityContext -}}
{{- end -}}

{{/* Image pull secrets */}}
{{- define "tbite.imagePullSecrets" -}}
{{- if .Values.image.pullSecrets }}
imagePullSecrets:
{{- range .Values.image.pullSecrets }}
  - name: {{ . | quote }}
{{- end }}
{{- end }}
{{- end -}}

{{/* Common annotations */}}
{{- define "tbite.podAnnotations" -}}
{{- with .Values.global.podAnnotations }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/* Common labels for pods include podLabels */}}
{{- define "tbite.podLabels" -}}
{{- with .Values.global.podLabels }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/* Standard deployment pod scheduling fragments */}}
{{- define "tbite.podScheduling" -}}
{{- with .Values.global.nodeSelector }}
nodeSelector:
{{ toYaml . | indent 2 }}
{{- end }}
{{- with .Values.global.tolerations }}
tolerations:
{{ toYaml . | indent 2 }}
{{- end }}
{{/* Prefer an app-role-specific affinity (.Values.tbite.affinity) so anti-
     affinity can be set without putting it under global.* — which Helm would
     propagate into subcharts whose helpers expect a string preset. Falls back
     to .Values.global.affinity. */}}
{{- $affinity := .Values.global.affinity -}}
{{- with .Values.tbite }}{{- with .affinity }}{{- $affinity = . }}{{- end }}{{- end }}
{{- with $affinity }}
affinity:
{{ toYaml . | indent 2 }}
{{- end }}
{{- end -}}

{{/* Generic readiness probe for HTTP roles */}}
{{- define "tbite.httpReadinessProbe" -}}
httpGet:
  path: /readyz
  port: {{ .port }}
initialDelaySeconds: 5
periodSeconds: 10
timeoutSeconds: 3
failureThreshold: 3
{{- end -}}

{{- define "tbite.httpLivenessProbe" -}}
httpGet:
  path: /healthz
  port: {{ .port }}
initialDelaySeconds: 15
periodSeconds: 20
timeoutSeconds: 3
failureThreshold: 3
{{- end -}}

{{/* TCP probe for headless workers/schedulers — checks metrics port */}}
{{- define "tbite.tcpProbe" -}}
tcpSocket:
  port: {{ .port }}
initialDelaySeconds: 10
periodSeconds: 20
timeoutSeconds: 3
failureThreshold: 3
{{- end -}}

{{/* Render env from a map */}}
{{- define "tbite.envMap" -}}
{{- range $k, $v := . }}
- name: {{ $k | quote }}
  value: {{ $v | quote }}
{{- end -}}
{{- end -}}

{{/* envFrom for a list of secret names */}}
{{- define "tbite.envFromSecrets" -}}
{{- range . }}
- secretRef:
    name: {{ . | quote }}
{{- end -}}
{{- end -}}

{{/*
Bridge secret refs into the Go binary's expected env var names. All
roles that touch Postgres, NATS, object storage, or OIDC consume the
same logical set of secret-backed variables; central them in one
helper so each Deployment template gets identical wiring.
*/}}
{{- define "tbite.appSecretEnv" -}}
- name: DATABASE_RW_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.api.database.rwUrlSecretRef.name | quote }}
      key:  {{ .Values.api.database.rwUrlSecretRef.key | quote }}
- name: DATABASE_RO_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.api.database.roUrlSecretRef.name | quote }}
      key:  {{ .Values.api.database.roUrlSecretRef.key | quote }}
- name: S3_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.global.s3.credentialsSecretRef.name | quote }}
      key:  {{ .Values.global.s3.credentialsSecretRef.accessKeyKey | quote }}
- name: S3_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.global.s3.credentialsSecretRef.name | quote }}
      key:  {{ .Values.global.s3.credentialsSecretRef.secretKeyKey | quote }}
- name: AUTHENTIK_API_TOKEN
  valueFrom:
    secretKeyRef:
      name: {{ .Values.global.authentik.apiTokenSecretRef.name | quote }}
      key:  {{ .Values.global.authentik.apiTokenSecretRef.key | quote }}
- name: AUTH_PROVIDER_AUTHENTIK_ISSUER_URL
  value: {{ .Values.global.oidcIssuerURL | quote }}
- name: AUTH_PROVIDER_AUTHENTIK_DISPLAY_NAME
  value: "Authentik"
  # client_id is not secret; sourced from global.authentik.clientID
- name: AUTH_PROVIDER_AUTHENTIK_CLIENT_ID
  value: {{ .Values.global.authentik.clientID | quote }}
- name: AUTH_PROVIDER_AUTHENTIK_CLIENT_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ .Values.global.oidcClientsSecretRef.name | quote }}
      key:  "apiClientSecret"
{{- end -}}
