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
Do it once; re-run only if certs expire or you switch to a different WFM.

### What you need to end up with

Three files in `ctt-runner/wfm-supplier/utils/fixtures/miaf/real/`:

```
client-svid-cert.pem    <- CTT's X.509-SVID (SPIFFE URI SAN)
client-svid-key.pem     <- the corresponding private key
trust-bundle-ca.pem     <- CA cert CTT uses to verify the WFM's server cert
```

`ctt-start.sh` copies these into the active cert directory automatically at
run time. You never reference them by path yourself.

**Check whether they already exist and are still valid:**

```bash
openssl x509 \
    -in ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem \
    -noout -enddate 2>/dev/null \
    && echo "Identity OK - check expiry date above" \
    || echo "No identity found - run identity setup below"
```

If the cert exists and the expiry is in the future, skip to Phase 2.

### Using the interactive menu (recommended)

```bash
bash ctt-runner/ctt-start.sh
# Select: 1) WFM Supplier → 1. Setup MIAF Identity
```

The menu prompts you to choose your setup path and guides through every step.
The sections below document each path in detail.

---

### Path 1 — External / Centralized MIS

Use this path when a centralized MIS is deployed. The MIS admin mints certs for CTT; you place
them in the fixture paths below. Each vendor WFM admin adds CTT's SPIFFE ID to
their WFM's authorized-clients list.

```
Centralized MIS  ──issues SVIDs──►  CTT (mock device)  ──mTLS──►  Vendor WFM
                 ──issues SVIDs──►  CTT (mock WFM)     ◄──mTLS──  Vendor device
```

CTT needs **one set of certs per persona** (one device SVID for WFM Supplier
testing, one WFM SVID for Device Supplier testing). These come from the MIS
admin. Each vendor WFM admin adds CTT's SPIFFE ID to their WFM's allowlist.

#### What to request from the MIS admin

Ask the MIS admin to issue two SVIDs for CTT and share the MIS root CA.
Provide them with the SPIFFE IDs below — they substitute their actual trust
domain and WFM IDs.

| Item | SPIFFE ID / file | Used for |
|---|---|---|
| CTT device SVID | `spiffe://<td>/margo/wfm/<vendor-wfm-id>/client/margo-ctt` | CTT acting as mock device → vendor WFM |
| CTT WFM SVID | `spiffe://<td>/margo/wfm/ctt-mock-wfm` | CTT acting as mock WFM ← vendor device |
| MIS root CA | `ca.crt` / `ca.pem` | Both: chain verification |

> **One CTT device SVID per vendor WFM** — each SPIFFE ID encodes the vendor's
> WFM ID. If five vendors bring WFMs, the MIS admin issues five device SVIDs
> for CTT (one per WFM under test).

#### Step 1 — Request SVIDs from the MIS admin

Contact the MIS admin and provide the SPIFFE IDs below. They will use their
deployment's tooling to mint the certs and share the files with you.

**For WFM Supplier testing (CTT acts as mock device):**

Request one device SVID per vendor WFM under test:

```
SPIFFE ID: spiffe://<trust-domain>/margo/wfm/<vendor-wfm-id>/client/margo-ctt
```

**For Device Supplier testing (CTT acts as mock WFM):**

Request one WFM SVID for the CTT mock WFM:

```
SPIFFE ID: spiffe://<trust-domain>/margo/wfm/ctt-mock-wfm
```

In both cases, also ask for the **MIS root CA certificate** — CTT needs it to
verify the peer's cert during the mTLS handshake.

#### Step 2 — Place received certs in CTT fixture paths

Once the MIS admin delivers the cert files (via scp, shared storage, or any
other method), copy them to the following locations:

