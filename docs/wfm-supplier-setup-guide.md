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
Do it once; the SVID cert is valid for 90 days — re-run when it expires.

### What you need to end up with

Three files in `ctt-runner/wfm-supplier/utils/fixtures/miaf/real/`:

```
client-svid-cert.pem    <- CTT's X.509-SVID (SPIFFE URI SAN, signed by your WFM's MIS)
client-svid-key.pem     <- the corresponding private key
trust-bundle-ca.pem     <- the root CA your WFM's MIS publishes
```

`ctt-start.sh` copies these into the active cert directory automatically at
run time. You never reference them by path yourself.

**Check whether they already exist and are still valid:**

```bash
openssl x509 \
    -in ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem \
    -noout -enddate 2>/dev/null \
    && echo "Identity OK - check expiry date above" \
    || echo "No identity found - follow one of the paths below"
```

If the cert exists and the expiry date is in the future, skip to Phase 2.

---

### Recommended: interactive setup script

The fastest way is the dedicated setup script — it guides you through all
three paths with prompts:

```bash
bash ctt-runner/wfm-supplier/setup-miaf-identity.sh
```

Or from the main menu:

```bash
bash ctt-runner/ctt-start.sh
# Select: 1) WFM Supplier → 1. Setup MIAF Identity
```

The script offers three modes — pick the one that matches your environment:

| Mode | When to use |
|---|---|
| **self-signed** | No real WFM yet, or WFM accepts any CA you provide. Generates a local CA + SVID. You give `trust-bundle-ca.pem` to your WFM's admin to import. |
| **sandbox** | Testing against the Margo reference Symphony instance. Mints SVID from the sandbox MIS automatically. Requires access to the sandbox VM or its MIS endpoint. |
| **external** | Testing a vendor WFM with its own SPIFFE infrastructure. You provide cert + key + CA that the vendor's SPIFFE admin issued for you. |

---

### Path A — Margo sandbox / Symphony reference environment

The sandbox runs a real MIS (SPIRE). The setup script automates all three
steps: mint the SVID, register the CTT's SPIFFE ID in Symphony's
accepted-client allowlist, and fetch the trust bundle.

> **Prerequisite:** the `provision-mis-identity.sh` script shells out to
> `scripts/lib/mis/svid-gen.sh` inside the Margo sandbox repo.  If that repo
> is not already cloned on your machine, you have two options:
>
> * Pass `--sandbox-repo-url <git-url>` and the script fetches just the
>   needed scripts via a sparse clone (no full checkout).
> * Or clone the sandbox manually first:
>   `git clone <sandbox-repo-url> ~/test/sandbox`

**Option 1 — via the interactive menu (recommended):**

```bash
bash ctt-runner/wfm-supplier/setup-miaf-identity.sh
# Select: 2) sandbox
# Press Enter at every prompt to accept the defaults
# If sandbox is not cloned: enter the sandbox git URL when prompted
```

**Option 2 — run the provision script directly:**

```bash
# Run from the repo root — all defaults match the sandbox VM
bash ctt-creator/common/scripts/provision-mis-identity.sh

# If sandbox scripts are not yet cloned on this machine:
bash ctt-creator/common/scripts/provision-mis-identity.sh \
    --sandbox-repo-url https://<sandbox-git-url>
```

> **Note:** the script defaults to `https://127.0.0.1:9443` for the MIS URL.
> Use this when running on the sandbox VM itself — the public hostname
> `mis.margo.org:9443` does not hairpin back from inside the same VM.
> From any other machine, use `--mis-base-url https://mis.margo.org:9443`.

The script does three things automatically:

```
Step 1 - Mints a fresh X.509-SVID from the MIS (svid-gen.sh --automated)
         -> writes: miaf/real/client-svid-{cert,key}.pem
         SPIFFE ID: spiffe://margo.org/margo/wfm/symphony-1/client/margo-ctt

Step 2 - Adds the CTT's SPIFFE ID to Symphony's authorized-clients.json

Step 3 - Fetches the trust bundle from https://127.0.0.1:9443
         -> writes: miaf/real/trust-bundle-ca.pem
```

**Verify the result:**

