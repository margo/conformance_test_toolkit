# CTT Creator — WFM Supplier Dev Guide

## Overview

The **WFM Supplier** persona verifies that a Workload Fleet Manager (WFM) conforms to the Margo specification. The CTT acts as a mock device (WFM Client) and sends requests to the vendor's real WFM over mTLS. Test cases are Margo scenario files (JSON) or Postman collections. This guide is for people who write or change the WFM test cases and groups.

## Directory Layout

```
ctt-creator/wfm-supplier/
├── scripts/margo-test-gen.sh        # OpenAPI → Postman collection (Portman)
├── utils/postman_collection.json    # The collection generated from the rc.3 OpenAPI file
└── dev-guide/README.md              # This file

test-suites/wfm-supplier/
├── core/
│   ├── group.json                   # Written by ctt-creator
│   └── test-cases/wfm-supplier.json # The full set of scenarios
└── <group>/
    ├── group.json
    └── test-cases/                  # <group>.json, or postman_collection.json

ctt-runner/wfm-supplier/
├── scripts/run_wfm_scenarios.js     # Runs scenario files and Postman collections over mTLS
└── utils/fixtures/                  # Identity (miaf/real), sample app package, provisioning script
```

## Key Concepts

| Term | Meaning |
|---|---|
| **Test Group** | A named list of test IDs with a spec version (e.g. `core`, `quick-check`, `identity`). Each group has a `group.json`, written by `ctt-creator`. |
| **group.json** | Describes a test group: its name, version, persona, description, the folder with its test-case files, and the scenario, step or Postman request IDs to run. |
| **Scenario JSON** | Declarative scenario file (`test-suites/wfm-supplier/<group>/test-cases/*.json`) read by the CTT Runner. |
| **Postman collection** | A Postman v2.1 export in the same folder. The runner converts each request into a step and sends it over mTLS. Postman test scripts are not executed. The rules are in `docs/margo-conformance-approach.md`, section "Postman Template for Expressing sequences of API exchanges". |
| **Contract tests** | A Postman collection generated from the Margo OpenAPI file with Portman (`ctt-creator` → WFM Supplier → *OpenAPI spec based contract tests*). The runner executes it from its own menu option of the same name: one check per documented response of each endpoint. |
| **MIAF / mTLS** | The WFM (`spiffe://<trust-domain>/margo/wfm/<wfm-id>`) must accept mTLS connections from its clients (`spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/<wfm-client-id>`). The CTT presents a client SVID issued by the MIS to test this. |
| **SVID Rotation** | The WFM must continue accepting connections as the device rotates its SVID without disruption. |

## Test Groups

| Group | Purpose |
|---|---|
| `core` | Every scenario of the suite — the conformance result |
| `quick-check` | Identity accepted, a device registered, manifest served, conditional request answered — first check of the setup |
| `management-interface` | Capabilities, desired-state retrieval, content negotiation, status reporting, ETag and cache headers |
| `deployment-delivery` | Multi-component `ApplicationDeployment` — manifest URLs, deployment YAML, bundle, digests |
| `identity` | mTLS handshake, trust bundle, refused clients, SVID rotation |
| `observability` | A device reporting an OpenTelemetry collector is accepted |
| `postman-lifecycle`, `postman-error-handling` | The same kind of tests written as Postman collections |

## Key Requirement IDs

| CR ID | Area |
|---|---|
| `MARGO-WFM-MANAGEMENTINTERFACE-004` | Desired-state endpoint exists |
| `MARGO-WFM-MANAGEMENTINTERFACE-007` | Default content type when no Accept header |
| `MARGO-WFM-MANAGEMENTINTERFACE-008` | 406 for unsupported Accept header (MI-008) |
| `MARGO-WFM-MANAGEMENTINTERFACE-023` | The server side of the API contract: invalid values rejected (422), content-addressed URLs, wrong digests answered with 404 |
| `MARGO-WFM-MANAGEMENTINTERFACE-030` | ETag / 304 Not Modified |
| `MARGO-WFM-MANAGEMENTINTERFACE-MIAF-001` | Accept valid SVID mTLS connection |
| `MARGO-WFM-MANAGEMENTINTERFACE-MIAF-003` | Accept connection after SVID rotation |

## Generating the Contract Collection from the OpenAPI Spec

```bash
bash ctt-creator/ctt-start.sh
# 1 (WFM Supplier) → 1 (OpenAPI spec based contract tests) → path or URL of the OpenAPI file
# Requires: node, npm (Portman is fetched with npx)
# Output: ctt-creator/wfm-supplier/utils/postman_collection.json
```

Run it from the runner with `ctt-runner/ctt-start.sh` → WFM Supplier → *3. OpenAPI spec based contract tests*.

## How to Add or Update a Test Group

1. Write or change the test cases in `test-suites/wfm-supplier/<group>/test-cases/`: a scenario file, or a Postman collection exported as v2.1. Every scenario, step and Postman request needs a unique `id`.
2. Run the creator and answer its prompts:

   ```bash
   bash ctt-creator/ctt-start.sh
   # 1 (WFM Supplier) → 2 (Functional tests) → 0 (new group, or the number of an existing group)
   # → group name → version (Enter = 1.0.0-rc.3) → description
   # → folder: test-suites/wfm-supplier/<group>/test-cases
   ```

   It writes `test-suites/wfm-supplier/<group>/group.json` with every test ID found in that folder.
3. A new scenario that belongs to the full set goes into `core/test-cases/wfm-supplier.json` as well; then run the creator again for `core`.

Working examples: `core` (scenario file), `postman-lifecycle` and `postman-error-handling` (Postman collections).

## Running Tests

The CTT acts as a device (WFM Client) that connects to the **vendor's real WFM**
over mTLS. You provide the WFM's base URL — everything before `/api/v1`; the CTT
adds the Margo paths (`/api/v1/capabilities/{deviceId}`, `/api/v1/deployments`, etc.).
The steps, including the identity setup, are in `docs/wfm-supplier-setup-guide.md`.

```bash
bash ctt-runner/ctt-start.sh
# 1 (WFM Supplier) → 1. Setup MIAF Identity (once) → 2. Functional tests → choose a group → enter the WFM URL
```

Or, once the identity is set up, one group without the menu:

```bash
bash ctt-runner/ctt-start.sh wfm quick-check https://localhost:8084/v1alpha2/margo
```

(`https://localhost:8084/v1alpha2/margo` is the sandbox Symphony WFM.)

To validate new tests against a conforming implementation, run the group against the
CTT's own mock WFM (`ctt-runner/ctt-start.sh` → Device Supplier → Start Mock WFM Server;
URL `https://localhost:3003/v1alpha2/margo`), with identities from the same MIS.

> **Deployment-delivery note:** the `wfm-multi-component-deployment` scenario (groups
> `deployment-delivery` and `core`) waits up to 5 minutes for a deployment to be assigned
> to the CTT's device. On the sandbox Symphony WFM the runner provisions it and the guide
> shows the assign step; on another WFM the operator assigns it in the WFM's own console.
