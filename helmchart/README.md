# Pre-requisites:
- Kubernetes runtime (k3s/k8s)
- Ensure that you have the container image for the device agent with you, if not you can build it using the following command(assuming that you cloned the entire dev-repo at one place). To build, please run the following command (you can use nordctl or other tools as per your preference):
```bash
cd ..
docker build -f poc/device/agent/Dockerfile . -t margo.org/device-agent:latest
docker save -o device-agent.tar margo.org/device-agent:latest
# use this command if on k8s cluster
ctr -n k8s.io image import device-agent.tar 
# use this command if on k3s cluster
k3s ctr -n k8s.io image import device-agent.tar 
cd helmchart

```

# Main steps:
1. Create a namespace
```bash
kubectl create namespace device-agent
```


2. Copy the config.yaml and capabilities.json files in this directory.
```bash
cp -r ../poc/device/agent/config/* .
```

3. Change the params as per your need in these config.yaml and capabilities.json files.



4. Install the chart in the namespace:
```bash
helm install device-agent . --namespace device-agent
```

5. Authentication Method:
This Helm chart uses ServiceAccount-based authentication to connect with the Kubernetes API server. The chart automatically creates:

ServiceAccount for the device-agent pod
ClusterRole with necessary permissions
ClusterRoleBinding to link ServiceAccount with permissions
The device-agent will authenticate using the ServiceAccount token automatically mounted by Kubernetes at /var/run/secrets/kubernetes.io/serviceaccount/token.

6. Verification:

```bash
# Check if pods are running
kubectl get pods -n device-agent

# Check ServiceAccount and RBAC resources
kubectl get serviceaccount,clusterrole,clusterrolebinding -n device-agent | grep device-agent

# Check logs
kubectl logs -n device-agent deployment/device-agent-device-agent-deploy

```
7. Cleanup:
```bash
# Uninstall Helm release
helm uninstall device-agent --namespace device-agent

# Clean up RBAC resources (if needed)
kubectl delete clusterrole device-agent-device-agent-role
kubectl delete clusterrolebinding device-agent-device-agent-binding

# Delete namespace
kubectl delete namespace device-agent

```