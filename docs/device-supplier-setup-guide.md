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

Verify:

```bash
go version      # need 1.24+
openssl version
jq --version
```

---

## Phase 1 — Generate certificates (one-time)

The mock WFM server needs TLS certificates, and the test runner needs device
identity certificates. One script generates everything:

```bash
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 1) Generate Certificates
```

Or run it directly:

```bash
cd ctt-runner/device-supplier
bash generate-certs.sh ./certs localhost
```

Replace `localhost` with your machine's hostname or IP if your real
device-agent will connect from a different machine (so the server TLS cert has
the right SAN):

```bash
bash generate-certs.sh ./certs 192.168.1.50
```

**What this generates in `ctt-runner/device-supplier/certs/`:**

| File | What it is | Used by |
|---|---|---|
| `ca-cert.pem` | Mock WFM CA | Test runner + any real device-agent (to trust the mock WFM) |
| `server-cert.pem` / `server-key.pem` | Mock WFM TLS cert | `bin/server` |
| `device-cert.pem` / `device-key.pem` | EC P-256 device identity (RFC 9421) | Test runner signing |
| `device-ec384-cert.pem` / `device-ec384-key.pem` | EC P-384 (MI-012 test) | Test runner — algorithm variant test |
| `device-rsa2048-cert.pem` / `device-rsa2048-key.pem` | RSA-2048 (MI-014 test) | Test runner — RSA signing test |
| `device-weakkey-cert.pem` / `device-weakkey-key.pem` | RSA-1024 weak key | Test runner — negative test |
| `svid-cert.pem` / `svid-key.pem` | X.509-SVID with SPIFFE URI (mTLS) | Test runner MIAF mTLS steps |
| `svid-ca.pem` | Same as `ca-cert.pem` | Test runner — trusts mock WFM's SVID |
| `untrusted-server-cert.pem` / `untrusted-server-key.pem` | Cert from a different CA | MI-018 negative test (wrong CA rejection) |

The SVID gets the SPIFFE ID `spiffe://margo.org/device/ctt-device-001` baked
in automatically. No external MIS is involved — the CTT is its own CA for
device-supplier tests.

**Cert validity:** ~2 years (825 days). You only need to re-run this script
when certs expire or if you change the server hostname.

---

## Phase 2 — Run the conformance suite

Once certs exist, run everything in one step:

```bash
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 2) Run scenario group tests
```

The runner will:
1. Build `bin/server` and `bin/run_tests` (Go compile, ~10s first time, cached after)
2. Kill any stale server on port 3001/3003
3. Start the mock WFM server in the background
4. Prompt for the mock WFM URL and MIAF mTLS URL
5. Prompt for a test group selection
6. Run all scenarios and print results as they execute
7. Stop the mock server
8. Write the HTML report

### URLs to enter

```
Enter Mock WFM Server URL [https://192.168.x.x:3001/v1alpha2/margo]:
```

Press Enter to use the default (auto-detected from your machine's IP). The
default is always correct for local runs.

```
Enter MIAF mTLS URL for mtls:true steps [https://192.168.x.x:3003/v1alpha2/margo]:
```

Press Enter. The mock server listens on 3001 for RFC 9421 steps and 3003 for
MIAF mTLS steps — these are separate ports.

### Select a test group

```
Available Device Test Groups:
  1) core        v1.0.0-rc.2  — Generic/positive/negative/edge coverage ...
  2) silver      v1.0.0-rc.2  — ...
  3) gold        v1.0.0-rc.2  — ...
```

For a first run, select `core`. It covers the full set of required
conformance scenarios for the device-agent role.

### Watch the output

Each step prints immediately:

```
=== SCENARIO: Capabilities Reporting ===

  [step-2.1]  Report Capabilities with POST
   ▶  PUT    /api/v1/capabilities/device-001  [signed]
   ⚙  MARGO-DEV-MANAGEMENTINTERFACE-016
   ✓  PASS  201 Created

  [step-2.2]  Report Capabilities with PUT (update)
   ▶  PUT    /api/v1/capabilities/device-001  [signed]
   ✓  PASS  200 OK

  [step-neg-1]  Request without signature is rejected
   ▶  POST   /api/v1/capabilities/device-001  [unsigned]
   ✓  PASS  401 Unauthorized

=== SCENARIO: Desired State Retrieval ===
  ...
```

### Final summary and report

```
══════════════════════════════════════════════════════════════════════
 Device Conformance Summary  ·  Group: core  ·  104 tests
══════════════════════════════════════════════════════════════════════

 Scenario                               Steps  Passed  Failed
 Capabilities Reporting                     8       8       0
 Desired State — Manifest Validation       12      12       0
 Deployment Status Reporting                6       6       0
 ...
 TOTAL                                    104     102       2
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
simulated one), use the mock WFM server as the target:

### Step 1 — Generate certs and start the mock server

```bash
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 1) Generate Certificates
# Then:   2) Device Supplier → 2) Start Mock WFM Server (standalone)
```

The server prints its URL when ready:

```
╔══════════════════════════════════════════════════════════════╗
║  Mock WFM Server is ready (MIAF/rc.2 — mTLS + X.509-SVID)   ║
║  WFM URL  : https://192.168.1.50:3001/v1alpha2/margo         ║
║  CA Cert  : ctt-runner/device-supplier/certs/ca-cert.pem     ║
╚══════════════════════════════════════════════════════════════╝
```

### Step 2 — Give the CA cert to your device-agent

Your device-agent needs to trust the mock WFM's TLS certificate. Copy
`ca-cert.pem` to your device-agent machine:

```bash
scp ctt-runner/device-supplier/certs/ca-cert.pem <device-agent-host>:/path/to/trust-store/
```

Or configure your device-agent to use it as the WFM CA:

```yaml
# Example device-agent config
wfm:
  url: https://192.168.1.50:3001/v1alpha2/margo
  ca_cert: /path/to/ca-cert.pem
