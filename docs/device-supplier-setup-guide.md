# Device Supplier Conformance — Setup and Execution Guide

**Persona:** Device Supplier  
**Specification baseline:** Margo `1.0.0-rc.3`  
**Audience:** Device suppliers, WFM Client engineers, Margo adopters

This guide describes how to verify an Edge Compute Device — more precisely the
Workload Fleet Management Client (WFM Client) it hosts — for conformance with
the Margo specification using the Margo Conformance Test Toolkit (CTT). It
covers the prerequisites, identity provisioning, test execution and the
conformance report.

## Overview

In this persona, the WFM Client of the device is the system under test. The CTT
acts as the WFM: it runs a mock WFM that exposes the
Margo Management Interface, and checks every request of the device for
conformance with the specification. Every check is traced to a conformance
requirement ID in the report.

For demonstration, the device agent of the Margo sandbox is used. A device
supplier is expected to bring their own device, against which conformance is
checked.

---

## Prerequisites

### Operating system

The CTT host must run Ubuntu 24.04, the operating system officially supported
by the CTT and used by the sandbox Setup Guide. Other Linux distributions are
not supported.

### Network connectivity

When the MIS, the CTT and the device run on different hosts, all hosts must be
able to reach each other (same network, static IP addresses). The device
connects to the mock WFM on port 3003 of the CTT host.

### Clone CTT repo

```bash
mkdir -p $HOME/workspace && cd $HOME/workspace
git clone https://github.com/margo/conformance_test_toolkit.git
cd conformance_test_toolkit
git checkout feature/multi-persona-conformance
```

### Dependencies

Check that the following tools are installed on the CTT host:

```bash
go version         # 1.21 or later
openssl version    # 1.1 or later
jq --version       # any version
```

---

## Phase 1 — Identity provisioning (MIAF)

**Centralized MIS:**
Centralized MIS is used to test this device-supplier persona. The MIS administrator issues the X.509-SVIDs for the CTT and the device, and the CTT CLI installs the ones
of the CTT.

### Step 1 — MIS deployment

The X.509-SVIDs of the CTT (mock WFM) and of the device must come from the same
MIS. This guide uses the sandbox MIS to generate the Root CAs and the
X.509-SVIDs of the mock WFM, of the CTT test client and of the device under test.