```bash
CTT_REPO=<path-to-ctt-repo>   # e.g. /home/margo/test/sandbox

# ── WFM Supplier fixtures (CTT as mock device) ───────────────────────────
cp <received>/device-svid-cert.pem \
    ${CTT_REPO}/ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem
cp <received>/device-svid-key.pem \
    ${CTT_REPO}/ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-key.pem
cp <received>/ca.pem \
    ${CTT_REPO}/ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem

# ── Device Supplier certs (CTT as mock WFM) ──────────────────────────────
cp <received>/wfm-svid-cert.pem \
    ${CTT_REPO}/ctt-runner/device-supplier/certs/server-cert.pem
cp <received>/wfm-svid-key.pem \
    ${CTT_REPO}/ctt-runner/device-supplier/certs/server-key.pem
cp <received>/ca.pem \
    ${CTT_REPO}/ctt-runner/device-supplier/certs/svid-ca.pem

chmod 644 \
    ${CTT_REPO}/ctt-runner/wfm-supplier/utils/fixtures/miaf/real/*.pem \
    ${CTT_REPO}/ctt-runner/device-supplier/certs/*.pem
```

> Rename files as needed — what matters is the destination path, not the
> source filename. The MIS admin may use different naming conventions.

#### Step 3 — Verify certs on the CTT machine

```bash
cd <path-to-ctt-repo>

# WFM Supplier (CTT device cert chains to MIS CA)
openssl verify \
    -CAfile ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem
# -> client-svid-cert.pem: OK

# Print CTT's device SPIFFE ID (share this with each vendor WFM admin)
openssl x509 \
    -in ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem \
    -text -noout | grep "URI:spiffe"

# Device Supplier (CTT WFM cert chains to MIS CA)
openssl verify \
    -CAfile ctt-runner/device-supplier/certs/svid-ca.pem \
    ctt-runner/device-supplier/certs/server-cert.pem
# -> server-cert.pem: OK

# Print CTT's WFM SPIFFE ID (share this with each vendor device admin)
openssl x509 \
    -in ctt-runner/device-supplier/certs/server-cert.pem \
    -text -noout | grep "URI:spiffe"
```

#### Step 4 — What to share with each vendor

**WFM vendors** (vendor brings WFM, CTT acts as device):

| Item | Value |
|---|---|
| CTT's device SPIFFE ID | printed by verify step above — add to their WFM allowlist |
| MIS root CA | `trust-bundle-ca.pem` — vendor WFM must trust this CA to verify CTT's cert |

**Device vendors** (vendor brings device-agent, CTT acts as WFM):

| Item | Value / command |
|---|---|
| CTT's mock WFM URL | `https://<ctt-machine-ip>:3001/v1alpha2/margo` |
| CTT's WFM SPIFFE ID | printed by verify step above |
| MIS root CA | `svid-ca.pem` — device must trust this CA to verify CTT mock WFM's cert |
| Device SVID | MIS admin issues one for the vendor's device-agent; vendor configures their device with it |

#### Pre-run checklist

```
Setup (one-time per MIS deployment):
  □ Provide MIS admin with SPIFFE IDs for CTT (one device SVID per vendor WFM + one WFM SVID)
  □ Receive cert files from MIS admin and place them in CTT fixture paths (Step 2 above)
  □ Verify all chains on CTT machine (Step 3 above)
  □ Confirm CTT machine can reach vendor WFM SBI ports (firewall / network)
  □ Share CTT's device SPIFFE ID with each WFM vendor (for their allowlist)
  □ Share CTT's WFM SPIFFE ID + mock WFM URL + MIS CA with each device vendor

Per vendor (each test run):
  □ WFM vendor confirms they added CTT's SPIFFE ID to their allowlist
  □ Device vendor confirms their device has MIS-issued SVID + CTT CA imported
  □ Run WFM Supplier core group → collect HTML report
  □ Run Device Supplier core group → collect HTML report
```

---

### Path 2 — Vendor WFM with its own SPIFFE infrastructure

The vendor's WFM runs its own MIS. Their SPIFFE admin issues CTT a client SVID
and provides the trust bundle CA. CTT does **not** need access to their MIS.
The `ctt-start.sh` menu option 2 walks through this interactively.