```

### Step 3 — Provision an mTLS identity (MIAF)

The mock WFM's MIAF port (3003) requires the device-agent to present an
X.509-SVID (a certificate with a SPIFFE URI `Subject Alternative Name`).

For the CTT's own simulated device-agent, this is done automatically
(`svid-cert.pem` from `generate-certs.sh`).

For **your real device-agent**, you need to mint an SVID for it. The mock WFM
trusts any cert signed by the CTT's CA (`ca-cert.pem` / `ca-key.pem`), so
you can generate one locally:

```bash
cd ctt-runner/device-supplier/certs

# Generate key for your device-agent
openssl ecparam -name prime256v1 -genkey -noout -out my-device-key.pem

# Create a CSR
openssl req -new -key my-device-key.pem \
    -subj "/CN=my-device-agent" -out my-device.csr

# Sign it with the CTT CA and embed your device's SPIFFE ID
openssl x509 -req -days 365 \
    -in my-device.csr \
    -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial \
    -out my-device-cert.pem \
    -extfile <(printf \
        "subjectAltName=URI:spiffe://margo.org/device/my-device-001\n\
basicConstraints=CA:FALSE\n\
extendedKeyUsage=clientAuth")
```

Give `my-device-cert.pem` and `my-device-key.pem` to your device-agent as its
mTLS identity for calls to port 3003.

### Step 4 — Connect your device-agent

Point your device-agent at the mock WFM:

```
SBI URL:       https://<mock-wfm-host>:3001/v1alpha2/margo    (RFC 9421 signed)
MIAF mTLS URL: https://<mock-wfm-host>:3003/v1alpha2/margo    (mTLS + SVID)
CA cert:       ca-cert.pem
Client cert:   my-device-cert.pem
Client key:    my-device-key.pem
```

The mock WFM logs every request to `/tmp/wfm-server.log`. Watch it while your
device-agent runs to see exactly what it sends and what the WFM validates:

```bash
tail -f /tmp/wfm-server.log
```

---

## Certificate expiry and renewal

Device-supplier certs are valid for ~2 years (825 days). Check expiry:

```bash
openssl x509 -in ctt-runner/device-supplier/certs/server-cert.pem \
    -noout -enddate
```

To renew all certs, just re-run `generate-certs.sh` (or the menu option).
The new certs replace the old ones in the same directory — no other
configuration changes needed.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `bin/server: no such file` | Not built yet | The menu auto-builds; or run `go build -o bin/server ./scripts/cmd/device-supplier` from `ctt-runner/device-supplier/` |
| `Failed to start mock server. Check /tmp/wfm-server.log` | Port 3001 or 3003 in use | `lsof -ti :3001 | xargs kill` then retry |
| `x509: certificate has expired` | Certs older than 825 days | Re-run `generate-certs.sh` |
| TLS handshake error in device-agent | Device-agent not using `ca-cert.pem` | Copy `ca-cert.pem` to the device-agent's trust store |
| 401 on all requests | Request not signed (RFC 9421) or SVID not presented (MIAF) | Device-agent must sign every request OR present SVID for mTLS port |
| mTLS handshake fails on port 3003 | Device SVID not issued by the CTT CA | Re-generate the device SVID using `ca-cert.pem` as the signing CA |
| Go build fails: module not found | Go module cache issue | Run `go mod download` from `ctt-runner/device-supplier/` |
| Report not generated | Test runner exited non-zero | Check `ctt-runner/reports/device-supplier/test-execution.log` |

---

## Quick-reference checklist

```
Phase 1 — Setup (once, renew after ~2 years)
  □ bash ctt-runner/ctt-start.sh → Device Supplier → Generate Certificates
  □ (Real device only) Copy ctt-runner/device-supplier/certs/ca-cert.pem
    to your device-agent machine
  □ (Real device only) Mint a SVID for your device-agent signed by the CTT CA

Phase 2 — Run (repeat for each test run)
  □ bash ctt-runner/ctt-start.sh → Device Supplier → Run scenario group tests
  □ Press Enter for mock WFM URL (auto-detected)
  □ Press Enter for MIAF mTLS URL (auto-detected)
  □ Select group: core (or the group your engagement specifies)
  □ Wait for run to complete (~1–3 min)
  □ Open report:
      ctt-runner/reports/device-supplier/conformance-report-<ts>.html
```
