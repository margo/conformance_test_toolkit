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
 │  port 3001    ←  RFC 9421 signed calls                   │
 │  port 3003    ←  MIAF mTLS (X.509-SVID) calls            │
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

The mock WFM is a full spec implementation: it signs responses with the right
headers, enforces mTLS, serves desired-state manifests and bundles with correct
ETag / digest, and validates every request body for structure and semantics.

The test runner acts as the device-agent, making every call the spec requires —
happy-path calls AND deliberately broken ones (missing signature, wrong cert,
bad enum, untrusted CA) — so that the mock WFM can validate it rejects the bad
calls correctly and the device-agent code handles the right responses.

**If you want to test your real device-agent**: start the mock WFM server,
point your device-agent at it, and observe how it behaves. The mock WFM logs
every request and validates it against the spec. See [Testing a real
device-agent](#testing-a-real-device-agent) below.

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.24+ | compiles `bin/server` and `bin/run_tests`; `apt install golang-go` or [golang.org](https://golang.org/doc/install) |
| OpenSSL | 1.1+ | certificate generation; usually pre-installed |
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
git clone https://github.com/eclipse-margo/margo.git sandbox
```

> If the sandbox is already cloned elsewhere, note the path — you will need
> `mis.sh` from `<sandbox>/scripts/mis.sh` in Phase 1.

**3. Verify tooling:**

```bash
go version      # need 1.24+
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

### Path 1 — Centralized MIS

**Step 1 — Generate SVIDs via your MIS**

Use your MIS to generate a WFM SVID and a client/device SVID.

For the Margo sandbox MIS (cloned to `~/sandbox` in Prerequisites):

```bash
# Generate SVIDs — follow prompts to generate the certs and create a WFM and a WFM-client SVID
bash ~/sandbox/scripts/mis.sh
sudo chown -R $USER:$USER ~/mis-deployment
```

For a vendor-provided or external MIS: use your MIS tooling to obtain a WFM
SVID cert/key pair, a client SVID cert/key pair, and the MIS CA cert, then
copy them to the CTT machine before Step 2.

**Step 2 — Place SVIDs into the CTT cert directory**
ls ~/mis-deployment/ | grep x509svid
x509svid-<wfm-id>            ← Symphony's WFM SVID (server identity)
x509svid-<wfm-id>-<client-id> ← CTT's device SVID (client identity)

The SVIDs must be on the CTT machine before this step. Two scenarios:

**MIS on the same machine as CTT** (sandbox setup):
`~/mis-deployment/` is already local — go straight to the CLI:

```bash
cd ~/workspace/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 1) Setup Identity
# CLI auto-detects ~/mis-deployment/x509svid-* and asks for confirmation
```

Set variables for the rest of the steps:

WFM_SVID_DIR=~/mis-deployment/x509svid-<wfm-id>
WFM_CLIENT_SVID_DIR=~/mis-deployment/x509svid-<client-id>
- The exact directory names depend on the IDs you entered in mis.sh.
- Check what was created: ls ~/mis-deployment/ | grep x509svid
- Example: if wfm-id="wfm", client-id="wfm-wfm-client" or "wfmclient":
-   WFM_SVID_DIR=~/mis-deployment/x509svid-wfm
-   WFM_CLIENT_SVID_DIR=~/mis-deployment/x509svid-wfm-wfm-client

**MIS on a different machine** (vendor setup):
Copy the SVID files to the CTT machine first, then run the CLI and provide explicit paths when prompted:

```bash
# On the MIS machine — copy files to CTT machine
scp <mis-dir>/x509svid-<wfm-id>/payload-cert.pem   ctt-host:~/wfm-svid-cert.pem
scp <mis-dir>/x509svid-<wfm-id>/payload-key.pem    ctt-host:~/wfm-svid-key.pem
scp <mis-dir>/x509svid-<client-id>/payload-cert.pem ctt-host:~/client-svid-cert.pem
scp <mis-dir>/x509svid-<client-id>/payload-key.pem  ctt-host:~/client-svid-key.pem
scp <mis-dir>/certs/ca.crt                          ctt-host:~/mis-ca.crt

All certs land in `ctt-runner/device-supplier/certs/`.

**Verify:**

```bash
cd ctt-runner/device-supplier
openssl verify -CAfile certs/svid-ca.pem certs/miaf-server-cert.pem
# → certs/miaf-server-cert.pem: OK
openssl verify -CAfile certs/svid-ca.pem certs/svid-cert.pem
# → certs/svid-cert.pem: OK
```

**SVID validity:** MIS issues SVIDs with a ~90-day TTL. Re-run both steps when
certs expire.

---

## Phase 2 — Start the mock WFM server

The CTT CLI builds the binaries automatically on first run if not already
present. Use option 2:

```bash
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 2) Start Mock WFM Server
```

The server starts on two ports:
- **Port 3001** — plain TLS (legacy, not used by current conformance tests)
- **Port 3003** — MIAF mTLS with the SVIDs from Phase 1


### Via the interactive menu

```bash
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 2) Run scenario group tests
```

The menu will prompt for MIAF cert paths if you want to use MIS SVIDs.

---

## Phase 3 — Run the conformance suite

### Via the interactive menu

```bash
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 2) Run scenario group tests
```

The menu will:
1. Build `bin/server` and `bin/run_tests` (Go compile, ~10s first time, cached after)
2. Kill any stale server on port 3001/3003
3. Start the mock WFM server in the background
4. Prompt for the mock WFM URL and MIAF mTLS URL
5. Prompt for a test group selection
6. Run all scenarios and print results as they execute
7. Stop the mock server
8. Write the HTML report

### URLs to enter (interactive menu)

```
Enter Mock WFM Server URL [https://192.168.x.x:3001/v1alpha2/margo]:
```

Press Enter to use the default (auto-detected from your machine's IP).

```
Enter MIAF mTLS URL for mtls:true steps [https://192.168.x.x:3003/v1alpha2/margo]:
```

Press Enter. The mock server listens on 3001 for RFC 9421 steps and 3003 for
MIAF mTLS steps — these are separate ports.

### Select a test group

```
Available Device Test Groups:
  1) core        v1.0.0-rc.3  — Generic/positive/negative/edge coverage ...
  2) silver      v1.0.0-rc.3  — ...
  3) gold        v1.0.0-rc.3  — ...
