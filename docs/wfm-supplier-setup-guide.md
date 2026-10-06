# WFM Supplier Conformance — Setup and Execution Guide

**Persona:** WFM Supplier  
**Spec baseline:** Margo `1.0.0-rc.3` (MIAF / mTLS authentication)  
**Audience:** WFM vendors, Margo adopters, integration engineers

This guide walks you from a clean checkout to a signed conformance report,
step by step. Follow it once and you will understand every moving part; on
subsequent runs only the identity setup (Phase 1) needs attention if certs
have expired.

---

## What the CTT does in this persona

The CTT acts as a **device / WFM-Client**. It fires real HTTPS requests at
your WFM, checks the responses against the Margo spec, and generates an HTML
report where every test row is traced to a specific requirement ID.

Authentication is **mutual TLS (MIAF)**: the CTT presents an X.509-SVID
(a SPIFFE-based certificate) that your WFM's MIS issued, and your WFM
presents its own. No username/password, no API keys.

```
CTT (device-agent role)          Your WFM
  svid-cert.pem  ──mTLS──►  validates against trust-bundle-ca.pem
                 ◄──mTLS──  validates CTT trusts WFM's own cert
        ▼
  run_wfm_scenarios.js
  fires scenario steps
        ▼
  ctt-runner/reports/wfm-supplier/
  wfm-scenario-report-core_<timestamp>.html
```

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Node.js | any recent LTS | drives `run_wfm_scenarios.js`; no `npm install` needed |
| OpenSSL | 1.1+ | certificate inspection; usually pre-installed |
| jq | any | group file parsing; `apt install jq` / `brew install jq` |
| bash | 4+ | macOS ships bash 3 — install bash 5 via Homebrew |

Clone or check out the repo, then verify:

```bash
node --version
openssl version
jq --version
```

---

## Phase 1 — Identity setup (one-time per WFM)

This is the only step that requires interacting with your WFM outside the CTT.
You do it once; the certs are valid for 90 days (re-run when they expire).

### What you need to end up with

Three files in `ctt-runner/wfm-supplier/utils/fixtures/miaf/real/`:

```
client-svid-cert.pem    ← your CTT's X.509-SVID, issued by your WFM's MIS
client-svid-key.pem     ← the corresponding private key
trust-bundle-ca.pem     ← the root CA your WFM's MIS publishes
```

`ctt-start.sh` copies these into the active cert directory automatically at
run time. You never reference them by path yourself.

---

### Path A — Symphony / Margo sandbox (reference environment)

The sandbox runs a real MIS at `mis.margo.org:9443`. The CTT's cert is
already minted and stored in `miaf/real/` — you only need to re-mint if it
has expired (90-day TTL, minted 2026-09-23).

**Check expiry:**

```bash
openssl x509 -in ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem \
    -noout -enddate
# notAfter=Dec 22 00:00:00 2026 GMT  ← still valid; skip to Phase 2
```

**Re-mint (if expired):**

```bash
# Step 1: fetch the current trust bundle from the MIS
node ctt-runner/wfm-supplier/scripts/run_wfm_scenarios.js \
    --fetch-trust-bundle https://127.0.0.1:9443 \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem

# Step 2: mint a new SVID via the MIS's API
# (The sandbox's MIS has a REST endpoint; adjust to your environment's mint command)
# Replace the output files in miaf/real/ with the new cert + key.
```

> **Note (sandbox-specific):** use `https://127.0.0.1:9443` instead of
> `https://mis.margo.org:9443` when running on the sandbox VM itself — the
> public hostname does not hairpin back on port 9443 from inside the same host.
> From any other machine, the public hostname works normally.

---

### Path B — Your own WFM (vendor path)

Your WFM runs its own MIS. The steps are the same; only the URLs change.

**Step 1 — Fetch the trust bundle**

The MIS publishes a discovery document at `/.well-known/margo`. Use the CTT's
built-in fetch command to pull the trust bundle and write it as a CA PEM file:

```bash
node ctt-runner/wfm-supplier/scripts/run_wfm_scenarios.js \
    --fetch-trust-bundle https://<your-mis-host>:<port> \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem
```

This calls `/.well-known/margo` (unauthenticated per spec), extracts the
`trustBundleUri`, fetches the bundle, and writes out the X.509 anchors. You
should see:

```
Fetching discovery document: https://your-mis-host:443/.well-known/margo
  trustDomain:    your-domain.com
  trustBundleUri: https://your-mis-host:443/v1/bundle
Fetching trust bundle: https://your-mis-host:443/v1/bundle
✓ wrote 1 trust anchor(s) to .../trust-bundle-ca.pem
```

**Step 2 — Get a client SVID for the CTT**

This step depends on your WFM's tooling. The outcome is an X.509 certificate
with a SPIFFE URI `Subject Alternative Name` that identifies the CTT as a
registered device/client in your WFM.

