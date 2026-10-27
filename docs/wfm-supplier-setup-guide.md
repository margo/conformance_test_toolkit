# WFM Supplier Conformance — Setup and Execution Guide

**Persona:** WFM Supplier (Margo persona: Fleet Management Supplier)  
**Specification baseline:** Margo `1.0.0-rc.3`  
**Audience:** WFM suppliers, Margo adopters, integration engineers

This guide describes how to verify a Workload Fleet Manager (WFM) for
conformance with the Margo specification using the Margo Conformance Test
Toolkit (CTT). It covers the prerequisites, identity provisioning, test
execution and the conformance report.

## Overview

In this persona, the WFM is the system under test. The CTT acts as a mock device (WFM Client): it accesses the Margo Management
Interface exposed by the WFM and checks each response for conformance with the
specification. Every check is traced to a conformance requirement ID in the
report.

For demonstration, the Symphony WFM of the Margo sandbox is used. A WFM
supplier is expected to bring their own WFM, against which conformance is
checked.

---

## Prerequisites

### Operating system

The CTT host must run Ubuntu 24.04, the operating system officially supported
by the CTT and used by the sandbox Setup Guide. Other Linux distributions are
not supported.

### Network connectivity

When the MIS, the WFM and the CTT run on different hosts, all hosts must be
able to reach each other (same network, static IP addresses).

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
node --version     # 18 or later
openssl version    # 1.1 or later
jq --version       # any version
oras version       # 1.x
```

---

## Phase 1 — Identity provisioning (MIAF)

**Centralized MIS:** a centralized MIS is used. The MIS administrator issues
the X.509-SVID for the CTT, and the CTT CLI installs it.

### Step 1 — MIS deployment

The CTT's X.509-SVID must come from the same MIS that issued the X.509-SVID of the
WFM under test. This guide uses the sandbox MIS to generate the Root CAs and the
X.509-SVIDs of the WFM under test and of the CTT (WFM Client).

1. **Set up the MIS**

   Follow the [Build and Run MIS](https://github.com/margo/sandbox/blob/main/docs/setup-guide.md#build-and-run-mis)
   section of the Margo sandbox [Setup Guide](https://github.com/margo/sandbox/blob/main/docs/setup-guide.md),
   and its CLI steps to generate the root CA certs.

2. **Generate the SVIDs**

   Generate only two SVIDs:
   - one for the WFM: WFM ID `wfm`
   - one for the CTT: WFM ID `wfm`, WFM Client ID `ctt-client`

> **Note:** For a vendor's WFM, the CTT needs a WFM Client SVID with this SPIFFE ID,
> issued by the vendor's MIS:
> `spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/ctt-client`

> **Note:** A vendor's WFM that retrieves the Trust Bundle from the sandbox MIS connects to
> `https://mis.margo.org:9443` (the name set by `EXPOSED_MIS_HOST` in the sandbox `mis.env`).
> On the WFM host, add `<ip-of-the-mis-host> mis.margo.org` to `/etc/hosts` and give the WFM
> the MIS HTTPS CA (`$HOME/mis-deployment/certs/https-ca.crt`). Not needed when the WFM is
> given the Trust Bundle by configuration.



### Step 2 — Symphony deployment

