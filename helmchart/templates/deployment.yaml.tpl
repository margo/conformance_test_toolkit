apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "agentchart.deploymentname" . }}
  namespace: {{ include "agentchart.namespace" . }}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: {{ include "agentchart.podname" . }}
  template:
    metadata:
      labels:
        app: {{ include "agentchart.podname" . }}
    spec:
      serviceAccountName: {{ include "agentchart.fullname" . }}-sa
      containers:
        - name: {{ include "agentchart.podname" . }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          command: ["./device-agent"]
          args: ["-config", "config/config.yaml"]
          env:
            - name: KUBERNETES_SERVICE_HOST
              value: "kubernetes.default.svc"
            - name: KUBERNETES_SERVICE_PORT
              value: "443"
          volumeMounts:
            - name: agent-config-volume
              mountPath: /config
              readOnly: true
            - name: data-volume
              mountPath: /data
              
      volumes:
        - name: agent-config-volume
          configMap:
            name: {{ include "agentchart.configmapname" . }}
        {{- if eq .Values.persistence.enabled true}}
        - name: data-volume
          persistentVolumeClaim:
            claimName: {{.Values.persistence.claimName}}
        {{- else }}
        - name: data-volume
          emptyDir: {}
        {{- end }}