1. **Set up the MIS**

   Follow the [Build and Run MIS](https://github.com/margo/sandbox/blob/main/docs/setup-guide.md#build-and-run-mis)
   section of the Margo sandbox [Setup Guide](https://github.com/margo/sandbox/blob/main/docs/setup-guide.md),
   and its CLI steps to generate the root CA certs.


2. **Generate the SVIDs**

   For the CTT, generate two SVIDs:
   - one for the mock WFM: WFM ID `ctt-mock-wfm`
   - one for the CTT: WFM ID `ctt-mock-wfm`, WFM Client ID `ctt-client`
     (the CTT uses it to set the Desired State on the mock WFM and to read the device's requests during the run)

   For the device under test, generate one SVID:
   - WFM ID `ctt-mock-wfm`, WFM Client ID of the device, for example `vendor-device-1`

### Step 2 — Device deployment

**Sandbox device** - 
Install the device-agent as per the setup guide - [On Each Device VM](https://github.com/margo/sandbox/blob/main/docs/setup-guide.md#on-each-device-vm) (it is started in Step 7), with these inputs so
that it connects to the mock WFM instead of Symphony:

| Step of the sandbox Setup Guide | Input for the CTT |
|---|---|
| Copy Security Files | Use `x509svid-ctt-mock-wfm-vendor-device-1` in place of `x509svid-wfm-docker-client` (the folders in `$HOME/mis-deployment` are owned by root: copy with `sudo`) |
| Environment (`device-agent.env`) | `WFM_HOST` = address of the CTT host (not `localhost`), `WFM_PORT=3003` |
| Add WFM SPIFFE ID in local allow list policy (`device-agent.sh` → 11) | `spiffe://margo.org/margo/wfm/ctt-mock-wfm` |
| `/etc/hosts` entry for the MIS | The address of the MIS host. `127.0.0.1` is not usable, also on the same host, because the device agent runs in a container |

> **Note:** If the device agent is already installed (for example for Symphony), place the
> new SVID in `$HOME/certs/compose-identity` and run the installation again
> (`cd $HOME/workspace/sandbox/scripts && sudo -E bash device-agent.sh docker install`).
> `start-docker` takes the SVID from the device agent's own configuration folder, which only
> the installation fills from `$HOME/certs`.

**Vendor's device:** 
From the same MIS, transfer the CA certificate and the SVID to the vendor's device.

| Item | File / value |
|---|---|
| X.509-SVID and key of the device | `$HOME/mis-deployment/x509svid-ctt-mock-wfm-<device-id>/payload-cert.pem`, `payload-key.pem` |
| Root CA certificate of the MIS (trust anchor) | `$HOME/mis-deployment/certs/ca.crt` |
| WFM SPIFFE ID to authorize on the device | `spiffe://margo.org/margo/wfm/ctt-mock-wfm` |
| Management Interface URL to connect to | `https://<ctt-host-ip>:3003/v1alpha2/margo` |

> **Note:** A vendor's device that retrieves the Trust Bundle from the sandbox MIS connects to
> `https://mis.margo.org:9443` (the name set by `EXPOSED_MIS_HOST` in the sandbox `mis.env`).
> On the device, add `<ip-of-the-mis-host> mis.margo.org` to `/etc/hosts` and give the device
> the MIS HTTPS CA (`$HOME/mis-deployment/certs/https-ca.crt`). Not needed when the device is
> given the Root CA certificate above by configuration.

### Step 3 — Transfer the CA certs and SVIDS to CTT Host

Only needed when the MIS runs on a different host than the CTT.
If the MIS runs on the same host as the CTT, the certs and SVIDs are generated in
`$HOME/mis-deployment`; skip this step and go to Step 4.

```bash
# On the MIS host — stage the files in one folder
cd $HOME/mis-deployment           # the folder that contains the x509svid-* folders
mkdir -p ~/ctt-certs
sudo cp x509svid-ctt-mock-wfm/payload-cert.pem            ~/ctt-certs/wfm-svid-cert.pem
sudo cp x509svid-ctt-mock-wfm/payload-key.pem             ~/ctt-certs/wfm-svid-key.pem
sudo cp x509svid-ctt-mock-wfm-ctt-client/payload-cert.pem ~/ctt-certs/client-svid-cert.pem
sudo cp x509svid-ctt-mock-wfm-ctt-client/payload-key.pem  ~/ctt-certs/client-svid-key.pem
sudo cp certs/ca.crt                                      ~/ctt-certs/mis-ca.crt
sudo chown -R $USER:$USER ~/ctt-certs

# Transfer the folder to the CTT host
scp -r ~/ctt-certs <user>@<ctt-host>:~/
```

### Step 4 — Add the X.509-SVIDs and Root CA certificate to the CTT

```bash
cd $HOME/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
# Select: 2. Device Supplier → 1. Setup Identity
#
# When prompted:
#   Folder containing the SVIDs [$HOME/mis-deployment]:
#       MIS on the same host:      press Enter
#       MIS on a different host:   ~/ctt-certs
#   Select the mock WFM SVID:
#       the number of x509svid-ctt-mock-wfm (not asked for ~/ctt-certs)
#   Select the WFM Client SVID:
#       the number of x509svid-ctt-mock-wfm-ctt-client (not asked for ~/ctt-certs)
```

All files are installed in `ctt-runner/device-supplier/certs/`.

**Verify:**

```bash
cd $HOME/workspace/conformance_test_toolkit/ctt-runner/device-supplier/certs
openssl verify -CAfile svid-ca.pem miaf-server-cert.pem svid-cert.pem
# → miaf-server-cert.pem: OK
# → svid-cert.pem: OK
```

> **Note:** The CTT needs no SPIFFE ID configuration. The mock WFM reads its own SPIFFE ID from
> its X.509-SVID and accepts every WFM Client of the form `<mock WFM SPIFFE ID>/client/<wfm-client-id>`,
> as the Margo WFM Identity Profile defines. Only the device must authorize the SPIFFE ID of the
> mock WFM (Step 2).

### Step 5 — Start the mock WFM

> **Note:** Perform a device cleanup always before starting the mock WFM
> ([Cleanup](#cleanup-after-a-test-run)). Start the device only after the mock WFM (Step 7), because the
> device reports its Device Capabilities when it starts.

```bash
cd $HOME/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
# Select: 2. Device Supplier → 2. Start Mock WFM Server
```

The CLI builds the mock WFM and the test runner (about one minute for the first
build) and prints the values a device requires:

```
  Management Interface : https://<ctt-host-ip>:3003/v1alpha2/margo
  WFM SPIFFE ID        : spiffe://margo.org/margo/wfm/ctt-mock-wfm
  Trust anchor (MIS)   : .../ctt-runner/device-supplier/certs/svid-ca.pem
  Health / telemetry   : https://<ctt-host-ip>:3001/health
  Server log           : /tmp/wfm-server.log
```

The Management Interface address is the one a device uses to reach the CTT host. If the
device reaches the host by a different address (for example at a PlugFest), start the CLI
with that address, e.g. `CTT_HOST_ADDRESS=192.168.10.20 bash ctt-runner/ctt-start.sh`.

The mock WFM continues to run after the menu is left. Option 4 stops it.

---

## Phase 2 — Run the conformance suite

Steps 6 and 7 must be completed before starting the test-scenario execution( For  CTT `core` group is used, `core` group has the collection of all the requirement based scenarios). Before starting a fresh run, clean
up both the mock WFM and the device (WFM Client) as described in Sandbox setup-guide section - 
[Cleanup](#cleanup-after-a-test-run).

### Step 6 — Run the Sanity

With the mock WFM running:

```bash
cd $HOME/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
# Select: 2. Device Supplier → 3. Run Tests → 8) self-check
# Press Enter at both URL prompts
```

`self-check` runs the test client of the CTT against the mock WFM and makes no
statement about a device.

### Step 7 — start the device and verify mock-WFM logs

**Sandbox device agent:** start the device agent as described in the sandbox Setup Guide,
section [Start Device Services](https://github.com/margo/sandbox/blob/main/docs/setup-guide.md#start-device-services).
This guide uses the Docker device ("On Docker Device VM"):

```bash
cd $HOME/workspace/sandbox/scripts
sudo -E bash device-agent.sh docker
# Select: 3) WFM-Client-Start(docker-compose-device)
# → workload-fleet-management-client docker-container started
```

On the CTT host, watch the requests of the device:

```bash
tail -f /tmp/wfm-server.log
# → [Router] PUT  /v1alpha2/margo/api/v1/capabilities/<device id> ... → 201 (200 if the device reported before)
# → [Router] GET  /v1alpha2/margo/api/v1/deployments ... → 200
# → [Router] POST /v1alpha2/margo/api/v1/deployments/<deployment id>/status ... → 200
```

> **Note:**
> Vendor's device: start the device as described by the vendor, and check that
> its requests appear in `/tmp/wfm-server.log`.

> **Note:** For the `observability` group, configure the OpenTelemetry Collector of the
> device before the run, as described in [Observability (OpenTelemetry)](#observability-opentelemetry).
> Without it, the observability steps are reported as `NOT RUN`.

### Step 8 — Launch the runner

```bash
cd $HOME/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
```

An interactive menu is displayed. Select:

```
2. Device Supplier
```

Then:

```
3. Run Tests              (select group)
```

Then select the group by its number in the list. For the full device
conformance test set:

```
1) core
```

A group is a named set of tests of the suite. The report states the group that
was executed.

| Group | Device required | Scope | Typical use |
|---|---|---|---|
| `core` | yes | All groups below that require a device | Conformance result of a device |
| `management-interface` | yes | Device Capabilities, Desired State, retrieval, Deployment Status, a deployment added and withdrawn | Work on the Management Interface client |
| `manifest-integrity` | yes | State Manifests and content a device MUST reject, one it MUST accept, state retained across a restart | Work on manifest validation |
| `identity` | yes | X.509-SVID of the device, rejection of a WFM the MIS did not certify, SVID rotation | Work on MIAF |
| `observability` | yes | The OpenTelemetry collector of the device publishes telemetry to the CTT | When a collector is configured ([Observability](#observability-opentelemetry); `NOT RUN` otherwise) |
| `self-check` | no | The test client of the CTT against the mock WFM: every API call, its errors, the State Manifests a device must reject | Before a device is connected (Step 6) |
| `quick-check` | no | Device Capabilities, State Manifest, deployment, Deployment Bundle, Deployment Status (11 steps) | Verification of the URLs and the identity setup |
| `random-order` | no | The `self-check` lifecycle scenarios in a different random order on every run | Demonstrates that no scenario depends on another |

### Step 9 — Enter the mock WFM endpoints

The runner prompts for two URLs:

```
Enter Mock WFM Server URL [https://localhost:3001/v1alpha2/margo]:
```

Press Enter. This is the health and telemetry port of the mock WFM.

```
Enter Management Interface (mTLS) URL [https://localhost:3003/v1alpha2/margo]:
```

Press Enter. This is the Margo Management Interface of the mock WFM, which the
device uses.

The execution starts. The runner confirms the URLs and the mock WFM, then runs
the scenarios one after the other:

```
▶️  Running Device Conformance Tests
   Mock WFM (health):           https://localhost:3001/v1alpha2/margo
   Management Interface (mTLS): https://localhost:3003/v1alpha2/margo
...
✅ WFM Server is ready

▶ Running Scenario: Real device — identity (rd-01-identity)
  → Step: The device connects over mTLS with its X.509-SVID and the mock WFM accepts it
    ✅ PASS - seen: POST /api/v1/deployments/<deployment id>/status → 200
```


### Device restart during the run to verify the scenario "state survives a restart"

The scenario *Real device — state survives a restart* (`rd-11-restart`, in the
`core` group selected for the test-scenario execution, and in `manifest-integrity`) verifies that the device retains its state
across a restart (MARGO-DEV-MANAGEMENTINTERFACE-015, MARGO-DEV-MANAGEMENTINTERFACE-017):

- after the restart, the device sends the ETag of the State Manifest it adopted
  before the restart;
- after the restart, the device still rejects a State Manifest whose version does
  not increase.

The CTT cannot restart the device; the operator restarts it when prompted. The same
restart can be used for the [SVID Rotation scenario](#svid-rotation-scenario-optional)
(`rd-12-svid-rotation`): install the reissued X.509-SVID before the device is started
again.

When this line appears, restart the device within 3 minutes:

```
👉 Restart the device under test now (within 3 minutes). To also test SVID rotation, replace its SVID with a re-issued one for the same SPIFFE ID before starting it.
```

Sandbox device agent (from `$HOME/workspace/sandbox/scripts`):

```bash
sudo -E bash device-agent.sh docker stop-docker     # answer n to "delete data folder"
sudo -E bash device-agent.sh docker start-docker
```

If the device is not restarted, the steps of "State is retained across a
restart" are reported as `NOT RUN`, not as failures.

### Step 10 — The HTML report

The report is written to:

```
ctt-runner/reports/device-supplier/conformance-report-<timestamp>.html
ctt-runner/reports/device-supplier/conformance-report-<timestamp>_signed.pdf
ctt-runner/reports/device-supplier/ca_cert.pem
```

The HTML report can be opened in any browser. It contains:

- A Requirements Coverage table with one row per requirement ID: `PASS` (at
  least one step that verifies it passed and none failed), `FAIL` (any step
  that verifies it failed) or `NOT RUN` (none of its steps was executed)
- A Scenario Summary table
- A Step Details table with result, method, endpoint, expected and actual HTTP
  status, and the reason for a failure

A step reported as `NOT RUN` was not executed because its precondition is
absent; it is neither a pass nor a failure. This applies to the SVID Rotation
scenario (below), to the restart scenario when the device was not restarted,
and to checks that do not apply to the device (the reason is stated).

The signed PDF is the same report, digitally signed at the end of every run;
`ca_cert.pem` is the certificate required to verify the signature. The first
run installs the PDF tools (requires `sudo` and internet access). The signed
PDF together with `ca_cert.pem` is the artifact submitted as evidence of
conformance.

Verify a signed report:

```bash
cd $HOME/workspace/conformance_test_toolkit
ctt-runner/reports/scripts/.venv/bin/python ctt-runner/reports/scripts/pdf_verify.py \
  ctt-runner/reports/device-supplier/conformance-report-<timestamp>_signed.pdf \
  ctt-runner/reports/device-supplier/ca_cert.pem
# RESULT    : signature OK
```

### SVID Rotation scenario (optional)

This scenario verifies that the device continues to operate after its WFM
Client X.509-SVID is reissued for the same SPIFFE ID. It is `NOT RUN` until a
reissued X.509-SVID is installed on the device. To run it:

1. Reissue the WFM Client SVID of the device for the **same** WFM ID and WFM
   Client ID (sandbox: `cd $HOME/mis-deployment`, then
   `sudo -E bash $HOME/workspace/sandbox/scripts/mis.sh` → `6) Generate SVID` →
   principal `2`, WFM ID `ctt-mock-wfm`, WFM Client ID `vendor-device-1`).
2. Start the test-scenario execution with the `core` group selected. When the restart line appears, install the reissued X.509-SVID on
   the device before it is started again. For the sandbox device agent, the
   procedure is described in
   [Renewal and Reissuance](https://github.com/margo/sandbox/blob/main/docs/identity-lifecycle.md#renewal-and-reissuance)
   of the sandbox Identity Lifecycle guide.

### Observability (OpenTelemetry)

The `observability` group verifies that the OpenTelemetry Collector of the
device exports telemetry to the CTT. Before the run, configure the exporters of
the collector with the following endpoints of the mock WFM:

| Telemetry | Protocol | Endpoint |
|---|---|---|
| Traces, metrics, logs | OTLP over HTTP | `http://<ctt-host-ip>:4318` |
| Metrics | Prometheus Remote Write | `http://<ctt-host-ip>:9090/api/v1/write` |

`<ctt-host-ip>` is the address of the Management Interface printed in Step 5.
OTLP over gRPC (port 4317) is not supported. If the collector runs on the CTT
host, its OTLP receiver must use a port other than 4318. Change the collector before
the mock WFM is started (Step 5); otherwise the mock WFM cannot open port 4318 and the
OTLP step is reported as `NOT RUN`.

Vendor's device: set these endpoints as exporters of its collector.

Sandbox device agent (collector installed with `device-agent.sh` option 8):

```bash
cd $HOME/sandbox/scripts/observability
sudo cp otel-collector-config.yml otel-collector-config.yml.orig   # keep the default
sudo nano otel-collector-config.yml
```

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:14318      # only if the device runs on the CTT host
      grpc:
        endpoint: 0.0.0.0:14317      # only if the device runs on the CTT host
exporters:
  otlphttp/ctt:                      # replaces otlp/jaeger
    endpoint: http://<ctt-host-ip>:4318
  prometheusremotewrite:
    endpoint: http://<ctt-host-ip>:9090/api/v1/write
service:
  pipelines:
    traces:
      exporters: [otlphttp/ctt, debug]
    metrics:
      exporters: [otlphttp/ctt, prometheusremotewrite, debug]
```

```bash
sudo docker restart otel-collector
curl -sk https://localhost:3001/health/telemetry     # on the CTT host
# → {"status":"receiving", ... "otlp_total":N, ... "prom_writes":N}
```

After the run, restore the default configuration:

```bash
cd $HOME/sandbox/scripts/observability
sudo cp otel-collector-config.yml.orig otel-collector-config.yml
sudo docker restart otel-collector
```

---

## Cleanup after a test run

Stop the mock WFM:

```bash
cd $HOME/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
# Select: 2. Device Supplier → 4. Stop Mock WFM Server
```

Clear the state of the device (sandbox device agent):

```bash
cd $HOME/workspace/sandbox/scripts
sudo -E bash device-agent.sh docker stop-docker      # answer y to "delete data folder"
```

Vendor's device: perform the device cleanup as described by the vendor (stop the device
and delete the deployments and data it stored).

The mock WFM retains every request received since its start, and a device
retains the deployments and the manifest version it adopted. The next run then
starts with Step 5 (start the mock WFM) and Step 7 (start the device).

To return the sandbox device agent to Symphony, restore `WFM_HOST` and
`WFM_PORT` in `device-agent.env`, set the local allow list policy to the WFM
SPIFFE ID of Symphony, install the WFM Client SVID issued for the WFM ID of
Symphony, and restart the device agent.

---

## Quick-reference checklist

```
Phase 1 — Identity provisioning (once; repeat when an X.509-SVID is reissued)

  Sandbox device agent (sandbox MIS):
  □ Step 1: MIS installed and Root CAs generated   (sandbox Setup Guide: Build and Run MIS)
            mis.sh → 3) Factory Bootstrap: Generate Root CAs, 4) Margo Identity Service: Install
            mis.sh → 6) Generate SVID, three times (WFM ID ctt-mock-wfm; WFM Client IDs ctt-client and vendor-device-1)
  □ Step 2: device agent installed                 (sandbox Setup Guide: On Each Device VM)
            SVID x509svid-ctt-mock-wfm-vendor-device-1, WFM_HOST = CTT host, WFM_PORT=3003,
            device-agent.sh docker → 11) Manage SPIFFE ID allowlist → 1) Set SPIFFE ID:
            spiffe://margo.org/margo/wfm/ctt-mock-wfm
  □ Step 3: only if the MIS runs on another host: copy the five files to ~/ctt-certs on the CTT host
  □ Step 4: ctt-start.sh → 2. Device Supplier → 1. Setup Identity
            (Enter for $HOME/mis-deployment, or ~/ctt-certs after a transfer; then select x509svid-ctt-mock-wfm and x509svid-ctt-mock-wfm-ctt-client)
            openssl verify -CAfile svid-ca.pem miaf-server-cert.pem svid-cert.pem → OK
  □ Before Step 5 (every run): device cleanup (device-agent.sh docker stop-docker, answer y to "delete data folder")
  □ Step 5: ctt-start.sh → 2. Device Supplier → 2. Start Mock WFM Server

  Vendor's device:
  □ Step 1: The MIS issues the device a WFM Client SVID: spiffe://<trust-domain>/margo/wfm/ctt-mock-wfm/client/<wfm-client-id>
  □ Step 2: Device configured: its X.509-SVID and key, Management Interface URL (port 3003),
            WFM SPIFFE ID authorized, Trust Bundle of the MIS
  □ Before Step 5 (every run): device cleanup as described by the vendor

Phase 2 — Run the conformance suite (every run)

  □ Step 6: ctt-start.sh → 2. Device Supplier → 3. Run Tests → 8) self-check; Enter at both URL prompts (90 passed, 2 not run)
  □ Optional, before Step 7: OpenTelemetry Collector of the device exports to <ctt-host-ip>:4318 and :9090 (Observability)
  □ Step 7: start the device (sandbox: device-agent.sh docker → 3) WFM-Client-Start(docker-compose-device));
            tail -f /tmp/wfm-server.log shows → 201, → 200
  □ Step 8: ctt-start.sh → 2. Device Supplier → 3. Run Tests → 1) core
  □ Step 9: Enter at both URL prompts; restart the device when requested (within 3 minutes)
  □ Step 10: Report:     ctt-runner/reports/device-supplier/conformance-report-<timestamp>.html
             Signed PDF: ctt-runner/reports/device-supplier/conformance-report-<timestamp>_signed.pdf (+ ca_cert.pem)
```

---


### Certificate expiry and renewal

| File in `ctt-runner/device-supplier/certs/` | Validity | Action on expiry |
|---|---|---|
| `miaf-server-cert.pem` — X.509-SVID of the mock WFM; `svid-cert.pem` — X.509-SVID of the test client | About 90 days (sandbox MIS default) | Generate new SVIDs and run Setup Identity again (Step 4) |
| `svid-ca.pem` — SVID Root CA certificate | Lifetime of the Root CA of the MIS | Run Setup Identity again (Step 4) |

Check the expiry date:

```bash
openssl x509 -in ctt-runner/device-supplier/certs/miaf-server-cert.pem -noout -enddate
openssl x509 -in ctt-runner/device-supplier/certs/svid-cert.pem -noout -enddate
```

### Troubleshooting

| Symptom | Likely cause | Resolution |
|---|---|---|
| `MIAF identity not found ... Please run 'Setup Identity' first` | Step 4 was not performed in this checkout | Run Setup Identity (Step 4) |
| Setup Identity: `... does not chain to the given SVID Root CA` | The X.509-SVIDs and the Root CA certificate originate from different MIS installations | Provide the folder (or Root CA certificate) of the MIS that issued the SVIDs. No file is changed by the failed attempt |
| Setup Identity: `Missing file: ...` | The folder does not contain all five files | Verify the folder; for a flat folder use the file names of Step 3 |
| `cannot use the WFM SVID: ... is not a WFM identity` | A WFM Client SVID was selected as WFM SVID | Run Setup Identity again and select the WFM SVID folder |
| `Failed to build mock server` or `Failed to build test runner` | Go is not installed, or no internet access for the first build | Install Go; the first build downloads the Go toolchain and modules |
| `Failed to start mock server` | Stated in the log lines printed with the error | Full log: `/tmp/wfm-server.log` |
| Every step fails: `server presented ..., expected the WFM ...` | The WFM Client SVID was issued for a different WFM ID than the WFM SVID | Issue the WFM Client SVID for the same WFM ID and run Setup Identity again |
| Every step fails: `x509: certificate signed by unknown authority` | The installed trust anchor is not the Root CA that issued the WFM SVID | Run Setup Identity again with the material of the correct MIS |
| `x509: certificate has expired` | An X.509-SVID has exceeded its validity | Generate new SVIDs and run Setup Identity again |
| Device: `TLS handshake error ... is not a client of this WFM` in `/tmp/wfm-server.log` | The X.509-SVID of the device belongs to a different WFM ID | Issue the device a WFM Client SVID for the WFM ID of the mock WFM (Step 1) |
| Device: `TLS handshake error ... unknown certificate authority` | The X.509-SVID of the device was issued by a different MIS | Use one MIS for the mock WFM and the device |
| Device: `[MIAF] answered 422 Semantic Error — ... \| <rule>: <reason>` | The request body of the device violates the specification | Deviation of the device; the rule and the reason follow the `\|` |
| Every step fails with `403` | The mTLS URL was set to port 3001 | Press Enter at the URL prompts to use the defaults (mTLS on port 3003) |
| Every step of a device group fails with `no request from any device was received` | The device does not reach the mock WFM, or does not authorize its WFM SPIFFE ID | Verify port 3003, the Management Interface URL and the local authorization policy of the device |
| Sandbox device agent continues to connect to Symphony | `WFM_HOST` and `WFM_PORT` in `device-agent.env` are unchanged | Set them to the CTT host and `3003`, then restart the device agent |
| Sandbox device agent rejects the mock WFM | The WFM SPIFFE ID of the mock WFM is not in its local allow list policy | `device-agent.sh` option 11 (Manage SPIFFE ID allowlist) |
| Sandbox device agent restarts repeatedly with `failed to retrieve margo spiffe trust bundle ... certificate signed by unknown authority` | Its HTTPS CA belongs to a different MIS | Copy the HTTPS CA of the correct MIS to the device agent and restart it ([Renewal and Reissuance](https://github.com/margo/sandbox/blob/main/docs/identity-lifecycle.md#renewal-and-reissuance)) |
| Sandbox device agent reports `potential rollback attack: new version ... < current version ...` | The mock WFM was restarted and serves a lower manifest version than the device agent adopted | Start the device agent with empty state |
| `Signed PDF not created` at the end of a run | Stated below the message. Typical causes: the repository folder is owned by root, or no internet access for the initial tool installation | Resolve the stated cause (ownership: `sudo chown -R $USER:$USER $HOME/workspace/conformance_test_toolkit`), then run the `pdf_ops.sh` command the run printed |
| Sandbox `otel-collector` restarts repeatedly with `listen tcp 0.0.0.0:4318: bind: address already in use` | The collector runs on the CTT host, where the mock WFM uses port 4318 | Move the OTLP receiver of the collector to another port ([Observability](#observability-opentelemetry)) |
| No report is written | The test runner stopped early | See `ctt-runner/reports/device-supplier/test-execution.log` |
