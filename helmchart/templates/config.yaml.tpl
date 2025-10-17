apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "agentchart.configmapname" . }}
  namespace: {{ include "agentchart.namespace" . }}
data:
  config.yaml: |
{{- .Files.Get "config.yaml" | nindent 4 }}
  capabilities.json: |
{{- .Files.Get "capabilities.json" | nindent 4 }}
