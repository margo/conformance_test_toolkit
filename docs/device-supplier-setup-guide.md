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
  1) core        v1.0.0-rc.3  — Generic/positive/negative/edge coverage ...
  2) silver      v1.0.0-rc.3  — ...
  3) gold        v1.0.0-rc.3  — ...
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

#### Step 1 — Generate CTT certificates (if not already done)

```bash
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 1. Generate Certificates
```

Or directly:

```bash
cd ctt-runner/device-supplier
bash generate-certs.sh ./certs localhost   # or use your machine's IP
```

#### Step 2 — Start the mock WFM server

```bash
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 2. Start Mock WFM Server
```

The server prints its URL when ready:

```
╔══════════════════════════════════════════════════════════════════════════════╗
║  Mock WFM Server is ready (MIAF/rc.3 — mTLS + X.509-SVID identity)         ║
╠══════════════════════════════════════════════════════════════════════════════╣
║  WFM URL  : https://192.168.1.50:3001/v1alpha2/margo                        ║
║  CA Cert  : ctt-runner/device-supplier/certs/ca-cert.pem                    ║
╚══════════════════════════════════════════════════════════════════════════════╝
```

#### Step 3 — Export identity for the sandbox device-agent

Use the menu to generate a SVID for the sandbox device-agent and a SPIFFE trust
bundle the device can use to verify the mock WFM's certificate:

```bash
bash ctt-runner/ctt-start.sh
# Select: 2) Device Supplier → 5. Export Identity for Sandbox Device-Agent
```

You will be prompted for:

| Prompt | What to enter |
|---|---|
| SPIFFE ID for sandbox device-agent | press Enter for `spiffe://margo.org/device/sandbox-device-001` |
| Output directory | press Enter for `~/ctt-sandbox-agent-identity` |

The script writes four files to the output directory:

| File | Purpose |
|---|---|
| `sandbox-device-svid-cert.pem` | Device mTLS client cert (SVID) — copy to device-agent |
| `sandbox-device-svid-key.pem` | Device mTLS private key — copy to device-agent |
| `ctt-ca-cert.pem` | CTT CA trust anchor — copy to device-agent |
| `ctt-trust-bundle.json` | SPIFFE JWKS trust bundle — copy to device-agent |

It also prints the exact config patch and copy commands for you to follow.

If you prefer to run the steps manually instead of using the menu:

```bash
cd ctt-runner/device-supplier/certs

DEVICE_SPIFFE="spiffe://margo.org/device/sandbox-device-001"

# Generate key for the sandbox device-agent
openssl ecparam -name prime256v1 -genkey -noout \
    -out sandbox-device-svid-key.pem

# Create a CSR
openssl req -new \
    -key sandbox-device-svid-key.pem \
    -subj "/CN=sandbox-device-agent/O=Margo CTT" \
    -out sandbox-device.csr

# Sign it with the CTT CA, embedding the device's SPIFFE ID
openssl x509 -req -days 365 \
    -in sandbox-device.csr \
    -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial \
    -out sandbox-device-svid-cert.pem \
    -extfile <(printf \
        "subjectAltName=URI:%s\nbasicConstraints=CA:FALSE\nextendedKeyUsage=clientAuth" \
        "$DEVICE_SPIFFE")

rm sandbox-device.csr

# Build the SPIFFE trust bundle in JWKS format from the CTT CA
DER_B64=$(openssl x509 -in ca-cert.pem -outform DER | base64 | tr -d '\n')
printf '{"keys":[{"kty":"RSA","use":"x509-svid","x5c":["%s"]}]}' "$DER_B64" \
    > ctt-trust-bundle.json
```

#### Step 4 — Copy files to the sandbox device-agent machine

```bash
AGENT_HOST=<sandbox-device-agent-machine>
CERTS=ctt-runner/device-supplier/certs

# SVID cert + key (device's mTLS identity)
scp $CERTS/sandbox-device-svid-cert.pem  $AGENT_HOST:~/sandbox/poc/device/agent/config/identity/
scp $CERTS/sandbox-device-svid-key.pem   $AGENT_HOST:~/sandbox/poc/device/agent/config/identity/

# SPIFFE trust bundle (to verify the mock WFM's cert without a live MIS)
scp $CERTS/ctt-trust-bundle.json          $AGENT_HOST:~/sandbox/poc/device/agent/config/mis/
```

If the CTT and the sandbox device-agent are on the **same machine**, use `cp`
instead of `scp`, or just point the config at the absolute paths directly.

#### Step 5 — Update the sandbox device-agent config

On the device-agent machine, open
`~/sandbox/poc/device/agent/config/config.yaml` and apply these changes:

