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
| `deployment-delivery`, `application-registry` | WFM | Delivering a deployment; the OCI registry |
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

## Status

This suite is under active development alongside the Margo spec itself.

## License

[MARGO](LICENSE)
