# Setting Up the Code First Sandbox - Simple Guide

## What You'll Need

**Three VMs (Virtual Machines):**
- **Main VM (WFM)**: 8 processors, 16GB memory, 100GB storage
- **Device VM 1 (K3s)**: 8 processors, 16GB memory, 50GB storage  
- **Device VM 2 (Docker)**: 8 processors, 16GB memory, 50GB storage

**Requirements:**
- Ubuntu or Debian operating system
- Internet connection
- GitHub account with access to MARGO repository
- GitHub access token - [How to generate](../pipeline/README.md#-prerequisites)
- All VMs must be able to talk to each other (same network with static IP addresses)

---

## Step 1: Get the Code

You need to download the MARGO code to all three VMs. Follow these steps on **each VM**:

1. **Open Terminal**
   - On your WFM VM, open the terminal/command line application

2. **Install Git (if not already installed)**
   ```bash
   sudo apt-get update
   sudo apt-get install git -y
   ```

3. **Create a workspace directory**
   ```bash
   mkdir -p $HOME/workspace
   cd $HOME/workspace
   ```

4. **Download the Repository**
   ```bash
   git clone https://github.com/margo/dev-repo.git
   ```
   
   When prompted, enter:
   - Your GitHub username
   - Your GitHub access token (not your password)

5. **Navigate to the Downloaded Folder**
   ```bash
   cd dev-repo
   ```

**Note:** Repeat these steps on all three VMs (WFM VM, K3s Device VM, and Docker Device VM).

**Important:** We're using `$HOME/workspace/dev-repo` instead of `$HOME/dev-repo` because the automation scripts will clone their own copies to `$HOME`. This keeps your working copy separate from the automated setup.

**Troubleshooting:**
- If you get "Permission denied" error, make sure your GitHub account has access to the MARGO repository
- If you get "Authentication failed" error, verify your GitHub access token is correct

---

## Step 2: Set Up Environment

On each VM, you need to configure environment variables (settings that tell the system where things are).

1. **Navigate to the pipeline folder**
   ```bash
   cd $HOME/workspace/dev-repo/pipeline
   ```

2. **Follow the detailed setup guide**
   
   Open and follow the [Environment Variables Setup Guide](../pipeline/README.md#step-1-environment-variables-setup)
   
   This will help you set up:
   - GitHub credentials
   - VM IP addresses
   - Network settings
   - Other required configurations

**Important:** Complete this step on all three VMs before proceeding.

---

## Step 3: Build Everything

### On the WFM VM:

1. **Navigate to the pipeline folder**
   ```bash
   cd $HOME/workspace/dev-repo/pipeline
   ```

2. **Install Basic Tools**
   ```bash
   sudo -E bash ./wfm.sh
   ```
   - A menu will appear
   - Type `1` and press Enter
   - Choose: `Option 1: PreRequisites Setup`
   
   This installs everything needed like Redis, Docker, Helm, and other tools. This may take 10-15 minutes.

3. **Set Up Storage and Code Repository**
   
   This happens automatically in Step 2 - it creates:
   - **Harbor**: For storing application container images and Helm charts as OCI-compliant manifests
   - **Gogs**: For storing application vendors' application packages

4. **Start the Workload Fleet Manager**
   ```bash
   sudo -E bash ./wfm.sh
   ```
   - Type `3` and press Enter
   - Choose: `Option 3: Symphony Start`
   
   This starts the Workload Fleet Manager service.

5. **Optional: Add Monitoring Tools**
   ```bash
   sudo -E bash ./wfm.sh
   ```
   - Type `5` and press Enter
   - Choose: `Option 5: ObservabilityStack Start`
   
   This adds tools to monitor your system's performance.

### On Each Device VM:

1. **Navigate to the pipeline folder**
   ```bash
   cd $HOME/workspace/dev-repo/pipeline
   ```

2. **Install Basic Tools**
   ```bash
   sudo -E bash ./device-agent.sh
   ```
   - Type `1` and press Enter
   - Choose: `Option 1: Install-prerequisites`
   
   This may take 10-15 minutes.

3. **Create Security Certificates**
   ```bash
   sudo -E bash device-agent.sh
   ```
   - First, type `11` and press Enter to choose: `Option 11: create_device_rsa_certs`
   - Then run the command again and type `12` and press Enter to choose: `Option 12: create_device_ecdsa_certs`
   
   These certificates allow secure communication between VMs and are automatically saved in `$HOME/certs` directory.

---

## Step 4: Deploy (Connect Everything)

### Copy Security File Between VMs

You need to copy a security file from the WFM VM to each Device VM.

**On the WFM VM:**

1. **Find your WFM VM's IP address**
   ```bash
   hostname -I
   ```
   Write down the first IP address shown.

2. **Locate the certificate file**
   ```bash
   cd $HOME/symphony/api/certificates
   ls -la ca-cert.pem
   ```
   You should see the `ca-cert.pem` file.

**Copy to Docker Device VM:**

3. **On the Docker Device VM, create the destination folder**
   ```bash
   mkdir -p $HOME/dev-repo/docker-compose/config
   ```

4. **Copy the file from WFM VM to Docker Device VM**
   
   Option A - Using SCP (from Docker Device VM):
   ```bash
   scp username@WFM-VM-IP:$HOME/symphony/api/certificates/ca-cert.pem $HOME/dev-repo/docker-compose/config/
   ```
   Replace `username` with your WFM VM username and `WFM-VM-IP` with the IP address from step 1.
   
   Option B - Manual copy:
   - Open the file on WFM VM and copy its contents
   - Create the file on Docker Device VM and paste the contents

**Copy to K3s Device VM:**

5. **Copy the file from WFM VM to K3s Device VM**
   
   Option A - Using SCP (from K3s Device VM):
   ```bash
   scp username@WFM-VM-IP:$HOME/symphony/api/certificates/ca-cert.pem $HOME/certs/
   ```
   Replace `username` with your WFM VM username and `WFM-VM-IP` with the IP address from step 1.
   
   Option B - Manual copy:
   - Open the file on WFM VM and copy its contents
   - Create the file `ca-cert.pem` in `$HOME/certs/` on K3s Device VM and paste the contents

**Note:** The `$HOME/certs` directory was automatically created when you generated the security certificates in Step 3.

### Start Device Services

**On Docker Device VM:**

1. **Navigate to the pipeline folder**
   ```bash
   cd $HOME/workspace/dev-repo/pipeline
   ```

2. **Start the device agent**
   ```bash
   sudo -E bash ./device-agent.sh
   ```
   - Type `3` and press Enter
   - Choose: `Option 3: Device-agent-Start(docker-compose-device)`

**On K3s Device VM:**

1. **Navigate to the pipeline folder**
   ```bash
   cd $HOME/workspace/dev-repo/pipeline
   ```

2. **Start the device agent**
   ```bash
   sudo -E bash ./device-agent.sh
   ```
   - Type `5` and press Enter
   - Choose: `Option 5: Device-agent-Start(k3s-device)`

### Optional: Add Monitoring to Devices

On each Device VM:
```bash
cd $HOME/workspace/dev-repo/pipeline
sudo -E bash ./device-agent.sh
```
- Type `8` and press Enter
- Choose: `Option 8: otel-collector-promtail-installation`

---

## Step 5: Run and Use

### Check Everything is Working

**On WFM VM:**

1. **Check the Workload Fleet Manager logs**
   ```bash
   docker logs -f symphony-api-container
   ```
   You should see log messages indicating the service is running. Press `Ctrl+C` to exit.

**On Device VMs:**

1. **Navigate to the pipeline folder**
   ```bash
   cd $HOME/workspace/dev-repo/pipeline
   ```

2. **Check device status**
   ```bash
   sudo -E bash ./device-agent.sh
   ```
   - Type `7` and press Enter
   - Choose: `Option 7: Device-agent-Status`

3. **View device logs**

   **For K3s Device VM:**
   ```bash
   # First, find the pod name
   kubectl get pods -n default | grep device-agent
   
   # Then view the logs (replace <pod-name> with actual pod name from above)
   kubectl logs -f <pod-name> -n default
   ```
   Example: `kubectl logs -f device-agent-deploy-7d8f9c5b6-xyz12 -n default`
   
   Press `Ctrl+C` to exit the logs.

   **For Docker Device VM:**
   ```bash
   # View the logs
   docker logs -f device-agent
   ```
   Press `Ctrl+C` to exit the logs.

### Use the EasyCLI

On the WFM VM:

1. **Navigate to the pipeline folder**
   ```bash
   cd $HOME/workspace/dev-repo/pipeline
   ```

2. **Make the script executable and run it**
   ```bash
   chmod +x wfm-cli.sh
   sudo -E bash ./wfm-cli.sh
   ```

3. **Use the interactive menu**
   
   This opens a menu where you can:
   - See all your applications (packages)
   - See all your devices
   - Upload new applications
   - Deploy applications to devices
   - Remove applications
   
   Simply type the number of the option you want and press Enter.

### View Monitoring (if installed)

To view the monitoring dashboards, you need your WFM VM's IP address.

1. **Find your WFM VM's IP address**
   ```bash
   hostname -I
   ```
   Write down the first IP address shown (for example: 192.168.1.100).

2. **Open your web browser and visit:**
   
   Replace `[WFM-VM-IP]` with your actual IP address from step 1.
   
   - **Grafana** (Charts and Graphs): `http://[WFM-VM-IP]:32000`
     - Username: `admin`
     - Password: `admin`
   
   - **Jaeger** (Performance Tracking): `http://[WFM-VM-IP]:32500`
   
   - **Prometheus** (Metrics): `http://[WFM-VM-IP]:30900`
   
   **Example:** If your WFM VM IP is 192.168.1.100, you would visit:
   - Grafana: `http://192.168.1.100:32000`
   - Jaeger: `http://192.168.1.100:32500`
   - Prometheus: `http://192.168.1.100:30900`

---

## Cleaning Up (Starting Fresh)

If you want to remove everything and start over:

### On WFM VM:

1. **Navigate to the pipeline folder**
   ```bash
   cd $HOME/workspace/dev-repo/pipeline
   ```

2. **Stop and clean up services**
   ```bash
   sudo -E bash ./wfm.sh  # Type 4 and press Enter - Option 4: Symphony Stop
   sudo -E bash ./wfm.sh  # Type 2 and press Enter - Option 2: PreRequisites Cleanup
   sudo -E bash ./wfm.sh  # Type 6 and press Enter - Option 6: ObservabilityStack Stop
   ```

### On Device VMs:

1. **Navigate to the pipeline folder**
   ```bash
   cd $HOME/workspace/dev-repo/pipeline
   ```

2. **Stop and clean up services**
   ```bash
   sudo -E bash ./device-agent.sh  # Type 4 (Docker) or 6 (K3s) - Device-agent Stop
   sudo -E bash ./device-agent.sh  # Type 2 - Uninstall-prerequisites
   sudo -E bash ./device-agent.sh  # Type 9 - otel-collector-promtail-uninstallation
   sudo -E bash ./device-agent.sh  # Type 10 - cleanup-residual
   ```

---

## Quick Summary

**The setup process in simple terms:**

1. **Build**: Install tools and start services on all VMs
   - WFM VM: Installs management tools and starts the Workload Fleet Manager
   - Device VMs: Installs device software and creates security certificates

2. **Deploy**: Connect devices to the WFM VM using security certificates
   - Copy the security file from WFM VM to each Device VM
   - Start the device services

3. **Run**: Use the EasyCLI to manage applications on your devices
   - Use the menu-driven EasyCLI tool to deploy applications
   - Monitor everything through web dashboards

**Sample Applications Included:**
- **Nextcloud**: File sharing and collaboration platform
- **Nginx**: Web server
- **Custom OTEL**: Monitoring application that demonstrates telemetry capabilities

These applications are pre-loaded and ready to deploy to your devices for testing.

---

## Need Help?

If something doesn't work:
1. Check that all VMs can communicate with each other (ping test)
2. Verify environment variables are set correctly
3. Make sure the ca-cert.pem file was copied correctly
4. Check the logs using the commands in "Check Everything is Working" section

