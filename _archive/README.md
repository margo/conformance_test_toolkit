# _archive

This directory preserves deprecated files from the pre-restructure layout of the
Margo Conformance Test Toolkit. Nothing here is part of the active test suite.

These files are kept for reference and rollback only. Once the new structure
(`ctt-creator/`, `ctt-runner/`, `test-suites/`) has been verified in production,
this directory will be deleted.

## Contents

| Directory | Was | Notes |
|---|---|---|
| `wfm-supplier/` | `wfm-supplier/` (old scripts, Postman collection, reports) | Postman collection pre-dates MIAF — references `/api/v1/onboarding` which no longer exists |
| `device-supplier/` | `device-supplier/` (bin, device-scenarios, old docs) | Pre-built binaries; docs superseded by `docs/ARCHITECTURE.md` |
| `testcases/` | `testcases/` (scattered per-requirement JSON) | Superseded by consolidated files in `test-suites/` |
| `manual-test-cases/` | `manual-test-cases/` | Not yet automated; kept for reference |
| `runner-scripts/` | `Runner/` helper scripts | Superseded by `ctt-runner/ctt-start.sh` |