**What to ask the vendor's SPIFFE administrator for:**

| File | What it is |
|---|---|
| `client-svid-cert.pem` | X.509-SVID for CTT, with SPIFFE URI SAN |
| `client-svid-key.pem` | Private key for the above cert |
| `trust-bundle-ca.pem` | Root CA PEM their MIS uses to sign SVIDs |

**What to share with the vendor's WFM admin:**

The SPIFFE ID CTT will present — read it from the cert after they issue it:

```bash
openssl x509 \
    -in ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem \
    -text -noout | grep "URI:spiffe"
```

The WFM admin adds this SPIFFE ID to their WFM's authorized-clients list.

**Verify the chain after copying files:**

```bash
openssl verify \
    -CAfile ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem
# -> client-svid-cert.pem: OK
```

If this fails, the cert and CA came from different sources — check with the
vendor's SPIFFE admin.

---

### Path 3 — When testing with sandbox / Symphony MIS

Use this path when running CTT against the Margo sandbox (Symphony as WFM) in
a local development environment.

Two sub-options depending on whether the sandbox MIS is available:

| | Path 3A — sandbox mis.sh | Path 3B — ctt-mis.sh |
|---|---|---|
| Requires sandbox MIS running | Yes | No |
| Symphony config changes needed | None | Yes (patch JSON + copy trust bundle) |
| Cert lifetime | 90 days (MIS-issued) | ~2 years (self-signed) |

After running Prerequisites (`sudo -E bash wfm.sh → 1`), Symphony prints
the three steps that must be completed before starting it:

```
1. Place your WFM SVID & Key in:   $HOME/symphony/api/certificates/
2. Place the MIS HTTPS CA cert in: $HOME/symphony/api/mis/https-ca.crt
3. Register SPIFFE IDs of WFM Clients via menu option 7
Then: Start Symphony via menu option 3
```

Both sub-paths below satisfy these same three steps — the only difference is
where the files come from.

#### Path 3A — Using sandbox mis.sh (simpler, recommended)

> **Important:** Run `mis.sh` as your regular user (no `sudo`). Running it with
> `sudo` causes `$HOME` to resolve to `/root`, placing certs in `/root/mis-deployment/`
> instead of `$HOME/mis-deployment/` — the paths below will be wrong and Symphony
> will fail to start with a TLS or cert mismatch error.

**Step 1 — Generate identity via sandbox mis.sh:**

```bash
# Run WITHOUT sudo so certs land in $HOME/mis-deployment/
bash /home/margo/sandbox/scripts/mis.sh
# Follow prompts to generate WFM SVID + device SVID
```

**Step 2 — Find the output directories from mis.sh:**

mis.sh writes SVIDs into `$HOME/mis-deployment/` in directories named after
the WFM ID and client ID you entered during generation:

```
x509svid-<wfm-id>            ← Symphony's WFM SVID (server identity)
x509svid-<wfm-id>-<client-id> ← CTT's device SVID (client identity)
```

Check what was generated:

```bash
ls $HOME/mis-deployment/ | grep x509svid
```

Set variables for the rest of the steps:

```bash
WFM_SVID_DIR=$HOME/mis-deployment/x509svid-<wfm-id>
WFM_CLIENT_SVID_DIR=$HOME/mis-deployment/x509svid-<wfm-id>-<client-id>
# Example: if wfm-id="wfm", client-id="wfm-client":
#   WFM_SVID_DIR=$HOME/mis-deployment/x509svid-wfm
#   WFM_CLIENT_SVID_DIR=$HOME/mis-deployment/x509svid-wfm-wfm-client
```

**Step 3 — Place WFM SVID & key in Symphony's certificates directory:**

