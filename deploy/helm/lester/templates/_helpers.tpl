{{- define "lester.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "lester.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "lester.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "lester.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "lester.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "lester.selectorLabels" -}}
app.kubernetes.io/name: {{ include "lester.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "lester.secretName" -}}
{{- default (printf "%s-secrets" (include "lester.fullname" .)) .Values.secrets.existingSecret }}
{{- end }}

{{- define "lester.image" -}}
{{- $image := index . 0 -}}
{{- $root := index . 1 -}}
{{- printf "%s:%s" $image.repository (default $root.Chart.AppVersion $image.tag) -}}
{{- end }}


{{/* Select only telemetry config and collector credentials for every Go service. */}}
{{- define "lester.telemetryEnv" -}}
{{- $root := index . 0 -}}
{{- $component := index . 1 -}}
- name: OTEL_SERVICE_NAME
  value: {{ index $root.Values.config.telemetry.serviceNames $component | quote }}
{{- range $key := list "OTEL_SDK_DISABLED" "OTEL_EXPORTER_OTLP_ENDPOINT" "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT" "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT" "OTEL_EXPORTER_OTLP_PROTOCOL" "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL" "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL" "OTEL_RESOURCE_ATTRIBUTES" "OTEL_TRACES_SAMPLER" "OTEL_TRACES_SAMPLER_ARG" "OTEL_TRACES_EXPORTER" "OTEL_METRICS_EXPORTER" "OTEL_METRIC_EXPORT_INTERVAL" }}
- name: {{ $key }}
  valueFrom:
    configMapKeyRef:
      name: {{ include "lester.fullname" $root }}-config
      key: {{ $key }}
{{- end }}
{{- range $key := list "OTEL_EXPORTER_OTLP_HEADERS" "OTEL_EXPORTER_OTLP_TRACES_HEADERS" "OTEL_EXPORTER_OTLP_METRICS_HEADERS" }}
- name: {{ $key }}
  valueFrom:
    secretKeyRef:
      name: {{ include "lester.secretName" $root }}
      key: {{ $key }}
      optional: true
{{- end }}
{{- end }}
