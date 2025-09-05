# Pre-requisites:
- Kubernetes runtime
- Ensure that you have the container image for the device agent with you, if not you can build it using the following command(assuming that you cloned the entire dev-repo at one place). To build, please run the following command (you can use nordctl or other tools as per your preference):
```bash
cd ..
docker build -f poc/device/agent/Dockerfile . -t margo.org/device-agent:dev-sprint-6
docker save -o device-agent.tar margo.org/device-agent:dev-sprint-6
ctr image import device-agent.tar  # use this command if on k8s cluster
k3s ctr -n k8s.io image import device-agent.tar # use this command if on k3s cluster
cd helmchart
```

# Main steps:
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
cp -r ../poc/device/agent/config/* .
```

4. Change the params as per your need in these config.yaml and capabilities.yaml .

5. Install the chart in the namespace:
```bash
helm install device-agent . --namespace device-agent
```