```bash
openssl verify \
    -CAfile ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem
# -> client-svid-cert.pem: OK

openssl x509 \
    -in ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem \
    -noout -enddate
```

---

### Path B — Your own WFM (vendor path)

Your WFM runs its own MIS/SPIFFE infrastructure. You (or your WFM's SPIFFE
administrator) issue an SVID for the CTT.

> The CTT does **not** need access to your MIS infrastructure.  Your SPIFFE
> admin issues the three files and hands them to you.  The CTT just uses them.

**Option 1 — interactive (recommended):**

```bash
bash ctt-runner/wfm-supplier/setup-miaf-identity.sh
# Select: 3) external
# Enter paths to cert, key, and CA when prompted
# The script validates the SPIFFE URI SAN and chain, then installs the files
```

**Option 2 — install files manually:**

**Step 1 — Get a client SVID from your WFM's SPIFFE administrator.**

Ask them to issue an X.509-SVID for the CTT runner.  The cert must have a
SPIFFE URI SAN in the WFM-client path format:

```
spiffe://<your-trust-domain>/margo/wfm/<wfm-instance-id>/client/margo-ctt
```

The administrator must also give you the trust bundle (root CA PEM) their MIS
publishes.  Copy all three files into place:

```bash
cp /path/to/issued/cert.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem

cp /path/to/issued/key.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-key.pem

cp /path/to/trust-bundle-ca.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem
```

**Step 2 — Verify the chain:**

```bash
openssl verify \
    -CAfile ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem
# -> client-svid-cert.pem: OK
```

If this fails, the cert was not issued by the same MIS whose bundle you have.
Check that both files came from the same SPIFFE administrator.

---

### Path C — Self-signed (no MIS, self-contained testing)

Use this when you have no SPIFFE infrastructure yet, or when your WFM is
configured to trust any CA you supply.  The CTT generates its own CA and SVID
— no external system required.

```bash
bash ctt-runner/wfm-supplier/setup-miaf-identity.sh --mode self-signed
```

This writes three files to `miaf/real/` and prints the SPIFFE ID that was
embedded in the cert.

> **Important:** your WFM must be configured to trust the generated
> `trust-bundle-ca.pem` before running tests.  Give this file to your WFM's
> admin and ask them to add it as a trusted SPIFFE CA.  Without this step,
> every mTLS handshake will fail with `unknown CA`.

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

  Interactive (any path):
  □ bash ctt-runner/wfm-supplier/setup-miaf-identity.sh
      -> choose: 1=self-signed  2=sandbox  3=external
  OR via the main menu:
  □ bash ctt-runner/ctt-start.sh
      -> 1) WFM Supplier -> 1. Setup MIAF Identity -> follow prompts

  Self-signed (no MIS):
  □ bash ctt-runner/wfm-supplier/setup-miaf-identity.sh --mode self-signed
  □ Import trust-bundle-ca.pem into your WFM's trusted-CA store

  Sandbox (Symphony reference):
  □ bash ctt-runner/wfm-supplier/setup-miaf-identity.sh --mode sandbox
      (pass --sandbox-repo-url <url> if sandbox scripts are not yet cloned)
  □ Verify: openssl verify -CAfile .../trust-bundle-ca.pem .../client-svid-cert.pem -> OK

  Vendor WFM (external SVID):
  □ Ask your WFM's SPIFFE admin for: cert.pem, key.pem, trust-bundle-ca.pem
  □ bash ctt-runner/wfm-supplier/setup-miaf-identity.sh --mode external \
        --cert cert.pem --key key.pem --ca trust-bundle-ca.pem
  □ Verify chain (same openssl verify command above)

Phase 2 — Run (repeat for each test run)
  □ bash ctt-runner/ctt-start.sh
  □ Select: 1) WFM Supplier -> 2. Run scenario group tests -> core
  □ Enter WFM SBI URL (mTLS port, e.g. https://localhost:8084/v1alpha2/margo)
  □ Press Enter for MIAF URL (same port unless WFM splits them)
  □ Multi-component prompts: press Enter x3 for Symphony, or enter NBI URL/creds
  □ Wait for run to complete (~2-5 min)
  □ Open report: ctt-runner/reports/wfm-supplier/wfm-scenario-report-core_<ts>.html
```
