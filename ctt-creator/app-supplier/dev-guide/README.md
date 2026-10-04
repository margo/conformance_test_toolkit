# CTT Creator — Application Supplier Dev Guide

## Overview

The **Application Supplier** persona verifies that a Margo-compliant application package conforms to the Margo Application Description specification. The CTT Creator for this persona manages the sample application packages and group configurations used for conformance validation.

## Directory Layout

```
app-supplier/
├── scripts/     # Shell scripts for package preparation and group management
├── utils/       # Sample application packages and supporting utilities
│   └── sample-package/      # A minimal valid Margo application package for testing
│       ├── margo.yaml       # Application description file (the main artifact under test)
│       └── resources/       # Supporting resources referenced by margo.yaml
└── dev-guide/
    └── README.md            # This file
```

## Key Concepts

| Term | Meaning |
|---|---|
| **margo.yaml** | The application description file. This is the primary artifact the Application Supplier persona validates. |
| **Application Package** | An OCI-compliant bundle containing `margo.yaml` and referenced resources, pushed to an OCI registry. |
| **CTT Runner (App Supplier)** | A Go binary (`ctt-runner/app-supplier/scripts/`) that pulls a package from the registry and validates `margo.yaml` against the spec. |
| **MARGO-APP-APPLICATIONDESCRIPTION-\*** | Requirement IDs for the Application Description spec. |

## Sample Package

The sample package at `utils/sample-package/` is a minimal valid Margo application that can be used to verify a passing conformance run. Push it to your OCI registry before running the CTT Runner.

```
sample-package/
├── margo.yaml
└── resources/
    ├── description.md
    ├── license.txt
    ├── release-notes.md
    └── opentelemtry-logo.png
```

## How to Prepare and Push the Sample Package

```bash
# Build and push to your OCI registry (example using ORAS)
oras push <registry>/<repo>:<tag> \
  --config /dev/null:application/vnd.margo.app-description.v1+json \
  utils/sample-package/margo.yaml:application/vnd.margo.app-description.v1+yaml \
  utils/sample-package/resources/
```

## How to Add or Update a Test Group

1. Edit or create `utils/groups/<group-name>/group.json` (create this folder if it doesn't exist yet).
2. Add or update the corresponding test-case file under `test-suites/app-supplier/<group>/test-cases/`.
3. Run `../../ctt-start.sh` → select **Application Supplier** to register the group.

## Running Tests

Tests are executed by the **CTT Runner**:

```bash
cd ../../ctt-runner
./ctt-start.sh
# Select: Application Supplier → enter the OCI image reference
```

Or directly:

```bash
cd ../../ctt-runner/app-supplier
go run ./scripts/... --image <registry>/<repo>:<tag>
```

## Key Requirement IDs

| CR ID | Area |
|---|---|
| `MARGO-APP-APPLICATIONDESCRIPTION-001` | margo.yaml must be present in the package |
| `MARGO-APP-APPLICATIONDESCRIPTION-002` | margo.yaml must conform to the application description schema |
| `MARGO-WFM-APPLICATIONDESCRIPTION-001/002` | WFM must serve packages with correct OCI media types |
