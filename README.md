# Margo Conformance Test Toolkit (CTT)

This repository provides a Toolkit to run conformance test suites for 
Vendor software against the [Margo](https://margo.org/) specification.

[The overall approach](docs/margo-conformance-approach.md) explains the purpose, 
high-level use-cases, the intended users and high-level design.

Margo defines how a **device** and a **WFM** (Workload Fleet Manager — the
cloud/control-plane side) talk to each other over HTTPS, and how an
**Application Package** must be structured. This toolkit lets a vendor who
built any one of those three pieces prove their implementation follows the
spec — by testing it against a correct, spec-conformant mock of whatever it
talks to, and producing a pass/fail HTML report with every assertion traced
back to a specific requirement (CR-ID).

This repo is the execution engine; the
[conformance_documentation](https://github.com/margo/conformance_documentation)
repo is its conceptual companion.

The repository organization is explained below, but briefly, it has 3 parts - 
- A CTT Creator for writing and adding test-cases to the Test-Suites
- A CTT Runner for running Conformance Test-Suites and getting Conformance Repeorts
- Test-Suites which contain the actual test-cases

---

## Current baseline of Margo Specifications used for this version of CTT

This suite targets **Margo spec `1.0.0-rc.3`**, which uses **MIAF** (the Margo
Identity and Authorization Framework): identity is an X.509-SVID (a
SPIFFE-based certificate) issued by a Margo Identity Service (MIS), and every
WFM Management Interface call is authenticated via **mutual TLS**. The
onboarding API and the per-request HTTP Message Signatures (RFC 9421) of
earlier spec versions are gone, and the suite no longer tests them.

[`docs/conformance-flows-and-miaf-migration.md`](docs/conformance-flows-and-miaf-migration.md)
explains the full flow, every term (SPIFFE ID, Trust Bundle, SVID, digest,
manifest, ...), and what changed from the earlier versions.

---

## The three personas

| Persona | The vendor brings... | This toolkit provides... |
|---|---|---|
| **WFM Supplier** | A real WFM (e.g. Eclipse Symphony) | A WFM Client that accesses the Margo Management Interface exposed by the WFM over mTLS |
| **Device Supplier** | A real device / device-agent | A mock WFM server for the device to connect to |
| **Application Supplier** | An Application Package (`margo.yaml` + resources) | A validator that checks its structure against the Application Description spec |

Every test is a real request/response pair, checked against the Margo specification — both
the "happy path" and deliberately-broken cases (identity that must be refused,
wrong content type, invalid enum value, wrong digest, etc.), so a report row
always reads as *expected vs. actual*, not just pass/fail.

---

## Repository structure

```
conformance_test_toolkit/
├── ctt-creator/               # Test-case authoring and group management
│   ├── ctt-start.sh           #   entry point: create or update a test group
│   ├── wfm-supplier/          #   OpenAPI → Postman collection generation, dev guide
│   ├── device-supplier/       #   dev guide
│   └── app-supplier/          #   dev guide
├── ctt-runner/                # Test execution and report generation
│   ├── ctt-start.sh           #   entry point: run a group against a real system
│   ├── reports/               #   generated HTML reports, one folder per persona
│   ├── wfm-supplier/          #   run_wfm_scenarios.js + utils/fixtures/ + utils/certs/
│   ├── device-supplier/       #   Go runner (run_tests.go) + mock WFM server + utils/
│   └── app-supplier/          #   Application Package structural validator (Go)
├── test-suites/               # Test cases, one folder per persona and group
│   ├── wfm-supplier/<group>/     # group.json + test-cases/
│   ├── device-supplier/<group>/  # group.json + test-cases/
│   └── app-supplier/             # Application Packages to validate
├── docs/                      # Setup guides, architecture, flows, test-case templates
└── _archive/                  # Deprecated files preserved for reference (see _archive/README.md)
```

The full set of each persona is the `core` group:
`test-suites/wfm-supplier/core/test-cases/wfm-supplier.json` (10 scenarios, 52
steps) and `test-suites/device-supplier/core/test-cases/device-supplier.json`
(13 scenarios, 61 steps). The device `core` judges a real device: it checks the
requests the device sends to the mock WFM. The device group `self-check` runs
the CTT's own device client against the mock WFM and needs no device.

---

## Prerequisites

The CTT is officially supported on **Ubuntu 24.04**. Other Linux distributions
are not supported.

| Tool | Needed for |
|---|---|
| **Node.js** (any recent LTS) | `run_wfm_scenarios.js` — no `npm install` required, uses only Node built-ins |
| **Go 1.24+** | `ctt-runner/device-supplier/` and `ctt-runner/app-supplier/` |
| **`jq`** | Reading `group.json` in both CLIs |
| **`oras` CLI** | WFM supplier: Application Registry checks |
| **`helm` CLI** | Application supplier: observability checks of Helm components |
| **OpenSSL** | Checking the SVIDs during identity setup |
| **npm** | Only for `ctt-creator`'s "OpenAPI spec based contract tests", which uses Portman to generate a Postman collection |

---

## Quick start

The suite is driven by two CLIs. Both are menu-driven; start with the setup
guide of the persona (see [Key documents](#key-documents)).

```bash
# 1) Author: put test cases in a folder and create a test group from them
bash ctt-creator/ctt-start.sh

# 2) Run: set up the identity once, pick a group, get the HTML report
bash ctt-runner/ctt-start.sh                           # interactive menu
bash ctt-runner/ctt-start.sh wfm <group> <WFM_URL>     # WFM Supplier
bash ctt-runner/ctt-start.sh device <group>            # Device Supplier
bash ctt-runner/ctt-start.sh application               # Application Supplier
bash ctt-runner/ctt-start.sh help
```

Reports land in `ctt-runner/reports/<persona>/`: the HTML report and, created
automatically at the end of the run, the same report as a signed PDF with the
signer's `ca_cert.pem`. Both personas use the same report layout.

**Identity:** every run needs an X.509-SVID. Issue it with the MIS of the Trust Domain (for the
sandbox: `mis.sh generate-svid`) and give the folder to the runner once:
WFM Supplier → *Setup MIAF Identity*, Device Supplier → *Setup Identity*. The
setup guides show the exact answers for each prompt.

---

## Test groups

A **group** is a named list of test IDs with a spec version. Its `group.json`
holds the name, version, description, the folder where the test files live,
and the IDs of the scenarios, steps or Postman requests to run. The runner
lists the groups with their descriptions and runs the selected one; the report
carries the group name and version.

| Group | Persona | What it is for |
|---|---|---|
| `quick-check` | both | A first check of the URL and the identity setup (no device needed) |
| `self-check`, `random-order` | Device | The CTT's own device client against the mock WFM (no device needed) |
| `management-interface` | both | The Management Interface API |
| `identity` | both | mTLS, trust, SVID rotation |
| `observability` | both | OpenTelemetry |
| `deployment-delivery` | WFM | Delivering a deployment |
| `postman-lifecycle`, `postman-error-handling` | WFM | Tests written in Postman |
| `manifest-integrity` | Device | Manifests and content a device must reject; state kept across a restart |
| `core` | both | The full set — run this for a conformance result. WFM: requests sent to the real WFM. Device: the requests a real device sends, judged by the mock WFM |

Why groups exist and how they are created is described in
[`docs/architecture.md`](docs/architecture.md#6-the-test-data-model).

---

## Key documents

| Document | What it's for |
|---|---|
| [`docs/wfm-supplier-setup-guide.md`](docs/wfm-supplier-setup-guide.md) | **WFM Supplier start here.** Step-by-step: identity setup, running a group, reading the report, troubleshooting. |
| [`docs/device-supplier-setup-guide.md`](docs/device-supplier-setup-guide.md) | **Device Supplier start here.** Step-by-step: identity setup, starting the mock WFM, running a group, connecting a real device-agent. |
| [`docs/ctt-explained.md`](docs/ctt-explained.md) | **Read this to understand or present the toolkit.** The rc.3 flow in plain words, how the CTT maps onto it, what each module does, the demo sequence with expected results, likely questions and answers, and the known gaps. |
| [`docs/architecture.md`](docs/architecture.md) | What the toolkit is, how the three personas and two CLIs fit together, test groups, and how reports trace back to spec requirements. |
| [`docs/margo-conformance-approach.md`](docs/margo-conformance-approach.md) | The conformance approach and the **test-case templates**: Postman collections for the WFM persona, the Margo scenario schema for the device persona. Read this before writing tests. |
| [`docs/conformance-flows-and-miaf-migration.md`](docs/conformance-flows-and-miaf-migration.md) | Every conformance flow explained end to end, terminology glossary, and what changed from the onboarding/signature flow to MIAF. |

---

## WFM Supplier: checks for the PR3 requirements

The WFM Supplier `core` group now also judges the PR3 WFM requirements
(problem details, ApplicationDeployment content, WFM SVID and TLS, Device
Capabilities). In this version it has 11 scenarios and 96 steps. The themed
groups `management-interface`, `identity` and `deployment-delivery` contain the
same checks. This section lists what to prepare before the run and how to read
the new results. For the setup itself, follow the
[WFM Supplier setup guide](docs/wfm-supplier-setup-guide.md).

### Before the run

| Prepare | Why |
|---|---|
| Nothing to install beyond the [Prerequisites](#prerequisites) | The runner reads the ApplicationDeployment YAML with js-yaml 4.1.0 (MIT licence), vendored in `ctt-runner/wfm-supplier/scripts/vendor/`. No `npm install` is needed. |
| Run against the port the WFM uses in production | MGMT-003 requires the Management Interface on port 443. The report shows the port the CTT connected to. |
| Assign a deployment whose Application Description has `deviceConstraints` and `parameters` (setup guide, Step 7) | MGMT-069 to MGMT-072, MGMT-075 and MGMT-076 judge those fields. When the deployment has none, these steps are NOT RUN. |
| Let the CTT's WFM Client report the deviceId `ctt-unreported-gateway/ctt-child-device` | MGMT-026 checks that a child device reported before its gateway is rejected with `404 Not Found`. If the WFM refuses that deviceId by local policy (`403 Forbidden`), the step is NOT RUN. |
| Accept a Device Capabilities document that has an `x-ctt-conformance-extensions` property at its root | MGMT-027, MGMT-029, EXT-001 and EXT-004: the WFM must accept the document and ignore the unknown specification extension. |
| Optional: a second WFM Client SVID from the same MIS for another `wfm-id`, placed in `ctt-runner/wfm-supplier/utils/certs/` as `other-wfm-client-svid-cert.pem` and `other-wfm-client-svid-key.pem` | IDENT-108: the WFM must refuse a validly issued client of another WFM. For the sandbox MIS: `cd ~/mis-deployment && sudo -E bash ~/sandbox/scripts/lib/mis/svid-gen.sh --automated --principal wfm-client --spiffe-id spiffe://<trust-domain>/margo/wfm/<another-wfm-id>/client/ctt-client`. Without this SVID the step is NOT RUN. |
| Optional: three more SVIDs from the same MIS, placed in the same folder as `<name>-cert.pem` and `<name>-key.pem` (see the table below) | Each one lets more identity and caller-scope checks run. Without it, those steps are NOT RUN and the report names the missing file. |

| SVID file name | SPIFFE ID to issue | Requirements |
|---|---|---|
| `non-client-svid` | `spiffe://<trust-domain>/margo/wfm/<wfm-id>`, without `/client/<id>` | IDENT-092, IDENT-093, IDENT-101: the WFM must not treat it as a client |
| `expired-client-svid` | The CTT's own client SPIFFE ID with a short TTL (for example 60 seconds), used after it has expired | IDENT-015, IDENT-036, IDENT-037: the WFM must refuse an expired SVID |
| `second-client-svid` | `spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/<another-client-id>`, also added to the WFM's accepted clients | MGMT-016: the second client must not see the CTT client's deployment, deployment YAML or bundle |
| `retired-client-svid` | `spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/<retired-client-id>`, still valid during the run. Add it to the WFM's accepted clients, then retire the client relationship before the run (on the sandbox Symphony, remove it from `~/symphony/api/mis/authorized-clients.json`) | MGMT-023, IDENT-106, IDENT-125: the WFM denies the request by local policy; MGMT-019: the denial is `403 Forbidden` with the `not-authorized` problem type |

On the sandbox MIS, issue each one with
`sudo docker exec margo-identity-service sh -c 'mkdir -p /tmp/ctt-svid && ./mis-cli mint x509 --spiffeID <spiffe-id> --ttl <seconds> --outputDir /tmp/ctt-svid'`,
then copy `/tmp/ctt-svid/payload-cert.pem` and `payload-key.pem` out of the
container with `docker cp`. The `svid-gen.sh` command above accepts only
client and WFM SPIFFE IDs with its default TTL, so use `mis-cli` directly for
the short-lived SVID.

Every mTLS step opens a new TLS connection with a full handshake, so the WFM
validates the client SVID on every request. Expect one handshake per step in
the WFM logs.

### NOT RUN results

A NOT RUN step means the requirement could not be observed on this WFM. It is
not a failure.

| Requirement | NOT RUN when | To cover it |
|---|---|---|
| IDENT-004, IDENT-005, IDENT-006, IDENT-007 | The WFM SVID key is of another type. Only the check for the key type the WFM presents runs. | Nothing to do. |
| IDENT-032, IDENT-117 | The WFM does not accept TLS 1.2 at all (TLS 1.3 only is conformant). | Nothing to do. If TLS 1.2 is enabled, the WFM must refuse the NULL-encryption cipher suites (RFC 9325). |
| MGMT-026, MGMT-094 (child device) | The WFM answers `403 Forbidden` for the child deviceId. | Allow the CTT's WFM Client to report `ctt-unreported-gateway/ctt-child-device`. |
| MGMT-069 to MGMT-072, MGMT-075, MGMT-076 | The assigned deployment has no `deviceConstraints` or `parameters`. | Assign a deployment that has them. |
| IDENT-108 | No SVID for another `wfm-id` is in the cert folder. | Issue one and place it as described in [Before the run](#before-the-run). |
| IDENT-015, IDENT-036, IDENT-037, IDENT-092, IDENT-101, MGMT-016 | The optional SVID for the check is not in the cert folder, the expired SVID has not expired yet, or the WFM does not accept the second client. | Issue the SVID and place it as described in [Before the run](#before-the-run). |
| EXT-002, EXT-003 | The served ApplicationDeployment has no `x-…-extensions` property in `spec`. | Nothing to do. If the WFM adds extensions, they must be named `x-<unique-name>-extensions` with an object value. |
| MGMT-035 | This is the first run against this WFM for the CTT's client, or the manifest has not changed since the last run. | Run the group, change the deployment assigned to the CTT's client, then run the group again. The CTT keeps the last `manifestVersion` and ETag per WFM URL and client in `ctt-runner/wfm-supplier/utils/certs/.manifest-history.json`, as a WFM Client must. |
| MGMT-036 | The `second-client-svid` is not in the cert folder, or the WFM does not accept that client. | Issue the second client SVID as described in [Before the run](#before-the-run). It must never have had a deployment assigned. |
| IDENT-008 | The WFM SVID key is not RSA, or the WFM does not accept TLS 1.2 (TLS 1.3 always signs the handshake with RSASSA-PSS). | Nothing to do. |
| IDENT-009 | The CTT's client SVID is not ECDSA P-256 with SHA-256. | Issue the client SVID with ECDSA P-256 (the MIS default). |
| IDENT-024 | No trust anchor in `svid-ca.pem` issued the CTT's client SVID chain. | Copy the MIS trust bundle into `svid-ca.pem`. |
| MGMT-106 | The WFM returned no vendor-defined problem type during the run, only `about:blank` or types from the Margo problem types catalogue. | Nothing to do. The check applies only to a WFM that defines its own problem types. |
| MGMT-019, MGMT-023, IDENT-106, IDENT-125 | The `retired-client-svid` is not in the cert folder, has expired, or is not another client of the same WFM. | Issue it as described in [Before the run](#before-the-run). It must still be valid, so that a denial comes from local policy and not from its validity. |
| MGMT-087, MGMT-088, MGMT-090, MGMT-091, MGMT-092 | The WFM does not compress the deployment YAML or the bundle when the CTT sends `Accept-Encoding: gzip, br` (compression is optional). | Nothing to do. If the WFM compresses, it must use an advertised coding, send `Vary: Accept-Encoding`, and keep the digest valid for the decoded body. |
| MGMT-093 | The WFM answers `200` to `If-None-Match` with the current digest (a `304` is optional). | Nothing to do. If the WFM answers `304`, the response must have no body. |

The client authentication steps (IDENT-014, IDENT-030, IDENT-040 to IDENT-042,
IDENT-112, IDENT-114 to IDENT-116, IDENT-120) need no preparation. The CTT
creates the certificates it presents: a self-signed substitute with its own
SPIFFE ID, and a leaf signed with its own SVID key, which a WFM must refuse
because an X.509-SVID has `cA=false`. A refusal at the TLS handshake and a
`401` or `403` response both pass. The WFM logs show the reason for each
refusal.

IDENT-043 and IDENT-032/IDENT-117 make the CTT offer TLS 1.1 and TLS 1.2 NULL
cipher suites. If the OpenSSL on the CTT host cannot offer them, the step fails
with "could not reach the WFM ... the TLS behaviour was not tested". The
supported Ubuntu 24.04 host can offer them.

### Repeating the TLS and problem checks by hand

Run these commands from the CTT host to see the same behaviour the report
shows. Replace `<wfm-host>:<port>` and `<base-path>` with the values entered at
Step 9 of the setup guide. `svid-cert.pem` and `svid-key.pem` are in
`ctt-runner/wfm-supplier/utils/certs/` after *Setup MIAF Identity*.

```bash
cd ctt-runner/wfm-supplier/utils/certs

# IDENT-031: the WFM negotiates TLS 1.3 by default (expect "New, TLSv1.3")
openssl s_client -connect <wfm-host>:<port> -cert svid-cert.pem -key svid-key.pem </dev/null 2>&1 | grep "^New,"

# IDENT-043: TLS 1.1 and older are refused (expect an alert and "Cipher is (NONE)")
openssl s_client -connect <wfm-host>:<port> -tls1_1 -cipher 'DEFAULT@SECLEVEL=0' \
  -cert svid-cert.pem -key svid-key.pem </dev/null 2>&1 | grep -E "alert|Cipher is"

# IDENT-032 / IDENT-117: with TLS 1.2, NULL-encryption cipher suites are refused
# (expect an alert; "alert protocol version" means TLS 1.2 is disabled, which the report shows as NOT RUN)
openssl s_client -connect <wfm-host>:<port> -tls1_2 -cipher 'eNULL:!aNULL@SECLEVEL=0' \
  -cert svid-cert.pem -key svid-key.pem </dev/null 2>&1 | grep -E "alert|Cipher is"

# MGMT-097, MGMT-101, MGMT-102: an unknown path returns an about:blank problem
# (expect 404, Content-Type application/problem+json, "title": "Not Found" and a detail)
curl -sk -i --cert svid-cert.pem --key svid-key.pem https://<wfm-host>:<port><base-path>/api/v1/ctt-unknown-resource

# IDENT-120: a request without a client certificate is refused (expect a TLS error, or 401/403)
curl -sSk -i -H 'Accept: application/vnd.margo.manifest.v1+json' https://<wfm-host>:<port><base-path>/api/v1/deployments

# IDENT-040 to IDENT-042: a forged Client-Cert header never replaces the mTLS identity
# (without a client certificate: expect a refusal; with one: expect 200 and the CTT client's own manifest)
CC=":$(openssl x509 -in svid-cert.pem -outform DER | base64 -w0):"
curl -sSk -i -H "Client-Cert: $CC" -H 'Accept: application/vnd.margo.manifest.v1+json' https://<wfm-host>:<port><base-path>/api/v1/deployments
```

### Device Supplier mock WFM

The mock WFM problem responses follow the rc.3 retry semantics and problem
types:

- `retryable` and `backoffStrategy` are on every problem response: `true` and
  `exponential` for 429, 500 and 503, and `false` and `none` for every other
  error.
- `errors[].field` names the request body field of each validation error.
- An unknown path returns an `about:blank` 404 problem.

The mock is rebuilt from source each time the runner starts it, so there is
nothing to do.

---

## Status

This suite is under active development alongside the Margo spec itself.

## License

[MARGO](LICENSE)
