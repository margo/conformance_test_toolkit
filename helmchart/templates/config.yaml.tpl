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
  kubeconfig: |
    apiVersion: v1
    clusters:
    - cluster:
        certificate-authority-data: {{ .Values.kubeconfig.certificateAuthorityData }}
        server: https://kubernetes.default.svc:443
      name: default
    contexts:
    - context:
        cluster: default
        user: default
      name: default
    current-context: default
    kind: Config
    preferences: {}
    users:
    - name: default
      user:
        client-certificate-data: {{ .Values.kubeconfig.clientCertificateData }}
        client-key-data: {{ .Values.kubeconfig.clientKeyData }}