```

For a first run, select `core`. It covers the full set of required
conformance scenarios for the device-agent role.

```
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
the same way it does in production; the only change is the WFM URL.

#### Step 1 — Complete Phase 1 (Path 1) above

Make sure the mock WFM has its MIS-issued SVID in `certs/miaf-server-cert.pem`
and the MIS CA in `certs/svid-ca.pem` before continuing.

#### Step 2 — Issue a device SVID for the sandbox device-agent

The sandbox device-agent already has an SVID from the MIS (used when it talks
to Symphony). Use that same SVID — no new cert is needed. Confirm it is in
place on the device-agent machine:

```bash
# On the device-agent machine:
openssl x509 \
    -in ~/sandbox/poc/device/agent/config/identity/client-svid-cert.pem \
    -text -noout | grep "URI:spiffe"
```

If the SVID is expired or missing, re-generate it on the MIS machine:

```bash
# On the MIS / CTT machine:
bash /home/margo/sandbox/scripts/mis.sh
# Follow prompts — enter the vendor WFM ID and device ID

sudo chown -R $USER:$USER ~/mis-deployment
# Then find the output dir:
ls ~/mis-deployment/ | grep x509svid
```

Then copy the files to the device-agent machine:

```bash
DEVICE_SVID_DIR=~/mis-deployment/x509svid-<wfm-id>-<device-id>
scp ${DEVICE_SVID_DIR}/payload-cert.pem \
    <agent-host>:~/sandbox/poc/device/agent/config/identity/client-svid-cert.pem
scp ${DEVICE_SVID_DIR}/payload-key.pem \
    <agent-host>:~/sandbox/poc/device/agent/config/identity/client-svid-key.pem
```

#### Step 3 — Start the mock WFM server with MIS SVIDs

On the CTT machine:

```bash
cd ctt-runner/device-supplier
MIAF_SERVER_CERT="certs/miaf-server-cert.pem" \
MIAF_SERVER_KEY="certs/miaf-server-key.pem" \
MIAF_TRUST_CA="certs/svid-ca.pem" \
./bin/server
```

Note the machine's IP from the startup banner — you will need it in Step 4.

#### Step 4 — Update the sandbox device-agent config

On the device-agent machine, open
`~/sandbox/poc/device/agent/config/config.yaml` and change only the WFM URL
to point at the CTT mock WFM's MIAF port:

