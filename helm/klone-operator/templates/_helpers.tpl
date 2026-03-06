{{/*
Expand the name of the chart.
*/}}
{{- define "klone-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "klone-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "klone-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "klone-operator.labels" -}}
helm.sh/chart: {{ include "klone-operator.chart" . }}
{{ include "klone-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "klone-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "klone-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "klone-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.name }}
{{- .Values.serviceAccount.name }}
{{- else }}
{{- include "klone-operator.fullname" . }}-controller-manager
{{- end }}
{{- end }}

{{/*
Return the proper image name
*/}}
{{- define "klone-operator.image" -}}
{{- printf "%s:%s" .Values.controllerManager.image.repository (.Values.controllerManager.image.tag | default .Chart.AppVersion) }}
{{- end }}

{{/*
Return the namespace
*/}}
{{- define "klone-operator.namespace" -}}
{{- .Values.namespace | default "klone" }}
{{- end }}
