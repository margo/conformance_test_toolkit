# Margo Conformance Test Toolkit (CTT)

A conformance test suite for the [Margo](https://margo.org/) specification.
Margo defines how a **device** and a **WFM** (Workload Fleet Manager — the
cloud/control-plane side) talk to each other over HTTPS, and how an
**Application Package** must be structured. This toolkit lets a vendor who
built any one of those three pieces prove their implementation follows the
spec — by testing it against a correct, spec-conformant mock of whatever it
talks to, and producing a pass/fail HTML report with every assertion traced
back to a specific requirement (CR-ID).

This repo works together with
[conformance_documentation](https://github.com/margo/conformance_documentation)
for the broader conformance program's overview material; this repo is the
actual test code.

---

## Current spec baseline

This suite targets **Margo spec `1.0.0-rc.3`**, which introduced **MIAF**
(the Margo Identity and Authorization Framework): identity is now an
X.509-SVID (a SPIFFE-based certificate) provisioned out of band by an
operator, and every WFM Management Interface call is authenticated via
**mutual TLS**, not the earlier per-request HTTP Message Signatures (RFC 9421)
scheme. If you're new to this change, **start with
[`docs/CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md`](docs/CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md)**
— it explains the full flow, every term (SPIFFE ID, Trust Bundle, SVID, digest,
manifest, ...), and exactly what changed and why, in plain language.

The suite supports **both** transports side by side while real-world WFMs
migrate: existing RFC 9421-signed scenario files keep working unchanged, and
newer scenario files opt into mTLS per-step (`"mtls": true`). Nothing needs
migrating all at once.

---

## The three personas

| Persona | The vendor brings... | This toolkit provides... |
|---|---|---|
| **WFM Supplier** | A real WFM (e.g. Eclipse Symphony) | A mock device / WFM-Client that fires real HTTP/mTLS calls at it |
| **Device Supplier** | A real device / device-agent | A mock WFM server for the device to connect to |
| **Application Supplier** | An Application Package (`margo.yaml` + resources) | A validator that checks its structure against the Application Description spec |

Every test is a real request/response pair, checked against the spec — both
the "happy path" and deliberately-broken cases (bad signature, wrong content
type, invalid enum value, missing required field, etc.), so a report row
always reads as *expected vs. actual*, not just pass/fail.

---

## Repository structure

```
conformance_test_toolkit/
├── ctt-creator/               # Test-case authoring and group management
│   ├── ctt-start.sh           #   entry point: create/edit test groups
│   ├── common/scripts/        #   shared helpers (e.g. provision-mis-identity.sh)
│   ├── wfm-supplier/          #   WFM group authoring scripts + utils/groups/
│   ├── device-supplier/       #   Device group authoring scripts + utils/groups/
│   └── app-supplier/          #   App Package group authoring
├── ctt-runner/                # Test execution and report generation
│   ├── ctt-start.sh           #   entry point: run tests against a real system
│   ├── reports/               #   generated HTML reports, organized by persona + group
│   ├── wfm-supplier/          #   run_wfm_scenarios.js + utils/fixtures/ + utils/certs/
│   ├── device-supplier/       #   Go runner (run_tests.go, mock WFM server) + utils/
│   └── app-supplier/          #   Application Package structural validator (Go)
├── test-suites/               # Declarative test cases, one folder per persona + group
│   ├── wfm-supplier/core/test-cases/wfm-supplier.json   # 8 scenarios, 29 steps
│   ├── device-supplier/core/test-cases/device-supplier.json  # 23 scenarios, 104 steps
│   └── app-supplier/
├── docs/                      # Architecture, MIAF migration guide, client-facing brief
└── _archive/                  # Deprecated files preserved for reference (see _archive/README.md)
```

---

## Prerequisites

| Tool | Needed for |
|---|---|
| **Node.js** (any recent LTS) | `run_wfm_scenarios.js` — no `npm install` required, uses only Node built-ins |
| **Go 1.24+** | `ctt-runner/device-supplier/` and `ctt-runner/app-supplier/` |
| **`npm install -g newman`** | Only if running the legacy Postman collection path (archived) |
| **`oras` CLI** | Application Registry (OCI) conformance checks |
| **OpenSSL** | Certificate/SVID generation used throughout test setup |

---

## Quick start

The suite is driven by two top-level CLIs:

```bash
# 1) Prepare test data — pick a persona, select/create a test group
./ctt-creator/ctt-start.sh

# 2) Execute tests against a real system under test, generate the HTML report
./ctt-runner/ctt-start.sh wfm <group> <WFM_URL>          # WFM Supplier
./ctt-runner/ctt-start.sh device <group-or-scenarios>    # Device Supplier
./ctt-runner/ctt-start.sh application                    # Application Supplier
./ctt-runner/ctt-start.sh help                           # full usage, all options
```

Reports land in `ctt-runner/reports/<persona>/`, organized by test group.

For an MIAF (mTLS) scenario group specifically, `ctt-runner/ctt-start.sh` detects the
declarative JSON format automatically and drives it through
`run_wfm_scenarios.js` directly (no Newman involved) — see
`run_wfm_scenario_group()` in `run-tests.sh` if you need to see exactly how.

**Provisioning an identity for MIAF testing:** `ctt-creator/common/scripts/provision-mis-identity.sh`
mints a client X.509-SVID from a real Margo Identity Service (MIS), registers
it with the WFM's accepted-client policy, and fetches the trust bundle — the
three one-time setup steps MIAF requires before any mTLS scenario can run.
This is a demo/self-test convenience for when the "WFM under test" is a
`margo/sandbox`-based reference deployment; a real vendor engagement brings
its own already-provisioned identity instead.

---

## Key documents

| Document | What it's for |
|---|---|
| [`docs/CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md`](docs/CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md) | **Start here.** Every conformance flow explained end to end, full terminology glossary, and the complete old-flow → MIAF migration analysis with CR-ID mapping. |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | System architecture: what the toolkit is, how the three personas and two CLIs fit together, the test data model, and how reports trace back to spec requirements. |
| [`docs/client-demo-brief.md`](docs/client-demo-brief.md) | One-page summary for explaining the suite to a vendor or stakeholder. |

---

## Status

This suite is under active development alongside the Margo spec itself. Some
scenario groups still use the pre-MIAF RFC 9421 transport pending real-world
WFM/device migrations; new work should default to the mTLS transport
(`"mtls": true` on a step) per `docs/CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md`.

## License

[MIT](LICENSE)