```bash
sudo cp ${WFM_SVID_DIR}/payload-cert.pem  $HOME/symphony/api/certificates/
sudo cp ${WFM_SVID_DIR}/payload-key.pem   $HOME/symphony/api/certificates/

# Verify the cert and key match (public keys must be identical):
openssl x509 -in $HOME/symphony/api/certificates/payload-cert.pem -noout -pubkey | openssl md5
openssl ec  -in $HOME/symphony/api/certificates/payload-key.pem  -pubout   | openssl md5
# Both lines must print the same MD5 hash — if they differ, re-run mis.sh
```

**Step 4 — Place MIS HTTPS CA in Symphony's mis directory:**

```bash
sudo cp $HOME/mis-deployment/certs/https-ca.crt  $HOME/symphony/api/mis/
```

**Step 5 — Add mis.margo.org to /etc/hosts:**

Symphony resolves the MIS endpoint by hostname. Without this entry it will try
to reach the public IP, time out, and exit at startup.

```bash
# Check if the entry already exists
grep "mis.margo.org" /etc/hosts

# If missing or pointing to a non-local IP, set it to 127.0.0.1:
sudo sed -i '/mis\.margo\.org/d' /etc/hosts
echo "127.0.0.1 mis.margo.org" | sudo tee -a /etc/hosts

# Verify:
grep "mis.margo.org" /etc/hosts
# Should print: 127.0.0.1 mis.margo.org
```

**Step 6 — Register CTT's device SPIFFE ID (wfm.sh option 7):**

```bash
sudo -E bash /home/margo/sandbox/scripts/wfm.sh
# → 7) Manage SPIFFE ID allowlist → 1) ➕ Add SPIFFE IDs
# → Enter the device SPIFFE ID printed by mis.sh above
```

**Step 7 — Copy MIS-generated certs to CTT fixture paths:**

```bash
# Run from the conformance_test_toolkit repo root
cd $HOME/workspace/conformance_test_toolkit

# WFM_CLIENT_SVID_DIR was set in Step 2 above
sudo cp ${WFM_CLIENT_SVID_DIR}/payload-cert.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem
sudo cp ${WFM_CLIENT_SVID_DIR}/payload-key.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-key.pem
sudo cp $HOME/mis-deployment/certs/ca.crt \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem
sudo chmod 644 ctt-runner/wfm-supplier/utils/fixtures/miaf/real/*.pem
```

**Step 8 — Verify certs before starting Symphony:**

```bash
# 1. CTT SVID cert chains to the trust bundle CA:
openssl verify \
    -CAfile ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem
# → client-svid-cert.pem: OK

# 2. MIS HTTPS server cert is reachable and chains to the same CA:
openssl verify \
    -CAfile $HOME/mis-deployment/certs/https-ca.crt \
    <(echo | openssl s_client -connect 127.0.0.1:9443 -quiet 2>/dev/null)
# → stdin: OK  (if this fails, re-run mis.sh to regenerate certs)
```

**Step 9 — Start Symphony:**

```bash
sudo -E bash /home/margo/sandbox/scripts/wfm.sh
# → 3) Symphony: Start
```

After a few seconds, verify it stays running:

```bash
sudo docker ps --filter name=symphony-api-container --format "{{.Names}}\t{{.Status}}"
# Should show: symphony-api-container   Up N seconds
# If it shows "Exited", check: sudo docker logs symphony-api-container 2>&1 | tail -20
```

---

#### Path 3B — Using ctt-mis.sh (no MIS dependency)

`ctt-mis.sh` generates all required certs locally. Symphony is configured to
use these self-signed certs instead of fetching from a live MIS.

**Step 1 — Run Symphony Prerequisites:**

```bash
sudo -E bash /home/margo/sandbox/scripts/wfm.sh
# → 1) PreRequisites: Setup
```

**Step 2 — Generate all certs via ctt-mis.sh:**

```bash
bash ctt-runner/ctt-mis.sh
# Certs land in ~/conformance-identities/
# CTT device SVID auto-installed to ctt-runner/wfm-supplier/utils/fixtures/miaf/real/
```

**Step 3 — Place WFM SVID & key in Symphony's certificates directory:**

