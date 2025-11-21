##### [Back To Main](../README.md)
## 🚀 How to deploy Sandbox

 **1. 3-VM Architecture**: You can setup the Code First Sandbox using 3 VMs on a single host, where one VM is for WFM, one for a K3s cluster as a single node, and one more for a single node docker compose device. The Sandbox is tested with non cloud environment and all 3 VMs must have static IPs with connectivity between them.

   1. **WFM-VM**: WFM setup has been done using Symphony, Harbor and Gogs. Also runs observability stack(Jaeger, Prometheus, Grafana and Loki)
   2. **K3s-Device-VM**: Runs device-agent as k3s pod. Also runs OTEL collector, promtail and workloads.
   3. **Docker-compose-Device-VM**: Runs device-agent as docker container. Also runs OTEL collector, promtail and workloads.
  

 **2. VM Environment**: Configuration details for each VM. This size might vary based on the number of workloads to be deployed on device and actual load post deployment of workloads. Below is for stable workload validation in development environment.

    | VM Type                | OS            | VM Size                    |
    |------------------------|---------------|----------------------------| 
    | WFM                    | Ubuntu/Debian | (8 vCPU, 16 GB RAM, 100 GB)|
    | K3s Device             | Ubuntu/Debian | (8 vCPU, 16 GB RAM, 50 GB) |
    | Docker-Compose Device  | Ubuntu/Debian | (8 vCPU, 16 GB RAM, 50 GB) |

  **Note:** Network configuration for the VMs should use the host-network, with static IPs assigned to the VMs.


 
**3. Deployment Configurations**:
In order to deploy the Sandbox, clone the [MARGO repository](https://github.com/margo/dev-repo) and run the following [Automation Scripts](https://github.com/margo/dev-repo/tree/main/pipeline).       

 **3.1** Deploy containerized instance of Symphony API on WFM VM.

  ```bash   
   # Choose option 3: Symphony: Start
    sudo -E bash ./wfm.sh  
  ```  
    
     
 **3.2** Generate certs on Device-VM.
 
  ```bash
    # This should be executed on device-agent 
    sudo -E bash device-agent.sh  # Choose Option 1:Install-prerequisites
   
    # Choose below options to generate RSA and ECDSA certificates once pre-requisites installed
    Option 11) create_device_rsa_certs
    Option 12) create_device_ecdsa_certs
  ```

 **3.3** Copy ca-cert.pem from WFM-VM to Device-VM.
 
   ```bash
    # ca-cert.pem is generated after the symphony-api is started on WFM-VM (Option 3 - Symphony: Start)    
    Locate the ca-cert.pem on the WFM-VM at : cd $HOME/symphony/api/certificates

    # Copy the ca-cert.pem file to the following destination paths on the Device VM:
    For Docker Compose–based Device-Agent: : $HOME/dev-repo/docker-compose/config
    For K3s-based Device-Agent: :  $HOME/certs

   ```
 
 **3.4** Deploy containerized instance of Device Agent.
    
   ```bash  
    # This should be run on Docker-Compose Device.
    sudo -E bash ./device-agent.sh   # Choose option 3: Device-agent-Start(docker-compose-device)
   
    # This should be run on K3s Device.
    sudo -E bash ./device-agent.sh   # Choose option 5: Device-agent-Start(k3s-device)

   ```
  
**4. Deployment Verification**:
  ```bash
  # Check WFM status
  docker logs -f symphony-api-container
  
  # Check Device Agent status
  sudo -E bash ./device-agent.sh  # Choose option 7: Device-agent-Status
    
  # Verify observability (if installed)
  Grafana: http://${WFM_IP}:32000 (admin/admin)
  Jaeger: http://${WFM_IP}:32500
  Prometheus: http://${WFM_IP}:30900
  ```

**5. Clean Deployment**:
  ```bash
  # On WFM
  sudo -E bash ./wfm.sh  # Option 4: Symphony Stop
  sudo -E bash ./wfm.sh  # Option 2: PreRequisites Cleanup
  sudo -E bash ./wfm.sh  # Option 6: ObservabilityStack Stop
  
  
  # On Device
  sudo -E bash ./device-agent.sh  # Option 4 or 6: Device-agent Stop
  sudo -E bash ./device-agent.sh  # Option 2: Uninstall-prerequisites
  sudo -E bash ./device-agent.sh  # Option 9: otel-collector-promtail-uninstallation
  sudo -E bash ./device-agent.sh  # Option 11: cleanup-residual
  ```