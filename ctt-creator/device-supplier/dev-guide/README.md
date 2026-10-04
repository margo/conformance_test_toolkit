# CTT Creator — Device Supplier Dev Guide

## Overview

The **Device Supplier** persona verifies that a Margo-compliant device correctly implements the Device Management Interface (SBI). The CTT Creator for this persona generates and manages test groups containing JSON test cases consumed by the CTT Runner.

## Directory Layout

```
device-supplier/
├── scripts/     # Shell scripts for test group authoring and management
├── utils/       # Supporting data: group configs, deployment templates, test scenarios
│   ├── groups/              # One subfolder per test group, each with a group.json
│   ├── data/                # Seed data (clients.json, deployments.json)
│   ├── deployment-template.yaml
│   └── assertions.json      # Reusable assertion definitions
└── dev-guide/
    └── README.md            # This file
```

## Key Concepts

| Term | Meaning |
|---|---|
| **Test Group** | A named collection of test cases (e.g. `core`, `silver`, `gold`). Each group has a `group.json` that lists the test-case files to run. |
| **group.json** | Describes a test group: its name, the persona, and the path to the consolidated test-case JSON file under `test-suites/`. |
| **Test Case JSON** | Declarative scenario file (`test-suites/device-supplier/<group>/test-cases/*.json`) read by the CTT Runner. |
| **MIAF / SVID** | The mTLS identity mechanism. The device presents an X.509-SVID (`spiffe://margo.org/device/<id>`) to authenticate to the WFM mock server. |
| **Negative Fixture** | A test-control mechanism (`PUT /api/v1/test/deployments` with `negativeFixture`) that arms the mock server to corrupt responses so the device's rejection logic can be tested. |
| **manifestVersion** | A monotonically increasing counter the WFM increments each time the desired state changes. Used by the device to detect updates via `If-None-Match`. |

## Test Groups

| Group | Purpose |
|---|---|
| `core` | Baseline conformance — capabilities, deployments, desired-state, status |
| `flex-order` | Same scenarios as core but executed in random order (`-flexible-order` flag) |
| `silver` | Extended set covering header integrity, negative fixtures, manifest semantics |
| `gold` | Multi-component deployments, bundle digest checks |
| `bronze` | Additional vendor-specific scenarios |
| `diamond` | Full suite including ORAS registry tests |

## How to Add or Update a Test Group

1. Edit or create `utils/groups/<group-name>/group.json` with the group metadata.
2. Add or update the corresponding test-case file under `test-suites/device-supplier/<group>/test-cases/`.
3. Run `../../ctt-start.sh` → select **Device Supplier** to register or update the group.

## Running Tests

Tests are executed by the **CTT Runner**, not this creator. To run:

```bash
cd ../../ctt-runner
./ctt-start.sh
# Select: Device Supplier → choose a test group
```

Or directly:

```bash
cd ../../ctt-runner/device-supplier
go run ./scripts/... \
  -scenario-file ../../test-suites/device-supplier/core/test-cases/device-supplier.json \
  -base-url http://<device-ip>:<port>
```

## MIAF Identity Setup

Before running identity tests, provision an X.509-SVID:

```bash
../../common/scripts/provision-mis-identity.sh
```

This writes `ctt-runner/device-supplier/utils/certs/svid-cert.pem`, `svid-key.pem`, and `svid-ca.pem`.

## Useful Flags (CTT Runner)

| Flag | Description |
|---|---|
| `-base-url` | Device SBI base URL (e.g. `http://192.168.1.10:8080`) |
| `-scenario-file` | Path to the test-case JSON file |
| `-flexible-order` | Randomise scenario execution order (stress test for state isolation) |
| `-miaf-cert`, `-miaf-key`, `-miaf-ca` | Paths to SVID cert/key/CA for mTLS steps |
