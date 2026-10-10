# Device Supplier Conformance — Setup and Execution Guide

**Persona:** Device Supplier  
**Spec baseline:** Margo `1.0.0-rc.3` (MIAF / mTLS authentication)  
**Audience:** Device vendors, device-agent engineers, Margo adopters

This guide walks you from a clean checkout to a conformance report.
The Device Supplier persona is self-contained: **no external WFM is needed**.
The CTT provides a spec-compliant mock WFM server locally and a simulated
device-agent that exercises every required protocol flow against it.

> **What the report covers.** The tests in Phase 3 run the CTT's *own*
> simulated device against the CTT's mock WFM. They prove the mock WFM is set
> up and working — the report says nothing about your device.
> To test a real device, do Phases 1 and 2, then follow
> [Testing a real device-agent](#testing-a-real-device-agent). There is no
> automatic report for a real device yet: you read the mock WFM's log, and that
> section tells you what to look for.

---

## How the Device Supplier test works

```
 ctt-runner/device-supplier/
 ┌───────────────────────────────────────────────────────────┐
 │  bin/server   ←  Mock WFM Server                         │
 │  port 3003    ←  Management Interface, mTLS (X.509-SVID)  │
 │  port 3001    ←  health / telemetry status only           │
 └──────────────────────────┬────────────────────────────────┘
                             │ HTTPS / mTLS
 ┌──────────────────────────▼────────────────────────────────┐
 │  bin/run_tests  ←  CTT simulated device-agent             │
 │  Fires: capability reports, desired-state polls,          │
 │  deployment-status updates, negative-test flows           │
 └───────────────────────────────────────────────────────────┘
                             │
                             ▼
 ctt-runner/reports/device-supplier/
   conformance-report-<timestamp>.html
```

The mock WFM implements the rc.3 Management Interface: it enforces mTLS and
accepts only SVIDs that belong to its own WFM identity, serves desired-state
manifests, bundles and deployments with correct ETag / digest, validates every
request body, and returns errors as RFC 9457 problem details.

The test runner acts as the device-agent, making every call the spec requires —
happy-path calls AND deliberately broken ones (bad enum, missing field, wrong
digest) — and checks the mock WFM's answer to each one. Requests to the
Management Interface that are not authenticated by mTLS are rejected.

**If you want to test your real device-agent**: start the mock WFM server,
point your device-agent at it, and observe how it behaves. The mock WFM logs
every request and validates it against the spec. See [Testing a real
device-agent](#testing-a-real-device-agent) below.

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.21+ (1.24 used) | compiles `bin/server` and `bin/run_tests`. Install from [go.dev](https://go.dev/doc/install), or `apt install golang-go` on Ubuntu 24.04. An older Go (1.21–1.23) downloads the 1.24 toolchain by itself on the first build, which needs internet access. |
| OpenSSL | 1.1+ | certificate checks; usually pre-installed |
| jq | any | group file parsing; `apt install jq` |
| bash | 4+ | |
| OS | Linux | tested on Ubuntu 24.04; macOS is not supported |

**1. Clone the CTT repo:**

```bash
mkdir -p ~/workspace && cd ~/workspace
git clone https://github.com/margo/conformance_test_toolkit.git
cd conformance_test_toolkit
git checkout feature/multi-persona-conformance
```

**2. Clone the Margo sandbox** (provides `mis.sh` — the MIS used for SVID generation):

```bash
cd ~
git clone https://github.com/margo/sandbox.git sandbox
```

> If the sandbox is already cloned elsewhere, note the path — you will need
> `mis.sh` from `<sandbox>/scripts/mis.sh` in Phase 1.

**3. Verify tooling:**

```bash
go version      # 1.21 or newer
openssl version
jq --version
```

---

## Phase 1 — Identity setup (one-time)

All conformance tests use MIAF mTLS (port 3003). The mock WFM and the CTT
test runner each need an X.509-SVID from a **shared MIS** — the same MIS that
the vendor's real device trusts. This CA alignment is what makes the test
production-representative.

```
MIS  ──issues SVIDs──►  mock WFM server  (MIAF TLS identity, port 3003)
     ──issues SVIDs──►  CTT test runner  (mTLS client identity)
     ──issues SVID──►   vendor device    (connects to mock WFM on port 3003)
```

> **CA alignment:** `certs/svid-ca.pem` placed here must be the CA that the
> vendor's device trusts. For sandbox testing this is the sandbox MIS CA. For
> real vendor conformance testing, use the CA from whichever MIS the vendor's
> device is enrolled in.

### Centralized MIS

**Step 1 — Generate SVIDs via MIS**

One MIS issues every SVID: one for the mock WFM, one for the CTT's test runner,
and later one for each real device you test. The examples below use the WFM ID
`ctt-mock-wfm`.

If the sandbox MIS is not installed on this machine yet, install it first by
following the Margo sandbox setup guide (`~/sandbox/docs/setup-guide.md`,
"Build and Run MIS": `mis.sh` options 1, 3 and 4).

Then generate the two SVIDs. Run the command from inside `~/mis-deployment` —
`mis.sh` writes each SVID folder into the directory you run it from:

```bash
cd ~/mis-deployment
sudo -E bash ~/sandbox/scripts/mis.sh generate-svid     # run 1: the mock WFM
sudo -E bash ~/sandbox/scripts/mis.sh generate-svid     # run 2: the CTT test runner
sudo chown -R $USER:$USER ~/mis-deployment
```

Answer the prompts like this:

| Prompt | Run 1 — mock WFM | Run 2 — CTT test runner |
|---|---|---|
| `Enter Trust Domain [default: margo.org]:` | press Enter | press Enter |
| `Enter choice [1/2]:` | `1` | `2` |
| `Enter WFM ID:` | `ctt-mock-wfm` | `ctt-mock-wfm` |
| `Enter WFM Client ID:` | (not asked) | `ctt-runner` |
| `Enter TTL in seconds [default: 7776000]:` | press Enter | press Enter |
| `DNS SANs:` | press Enter | press Enter |
| `Proceed? [Y/n]:` | press Enter | press Enter |

The WFM ID must be the same in both runs. The client's SPIFFE ID is then
`<WFM SPIFFE ID>/client/<client-id>`, which is what the spec requires and what
both the mock WFM and the test runner check.

**Step 2 — Check what was generated:**

```bash
ls ~/mis-deployment/ | grep x509svid
# x509svid-ctt-mock-wfm              ← WFM SVID (the mock WFM's identity)
# x509svid-ctt-mock-wfm-ctt-runner   ← client SVID (the CTT test runner's identity)
```

You do not need to copy these by hand — the CTT CLI lists them and copies the
right files in Step 3.

For a vendor-provided or external MIS: use your MIS tooling to obtain a WFM
SVID cert/key pair, a client SVID cert/key pair, and the MIS CA cert, then
copy them to the CTT machine before Step 3.

**Step 3 — Place SVIDs into the CTT cert directory**

The SVIDs must be on the CTT machine before this step. Two scenarios:

**MIS on the same machine as CTT** (sandbox setup):
`~/mis-deployment/` is already local — go straight to the CTT CLI:
  
Now, execute below CTT command, it will copy the certs generated by Sandbox mis.sh script to the certs folder inside CTT.
```bash
cd ~/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 1) Setup Identity
#
# When prompted:
#   Folder containing the SVIDs [~/mis-deployment]:   press Enter
#   Which one is the WFM SVID?    [1]:   the number of x509svid-ctt-mock-wfm
#   Which one is the Client SVID? [2]:   the number of x509svid-ctt-mock-wfm-ctt-runner
#
# The CLI lists every SVID folder it finds. With only the two above, the
# defaults [1] and [2] are right — press Enter twice.
```

It must end with `Identity setup complete — both SVIDs verified against the MIS CA`
and print the two SPIFFE IDs.

**MIS on a different machine** (vendor setup):

On the MIS machine, collect the required files into a staging directory, then transfer the whole directory to the CTT machine using `scp -r` — scp will create the destination directory automatically:

```bash
# On the MIS machine — stage cert files into a local folder
mkdir -p ~/vendor-certs
cp MIS_DIR/x509svid-WFM_ID/payload-cert.pem           ~/vendor-certs/wfm-svid-cert.pem
cp MIS_DIR/x509svid-WFM_ID/payload-key.pem            ~/vendor-certs/wfm-svid-key.pem
cp MIS_DIR/x509svid-WFM_ID-CLIENT_ID/payload-cert.pem ~/vendor-certs/client-svid-cert.pem
cp MIS_DIR/x509svid-WFM_ID-CLIENT_ID/payload-key.pem  ~/vendor-certs/client-svid-key.pem
cp MIS_DIR/certs/ca.crt                               ~/vendor-certs/mis-ca.crt

# Transfer the folder to the CTT machine's home directory.
# Creates ~/vendor-certs/ there on the first run; on a re-run it overwrites the files in place.
scp -r ~/vendor-certs <CTT_HOST>:~/
```

Then on the CTT machine, run the CLI and point it at the folder the files were transferred to:

```bash
cd ~/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 1) Setup Identity
#
# When prompted:
#   Folder containing the SVIDs [~/mis-deployment]:   ~/vendor-certs
```

The CLI picks up the five files by the names used above. The folder can be
anywhere on the CTT machine and may be owned by root or by your user — the CLI
uses `sudo` only when it cannot read the folder itself, and never changes the
source folder. A whole MIS output folder (with `x509svid-*` sub-folders and
`certs/ca.crt`) can be given instead of a flat folder.

Before anything is replaced, the CLI checks that both SVIDs were issued by the
given MIS CA. If a file is missing or the CA does not match, it stops and leaves
the existing certs untouched.

All certs land in `ctt-runner/device-supplier/certs/`.

**Verify:**

```bash
cd ctt-runner/device-supplier
openssl verify -CAfile certs/svid-ca.pem certs/miaf-server-cert.pem
# → certs/miaf-server-cert.pem: OK
openssl verify -CAfile certs/svid-ca.pem certs/svid-cert.pem
# → certs/svid-cert.pem: OK
```

**SVID validity:** MIS issues SVIDs with a ~90-day TTL. Re-run Steps 1 and 3
when certs expire.

---

## Phase 2 — Start the mock WFM server

The CTT CLI builds the binaries from the current source every time it starts
the server (a few seconds; ~1 minute the very first time). Use option 2:

```bash
cd ~/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 2) Start Mock WFM Server
```

The CLI prints what a device needs to connect:

```
  Management Interface : https://<ctt-host-ip>:3003/v1alpha2/margo
  WFM SPIFFE ID        : spiffe://<trust-domain>/margo/wfm/<wfm-id>
  Trusted CA (MIS)     : .../ctt-runner/device-supplier/certs/svid-ca.pem
  Health / telemetry   : https://<ctt-host-ip>:3001/health
  Server log           : /tmp/wfm-server.log
```

The server listens on two ports:
- **Port 3003** — the Margo Management Interface, mTLS with the SVIDs from Phase 1.
  This is the only port a device talks to.
- **Port 3001** — health and telemetry status. Management Interface requests sent
  here are rejected with `403`, because they are not authenticated by mTLS.

The server keeps running after you leave the menu. Stop it with option 4.

---

## Phase 3 — Run the conformance suite

### Via the interactive menu

The mock WFM server must already be running (Phase 2).

```bash
cd ~/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 3) Run Tests
```

The menu will:
1. Prompt for a test group selection
2. Prompt for the two server URLs
3. Run all scenarios and print results as they execute
4. Write the HTML report

The server is left running afterwards; stop it with option 4 when you are done.

### Select a test group

```
📋 Available Device Test Groups:
  1) bronze                 (v1.0.0) - 5 selected scenarios
  2) core                   (v1.0.0-rc.3) - all scenarios
  3) device-conformance     (v1.0.0-rc.3) - 13 selected scenarios
  ...
```

Select `core` (enter its number from the list — `2` above). It runs the full
set of rc.3 device-supplier scenarios; the other groups are subsets of it.
Groups whose version is not `1.0.0-rc.3` show a version-mismatch question
before they run.

### URLs to enter

```
Enter Mock WFM Server URL [https://192.168.x.x:3001/v1alpha2/margo]:
```

Press Enter to use the default (auto-detected from your machine's IP).

```
Enter Management Interface (mTLS) URL [https://192.168.x.x:3003/v1alpha2/margo]:
```

Press Enter. All conformance steps go to the mTLS port (3003); port 3001 is
only used for the health check and telemetry status.

A full `core` run takes about 5 minutes. Two steps of the optional
observability scenario wait up to 2 minutes each for telemetry from a real
device and are reported as skipped when none arrives.

The HTML report is written to two locations:

```
ctt-runner/device-supplier/reports/conformance-report-<timestamp>.html
ctt-runner/reports/device-supplier/conformance-report-<timestamp>.html
```

Open either in any browser. The report includes a conformance requirement
coverage summary, per-scenario table, and a full step-detail table.

---

## Testing a real device-agent

The mock WFM from Phase 2 is the target. The device gets its identity from the
same MIS, connects to the mock WFM, and you read the mock WFM's log to see what
it sent and how each request was answered.

### Step 1 — Issue the device an SVID

On the MIS machine, generate one more client SVID for the **same WFM ID** as
the mock WFM, with a client ID that names the device:

```bash
cd ~/mis-deployment
sudo -E bash ~/sandbox/scripts/mis.sh generate-svid
# Enter choice [1/2]:     2
# Enter WFM ID:           ctt-mock-wfm
# Enter WFM Client ID:    vendor-device-1        ← one name per device
# every other prompt:     press Enter
sudo chown -R $USER:$USER ~/mis-deployment
```

This creates `~/mis-deployment/x509svid-ctt-mock-wfm-vendor-device-1/`.

### Step 2 — Give the device owner these five things

| What | Where it comes from |
|---|---|
| Device SVID and key | `~/mis-deployment/x509svid-ctt-mock-wfm-vendor-device-1/payload-cert.pem` and `payload-key.pem` |
| Management Interface URL | printed by Phase 2: `https://<ctt-host-ip>:3003/v1alpha2/margo` |
| WFM SPIFFE ID to accept | printed by Phase 2: `spiffe://margo.org/margo/wfm/ctt-mock-wfm` |
| Trust, option A — a CA file | `~/mis-deployment/certs/ca.crt` (the CA that signed every SVID) |
| Trust, option B — fetch it from the MIS | MIS address `https://mis.margo.org:9443`, its HTTPS CA `~/mis-deployment/certs/https-ca.crt`, and a hosts entry on the device: `<MIS-machine-IP> mis.margo.org` |

Use option A or B, whichever the device supports. The device must be able to
reach the CTT machine on port **3003** (and the MIS machine on **9443** for
option B).

### Step 3 — Start the device and watch the log

```bash
tail -f /tmp/wfm-server.log
```

Every request is logged with the status it was answered with. A device that
behaves correctly produces lines like these, in this order:

```
[MIAF/Capabilities] accepted for spiffe://margo.org/margo/wfm/ctt-mock-wfm/client/vendor-device-1 (deviceId=<its device id>)
[Router] PUT  /v1alpha2/margo/api/v1/capabilities/<device id> ... → 201
[Router] GET  /v1alpha2/margo/api/v1/deployments ... → 200
[Router] GET  /v1alpha2/margo/api/v1/deployments/<deployment id>/sha256:<digest> ... → 200
[MIAF/Status] update for deployment <deployment id> from spiffe://.../client/vendor-device-1
[Router] POST /v1alpha2/margo/api/v1/deployments/<deployment id>/status ... → 200
[Router] GET  /v1alpha2/margo/api/v1/deployments ... → 304
```

What to check:

| You should see | It shows the device |
|---|---|
| `PUT .../capabilities/...  → 201` (or `200` on a repeat) | reports valid capabilities |
| `GET .../deployments → 200`, then a `GET` of each deployment (or the bundle) by digest `→ 200` | polls the desired state and fetches what it lists |
| `POST .../status → 200` after the fetch | reports status, with `adoptedManifestVersion` |
| later polls `GET .../deployments → 304` | re-polls with `If-None-Match` instead of downloading again |

Lines that mean something is wrong:

| Line | Meaning |
|---|---|
| `[MIAF] answered 422 Semantic Error — ... \| capabilities-006: properties.modelNumber is required` | the device sent a body the spec does not allow; the rule and reason follow the `\|` |
| `TLS handshake error ... is not a client of this WFM (expected .../client/<wfm-client-id>)` | the device's SVID was issued for a different WFM ID — repeat Step 1 with the mock WFM's ID |
| `TLS handshake error ... client didn't provide a certificate` | the device connected without its SVID |
| `TLS handshake error ... unknown certificate authority` | the device's SVID comes from a different MIS |
| nothing at all | the device cannot reach port 3003, or does not accept the WFM SPIFFE ID |

The mock WFM assigns every new device one sample deployment. Its component
points at a placeholder package that cannot be pulled, so a device is expected
to report that deployment as `failed`. To test a real installation, stop the
mock WFM, export a package the device can pull, and start it again:

```bash
export CTT_SAMPLE_REPOSITORY=oci://<registry>/<path>   # e.g. a compose package in your Harbor
export CTT_SAMPLE_REVISION=<version>                    # e.g. 1.0.0
bash ctt-runner/ctt-start.sh    # → 2) Device Supplier → 2) Start Mock WFM Server
```

---

### Integration with the Margo sandbox device-agent

The Margo sandbox ships a reference device-agent (`workload-fleet-management-client`)
that is the canonical example of a correct, spec-conformant device. Testing the
CTT mock WFM against it is the strongest integration validation: if the mock WFM
handles the sandbox device-agent correctly, vendors can use it with confidence.

With the sandbox MIS, both the mock WFM and the real device-agent use SVIDs
from the **same MIS** — no cert exchange between teams, no trust-bundle
configuration on the device side. The device-agent continues to talk to MIS
the same way it does in production; the only changes are the WFM URL and,
if the mock WFM has its own WFM ID, one allowlist entry.

#### Step 1 — Complete Phase 1 and Phase 2 above

A sandbox device-agent that is already set up for Symphony has an SVID for
Symphony's WFM ID (for example `wfm`). The simplest way to test it is to give
the mock WFM that same WFM ID: in Phase 1, pick the folder of that WFM SVID
(for example `x509svid-wfm`) as "WFM SVID" instead of `x509svid-ctt-mock-wfm`,
and generate the CTT test runner's client SVID for that WFM ID too. The device
then needs no new SVID: the mock WFM only accepts clients of its own WFM ID,
and the device only accepts the WFM its SVID belongs to.

Then start the mock WFM (Phase 2) and note the two values the CLI prints:

```
  Management Interface : https://<ctt-host-ip>:3003/v1alpha2/margo
  WFM SPIFFE ID        : spiffe://<trust-domain>/margo/wfm/<wfm-id>
```

#### Step 2 — Check the device-agent's SVID and allowlist

The sandbox device-agent already has an SVID from the MIS (used when it talks
to Symphony). Use that same SVID — no new cert is needed. Confirm its SPIFFE ID
starts with the WFM SPIFFE ID from Step 1:

```bash
# On the device-agent machine. The cert is the file that miaf.x509.certPath
# points to in ~/sandbox/poc/device/agent/config/config.yaml.
openssl x509 -in <path-to-device-svid-cert> -noout -ext subjectAltName
# → URI:spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/<client-id>
```

The device-agent only talks to WFMs on its SPIFFE ID allowlist. If the mock WFM
uses the same WFM SVID as Symphony, it is already on the list. Otherwise add the
WFM SPIFFE ID from Step 1:

```bash
# On the device-agent machine:
sudo -E bash ~/sandbox/scripts/device-agent.sh
# → 11) Manage SPIFFE ID allowlist → add the WFM SPIFFE ID
```

If the device SVID is expired or missing, generate a new one with `mis.sh`
(Phase 1, Step 1 — option 6, principal 2, same WFM ID) and install it on the
device-agent machine the same way it was installed for Symphony.

#### Step 3 — Point the sandbox device-agent at the mock WFM

`device-agent.sh` writes the WFM URL into the agent's config from
`device-agent.env` every time it starts the agent, so change it there (editing
`config.yaml` by hand is overwritten on the next start):

```bash
# On the device-agent machine, in ~/sandbox/scripts/device-agent.env:
export WFM_HOST=<ctt-host-ip>     # the CTT machine from Step 1
export WFM_PORT=3003              # the mock WFM's mTLS port
```

Leave all MIAF / MIS settings unchanged — the device-agent continues to use
the same MIS as before.

#### Step 4 — Restart the sandbox device-agent

```bash
# On the device-agent machine:
sudo -E bash ~/sandbox/scripts/device-agent.sh docker stop-docker
sudo -E bash ~/sandbox/scripts/device-agent.sh docker start-docker
```

For K3s:

```bash
sudo -E bash ~/sandbox/scripts/device-agent.sh k3s stop-k3s
sudo -E bash ~/sandbox/scripts/device-agent.sh k3s start-k3s
```

#### Step 5 — Watch the mock WFM logs

Back on the CTT machine:

```bash
tail -f /tmp/wfm-server.log
```

You should see the lines listed under
[Step 3 — Start the device and watch the log](#step-3--start-the-device-and-watch-the-log)
above: capabilities accepted, the desired-state poll, the deployment fetch, and
status reports.

#### What the mock WFM does with the real device-agent

The mock WFM accepts a device whose SVID chains to the MIS CA and belongs to
this WFM:

- Validates the mTLS client cert chains to the MIS CA (`svid-ca.pem`)
- Extracts the device's SPIFFE ID from the cert's `Subject Alternative Name URI`
  and checks it is `<WFM SPIFFE ID>/client/<client-id>`
- Creates a client record keyed by SPIFFE ID
- Serves a desired-state manifest and tracks deployment status
- Enforces all spec requirements (correct headers, ETag, digest, content-type)

Any spec violation in either direction (wrong headers from the device, wrong
response from the WFM) will be visible in the logs.

#### Restoring the sandbox device-agent to its normal config

After integration testing, point the device-agent back at Symphony and restart it:

```bash
# On the device-agent machine, in ~/sandbox/scripts/device-agent.env:
export WFM_HOST=symphony.machine   # or your real WFM hostname
export WFM_PORT=8084

sudo -E bash ~/sandbox/scripts/device-agent.sh docker stop-docker
sudo -E bash ~/sandbox/scripts/device-agent.sh docker start-docker
```

---

## Certificate expiry and renewal

| Cert type | Validity | Renewal |
|---|---|---|
| MIS SVIDs (`miaf-server-cert.pem`, `svid-cert.pem`) | ~90 days (MIS default TTL) | Re-run Steps 1 and 3 from Phase 1 |
| MIS CA (`svid-ca.pem`) | Long-lived (MIS CA cert) | Rare; re-run Step 3 from Phase 1 if the MIS CA rotates |

Check expiry:

```bash
# MIS SVID (mock WFM server identity)
openssl x509 -in ctt-runner/device-supplier/certs/miaf-server-cert.pem \
    -noout -enddate

# MIS SVID (CTT device identity)
openssl x509 -in ctt-runner/device-supplier/certs/svid-cert.pem \
    -noout -enddate
```

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `MIAF identity not found ... Please run 'Setup Identity' first` | Phase 1 Step 3 not done in this checkout | Run option 1) Setup Identity |
| Setup Identity: `... was not issued by the given MIS CA` | SVIDs and CA come from different MIS setups | Give the folder (or CA file) that belongs to the MIS that issued the SVIDs; nothing was changed in `certs/` |
| Setup Identity: `Missing file: ...` | Folder does not hold all five files | Check the folder; for a flat folder use the file names shown in Phase 1 |
| `Failed to build mock server` / `Failed to build test runner` | Go missing, or no internet for the first build | Install Go (Prerequisites); the first build downloads the Go toolchain and modules |
| `Failed to start mock server` | The last log lines are printed with the error | Read them; full log in `/tmp/wfm-server.log` |
| `cannot use the WFM SVID: ... is not a WFM identity` | The cert chosen as WFM SVID is a client SVID (or not an SVID) | Re-run Setup Identity and pick the WFM SVID folder for "WFM SVID" |
| Every step fails: `server presented ..., expected the WFM ...` | Client SVID was generated for a different WFM ID than the WFM SVID | Generate the client SVID for the same WFM ID (Phase 1 Step 1), re-run Setup Identity |
| Every step fails: `x509: certificate signed by unknown authority` | `svid-ca.pem` is not the CA that issued the WFM SVID | Re-run Setup Identity with the right MIS folder |
| Every step fails with `403` (or a certificate error naming port 3001) | The Management Interface (mTLS) URL was pointed at port 3001 | Press Enter at the URL prompts to use the defaults (mTLS on 3003) |
| `x509: certificate has expired` | SVID older than its TTL (~90 days) | Generate new SVIDs (Phase 1 Step 1) and re-run Setup Identity |
| Device: `TLS handshake error ... is not a client of this WFM` in `/tmp/wfm-server.log` | Device SVID belongs to a different WFM ID | Issue the device an SVID for the mock WFM's ID (Testing a real device-agent, Step 1) |
| Device: `TLS handshake error ... unknown certificate authority` | Device SVID issued by a different MIS than `svid-ca.pem` | Use the CA of the MIS that issued the device's SVID |
| Sandbox device-agent keeps talking to Symphony | `WFM_HOST` / `WFM_PORT` in `device-agent.env` unchanged | Set them to the CTT host and `3003`, then restart the device-agent |
| Sandbox device-agent refuses the mock WFM | Mock WFM's SPIFFE ID not on the device's allowlist | `device-agent.sh` → 11) Manage SPIFFE ID allowlist |
| `container 'margo-identity-service' is not running` from `mis.sh` | MIS not installed/started, or `mis.sh` run without `sudo` | `sudo -E bash ~/sandbox/scripts/mis.sh` → 4) Margo Identity Service: Install |
| `~/mis-deployment/` owned by root | `mis.sh` runs as root | `sudo chown -R $USER:$USER ~/mis-deployment` (optional — Setup Identity can read root-owned folders) |
| Report not generated | Test runner stopped early | Check `ctt-runner/reports/device-supplier/test-execution.log` |

---

## Quick-reference checklist

```
Phase 1 — Identity setup (once per MIS deployment; re-run when SVIDs expire)
  □ MIS installed (sandbox setup guide: mis.sh options 1, 3, 4)
  □ Generate SVIDs: cd ~/mis-deployment && sudo -E bash ~/sandbox/scripts/mis.sh generate-svid
                    run 1: choice 1, WFM ID ctt-mock-wfm
                    run 2: choice 2, WFM ID ctt-mock-wfm, client ID ctt-runner
  □ Fix ownership:  sudo chown -R $USER:$USER ~/mis-deployment
  □ Copy to CTT:    ctt-start.sh → 2) Device Supplier → 1) Setup Identity
                    (enter the folder holding the SVIDs — default ~/mis-deployment; root-owned folders are fine)
  □ The CLI must end with: Identity setup complete — both SVIDs verified against the MIS CA

Phase 2 — Start the mock WFM
  □ ctt-start.sh → 2) Device Supplier → 2) Start Mock WFM Server  (builds from source each time)
  □ Note the Management Interface URL and WFM SPIFFE ID it prints

Phase 3 — CTT self-test (CTT simulates device-agent against mock WFM)
  □ ctt-start.sh → 2) Device Supplier → 3) Run Tests → core → Enter, Enter at the URL prompts
  □ Open report: ctt-runner/reports/device-supplier/conformance-report-<ts>.html

Testing a real device (the Phase 3 report does not cover it)
  □ Issue it an SVID: generate-svid → choice 2, WFM ID ctt-mock-wfm, client ID = a name for the device
  □ Give the owner: the SVID + key, the Management Interface URL, the WFM SPIFFE ID,
      and either ~/mis-deployment/certs/ca.crt or the MIS address + https-ca.crt + hosts entry
  □ Mock WFM running (Phase 2); device can reach port 3003
  □ tail -f /tmp/wfm-server.log → capabilities 201, deployments 200, deployment fetch 200, status 200, later polls 304
  □ Sandbox device-agent: see "Integration with the Margo sandbox device-agent"
```
