# Margo Conformance Test Toolkit — Architecture

**Spec baseline:** Margo `1.0.0-rc.3` (MIAF — see
[`CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md`](CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md)
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
for whatever that persona talks to (a mock WFM, a mock device, or a
structural validator for a package), drives a battery of both valid and
deliberately-broken requests through it, and produces a pass/fail report
where every check is traceable back to a specific requirement ID (CR-ID) in
the spec.

In short: **bring your implementation, get back a report showing exactly
which parts of the Margo spec it does and does not conform to.**

---

## 2. Architecture at a Glance

```
                         ┌─────────────────────────────┐
                         │        conformance.sh        │   Prepare
                         │  (pick persona, build/select  │
                         │   a test group, generate      │
                         │   group.json + certs)         │
                         └───────────────┬───────────────┘
                                         │
                                         ▼
                         ┌─────────────────────────────┐
                         │         run-tests.sh          │   Execute
                         │  (dispatches to the runner    │
                         │   for the chosen persona)      │
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
                         │    Runner/<persona>/*.html    │   HTML report,
                         │   pass/fail, per CR-ID          │   one per run
                         └─────────────────────────────┘
```

Two CLIs drive the whole system:

| CLI | Role |
|---|---|
| `conformance.sh` | **Prepare** — pick a persona, author or select a test group, decide which test IDs it contains |
| `run-tests.sh` | **Execute** — run the chosen group's tests against a real system under test and generate the HTML report |

---

## 3. The Three Personas

| Persona | The vendor brings... | The toolkit provides... | Direction of the test traffic |
|---|---|---|---|
| **WFM Supplier** | A real WFM (e.g. Eclipse Symphony) | A mock device / WFM-Client that fires real requests at it | Toolkit → vendor's WFM |
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

The toolkit plays the role of a conformant WFM and waits for the vendor's
real device agent to connect, checking that the device calls the right
endpoints, authenticates correctly, and handles the manifests it is served
correctly.

### Application Supplier

There is no live protocol exchange here — the toolkit reads the vendor's
Application Package (its `margo.yaml` manifest plus referenced resources)
and checks its structure, required fields, and cross-references against the
Application Description spec.

---

## 4. Identity & Transport Model

As of spec baseline `1.0.0-rc.3`, WFM ↔ device traffic is authenticated via
**MIAF** (the Margo Identity and Authorization Framework): each side holds an
**X.509-SVID** — a SPIFFE-based identity certificate, provisioned out of band
by an operator or a Margo Identity Service (MIS) — and every Management
Interface call runs over **mutual TLS**. This replaced the earlier scheme,
where each request carried its own HTTP Message Signature (RFC 9421).

The toolkit supports **both transports side by side**: existing signed
scenarios keep working unchanged, and newer scenarios opt into mTLS per-step.
This lets the WFM Supplier and Device Supplier runners test a vendor
implementation regardless of which transport generation it has migrated to.

The full identity model — SPIFFE IDs, trust bundles, SVID issuance and
rotation, and exactly what changed between the two transports — is
out of scope for an architecture document and is covered in depth in
[`CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md`](CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md).

---

## 5. Repository Structure

```
conformance_test_toolkit/
├── conformance.sh                   # CLI #1: prepare test data/groups
├── run-tests.sh                     # CLI #2: execute tests, generate reports
│
├── wfm-supplier/                    # WFM Supplier persona
│   ├── run_wfm_scenarios.js         #   declarative scenario runner (mTLS + RFC 9421)
│   ├── postman_collection.json      #   legacy Postman/Newman collection
│   └── fixtures/                    #   MIAF SVID + trust-bundle fixtures, sample app packages
│
├── device-supplier/                 # Device Supplier persona (Go)
│   ├── run_tests.go                 #   scenario runner driving the mock WFM
│   └── cmd/device-supplier/         #   the mock WFM server itself
│
├── Application-Supplier-Service/    # Application Package structural validator (Go)
├── Application-Supplier/            # Sample/reference Application Package
│
├── testcases/                       # Declarative scenario JSON, one folder per test group
├── Data-Generator/                  # conformance.sh's working area: group + test-ID authoring
├── Runner/                          # Generated HTML reports land here, one subfolder per persona
├── scripts/                         # One-off setup helpers (e.g. MIS identity provisioning)
│
├── docs/                            # This document, the MIAF migration guide, the client brief
└── manual-test-cases/               # Test cases not yet automated into the runners above
```

Each persona's runner is independent of the other two — there is no shared
runtime dependency between `wfm-supplier/`, `device-supplier/`, and
`Application-Supplier-Service/` beyond the common test-data conventions
described below.

---

## 6. The Test Data Model

Regardless of persona, a **test group** is the core organizational unit: a
named collection of test IDs a vendor runs together (e.g. a "core" group for
baseline conformance, a larger group for full coverage).

- **`group.json`** — declares a group's name, version, persona, and the list
  of test/scenario/step IDs it includes.
- **Scenario files** (`testcases/<group>/*.json`) — declare the actual
  requests, expected responses, and validations. A scenario is a named
  sequence of steps; a step can be included in a group individually, or a
  whole scenario can be included at once.
- **CR-ID tagging** — every validation is tagged with the spec requirement
  ID it checks, so the final report can show, per requirement, whether it
  was covered and whether it passed.

This model is intentionally the same shape across all three personas, so a
vendor who has learned how groups work for one persona already understands
it for the others.

---

## 7. The Two-CLI Workflow

1. **`conformance.sh`** — interactive: pick a persona, then either select an
   existing test group or build a new one by choosing which scenarios/steps
   to include. Writes into `Data-Generator/`.
2. **`run-tests.sh`** — pick a persona and a group (or, for Application
   Supplier, a package to check), point it at the real system under test,
   and it runs the appropriate persona's runner and writes the HTML report
   into `Runner/`.

Exact commands and flags are covered in the top-level
[`README.md`](../README.md); this document only describes the roles the two
CLIs play in the overall system.

---

## 8. Reporting & Traceability

Every run produces a single HTML report scoped to the group (or package)
that was tested. A report always shows, for every check that ran:

- the request/response or validation that was exercised,
- the expected outcome vs. the actual outcome, and
- the spec requirement ID (CR-ID) that check is evidence for.

A requirement is shown as "covered" once at least one check tagged with its
CR-ID has run; whether it is shown as passing depends on whether that check
(or any of that requirement's checks) succeeded. This is what lets a vendor,
or a spec reviewer, go from a single failing row straight to the specific
clause of the spec it violates — rather than a suite that only reports an
aggregate pass/fail.

---

## 9. Where to Go Next

| Document | What it's for |
|---|---|
| [`README.md`](../README.md) | Prerequisites and exact commands to run each persona |
| [`CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md`](CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md) | Full protocol detail: every conformance flow end to end, terminology glossary, and the RFC 9421 → MIAF migration analysis |
| [`client-demo-brief.md`](client-demo-brief.md) | One-page summary for explaining the suite to a vendor or stakeholder |