```yaml
# Change the WFM URL from Symphony to the CTT mock WFM MIAF port (3003)
wfm:
  sbiUrl: https://<ctt-host-ip>:3003/v1alpha2/margo

# Update the MIAF identity to use the CTT-signed SVID
miaf:
  x509:
    certPath: "./config/identity/sandbox-device-svid-cert.pem"
    keyPath:  "./config/identity/sandbox-device-svid-key.pem"
  mis:
    # endpoint: "https://mis.margo.org:9443"   # disable - CTT mock WFM has no live MIS
    # caPath: "./config/mis/https-ca.crt"       # disable
    cacheInterval: 60
    trustBundle:
      path: "./config/mis/ctt-trust-bundle.json"   # static SPIFFE JWKS from CTT CA
```

Replace `<ctt-host-ip>` with the IP address of the machine running the CTT mock
WFM (the value printed when you started it in Step 2).

#### Step 6 — Restart the sandbox device-agent

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

#### Step 7 — Watch the mock WFM logs

Back on the CTT machine:

```bash
tail -f /tmp/wfm-server.log
```

You should see the sandbox device-agent connecting, presenting its SVID, and
making capability reports and desired-state polls. The mock WFM identifies it
by its SPIFFE ID (`spiffe://margo.org/device/sandbox-device-001`) and serves
the standard desired-state manifest.

#### What the mock WFM does with the real device-agent

The mock WFM treats the sandbox device-agent exactly the same as the CTT's
own simulated device-agent:

- Validates the mTLS client cert is signed by the CTT CA
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
The mock WFM trusts any client cert signed by its CA, so:

1. Generate a SVID for your device using the CTT CA (Step 3 commands above,
   replacing the SPIFFE ID with your device's own ID)
2. Configure your device-agent's mTLS cert to use the generated cert/key
3. Configure your device-agent's trust store to use `ctt-ca-cert.pem`
4. Point your device-agent at port 3003 for MIAF mTLS calls

```
MIAF mTLS URL: https://<mock-wfm-host>:3003/v1alpha2/margo
CA cert:       ctt-runner/device-supplier/certs/ca-cert.pem
Client cert:   <your-device-svid-cert.pem>
Client key:    <your-device-svid-key.pem>
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
| Sandbox device-agent fails to connect | `config.yaml` still pointing at Symphony | Check `wfm.sbiUrl` is `https://<ctt-host>:3003/...` |
| Trust bundle error in device-agent logs | `ctt-trust-bundle.json` not in place or wrong path | Re-run option 5 → copy output file to `config/mis/ctt-trust-bundle.json` on agent machine |
| Device-agent connects but no requests in WFM log | Mock WFM server not running | Start mock WFM (option 2) before the device-agent connects |
| 401 errors in mock WFM log for sandbox device-agent | SVID presented by device is not signed by CTT CA | Use the SVID from option 5 output, not from `~/certs/` on the sandbox |
| Go build fails: module not found | Go module cache issue | Run `go mod download` from `ctt-runner/device-supplier/` |
| Report not generated | Test runner exited non-zero | Check `ctt-runner/reports/device-supplier/test-execution.log` |

---

## Quick-reference checklist

```
Phase 1 — Setup (once, renew after ~2 years)
  □ bash ctt-runner/ctt-start.sh → Device Supplier → 1. Generate Certificates

Phase 2 — Run CTT self-test (CTT simulates device-agent)
  □ bash ctt-runner/ctt-start.sh → Device Supplier → 2. Start Mock WFM Server
  □ bash ctt-runner/ctt-start.sh → Device Supplier → 3. Run Tests
  □ Press Enter for mock WFM URL (auto-detected)
  □ Press Enter for MIAF mTLS URL (auto-detected)
  □ Select group: core (or the group your engagement specifies)
  □ Wait for run to complete (~1–3 min)
  □ Open report:
      ctt-runner/reports/device-supplier/conformance-report-<ts>.html

Phase 3 — Integration test with real sandbox device-agent (optional)
  □ bash ctt-runner/ctt-start.sh → Device Supplier → 2. Start Mock WFM Server
  □ bash ctt-runner/ctt-start.sh → Device Supplier → 5. Export Identity for Sandbox Device-Agent
      → Press Enter for SPIFFE ID (spiffe://margo.org/device/sandbox-device-001)
      → Press Enter for output dir (~/.ctt-sandbox-agent-identity)
      → Follow the printed copy + config patch instructions
  □ On device-agent machine: edit config/config.yaml
      → wfm.sbiUrl: https://<ctt-host>:3003/v1alpha2/margo
      → miaf.x509.certPath/keyPath: sandbox-device-svid-cert/key.pem
      → miaf.mis.trustBundle.path: ./config/mis/ctt-trust-bundle.json
      → comment out miaf.mis.endpoint and miaf.mis.caPath
  □ Restart device-agent (docker stop-docker / start-docker)
  □ Watch CTT mock WFM logs: tail -f /tmp/wfm-server.log
  □ Restore config.yaml to point back at Symphony when done
```
