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
      containers:
        - name: {{ include "agentchart.podname" . }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          command: ["./device-agent"]
          args: ["-config", "config/config.yaml"]
          volumeMounts:
            - name: kubeconfig-volume
              mountPath: "/root/.kube/config" 
              readOnly: true
            - name: agent-config-volume
              mountPath: "/config"
              readOnly: true
              
      volumes:
        - name: kubeconfig-volume
          secret:
            # References the secret created in the step above.
            secretName: {{ include "agentchart.k8ssecret" . }}
            items:
              # Maps the 'kubeconfig' key in the secret to the filename 'config' in the volume.
              - key: kubeconfig
                path: config
        - name: agent-config-volume
          configMap:
            name: {{ include "agentchart.configmapname" . }}