```yaml
wfm:
  sbiUrl: https://<ctt-host-ip>:3003/v1alpha2/margo
```

Leave all MIAF / MIS config unchanged — the device-agent continues to use
the same MIS as before. The mock WFM trusts any SVID whose chain validates
against the MIS CA (`svid-ca.pem`).

Replace `<ctt-host-ip>` with the IP of the CTT machine from Step 3.

#### Step 5 — Restart the sandbox device-agent

```bash
# On the device-agent machine:
bash ~/sandbox/scripts/device-agent.sh docker stop-docker
bash ~/sandbox/scripts/device-agent.sh docker start-docker
```

For K3s:

```bash
bash ~/sandbox/scripts/device-agent.sh k3s stop-k3s
bash ~/sandbox/scripts/device-agent.sh k3s start-k3s
```

#### Step 6 — Watch the mock WFM logs

Back on the CTT machine:

```bash
tail -f /tmp/wfm-server.log
```

You should see the sandbox device-agent connecting over mTLS, presenting its
MIS-issued SVID, and making capability reports and desired-state polls.

#### What the mock WFM does with the real device-agent

The mock WFM accepts any device whose SVID chains to the MIS CA:

- Validates the mTLS client cert chains to `MIAF_TRUST_CA` (`svid-ca.pem`)
- Extracts the device's SPIFFE ID from the cert's `Subject Alternative Name URI`
- Creates a client record keyed by SPIFFE ID
- Serves a desired-state manifest and tracks deployment status
- Enforces all spec requirements (correct headers, ETag, digest, content-type)

Any spec violation in either direction (wrong headers from the device, wrong
response from the WFM) will be visible in the logs.

#### Restoring the sandbox device-agent to its normal config

After integration testing, restore `config.yaml` to point back at Symphony:

```bash
# On the device-agent machine:
WFM_HOST=symphony.machine   # or your real WFM hostname
WFM_PORT=8084

sed -i "s|sbiUrl:.*|sbiUrl: https://$WFM_HOST:$WFM_PORT/v1alpha2/margo|" \
    ~/sandbox/poc/device/agent/config/config.yaml

# Re-enable the real MIS endpoint by editing config.yaml:
# - uncomment  miaf.mis.endpoint and miaf.mis.caPath
# - comment out miaf.mis.trustBundle
```

---

### Integration with any device-agent (generic)

The same approach works for any custom device-agent, not just the Margo sandbox.
The mock WFM trusts any client cert whose chain validates against `MIAF_TRUST_CA`
(the MIS CA). If your device-agent already has a MIS-issued SVID, no cert
generation is needed — just start the mock WFM with `MIAF_TRUST_CA` pointing
at the shared MIS CA and point your device at port 3003.

```
MIAF mTLS URL: https://<mock-wfm-host>:3003/v1alpha2/margo
Client cert:   <your-device-svid-cert.pem>   (MIS-issued, any SPIFFE ID)
Client key:    <your-device-svid-key.pem>
Trust CA:      ctt-runner/device-supplier/certs/svid-ca.pem  (MIS CA)
```

If your device-agent does **not** have a MIS-issued SVID yet, issue one
via `mis.sh` as shown in Phase 1, Path 1, Steps 2–3 above.

The mock WFM logs every request to `/tmp/wfm-server.log`. Watch it while your
device-agent runs to see exactly what it sends and what the WFM validates:

```bash
tail -f /tmp/wfm-server.log
```

---

## Certificate expiry and renewal

| Cert type | Validity | Renewal |
|---|---|---|
| RFC 9421 certs (`server-cert.pem`, `device-*.pem`) | ~2 years (825 days) | Re-run `generate-certs.sh` |
| MIS SVIDs (`miaf-server-cert.pem`, `svid-cert.pem`) | ~90 days (MIS default TTL) | Re-run Steps 2–3 from Phase 1 Path 1 |
| MIS CA (`svid-ca.pem`) | Long-lived (MIS CA cert) | Rare; re-copy from `~/mis-deployment/certs/ca.crt` if MIS CA rotates |

Check expiry:

