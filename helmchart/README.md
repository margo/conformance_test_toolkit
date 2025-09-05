docker build -f poc/device/agent/Dockerfile . -t margo.org/device-agent:dev-sprint-6


1. Create a namespace
```bash
kubectl create namespace device-agent
```

2. Then create a secret in this namespace containing the kubeconfig file used to reach out to the kubernetes runtime on the device.
The agent will use this kubeconfig to connect with the kubernetes runtime.
Note: There is no provisioning to provide service account, rbac etc at this point of time.

```bash
kubectl create secret generic agent-kubeconfig \
  --from-file=kubeconfig=/home/user/.kube/config \
  --namespace=device-agent
```

3. Copy the config.yaml and capabilities.yaml files in this directory.
```bash
cp ../poc/device/agent/config .
```

4. Change the params as per your need in these config.yaml and capabilities.yaml .

5. 