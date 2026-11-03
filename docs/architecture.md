# Margo Conformance Test Toolkit — Architecture

**Spec baseline:** Margo `1.0.0-rc.3` (MIAF — see
[`conformance-flows-and-miaf-migration.md`](conformance-flows-and-miaf-migration.md)
for the full protocol-level detail this document intentionally leaves out)

---

## Table of Contents

1. [What Is This Toolkit?](#1-what-is-this-toolkit)
2. [Architecture at a Glance](#2-architecture-at-a-glance)
3. [The Three Personas](#3-the-three-personas)
4. [Identity & Transport Model](#4-identity--transport-model)
5. [Repository Structure](#5-repository-structure)
6. [The Test Data Model](#6-the-test-data-model)
7. [The Two-CLI Workflow](#7-the-two-cli-workflow)
8. [Reporting & Traceability](#8-reporting--traceability)
9. [Where to Go Next](#9-where-to-go-next)

---

## 1. What Is This Toolkit?

Margo defines a contract between three separate pieces built by three separate
kinds of vendors:

- a **device** (or device-agent) running at the edge,
- a **WFM** (Workload Fleet Manager) — the cloud/control-plane side that tells
  devices what to run,
- and an **Application Package** — the bundle describing a workload, so any
  conformant WFM can deploy it to any conformant device.

A vendor who builds *any one* of those three pieces needs a way to prove their
implementation actually follows the spec, without needing a live copy of the
other two pieces to test against. That is what this toolkit provides: for
each of the three personas, it supplies a correct, spec-conformant stand-in
for whatever that persona talks to (a mock WFM, a WFM Client, or a
structural validator for a package), drives a battery of both valid and
deliberately-broken requests through it, and produces a pass/fail report
where every check is traceable back to a specific requirement ID (CR-ID) in
the spec.

In short: **the supplier provides the implementation and receives a report showing exactly
which parts of the Margo spec it does and does not conform to.**

---

## 2. Architecture at a Glance

```
                         ┌─────────────────────────────┐
                         │   ctt-creator/ctt-start.sh    │   Author
                         │  (pick persona, point at a    │
                         │   folder of test cases,       │
                         │   create the group.json)      │
                         └───────────────┬───────────────┘
                                         │  test-suites/<persona>/<group>/
                                         ▼
                         ┌─────────────────────────────┐
                         │    ctt-runner/ctt-start.sh    │   Execute
                         │  (set up identity, pick a     │
                         │   group, run its tests)       │
                         └──┬──────────┬──────────┬──────┘
                            │          │          │
              ┌─────────────┘          │          └─────────────┐
              ▼                        ▼                        ▼
   ┌────────────────────┐  ┌─────────────────────┐  ┌───────────────────────┐
   │   WFM Supplier      │  │   Device Supplier    │  │  Application Supplier  │
   │                     │  │                      │  │                       │
   │ Toolkit acts as a   │  │ Toolkit acts as a    │  │ Toolkit statically     │
   │ conformant device,  │  │ conformant mock WFM, │  │ validates an           │
   │ drives requests at  │  │ accepts connections   │  │ Application Package    │
   │ the vendor's real   │  │ from the vendor's     │  │ against the spec's     │
   │ WFM                 │  │ real device agent     │  │ structural rules       │
   └──────────┬──────────┘  └──────────┬───────────┘  └───────────┬───────────┘
              │                        │                          │
              └────────────────────────┴──────────────────────────┘
                                        ▼
                         ┌─────────────────────────────┐
                         │ ctt-runner/reports/<persona>/ │   HTML report,
                         │   pass/fail, per CR-ID          │   one per run
                         └─────────────────────────────┘
```

Two CLIs drive the whole system:

| CLI | Role |
|---|---|
| `ctt-creator/ctt-start.sh` | **Author** — pick a persona, point at a folder of test cases, and create or update a test group from them |
| `ctt-runner/ctt-start.sh` | **Execute** — set up the identity, run a group's tests against a real system under test, and generate the HTML report |

---

## 3. The Three Personas

| Persona | The vendor brings... | The toolkit provides... | Direction of the test traffic |
|---|---|---|---|
| **WFM Supplier** | A real WFM (e.g. Eclipse Symphony) | A WFM Client that accesses the Margo Management Interface exposed by the WFM | Toolkit → vendor's WFM |
| **Device Supplier** | A real device / device-agent | A mock WFM server for the device to connect to | Vendor's device → toolkit |
| **Application Supplier** | An Application Package (`margo.yaml` + resources) | A structural validator checked against the Application Description spec | Static check, no network traffic |

Each of the three is a fully independent conformance surface — a vendor only
needs to run the one(s) that match what they built. All three ultimately
produce the same kind of artifact: an HTML report where every check is
labeled pass/fail and tied back to a specific spec requirement.

### WFM Supplier

The toolkit plays the role of a conformant device and sends a scripted
sequence of requests — valid, invalid, and edge-case — at the vendor's real
WFM, checking that it responds with the right status codes, headers, and
bodies.

### Device Supplier

The toolkit plays the role of a conformant WFM (the mock WFM, mTLS on port
3003). It is used in two ways:

- **Conformance of a device (`core` and its subsets).** The vendor's real
  device agent connects to the mock WFM. A device only ever calls out, so
  nothing is sent to it: the mock WFM records every request it receives and
  judges it with the rules of `assertions.json`, which it loads at start. The
  group changes the desired state (also to manifests and content a device
  must reject), and each step reads what the device then sent or did not
  send, and the mock WFM's verdict on it.
- **Check of the setup (`self-check`, `quick-check`, `random-order`).** The
  toolkit's own device client runs scenarios against the mock WFM. This
  checks the mock WFM and the identity setup; it says nothing about a
  vendor's device.

### Application Supplier

There is no live protocol exchange here — the toolkit reads the vendor's
Application Package (its `margo.yaml` manifest plus referenced resources)
and checks its structure, required fields, and cross-references against the
Application Description spec.

---

## 4. Identity & Transport Model

In spec baseline `1.0.0-rc.3`, WFM ↔ device traffic is authenticated via
**MIAF** (the Margo Identity and Authorization Framework): each side holds an
**X.509-SVID** — a SPIFFE-based identity certificate issued by a Margo
Identity Service (MIS) — and every Management Interface call runs over
**mutual TLS**. The earlier scheme (an onboarding API call, then an HTTP
Message Signature on each request) no longer exists in the spec, and the
suite does not test it.

| Side | SPIFFE ID |
|---|---|
| WFM | `spiffe://<trust-domain>/margo/wfm/<wfm-id>` |
| WFM Client (device) | `spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/<wfm-client-id>` |

The toolkit does not issue identities. The SVIDs come from the MIS of the
trust domain (for the sandbox: `mis.sh generate-svid`) and are given to the
runner once, as a folder, in *Setup MIAF Identity* (WFM Supplier) or *Setup
Identity* (Device Supplier). On every request the toolkit presents its SVID,
validates the X.509-SVID of its peer against the trust anchor of the Trust Bundle (the SVID Root CA certificate of the MIS, provided out of band), and checks that the
other side's SPIFFE ID is the one that belongs to its own.

The full identity model — trust bundles, SVID issuance and rotation, and
exactly what changed from the earlier scheme — is covered in
[`conformance-flows-and-miaf-migration.md`](conformance-flows-and-miaf-migration.md).

---

## 5. Repository Structure

```
conformance_test_toolkit/
├── ctt-creator/                     # CLI #1: author test groups
│   ├── ctt-start.sh                 #   entry point
│   └── <persona>/dev-guide/         #   developer guide per persona
│
├── ctt-runner/                      # CLI #2: execute tests, generate reports
│   ├── ctt-start.sh                 #   entry point
│   ├── wfm-supplier/                #   WFM Supplier persona
│   │   ├── scripts/run_wfm_scenarios.js   # runs scenario files and Postman collections over mTLS
│   │   └── utils/                   #   identity (fixtures/miaf/real), sample app package
│   ├── device-supplier/             #   Device Supplier persona (Go)
│   │   ├── scripts/run_tests.go     #   the toolkit's device client (scenario runner)
│   │   ├── scripts/cmd/device-supplier/   # the mock WFM server
│   │   └── utils/                   #   assertions.json, deployment templates, sample apps
│   ├── app-supplier/scripts/        #   Application Package structural validator (Go)
│   └── reports/<persona>/           #   generated HTML reports
│
├── test-suites/                     # Test cases and groups
│   ├── wfm-supplier/<group>/        #   group.json + test-cases/*.json
│   ├── device-supplier/<group>/     #   group.json + test-cases/*.json
│   └── app-supplier/<package>/      #   Application Packages (margo.yaml + resources)
│
├── docs/                            # Setup guides, this document, flows, test-case templates
└── _archive/                        # Deprecated files from the earlier layout
```

Each persona's runner is independent of the other two — there is no shared
runtime dependency between them beyond the common test-data conventions
described below.

---

## 6. The Test Data Model

### Test cases

- **Scenario files** (`test-suites/<persona>/<group>/test-cases/*.json`)
  declare the actual requests, expected responses, and validations. A
  scenario is a named sequence of steps. The WFM Supplier persona also
  accepts a **Postman collection** in the same folder.
- **CR-ID tagging** — a scenario or step lists the spec requirement IDs it is
  evidence for (`crIds`), so the report can show them next to each result.
  A step carries only the IDs that its own checks judge: if the step can fail
  for a reason that has nothing to do with one of its IDs, that check belongs
  in a separate step. The report marks a requirement FAIL when any step that
  carries it fails, so a step with unrelated IDs would fail all of them at once.
  A scenario may list many IDs; a step normally has one to three.

The format of both is described in
[`margo-conformance-approach.md`](margo-conformance-approach.md).

### Test groups

A **group** is a named list of test IDs with a spec version: `group.json`
holds the name, version, description, the folder where the test files live,
and the IDs of the scenarios, steps or Postman requests to run. The tests
themselves stay in the test files; the group only selects from them.

```json
{
  "name": "quick-check",
  "version": "1.0.0-rc.3",
  "persona": "wfm-supplier",
  "description": "A first check of the URL and the identity setup, a few seconds: ...",
  "FolderPath": ["../test-suites/wfm-supplier/quick-check/test-cases"],
  "testCases": ["wfm-core-capabilities", "wfm-core-cap-1", "wfm-core-cap-2", "..."]
}
```

How it helps:

- **Spec authors / Margo:** they can add or regroup tests without touching
  the runner. They put the test files in a folder and run `ctt-creator`,
  which asks for the group's name, version and description and writes the
  `group.json` with every test ID it finds in that folder.
- **Vendors:** they pick one name instead of choosing tests. They can start
  with a small group while developing (`quick-check`, or the group for the
  area they are working on) and run `core` when ready.
- **Reviewers / plugfest:** the report carries the group name and version, so
  "passed `core` 1.0.0-rc.3" means the same thing on every machine, and two
  vendors' results are comparable.

The groups shipped with the toolkit:

| Group | Persona | What it checks |
|---|---|---|
| `quick-check` | both | A first check of the URL and the identity setup, a few seconds (no device needed) |
| `self-check`, `random-order` | Device | The CTT's own device client against the mock WFM (no device needed) |
| `management-interface` | both | The Management Interface API, accepted and rejected requests |
| `identity` | both | mTLS handshake, trust, SVID rotation |
| `observability` | both | OpenTelemetry |
| `deployment-delivery` | WFM | A multi-component deployment: URLs, deployment YAML, bundle, digests |
| `postman-lifecycle`, `postman-error-handling` | WFM | The same kind of tests written as Postman collections |
| `manifest-integrity` | Device | Manifests and content a device must reject, one it must accept, state kept across a restart |
| `core` | both | The full set — the conformance result. WFM: every scenario. Device: every real-device scenario |

`core` holds the complete test-case file of a persona. Each other group has
its own `test-cases` folder with the scenarios it runs, so a scenario that is
changed in `core` has to be changed in the group that also carries it.

---

## 7. The Two-CLI Workflow

1. **`ctt-creator/ctt-start.sh`** — interactive: pick a persona, enter a
   group name (new or existing), the spec version and a description, then
   the folder that holds the test-case files. It writes
   `test-suites/<persona>/<group>/group.json`.
2. **`ctt-runner/ctt-start.sh`** — pick a persona, set up the identity once,
   pick a group (or, for Application Supplier, a package to check), enter the
   URL of the system under test, and it runs that persona's runner and writes
   the HTML report into `ctt-runner/reports/<persona>/`.

The two steps can be done by different people: a spec author creates and
commits a group; any vendor who clones the repository can run it.

Exact commands are in the top-level [`README.md`](../README.md) and the two
setup guides; this document only describes the roles the two CLIs play in
the overall system.

---

## 8. Reporting & Traceability

Every run produces a single HTML report scoped to the group (or package)
that was tested. A report always shows, for every check that ran:

- the request/response or validation that was exercised,
- the expected outcome vs. the actual outcome, and
- the spec requirement ID (CR-ID) that check is evidence for.

This is what lets a vendor, or a spec reviewer, go from a single failing row
straight to the specific clause of the spec it violates — rather than a suite
that only reports an aggregate pass/fail. A check that could not be run
(for example an SVID rotation test when no SVID was rotated) is reported as
NOT RUN with the reason, not as a pass.

Both runners write the same report template — header (persona, group,
versions, system under test, run time), result line, Requirements Coverage,
Scenario Summary, Step Details — so a WFM report and a device report read the
same way. A requirement is FAIL when any step that checks it failed, PASS when
at least one passed and none failed, NOT RUN when none of its steps ran.

**Signed report.** At the end of every run `ctt-runner/ctt-start.sh` calls
`ctt-runner/reports/scripts/pdf_ops.sh`, which converts the HTML report to PDF and signs it
(`ctt-runner/reports/scripts/pdf_signer.py`). The signed PDF and the signer's CA certificate
(`ca_cert.pem`) are written next to the HTML report; `ctt-runner/reports/scripts/pdf_verify.py`
checks the signature. The default signer is a certificate authority the CTT
creates once per machine (`ctt-runner/reports/scripts/signing-identity/`); `CTT_SIGN_CERT` signs
with a supplied `.p12` instead, and `CTT_SIGN_REPORT=no` skips signing.

---

## 9. Where to Go Next

| Document | What it's for |
|---|---|
| [`README.md`](../README.md) | Prerequisites and the commands of the two CLIs |
| [`wfm-supplier-setup-guide.md`](wfm-supplier-setup-guide.md) | Step-by-step for testing a WFM |
| [`device-supplier-setup-guide.md`](device-supplier-setup-guide.md) | Step-by-step for testing a device |
| [`margo-conformance-approach.md`](margo-conformance-approach.md) | The conformance approach and the test-case templates (Postman, Margo scenario schema) |
| [`conformance-flows-and-miaf-migration.md`](conformance-flows-and-miaf-migration.md) | Full protocol detail: every conformance flow end to end, terminology glossary, and what changed from the onboarding/signature flow to MIAF |