```bash
cp ~/conformance-identities/wfm-svid-cert.pem  $HOME/symphony/api/certificates/
cp ~/conformance-identities/wfm-svid-key.pem   $HOME/symphony/api/certificates/
```

**Step 4 — Place CTT CA as Symphony's HTTPS CA:**

```bash
cp ~/conformance-identities/ca-cert.pem  $HOME/symphony/api/mis/https-ca.crt
```

**Step 5 — Copy CTT trust bundle to Symphony's mis directory:**

```bash
cp ~/conformance-identities/trust-bundle.json \
    $HOME/symphony/api/mis/trustbundle.json
```

**Step 6 — Patch Symphony's MIS config to use static trust bundle:**

This replaces the live MIS endpoint (not reachable when using ctt-mis.sh)
with the local trust bundle generated above:

```bash
jq '.bindings[1].config.miaf.mis = {
  "trustDomain": "margo.org",
  "trustbundle": {"path": "./mis/trustbundle.json"},
  "cacheInterval": 60
}' $HOME/symphony/api/symphony-api-margo.json \
   > /tmp/symphony-api-margo-ctt.json \
&& mv /tmp/symphony-api-margo-ctt.json \
      $HOME/symphony/api/symphony-api-margo.json
```

**Step 7 — Register CTT's device SPIFFE ID (wfm.sh option 7):**

```bash
# Print CTT's device SPIFFE ID:
openssl x509 \
    -in ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem \
    -text -noout | grep "URI:spiffe"

sudo -E bash /home/margo/sandbox/scripts/wfm.sh
# → 7) Manage SPIFFE ID allowlist → 1) ➕ Add SPIFFE IDs
# → Enter the SPIFFE ID printed above
```

**Step 8 — Start Symphony:**

```bash
sudo -E bash /home/margo/sandbox/scripts/wfm.sh
# → 3) Symphony: Start
```

**Verify:**

```bash
openssl verify \
    -CAfile ~/conformance-identities/ca-cert.pem \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem
# -> client-svid-cert.pem: OK
```

> **Restore after testing:** Revert Symphony's MIS config before using real devices:
>
> ```bash
> jq '.bindings[1].config.miaf.mis = {
>   "endpoint": "https://mis.margo.org:9443",
>   "caPath": "./mis/https-ca.crt",
>   "cacheInterval": 60
> }' $HOME/symphony/api/symphony-api-margo.json \
>    > /tmp/restore.json \
> && mv /tmp/restore.json $HOME/symphony/api/symphony-api-margo.json
> # Then: wfm.sh → 4) Symphony: Stop → 3) Symphony: Start
> ```

---

### Path 4 — CTT mock WFM and CTT mock device

Use this path for fully self-contained testing — no real WFM and no external
MIS. `ctt-mis.sh` generates all identities locally. CTT acts as both the
WFM client (device role) and the WFM server (mock WFM).

```bash
bash ctt-runner/ctt-mis.sh
# Generates: CA + WFM SVID + device SVID + trust bundle
# Auto-installs device SVID to ctt-runner/wfm-supplier/utils/fixtures/miaf/real/
```

**Via the main menu:**

```bash
bash ctt-runner/ctt-start.sh
# Select: 1) WFM Supplier → 1. Setup MIAF Identity → 3
```

