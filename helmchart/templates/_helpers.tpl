{{- define "agentchart.fullname" -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "agentchart.namespace" -}}
{{- printf "%s" .Release.Namespace -}}
{{- end -}}

{{- define "agentchart.deploymentname" -}}
{{- printf "%s-%s" ( include "agentchart.fullname" . ) "-deploy" -}}
{{- end -}}

{{- define "agentchart.podname" -}}
{{- printf "%s-%s" ( include "agentchart.fullname" . ) "-pod" -}}
{{- end -}}

{{- define "agentchart.configmapname" -}}
{{- printf "%s-%s" ( include "agentchart.fullname" . ) "-cm" -}}
{{- end -}}


{{- define "agentchart.k8ssecret" -}}
{{- printf "%s" .Values.kubeconfig.secretName -}}
{{- end -}}