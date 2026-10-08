# CTT Creator

The CTT Creator is responsible for **authoring and managing test groups** for each Margo persona. It prepares the test data, group configurations, and test case structures that the CTT Runner will execute.

## Structure

```
ctt-creator/
├── common/           # Shared scripts used across all personas (e.g. identity provisioning)
├── device-supplier/  # Test authoring for the Device Supplier persona
├── wfm-supplier/     # Test authoring for the WFM Supplier persona
├── app-supplier/     # Test authoring for the Application Supplier persona
└── ctt-start.sh      # Entry point — launches the interactive test group setup CLI
```

## Quick Start

```bash
./ctt-start.sh
```

Select the persona you want to author or update test groups for. The CLI will guide you through creating, listing, or updating test groups.

## Personas

| Persona | Dev Guide |
|---|---|
| Device Supplier | [device-supplier/dev-guide/README.md](device-supplier/dev-guide/README.md) |
| WFM Supplier | [wfm-supplier/dev-guide/README.md](wfm-supplier/dev-guide/README.md) |
| Application Supplier | [app-supplier/dev-guide/README.md](app-supplier/dev-guide/README.md) |

## Common

`common/scripts/` — shared helper scripts used across personas.