Common approaches:
- WFM admin console: "Register device" or "Issue client certificate" → download
- CLI tool your WFM ships: `wfm-cli issue-svid --client margo-ctt --ttl 90d`
- Your MIS's REST API directly

The issued cert will have a SPIFFE ID embedded, for example:

```
SAN URI: spiffe://your-domain.com/margo/wfm/my-wfm-instance/client/margo-ctt
```

**You do not need to know or record this URI** — the CTT reads it from the
cert automatically at run time and uses it wherever the SPIFFE ID is needed
(including as the device target in the multi-component deployment scenario).

**Step 3 — Place the files**

```bash
# Replace with the actual paths your WFM tooling wrote them to
cp /path/to/issued/client-cert.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem
cp /path/to/issued/client-key.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-key.pem
# trust-bundle-ca.pem was written by --fetch-trust-bundle above
```

**Step 4 — Verify the chain**

```bash
openssl verify \
    -CAfile ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem
# → client-svid-cert.pem: OK
```

If this fails, the cert was not issued by the MIS whose bundle you fetched.
Re-check Step 1 is pointing at the same MIS that issued the cert.

---

## Phase 2 — Run the conformance suite

Once the three files in `miaf/real/` are in place, everything else is
automated.

### Launch the runner

```bash
cd /path/to/conformance_test_toolkit
bash ctt-runner/ctt-start.sh
```

You will see an interactive menu. Select:

```
1) WFM Supplier
```

Then:

```
2) Run scenario group tests
```

Then select the group. For the full WFM conformance test set:

```
core  (wfm-supplier/core)
```

### Enter your WFM endpoints

The runner will prompt for two URLs:

```
Enter WFM SBI URL (e.g. https://<your-wfm-host>:<port>/v1alpha2/margo):
```

This is the **SBI (South-Bound Interface)** — the port the WFM exposes for
device agents. It is the mTLS port. For Symphony sandbox:
`https://localhost:8084/v1alpha2/margo`

```
Enter MIAF mTLS URL for mtls:true steps [Enter = same as above, ...]:
```

If your WFM uses a single port for both plain-TLS and mTLS steps, press Enter
to use the same URL. If it has separate ports (Symphony does: 8082 NBI, 8084
SBI), enter the mTLS SBI URL here. For Symphony sandbox: press Enter.

### Multi-component deployment — fully automated

If the selected group includes the `wfm-multi-component-deployment` scenario,
the runner detects it and runs a provisioning step automatically:

```
────────────────────────────────────────────────────────────────
[INFO] Group includes 'wfm-multi-component-deployment'
[INFO] Pre-seeding a multi-component ApplicationDeployment via the WFM NBI...
[INFO]   Detected device SPIFFE ID: spiffe://your-domain.com/.../margo-ctt

  WFM NBI base URL [https://localhost:8082/v1alpha2]:
  NBI username [admin]:
  NBI password (leave empty for Symphony default — empty password):
────────────────────────────────────────────────────────────────
```

This scenario requires a deployment to exist in the WFM **before** the CTT
starts polling for it. The CTT provisions it automatically here, so you do not
need to create it manually in your WFM console.

**What to enter at these prompts:**

| Prompt | Symphony sandbox | Your WFM |
|---|---|---|
| NBI base URL | press Enter (uses default) | your WFM's operator API base URL |
| NBI username | press Enter (uses `admin`) | your WFM's operator username |
| NBI password | press Enter (empty = Symphony default) | your operator password |

The SPIFFE ID is read automatically from the cert you placed in `miaf/real/`
in Phase 1 — you do not enter it.

After provisioning, the runner proceeds into the scenario execution loop.

### Watch the output

Each step prints immediately as it runs:

```
  [wfm-core-cap-1]  Report Device Capabilities
   ▶  POST   /api/v1/capabilities/<device-id>  [mTLS · X.509-SVID]
   ⚙  MARGO-WFM-MANAGEMENTINTERFACE-001
   ✓  PASS  201 Created

  [wfm-core-ds-1]  Retrieve Desired State
   ▶  GET    /api/v1/deployments  [mTLS · X.509-SVID]
   ⚙  MARGO-WFM-MANAGEMENTINTERFACE-005
   ✓  PASS  200 OK
        ↳ deploymentId = "ctt-multi-component-20261006120000"

  [efd8ad9d]  Desired State — Cache-Control must not be immutable
   ▶  GET    /api/v1/deployments  [mTLS · X.509-SVID]
   ✗  FAIL  expected 200 OK  ·  got 200 OK
        • _headers.cache-control must not contain "immutable" ...
```

Failures show the exact field, the expected value, and the actual value — so
you can trace each failure directly to a spec requirement.

### Final summary

