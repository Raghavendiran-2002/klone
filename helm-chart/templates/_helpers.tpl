{{/*
Expand the name of the chart.
*/}}
{{- define "klone-training.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "klone-training.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
klone component labels
*/}}
{{- define "klone.labels" -}}
{{ include "klone-training.labels" . }}
app.kubernetes.io/part-of: klone-training
app.kubernetes.io/component: klone
{{- end }}
