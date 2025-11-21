{{- define "agentchart.fullname" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "agentchart.namespace" -}}
{{- .Release.Namespace -}}
{{- end -}}

{{- define "agentchart.deploymentname" -}}
{{- printf "%s-deploy" (include "agentchart.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "agentchart.podname" -}}
{{- printf "%s-pod" (include "agentchart.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "agentchart.configmapname" -}}
{{- printf "%s-cm" (include "agentchart.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "agentchart.certsecretname" -}}
{{- printf "%s-certs" (include "agentchart.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "agentchart.pvcname" -}}
{{- printf "%s-data" (include "agentchart.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "agentchart.serviceaccountname" -}}
{{- printf "%s-sa" (include "agentchart.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "agentchart.rolename" -}}
{{- printf "%s-role" (include "agentchart.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "agentchart.rolebindingname" -}}
{{- printf "%s-binding" (include "agentchart.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "agentchart.k8ssecret" -}}
{{- .Values.kubeconfig.secretName -}}
{{- end -}}

{{/* Common labels */}}
{{- define "agentchart.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}