Install Symphony as per the [Build and Run WFM(Symphony)](https://github.com/margo/sandbox/blob/main/docs/setup-guide.md#build-and-run-wfmsymphony) section of the same Setup Guide.
In its step "Add WFM Client SPIFFE IDs as authorised clients" (`wfm.sh` → 7), add the SPIFFE ID of the CTT:
`spiffe://margo.org/margo/wfm/wfm/client/ctt-client`

### Step 3 — Transfer the CA certs and SVIDS to CTT Host

Only needed when the MIS runs on a different host than the CTT.
If the MIS runs on the same host as the CTT, the certs and SVIDs are generated in
`$HOME/mis-deployment`; skip this step and go to Step 4.

```bash
# On the MIS host — stage the files in one folder
cd $HOME/mis-deployment           # the folder that contains the x509svid-* folders
mkdir -p ~/ctt-certs
sudo cp x509svid-<wfm-id>-<wfm-client-id>/payload-cert.pem ~/ctt-certs/client-svid-cert.pem
sudo cp x509svid-<wfm-id>-<wfm-client-id>/payload-key.pem  ~/ctt-certs/client-svid-key.pem
sudo cp certs/ca.crt                                       ~/ctt-certs/mis-ca.crt
sudo chown -R $USER:$USER ~/ctt-certs

# Transfer the folder to the CTT host
scp -r ~/ctt-certs <user>@<ctt-host>:~/
```

### Step 4 — Add the WFM Client X.509-SVID and Root CA certificate to the CTT

```bash
cd $HOME/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
# Select: 1. WFM Supplier → 1. Setup MIAF Identity → 1) MIS-issued identity
#
# When prompted:
#   Folder containing the client SVID [$HOME/mis-deployment]:
#       MIS on the same host:      press Enter
#       MIS on a different host:   ~/ctt-certs
#   Which one is the CTT's client SVID?:
#       the number of x509svid-wfm-ctt-client (not asked for ~/ctt-certs)
```

All files are installed in `ctt-runner/wfm-supplier/utils/fixtures/miaf/real/`.

**Verify:**

```bash
cd $HOME/workspace/conformance_test_toolkit/ctt-runner/wfm-supplier/utils/fixtures/miaf/real
openssl verify -CAfile trust-bundle-ca.pem client-svid-cert.pem
# → client-svid-cert.pem: OK
```

> **Note:** The CTT needs no SPIFFE ID configuration. It reads its own SPIFFE ID from its
> X.509-SVID and accepts only the WFM that this ID belongs to: for
> `spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/ctt-client` that is
> `spiffe://<trust-domain>/margo/wfm/<wfm-id>`. The SPIFFE ID of the CTT is added only on the
> WFM side (Step 2).

### Step 5 — Start the WFM (Symphony)

```bash
cd $HOME/workspace/sandbox/scripts
sudo -E bash wfm.sh
# Select: 3) Symphony: Start
```
> **Note:** 
> Vendor's WFM: Start the WFM as described by the vendor, and check that its
> Margo Management Interface is reachable from the CTT host.

After a few seconds, verify that the container remains running:

```bash
sudo docker ps --filter name=symphony-api-container --format "{{.Names}}\t{{.Status}}"
# Expected: symphony-api-container   Up N seconds
# If the status is "Exited", inspect the log: sudo docker logs symphony-api-container 2>&1 | tail -20
```

---

## Phase 2 — Run the conformance suite

Steps 6 and 7 must be completed before the tests are started.

### Step 6 — Application Package in the Application Registry

**Sandbox Symphony:** the `core` group (selected for the test-scenario execution) uses one Application Package in the sandbox Harbor registry,
for the Application Registry checks and for the deployment the WFM delivers as Desired State.

```bash
sudo chown -R $USER:$USER $HOME/.docker     # once; the sandbox scripts leave it owned by root
cd $HOME/workspace/conformance_test_toolkit
bash ctt-runner/wfm-supplier/utils/fixtures/margo-ctt-package/push.sh
# → Pushed [registry] harbor.machine:8443/library/margo-ctt-hello-world:1.0.0
```

**Vendor's WFM:** the `core` group (selected for the test-scenario execution) also verifies the Application Registry. Before the CLI is
started, name one Application Package in the registry of the
supplier:

```bash
oras login <registry-host>                          # only if the registry requires credentials
export REGISTRY_REF=<registry-host>/<repository>:<tag>

# Example: the Application Package used for the test-scenario execution of the core group against the sandbox
oras login harbor.machine:8443 -u admin -p Harbor12345
export REGISTRY_REF=harbor.machine:8443/library/margo-ctt-hello-world:1.0.0
```

The CTT host must trust the TLS certificate of the registry. If the registry
uses a private CA, add that CA to the trust store of the host first
(`sudo cp registry-ca.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates`).

### Step 7 — Assign the test deployment to the CTT

The `core` group and the `deployment-delivery` group, verify how the WFM delivers a
deployment. A deployment must therefore be assigned to the WFM Client identity
of the CTT. For the sandbox Symphony a helper performs this assignment; it
requires the identity installed in Step 4 and Step 6:

```bash
cd $HOME/workspace/conformance_test_toolkit
bash ctt-runner/wfm-supplier/utils/fixtures/provision-multi-component.sh
# → Check on the Management Interface (...): ok
# → Done. Multi-component deployment provisioned:
```

Run the helper again after a different WFM Client SVID is installed, after a
reboot of the host, and after Symphony is restarted or rebuilt. In those cases
Symphony may list no deployment in the State Manifest or fail the Deployment Bundle download; the helper detects
this and assigns the deployment again. Details are given in
[Multi-component deployment](#multi-component-deployment).

**Vendor's WFM:** Assign an application deployment to the WFM Client identity of the CTT. The requirements for that deployment are given in 
[Multi-component deployment](#multi-component-deployment).

### Step 8 — Launch the runner

```bash
cd $HOME/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
```

An interactive menu is displayed. Select:

```
1. WFM Supplier
```

Then:

```
2. Functional tests      (Group-based test management)
```

Then select the group by its number in the list. For the full WFM conformance
test set:

```
2) core
```

A group is a named set of tests of the suite. The report states the group that
was executed.

| Group | Scope | Typical use |
|---|---|---|
| `core` | All groups below | Conformance result |
| `quick-check` | mTLS authentication, Device Capabilities, Desired State, conditional request (6 steps) | Verification of the URL and the identity setup |
| `management-interface` | Margo Management Interface: Device Capabilities, Desired State, content negotiation, Deployment Status, ETag and cache headers | Work on the Management Interface |
| `deployment-delivery` | Delivery of a multi-component deployment: State Manifest URLs, ApplicationDeployment YAML, Deployment Bundle and their digests | Work on deployment delivery |
| `identity` | MIAF: mTLS handshake, rejected clients, SVID rotation | Work on MIAF |
| `application-registry` | Application Registry (OCI): manifest, media types, pulls, tags | Work on the registry |
| `observability` | A device that declares an OpenTelemetry collector is accepted | Work on observability |
| `postman-lifecycle`, `postman-error-handling` | Positive and negative requests authored in Postman | Examples for tests authored in Postman |

### Step 9 — Enter the WFM endpoints

The runner prompts for the WFM URL:

```
Enter WFM URL:
```

This is the base URL of the **Margo Management Interface** exposed by the WFM:
the address up to, but not including, `/api/v1`. It is the mTLS endpoint. For
the sandbox Symphony: `https://localhost:8084/v1alpha2/margo`

The execution starts. The runner confirms the group and the WFM URL, then runs
the scenarios one after the other:

```
🚀 Starting WFM Supplier Test Execution (Group Mode)
   Group: core
   WFM Server: https://localhost:8084/v1alpha2/margo
...
════════════════════════════════════════════════════════════════════════
 Margo WFM Conformance Test Runner
 Group: core (v1.0.0-rc.3)
 WFM:   https://localhost:8084/v1alpha2/margo
════════════════════════════════════════════════════════════════════════

────────────────────────────────────────────────────────────────────────
 SCENARIO 1 of <n>  ·  <scenario name>
────────────────────────────────────────────────────────────────────────

  [<step id>]  <step name>
   ▶  <method> <path>  [mTLS · X.509-SVID]
   ⚙  <requirement ID>
   ✓  PASS  <status>
```

### Multi-component deployment

If the selected group includes the `wfm-multi-component-deployment` scenario
(in the `core` group and in `deployment-delivery`), the runner detects it and states
what is required:

```
────────────────────────────────────────────────────────────────
ℹ️  This group tests deployment delivery. The WFM must have an application
ℹ️  deployment assigned to the CTT's client:
ℹ️      spiffe://margo.org/margo/wfm/wfm/client/ctt-client
ℹ️  with two components, one 'wait: true' and one 'wait: false'.
────────────────────────────────────────────────────────────────
```

Also `core` and `deployment-delivery` group, need a deployment in the WFM for the
CTT. If it was assigned before the run
([Step 7](#step-7--assign-the-test-deployment-to-the-ctt)), nothing has to be done
here.

If it is missing, the run prints the CTT's SPIFFE ID and waits up to 5 minutes.
Assign the deployment in the WFM during that time: two components, one with
`wait: true` and one with `wait: false` (example:
`ctt-runner/wfm-supplier/utils/fixtures/multi-component-app-deployment.yaml`).
Name the two components `wait-true-component` and `wait-false-component`, as in
the example: the CTT uses these names in the Deployment Status reports it sends.
If no deployment appears, those steps are reported as `NOT RUN`, not as
failures.

### Step 10 — The HTML report

The report is written to:

```
ctt-runner/reports/wfm-supplier/wfm-scenario-report-core_<timestamp>.html
ctt-runner/reports/wfm-supplier/wfm-scenario-report-core_<timestamp>_signed.pdf
ctt-runner/reports/wfm-supplier/ca_cert.pem
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
scenario (below) and to the multi-component deployment scenario when no
deployment is assigned.

The signed PDF is the same report, digitally signed at the end of every run;
`ca_cert.pem` is the certificate required to verify the signature. The first
run installs the PDF tools (requires `sudo` and internet access). The signed
PDF together with `ca_cert.pem` is the artifact submitted as evidence of
conformance.

Verify a signed report:

```bash
cd $HOME/workspace/conformance_test_toolkit
ctt-runner/reports/scripts/.venv/bin/python ctt-runner/reports/scripts/pdf_verify.py \
  ctt-runner/reports/wfm-supplier/wfm-scenario-report-core_<timestamp>_signed.pdf \
  ctt-runner/reports/wfm-supplier/ca_cert.pem
# RESULT    : signature OK
```

### SVID Rotation scenario (optional)

This scenario verifies that the WFM continues to accept the CTT after its WFM
Client X.509-SVID is reissued, without any change to the local authorization
policy. It requires two X.509-SVIDs for the same SPIFFE ID and is therefore
`NOT RUN` after the initial setup. To run it:

1. Complete Phase 1 and run the suite once with the first X.509-SVID.
2. Reissue the WFM Client SVID of the CTT for the **same** WFM ID and WFM
   Client ID (sandbox: `cd $HOME/mis-deployment`, then
   `sudo -E bash $HOME/workspace/sandbox/scripts/mis.sh` → `6) Generate SVID` →
   principal `2`, same IDs).
3. Run Setup MIAF Identity again (Step 4). The CLI installs the new
   X.509-SVID, retains the one it replaces and reports this.
4. Run the suite again. The scenario sends one request with the previous
   X.509-SVID and one with the new one; both must succeed.

### Observability (OpenTelemetry)

The `observability` group verifies that the WFM accepts Device Capabilities that
declare whether the device has an OpenTelemetry Collector (`otelCollector`
`true` or `false`), and rejects a value that is not a boolean. The CTT does not
receive telemetry from the WFM; no OpenTelemetry configuration is required for
this persona.

---

## Cleanup after a test run

The helper creates the deployment under a timestamp-based name
(`ctt-multi-component-<timestamp>`), so repeated runs do not collide with
earlier deployments. To remove the Application Package registration and all
deployments created by the helper from the sandbox Symphony:

```bash
bash ctt-runner/wfm-supplier/utils/fixtures/provision-multi-component.sh --cleanup
```

For a Symphony whose operator API is not at the default address:

```bash
bash ctt-runner/wfm-supplier/utils/fixtures/provision-multi-component.sh \
    --nbi-url <nbi-url> \
    --nbi-user <username> \
    --nbi-pass <password> \
    --cleanup
```

Vendor's WFM: remove the test deployment by the same means it was
assigned.

---

## Quick-reference checklist

```
Phase 1 — Identity provisioning (once per WFM; repeat when an X.509-SVID is reissued)

  Sandbox Symphony (sandbox MIS):
  □ Step 1: MIS installed and Root CAs generated   (sandbox Setup Guide: Build and Run MIS)
            mis.sh → 3) Factory Bootstrap: Generate Root CAs, 4) Margo Identity Service: Install
            mis.sh → 6) Generate SVID, twice       (WFM SVID, then WFM Client SVID for the CTT: WFM ID wfm, WFM Client ID ctt-client)
  □ Step 2: Symphony set up                        (sandbox Setup Guide: Build and Run WFM(Symphony))
            wfm.sh → 7) Manage SPIFFE ID allowlist → 1) Add SPIFFE IDs:
            spiffe://margo.org/margo/wfm/wfm/client/ctt-client
  □ Step 3: only if the MIS runs on another host: copy client SVID, key and ca.crt to ~/ctt-certs on the CTT host
  □ Step 4: ctt-start.sh → 1. WFM Supplier → 1. Setup MIAF Identity → 1) MIS-issued identity
            (Enter for $HOME/mis-deployment, or ~/ctt-certs after a transfer; then select x509svid-wfm-ctt-client)
            openssl verify -CAfile trust-bundle-ca.pem client-svid-cert.pem → OK
  □ Step 5: only if Symphony is not running: wfm.sh → 3) Symphony: Start

  Vendor's WFM:
  □ Step 1: The MIS of the Trust Domain issues the CTT a WFM Client SVID: <WFM SPIFFE ID>/client/<wfm-client-id>
  □ Step 2: The WFM Client SPIFFE ID of the CTT added to the local authorization policy of the WFM
  □ Step 3: client-svid-cert.pem, client-svid-key.pem and mis-ca.crt in one folder on the CTT host
  □ Step 4: ctt-start.sh → 1. WFM Supplier → 1. Setup MIAF Identity → 1) MIS-issued identity → that folder