At the end you see a pass/fail table per scenario and a list of every failed
step:

```
══════════════════════════════════════════════════════════════════════
 CONFORMANCE SUMMARY  ·  Group: core  ·  31 tests
══════════════════════════════════════════════════════════════════════

 Scenario                               Steps  Passed  Failed
 ──────────────────────────────────────────────────────────────────
 WFM Core Capabilities                      4       3       1
 WFM Desired State                          3       3       0
 WFM Multi-Component Deployment             4       3       1
 ...

 FAILED TESTS:
  ✗  efd8ad9d  [WFM Multi-Component Deployment]  Cache-Control immutable
     GET /api/v1/deployments
     Expected 200 OK  ·  Got 200 OK
     • _headers.cache-control must not contain "immutable" (MI-035)
```

### The HTML report

The report is written to:

```
ctt-runner/reports/wfm-supplier/wfm-scenario-report-core_<timestamp>.html
```

Open it in any browser. It contains:
- A conformance requirement coverage table (green = at least one passing step covers the CR-ID, red = all steps for that CR-ID failed)
- A scenario-level summary table
- A full step-detail table with status, method, endpoint, expected/actual HTTP code, and failure reason

This is the artifact you submit as evidence of conformance.

---

## Certificate expiry and renewal

| File | TTL | What to do when expired |
|---|---|---|
| `miaf/real/client-svid-cert.pem` | 90 days (MIS-issued) | Re-mint via your MIS (Path A or B above, Step 2) |
| `miaf/real/trust-bundle-ca.pem` | Rotates when MIS rotates CA | Re-run `--fetch-trust-bundle` |

Check expiry any time:

```bash
openssl x509 -in ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem \
    -noout -enddate
```

If you see TLS handshake errors (`certificate required`, `unknown CA`,
`certificate has expired`) during a test run, the SVID or trust bundle is the
first thing to check.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `SVID certs not found at miaf/ — required for mtls:true steps` | `miaf/real/` is empty | Complete Phase 1 |
| TLS transport error: `certificate required` | The mTLS SBI URL you entered does not require mTLS, or the cert path is wrong | Verify the SBI URL is the mTLS port; re-check Phase 1 |
| TLS transport error: `unknown CA` or `certificate has expired` | Stale trust bundle or expired SVID | Re-run `--fetch-trust-bundle` and re-mint SVID |
| `openssl verify` fails | Cert issued by a different MIS than the bundle you fetched | Re-run both steps from the same MIS |
| Multi-component poll times out (300s) | Provision script failed | Check the NBI URL / credentials you entered; run `bash ctt-runner/wfm-supplier/utils/fixtures/provision-multi-component.sh` manually with `--nbi-url` / `--nbi-user` to debug |
| `ERROR: failed to get auth token` from provision script | NBI credentials wrong | Verify username/password against your WFM's operator API |
| Steps fail with unexpected 500 | WFM internal error | This is a WFM implementation gap — note the CR-ID in the report and report to your WFM vendor |
| `Cache-Control: immutable` failure on manifest endpoint | WFM sets immutable on a mutable resource | WFM implementation gap — the manifest changes when deployments are added/removed (MI-035) |

---

## Cleanup after a test run

The provision script creates a deployment with a timestamp-based name
(`ctt-multi-component-<timestamp>`) so re-runs never collide with leftovers.
To clean up all CTT-created deployments from your WFM after testing:

```bash
bash ctt-runner/wfm-supplier/utils/fixtures/provision-multi-component.sh \
    --nbi-url <your-nbi-url> \
    --nbi-user <your-username> \
    --cleanup
```

For Symphony sandbox (no args):

```bash
bash ctt-runner/wfm-supplier/utils/fixtures/provision-multi-component.sh --cleanup
```

---

## Quick-reference checklist

```
Phase 1 — Identity (once per WFM, renew after 90 days)
  □ Fetch trust bundle:
      node .../run_wfm_scenarios.js --fetch-trust-bundle <mis-url> .../trust-bundle-ca.pem
  □ Obtain client SVID from your WFM's MIS (cert + key)
  □ Place both files in ctt-runner/wfm-supplier/utils/fixtures/miaf/real/
  □ Verify: openssl verify -CAfile trust-bundle-ca.pem client-svid-cert.pem → OK

Phase 2 — Run (repeat for each test run)
  □ bash ctt-runner/ctt-start.sh
  □ Select: WFM Supplier → Run scenario group tests → core
  □ Enter WFM SBI URL (mTLS port)
  □ Press Enter for MIAF URL (same port, usually)
  □ Multi-component prompts: enter NBI URL / credentials (or press Enter for Symphony)
  □ Wait for run to complete (~2–5 min)
  □ Open report: ctt-runner/reports/wfm-supplier/wfm-scenario-report-core_<ts>.html
```