> **If testing against a real WFM without SPIFFE infrastructure:** give
> `~/conformance-identities/ca-cert.pem` to the WFM admin. They import it as a
> trusted CA so the WFM accepts CTT's client cert. This requires the vendor WFM
> to support operator-configurable CA trust (not a Margo spec requirement —
> confirm with the vendor first).

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
| `miaf/real/client-svid-cert.pem` | ~2 years (ctt-mis.sh) or 90 days (MIS-issued) | Re-run the identity setup for your path |
| `miaf/real/trust-bundle-ca.pem` | Matches source CA lifetime | Re-copy from MIS or re-run ctt-mis.sh |

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
Phase 1 — Identity (once per WFM, re-run only if cert expires)

  Quickest: bash ctt-runner/ctt-start.sh → 1) WFM Supplier → 1. Setup MIAF Identity
  The menu guides you through one of three paths:

  Path 1 — External / Centralized MIS (primary):
  □ MIS admin mints CTT device SVID + CTT WFM SVID + shares CA cert
  □ Transfer / docker cp certs to CTT fixture paths (see Path 1 Step 2 above)
  □ Verify chains: openssl verify (see Path 1 Step 3 above)
  □ Share CTT's device SPIFFE ID with each WFM vendor (for their allowlist)
  □ Share CTT's WFM SPIFFE ID + mock WFM URL + MIS CA with each device vendor

  Path 2 — Vendor WFM with own SPIFFE:
  □ Ask vendor's SPIFFE admin for: client-svid-cert.pem, client-svid-key.pem, trust-bundle-ca.pem
  □ Use ctt-start.sh menu → option 2 to copy files interactively
  □ Share CTT's SPIFFE ID (from cert) with vendor's WFM admin for their allowlist
  □ Verify: openssl verify -CAfile .../trust-bundle-ca.pem .../client-svid-cert.pem → OK

  Path 3A — Sandbox / Symphony MIS (mis.sh):
  □ bash /home/margo/sandbox/scripts/mis.sh → 6) Generate SVID (WFM SVID + device SVID)
  □ ls $HOME/sandbox/scripts/ | grep x509svid   (note the directory names)
  □ sudo cp x509svid-<wfm-id>/payload-cert.pem + payload-key.pem → $HOME/symphony/api/certificates/
  □ sudo cp $HOME/mis-deployment/certs/https-ca.crt  $HOME/symphony/api/mis/
  □ sudo -E bash wfm.sh → 7 → 1 → add CTT's device SPIFFE ID
  □ sudo cp x509svid-<wfm-id>-<client-id>/payload-cert.pem + payload-key.pem → ctt-runner/wfm-supplier/utils/fixtures/miaf/real/
  □ sudo cp $HOME/mis-deployment/certs/https-ca.crt  → trust-bundle-ca.pem
  □ sudo -E bash wfm.sh → 3) Symphony: Start

  Path 3B — Sandbox / Symphony MIS (ctt-mis.sh, no MIS needed):
  □ sudo -E bash wfm.sh → 1) PreRequisites: Setup
  □ bash ctt-runner/ctt-mis.sh
  □ cp wfm-svid-cert.pem + wfm-svid-key.pem → $HOME/symphony/api/certificates/
  □ cp ca-cert.pem → $HOME/symphony/api/mis/https-ca.crt
  □ cp trust-bundle.json → $HOME/symphony/api/mis/trustbundle.json
  □ jq patch symphony-api-margo.json (see Path 3B in guide above)
  □ sudo -E bash wfm.sh → 7 → 1 → CTT device SPIFFE ID (from cert)
  □ sudo -E bash wfm.sh → 3) Symphony: Start

  Path 4 — CTT mock WFM and CTT mock device (self-contained):
  □ bash ctt-runner/ctt-mis.sh   (or via ctt-start.sh menu → option 3)
  □ Give ~/conformance-identities/ca-cert.pem to WFM admin if needed (non-standard)

Phase 2 — Run (repeat for each test run)
  □ bash ctt-runner/ctt-start.sh
  □ Select: 1) WFM Supplier -> 2. Run scenario group tests -> core
  □ Enter WFM SBI URL (mTLS port, e.g. https://localhost:8084/v1alpha2/margo)
  □ Press Enter for MIAF URL (same port unless WFM splits them)
  □ Multi-component prompts: press Enter x3 for Symphony, or enter NBI URL/creds
  □ Wait for run to complete (~2-5 min)
  □ Open report: ctt-runner/reports/wfm-supplier/wfm-scenario-report-core_<ts>.html
```