Phase 2 — Run the conformance suite (every run)

  Sandbox Symphony:
  □ Step 6: sudo chown -R $USER:$USER $HOME/.docker
            bash ctt-runner/wfm-supplier/utils/fixtures/margo-ctt-package/push.sh
  □ Step 7: bash ctt-runner/wfm-supplier/utils/fixtures/provision-multi-component.sh

  Vendor's WFM:
  □ Step 6: export REGISTRY_REF=<registry-host>/<repository>:<tag>
  □ Step 7: Application deployment (two components) assigned to the WFM Client SPIFFE ID of the CTT

  Run:
  □ Step 8: bash ctt-runner/ctt-start.sh → 1. WFM Supplier → 2. Functional tests → 2) core
  □ Step 9: WFM URL: the mTLS endpoint, everything before /api/v1 (Symphony: https://localhost:8084/v1alpha2/margo)
            Wait for the run to complete (about one minute once the deployment is assigned; up to 5 minutes while polling)
  □ Step 10: Report:     ctt-runner/reports/wfm-supplier/wfm-scenario-report-core_<timestamp>.html
             Signed PDF: ctt-runner/reports/wfm-supplier/wfm-scenario-report-core_<timestamp>_signed.pdf (+ ca_cert.pem)
```

---

## Reference

### Reference result: sandbox Symphony

A test-scenario execution with the `core` group selected takes about one minute. Against the sandbox Symphony build
verified with this guide (October 2026) the result is **52 steps: 39 passed,
11 failed, 2 not run**, and **28 of 36 requirements passing, 7 failing,
1 not run**. The failures are deviations of that Symphony build from Margo
`1.0.0-rc.3`; they do not indicate a setup error:

| Failing step | Behaviour of Symphony | Requirement reported as FAIL |
|---|---|---|
| `wfm-core-cap-3`, `wfm-core-cap-4`, `wfm-core-cap-5` | Accepts Device Capabilities with an invalid deployment type, an invalid runtime, or without `vendor` (201 instead of 422) | MANAGEMENTINTERFACE-023 |
| `wfm-core-cn-1` | Answers an unsupported `Accept` header with 400 instead of 406 | MANAGEMENTINTERFACE-008 |
| `wfm-core-ds-2` | Answers a request without `Accept` header with 400 instead of the State Manifest | MANAGEMENTINTERFACE-007 |
| `wfm-core-ds-not-immutable`, `wfm-mc-manifest-not-immutable` | Marks the State Manifest `Cache-Control: ... immutable` | MANAGEMENTINTERFACE-035 |
| `wfm-core-status-1` | Answers a Deployment Status report for an unknown deployment with 500 | MANAGEMENTINTERFACE-023 |
| `step-wfm-hi-1.3` | The `ETag` of the State Manifest is not the digest of the response body | MANAGEMENTINTERFACE-015, -036 |
| `wfm-mc-manifest-urls` | URLs in the State Manifest have the form `/api/v1/clients/<spiffe-id>/...` | MANAGEMENTINTERFACE-023 |
| `wfm-mc-bundle-cache` | `Cache-Control` of the Deployment Bundle is `public` instead of `private` | MANAGEMENTINTERFACE-038 |

Each step carries only the requirement IDs its own checks verify; one deviation
therefore affects only its own requirement. The 2 steps not run belong to the
SVID rotation scenario described above.

### Certificate expiry and renewal

| File in `ctt-runner/wfm-supplier/utils/fixtures/miaf/real/` | Validity | Action on expiry |
|---|---|---|
| `client-svid-cert.pem` — X.509-SVID of the CTT | About 90 days (sandbox MIS default) | Generate a new WFM Client SVID and run Setup MIAF Identity again (Step 4) |
| `trust-bundle-ca.pem` — SVID Root CA certificate | Lifetime of the Root CA of the MIS | Run Setup MIAF Identity again (Step 4) |

Check the expiry date:

```bash
openssl x509 -in ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem \
    -noout -enddate
```

TLS handshake errors during a run (`certificate required`, `unknown CA`,
`certificate has expired`) indicate that the X.509-SVID or the trust anchor
must be checked first.

### Troubleshooting

| Symptom | Likely cause | Resolution |
|---|---|---|
| Setup MIAF Identity: `The client X.509-SVID does not chain to the given SVID Root CA` | The X.509-SVID and the Root CA certificate originate from different MIS installations | Provide the folder (or Root CA certificate) of the MIS that issued the X.509-SVID. No file is changed by the failed attempt |
| Setup MIAF Identity: `That is not a client SVID` | The SVID folder of the WFM was selected | Run it again and select the WFM Client SVID (`.../client/<wfm-client-id>`) |
| Setup MIAF Identity: `No known layout found` | The folder contains neither `x509svid-*` folders nor `client-svid-cert.pem`, `client-svid-key.pem` and `mis-ca.crt` | Provide the folder that contains the SVID folders, or the folder with these three files |
| `MIAF cert validation failed` when a run starts | Identity files were replaced manually with an inconsistent certificate, key or Root CA | Run Setup MIAF Identity again (Step 4) |
| Every step: `unable to verify the first certificate` | The X.509-SVID of the WFM does not chain to the trust anchor in `trust-bundle-ca.pem`: Phase 1 was skipped, or a different MIS issued the WFM SVID | Run Setup MIAF Identity again with the folder of the MIS that issued the WFM SVID |
| Every step: `WFM presented ..., expected ...` | The WFM SPIFFE ID is not the one the WFM Client SPIFFE ID of the CTT is derived from | Issue the CTT a WFM Client SVID for the WFM ID of this WFM (`<WFM SPIFFE ID>/client/<wfm-client-id>`) |
| Every step: `certificate required`, `bad certificate` or `403` | The WFM does not authorize the CTT | Add the WFM Client SPIFFE ID printed by Setup MIAF Identity to the local authorization policy of the WFM (Symphony: `wfm.sh` → 7) |
| Every step: `ECONNREFUSED` or `could not reach the WFM` | Incorrect Margo Management Interface URL, or the WFM is not running | Verify the URL and port (Symphony: `https://localhost:8084/v1alpha2/margo`) |
| `certificate has expired` | The X.509-SVID of the CTT or of the WFM has exceeded its validity | Generate new SVIDs and run Setup MIAF Identity again |
| Application Registry steps fail | `oras` is not installed or not logged in, or the Application Package is not in the registry | Install `oras`; run `oras login <registry-host>`. Sandbox Symphony: run `push.sh` (Step 6). Vendor's WFM: set `REGISTRY_REF` |
| Application Registry steps fail with `tls: failed to verify certificate` | The CTT host does not trust the TLS certificate of the registry | Add the CA of the registry to the trust store of the CTT host |
| Steps of the multi-component scenario are `NOT RUN` | No deployment is assigned to the WFM Client identity of the CTT | Sandbox Symphony: run `provision-multi-component.sh` (Step 7), then repeat the run. Vendor's WFM: assign the deployment to the SPIFFE ID the run printed |
| `wfm-mc-bundle-fetch` and `wfm-mc-bundle-contents` fail with HTTP 500 on Symphony | The host was rebooted or Symphony was restarted | Run `provision-multi-component.sh` again, then repeat the run |
| `ERROR: failed to get auth token` from `provision-multi-component.sh` | Symphony is not running, or its operator API is not at `https://localhost:8082/v1alpha2` with user `admin`. The helper supports Symphony only | Start Symphony, or pass `--nbi-url`, `--nbi-user`, `--nbi-pass`. Vendor's WFM: assign the deployment by the means the WFM provides |
| `Signed PDF not created` at the end of a run | Stated below the message. Typical causes: the repository folder is owned by root, or no internet access for the initial tool installation | Resolve the stated cause (ownership: `sudo chown -R $USER:$USER $HOME/workspace/conformance_test_toolkit`), then run the `pdf_ops.sh` command the run printed |
| A step fails with an unexpected 500 | Internal error of the WFM | Deviation of the WFM; refer to the requirement ID in the report |