```bash
# RFC 9421 server cert
openssl x509 -in ctt-runner/device-supplier/certs/server-cert.pem \
    -noout -enddate

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
| `bin/server: no such file` | Not built yet | `go build -o bin/server ./scripts/cmd/device-supplier` from `ctt-runner/device-supplier/` |
| `Failed to start mock server. Check /tmp/wfm-server.log` | Port 3001 or 3003 in use | `lsof -ti :3001 \| xargs kill` then retry |
| `x509: certificate signed by unknown authority` on port 3003 | `MIAF_TRUST_CA` not set or wrong CA | Set `MIAF_TRUST_CA=certs/svid-ca.pem` when starting `bin/server`; ensure `svid-ca.pem` is the MIS CA |
| `openssl verify` fails on `svid-cert.pem` / `svid-ca.pem` | `generate-certs.sh` run after MIS copy, overwriting MIS certs | Re-copy: `cp ~/mis-deployment/certs/ca.crt certs/svid-ca.pem` and re-copy SVID files from `~/mis-deployment/` |
| `x509: certificate has expired` (MIS SVID) | SVID older than 90 days | Re-run `mis.sh`, `sudo chown -R $USER:$USER ~/mis-deployment`, and re-copy cert/key files |
| `x509: certificate has expired` (RFC 9421) | Certs older than 825 days | Re-run `generate-certs.sh` |
| mTLS handshake fails on port 3003 | Device SVID not issued by the same MIS CA | Verify `openssl verify -CAfile certs/svid-ca.pem certs/svid-cert.pem` returns OK |
| 401 on all requests (port 3001) | Request not signed (RFC 9421) | Device-agent must HTTP-sign every request on port 3001 |
| Sandbox device-agent fails to connect | `config.yaml` WFM URL still points at Symphony | Set `wfm.sbiUrl: https://<ctt-host>:3003/v1alpha2/margo` |
| Device-agent connects but WFM log is silent | Mock WFM server not running | Start `bin/server` with MIAF env vars before the device-agent connects |
| `container 'margo-identity-service' is not running` | MIS container stopped | Re-run `bash ~/sandbox/scripts/mis.sh` and follow prompts |
| `~/mis-deployment/` dirs still owned by root | `mis.sh` runs as sudo | `sudo chown -R $USER:$USER ~/mis-deployment` |
| `cp: cannot create regular file 'certs/svid-key.pem': Permission denied` | Files in `certs/` are root-owned or mode 400 | `sudo chown -R $USER:$USER certs/ && chmod -R u+w certs/` then re-copy |
| Go build fails: module not found | Go module cache issue | `go mod download` from `ctt-runner/device-supplier/` |
| Report not generated | Test runner exited non-zero | Check `ctt-runner/reports/device-supplier/test-execution.log` |

---

## Quick-reference checklist

```
Phase 1 — Identity setup (once per MIS deployment; re-run when SVIDs expire)
  □ Verify MIS is running: docker ps --filter name=margo-identity-service
  □ Generate SVIDs: bash ~/sandbox/scripts/mis.sh  (follow prompts for WFM + client SVID)
  □ Fix ownership:  sudo chown -R $USER:$USER ~/mis-deployment
  □ Copy to CTT:    ctt-start.sh → 2) Device Supplier → 1) Setup Identity
                    (same machine: auto-detects ~/mis-deployment; different machine: enter explicit paths)
  □ Verify: openssl verify -CAfile certs/svid-ca.pem certs/miaf-server-cert.pem → OK

Phase 2 — CTT self-test (CTT simulates device-agent against mock WFM)
  □ ctt-start.sh → 2) Device Supplier → 2) Start Mock WFM Server  (auto-builds on first run)
  □ ctt-start.sh → 2) Device Supplier → 3) Run Tests → core
  □ Open report: ctt-runner/reports/device-supplier/conformance-report-<ts>.html

Phase 3 — Integration test with real sandbox device-agent (optional)
  □ Start mock WFM as in Phase 2
  □ On device-agent machine: edit config/config.yaml
      → wfm.sbiUrl: https://<ctt-host>:3003/v1alpha2/margo
      → leave all miaf / mis config unchanged (device keeps using its own MIS SVID)
  □ Use device-agent.sh option 11 to add mock WFM SPIFFE ID to device's authorized.json
  □ Restart device-agent (docker stop-docker / start-docker)
  □ Watch CTT mock WFM logs: tail -f /tmp/wfm-server.log
  □ Restore config.yaml to point back at Symphony when done
```
