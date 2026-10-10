# Device Supplier Conformance — Setup and Execution Guide

**Persona:** Device Supplier  
**Spec baseline:** Margo `1.0.0-rc.3` (MIAF / mTLS authentication)  
**Audience:** Device vendors, device-agent engineers, Margo adopters

This guide walks you from a clean checkout to a signed conformance report.
The Device Supplier persona is self-contained: **no external WFM is needed**.
The CTT provides a spec-compliant mock WFM server locally and a simulated
device-agent that exercises every required protocol flow against it.

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
| jq | any | group file parsing; `apt install jq` / `brew install jq` |
| bash | 4+ | macOS ships bash 3 — install bash 5 via Homebrew |

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

Use MIS to generate a WFM SVID and a client/device SVID.

For the Margo sandbox MIS (cloned to `~/sandbox` in Prerequisites):

```bash
sudo -E bash ~/sandbox/scripts/mis.sh
# In the mis.sh menu — first time on this machine, in this order, in one session:
#   1) PreRequisites: Setup
#   3) Factory Bootstrap: Generate Root CAs
#   4) Margo Identity Service: Install
#   6) Generate SVID      ← run it twice: once for the WFM (principal 1),
#                            once for the WFM client (principal 2)

sudo chown -R $USER:$USER ~/mis-deployment
```

If the MIS is already installed and you only need new SVIDs, start `mis.sh`
from inside `~/mis-deployment` so the SVID folders are written there:

```bash
cd ~/mis-deployment
sudo -E bash ~/sandbox/scripts/mis.sh      # → 6) Generate SVID, twice as above
sudo chown -R $USER:$USER ~/mis-deployment
```

> `mis.sh` writes each SVID folder into the directory it is working in and
> prints it as `Output Dir`. That is `~/mis-deployment` in both flows above. If
> yours printed a different folder, give that folder to the CLI in Step 3.

The client SVID must be generated for the **same WFM ID** as the WFM SVID. Its
SPIFFE ID is then `<WFM SPIFFE ID>/client/<client-id>`, which is what the spec
requires and what both the mock WFM and the test runner check.

**Step 2 — Find the output directories from mis.sh:**

mis.sh names the SVID directories after the WFM ID and client ID you entered during generation:

```
x509svid-WFM_ID              ← WFM SVID (server identity)
x509svid-WFM_ID-CLIENT_ID    ← CTT device SVID (client identity)
```

Check what was generated:

```bash
ls ~/mis-deployment/ | grep x509svid
# The exact directory names depend on the IDs you entered in mis.sh.
# Example with wfm-id="wfm" and client-id="wfm-client":
#   x509svid-wfm
#   x509svid-wfm-wfm-client
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
#   Which one is the WFM SVID?    [1]:                press Enter (or pick from the list)
#   Which one is the Client SVID? [2]:                press Enter (or pick from the list)
```

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

If you want to test **your own device-agent implementation** (not the CTT's
simulated one), use the mock WFM server as the target. This is useful to:

- Verify your device-agent is spec-conformant before running on a real WFM
- Use the Margo sandbox device-agent as a reference implementation against the mock WFM
- Confirm the CTT mock WFM is itself correct from the perspective of a real device

The mock WFM logs every request and validates it against the spec — so you can
see exactly what your device sends, what the WFM validates, and where any gaps
are.

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

Use the **same WFM SVID** the sandbox device-agent was enrolled for (the WFM ID
it uses with Symphony) as the mock WFM's identity in Phase 1. The device's own
SVID is `<WFM SPIFFE ID>/client/<client-id>`; the mock WFM only accepts clients
of its own WFM ID, and the device only accepts the WFM its SVID belongs to.

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

You should see the sandbox device-agent connecting over mTLS, presenting its
MIS-issued SVID, and making capability reports and desired-state polls. A
device whose SVID is not accepted shows up as a `TLS handshake error` line that
names the reason.

The mock WFM assigns every new client one sample deployment. Its component
points at a placeholder OCI reference that cannot be pulled, so a device is
expected to report that deployment as `failed`. To test a real installation,
stop the mock WFM, export an artifact your device can pull, and start it again:

```bash
export CTT_SAMPLE_REPOSITORY=oci://<registry>/<path>   # e.g. a compose package in your Harbor
export CTT_SAMPLE_REVISION=<version>                    # e.g. 1.0.0
bash ctt-runner/ctt-start.sh    # → 2) Device Supplier → 2) Start Mock WFM Server
```

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

### Integration with any device-agent (generic)

The same approach works for any custom device-agent, not just the Margo sandbox.
Give the mock WFM the WFM SVID of the WFM ID your device is enrolled for and the
CA of the MIS that issued your device's SVID (Phase 1), start it (Phase 2), and
point your device at port 3003.

```
Management Interface URL: https://<mock-wfm-host>:3003/v1alpha2/margo
Client cert:   <your-device-svid-cert.pem>   (<WFM SPIFFE ID>/client/<client-id>)
Client key:    <your-device-svid-key.pem>
Trust CA:      ctt-runner/device-supplier/certs/svid-ca.pem  (MIS CA)
WFM identity:  the WFM SPIFFE ID printed when the mock WFM starts
```

If your device-agent does **not** have a MIS-issued SVID yet, issue one
via `mis.sh` as shown in Phase 1, Step 1 above.

The mock WFM logs every request to `/tmp/wfm-server.log`. Watch it while your
device-agent runs to see exactly what it sends and what the WFM validates:

```bash
tail -f /tmp/wfm-server.log
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
| Device: `TLS handshake error ... is not a client of this WFM` in `/tmp/wfm-server.log` | Device SVID belongs to a different WFM ID | Give the mock WFM the WFM SVID of the device's WFM ID, or issue the device an SVID under the mock WFM's ID |
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
  □ Generate SVIDs: sudo -E bash ~/sandbox/scripts/mis.sh
                    (first time: 1, 3, 4, then 6 twice — WFM, then WFM client for the same WFM ID)
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

Integration test with a real sandbox device-agent (optional)
  □ Mock WFM uses the WFM SVID of the device's WFM ID, and is running (Phase 2)
  □ On device-agent machine: device-agent.env → WFM_HOST=<ctt-host>, WFM_PORT=3003
      → leave all miaf / mis config unchanged (device keeps using its own MIS SVID)
  □ If the mock WFM has its own WFM ID: device-agent.sh option 11 → add its SPIFFE ID
  □ Restart device-agent (docker stop-docker / start-docker)
  □ Watch CTT mock WFM logs: tail -f /tmp/wfm-server.log
  □ Set WFM_HOST / WFM_PORT back to Symphony and restart when done
```
