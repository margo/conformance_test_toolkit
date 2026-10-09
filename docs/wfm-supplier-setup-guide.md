# WFM Supplier Conformance — Setup and Execution Guide

**Persona:** WFM Supplier  
**Spec baseline:** Margo `1.0.0-rc.3` (MIAF / mTLS authentication)  
**Audience:** WFM vendors, Margo adopters, integration engineers

This guide walks you from a clean checkout to a signed conformance report,
step by step. Follow it once and you will understand every moving part; on
subsequent runs only the identity setup (Phase 1) needs attention if certs
have expired.

---

## What CTT does in this persona

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

Clone the repo and check out the conformance branch:

```bash
mkdir -p ~/workspace && cd ~/workspace
git clone https://github.com/margo/conformance_test_toolkit.git
cd conformance_test_toolkit
git checkout feature/multi-persona-conformance
```

Then verify tooling:

```bash
node --version
openssl version
jq --version
```

---

### Identity Setup — Sandbox MIS as Centralized SVID Authority

The Margo sandbox MIS (`mis.sh`) acts as the centralized SVID authority for
all CTT identity setup. 
Use this path when testing CTT against Symphony (WFM)
in a local or sandbox environment.


Run prerequisites first — this creates the symphony directory structure:

```bash
sudo -E bash /home/margo/sandbox/scripts/wfm.sh
# → 1) PreRequisites: Setup
```
Note:
    > **If `$HOME/symphony/` does not exist after prerequisites:** wfm.sh may have
    > installed symphony to `/root/symphony/` (happens if the `-E` flag was not
    > passed or was dropped internally). Fix it with:
    > ```bash
    > sudo mv /root/symphony $HOME/symphony
    > sudo chown -R $USER:$USER $HOME/symphony
    > ```

After prerequisites, Symphony prints the three steps that must be completed
before starting it:

```
1. Place your WFM SVID & Key in:   $HOME/symphony/api/certificates/
2. Place the MIS HTTPS CA cert in: $HOME/symphony/api/mis/https-ca.crt
3. Register SPIFFE IDs of WFM Clients via menu option 7
Then: Start Symphony via menu option 3
```

Both sub-paths below satisfy these same three steps — the only difference is
where the files come from.

**Step 1 — Generate identity via sandbox mis.sh:**

```bash
bash /home/margo/sandbox/scripts/mis.sh
# Follow prompts to generate WFM SVID + device SVID

# mis.sh creates ~/mis-deployment/ but with root ownership — fix it:
sudo chown -R $USER:$USER ~/mis-deployment
```

**Step 2 — Find the output directories from mis.sh:**

mis.sh writes SVIDs into `~/mis-deployment/` in directories named after
the WFM ID and client ID you entered during generation:

```
x509svid-<wfm-id>            ← Symphony's WFM SVID (server identity)
x509svid-<wfm-id>-<client-id> ← CTT's device SVID (client identity)
```

Check what was generated:

```bash
ls ~/mis-deployment/ | grep x509svid
```

Set variables for the rest of the steps:

```bash
WFM_SVID_DIR=~/mis-deployment/x509svid-<wfm-id>
WFM_CLIENT_SVID_DIR=~/mis-deployment/x509svid-<client-id>
# The exact directory names depend on the IDs you entered in mis.sh.
# Check what was created: ls ~/mis-deployment/ | grep x509svid
# Example: if wfm-id="wfm", client-id="wfm-wfm-client" or "wfmclient":
#   WFM_SVID_DIR=~/mis-deployment/x509svid-wfm
#   WFM_CLIENT_SVID_DIR=~/mis-deployment/x509svid-wfm-wfm-client
```

**Step 3 — Place WFM SVID & key in Symphony's certificates directory:**

```bash
sudo cp ${WFM_SVID_DIR}/payload-cert.pem  $HOME/symphony/api/certificates/
sudo cp ${WFM_SVID_DIR}/payload-key.pem   $HOME/symphony/api/certificates/
sudo chown $USER:$USER \
    $HOME/symphony/api/certificates/payload-cert.pem \
    $HOME/symphony/api/certificates/payload-key.pem

# Verify the cert and key match (public keys must be identical):
openssl x509 -in $HOME/symphony/api/certificates/payload-cert.pem -noout -pubkey | openssl md5
openssl ec  -in $HOME/symphony/api/certificates/payload-key.pem  -pubout   | openssl md5
# Both lines must print the same MD5 hash — if they differ, re-run mis.sh
```

**Step 4 — Place MIS HTTPS CA in Symphony's mis directory:**

```bash
sudo cp ~/mis-deployment/certs/https-ca.crt  $HOME/symphony/api/mis/
sudo chown $USER:$USER $HOME/symphony/api/mis/https-ca.crt
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
sudo cp ~/mis-deployment/certs/ca.crt \
    ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem
sudo chown $USER:$USER ctt-runner/wfm-supplier/utils/fixtures/miaf/real/*.pem
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
echo | openssl s_client \
    -connect 127.0.0.1:9443 \
    -CAfile ~/mis-deployment/certs/https-ca.crt 2>&1 | grep "Verify return code"
# → Verify return code: 0 (ok)  (if non-zero, re-run mis.sh to regenerate certs)
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
2) Functional tests   (Group-based test management)
```

Then select the group. For the full WFM conformance test set:

```
3) core  (wfm-supplier/core)
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
  □ bash /home/margo/sandbox/scripts/mis.sh → follow prompts (WFM SVID + device SVID)
  □ sudo chown -R $USER:$USER ~/mis-deployment
  □ ls ~/mis-deployment/ | grep x509svid   (note the directory names)
  □ sudo cp ~/mis-deployment/x509svid-<wfm-id>/payload-cert.pem + payload-key.pem → $HOME/symphony/api/certificates/
  □ sudo cp ~/mis-deployment/certs/https-ca.crt  $HOME/symphony/api/mis/
  □ sudo -E bash wfm.sh → 7 → 1 → add CTT's device SPIFFE ID
  □ sudo cp ~/mis-deployment/x509svid-<wfm-id>-<client-id>/payload-cert.pem + payload-key.pem → ctt-runner/wfm-supplier/utils/fixtures/miaf/real/
  □ sudo cp ~/mis-deployment/certs/ca.crt → trust-bundle-ca.pem
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
  □ Select: 1) WFM Supplier → 2) Functional tests → 3) core
  □ Enter WFM SBI URL (mTLS port, e.g. https://localhost:8084/v1alpha2/margo)
  □ Press Enter for MIAF URL (same port unless WFM splits them)
  □ Multi-component prompts: press Enter x3 for Symphony, or enter NBI URL/creds
  □ Wait for run to complete (~2-5 min)
  □ Open report: ctt-runner/reports/wfm-supplier/wfm-scenario-report-core_<ts>.html
```
