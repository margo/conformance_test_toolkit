# CTT Creator — WFM Supplier Dev Guide

## Overview

The **WFM Supplier** persona verifies that a Margo-compliant Workload Fleet Manager (WFM) correctly implements the Device Management Interface (SBI) from the WFM side. The CTT Creator for this persona generates and manages test groups containing Newman/Postman collections and the JSON scenario files consumed by the CTT Runner.

## Directory Layout

```
wfm-supplier/
├── scripts/     # Shell scripts for test generation (margo-test-gen.sh)
├── utils/       # Supporting data: group configs, Postman collections, Newman data
│   ├── groups/              # One subfolder per test group, each with a group.json
│   ├── newman-data/         # Certificates and environment files for Newman runs
│   ├── postman_collection.json
│   └── postman_collection_functional.json
└── dev-guide/
    └── README.md            # This file
```

## Key Concepts

| Term | Meaning |
|---|---|
| **Test Group** | A named collection of test cases (e.g. `core`, `silver`, `header-integrity`). Each group has a `group.json`. |
| **group.json** | Describes a test group: its name, persona, and path to the scenario JSON under `test-suites/`. |
| **Scenario JSON** | Declarative scenario file (`test-suites/wfm-supplier/<group>/test-cases/*.json`) read by the CTT Runner. |
| **Newman** | Postman's CLI runner. Used in older/legacy WFM test flows. New scenarios use the native JS runner (`run_wfm_scenarios.js`). |
| **margo-test-gen.sh** | Script that uses Portman to auto-generate a `postman_collection.json` from the Margo OpenAPI spec. Run this when the spec changes. |
| **MIAF / mTLS** | The WFM must accept mTLS connections from devices presenting a valid X.509-SVID (`spiffe://margo.org/device/<id>`). The CTT presents its own SVID to test this. |
| **SVID Rotation** | The WFM must continue accepting connections as the device rotates its SVID without disruption. |

## Test Groups

| Group | Purpose |
|---|---|
| `core` | Baseline: capabilities, desired-state retrieval, ETag, content negotiation, status reporting |
| `header-integrity` | ETag correctness and Content-Type validation on the manifest endpoint |
| `multi-component` | Multi-component `ApplicationDeployment` — wait=true/false, digest & caching contract |
| `application-registry` | OCI registry conformance (AR-002 through AR-012) |
| `silver` | Extended set including MIAF identity scenarios |
| `diamond` | Full suite |

## Key Requirement IDs

| CR ID | Area |
|---|---|
| `MARGO-WFM-MANAGEMENTINTERFACE-004` | Desired-state endpoint exists |
| `MARGO-WFM-MANAGEMENTINTERFACE-007` | Default content type when no Accept header |
| `MARGO-WFM-MANAGEMENTINTERFACE-008` | 406 for unsupported Accept header (MI-008) |
| `MARGO-WFM-MANAGEMENTINTERFACE-021/022` | Capabilities: register and update |
| `MARGO-WFM-MANAGEMENTINTERFACE-023` | Capabilities: reject invalid values (422) |
| `MARGO-WFM-MANAGEMENTINTERFACE-030` | ETag / 304 Not Modified |
| `MARGO-WFM-MANAGEMENTINTERFACE-MIAF-001` | Accept valid SVID mTLS connection |
| `MARGO-WFM-MANAGEMENTINTERFACE-MIAF-003` | Accept connection after SVID rotation |

## Generating Test Collections from the OpenAPI Spec

```bash
cd scripts/
./margo-test-gen.sh
# Requires: node, portman (npm install -g @apideck/portman)
# Output: ../utils/postman_collection.json
```

## How to Add or Update a Test Group

1. Edit or create `utils/groups/<group-name>/group.json`.
2. Add or update the corresponding test-case file under `test-suites/wfm-supplier/<group>/test-cases/`.
3. Run `../../ctt-start.sh` → select **WFM Supplier** to register or update the group.

## Running Tests

Tests are executed by the **CTT Runner**:

```bash
cd ../../ctt-runner
./ctt-start.sh
# Select: WFM Supplier → choose a test group
```

Or directly:

```bash
cd ../../ctt-runner/wfm-supplier
node scripts/run_wfm_scenarios.js \
  --base-url https://<wfm-host>:<port> \
  --scenario-file ../../test-suites/wfm-supplier/core/test-cases/wfm-supplier.json
```

## MIAF Identity Setup

Before running identity tests, provision SVID certificates:

```bash
../../common/scripts/provision-mis-identity.sh
```

Writes certs to `ctt-runner/wfm-supplier/utils/certs/`.
