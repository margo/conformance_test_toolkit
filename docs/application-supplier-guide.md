# Application Supplier Validation Module

## Overview

The Application Supplier Validation Module is responsible for validating the `margo.yaml` file provided by an Application Supplier.

The supplier provides an application package (application folder) containing the application artifacts and the `margo.yaml` file. The validation process verifies that all mandatory keys and their values comply with the expected schema and validation rules.

The validation logic has been implemented in Go and integrated into the CTT Runner framework.

---

## Workflow

### Step 1: Application Package Submission

The Application Supplier provides an application folder containing the application resources and the required `margo.yaml` file.

Example:

```text
application-package/
│
├── margo.yaml
├── manifests/
├── charts/
└── other application artifacts
```

---

### Step 2: Application Selection

From the CTT Runner Application Supplier menu, the user selects the application package to validate.

Example:

```text
Application Supplier

Available Applications

1) sample-app
2) demo-app

Select Application: 1
```

Once selected, the validation process begins.

---

### Step 3: YAML File Discovery

The validator searches the selected application package for the Application Description:

```text
margo.yaml
```

If there is no `margo.yaml`, the validator uses a YAML file with the top-level keys `apiVersion`, `metadata` and `deploymentProfiles`. The Application Description has no `kind` field. If no Application Description is found, validation immediately fails and an appropriate error is reported.

---

### Step 4: YAML Parsing

The `margo.yaml` file is loaded and parsed using the Go implementation.

The validation process checks that:

- The file exists.
- The file is readable.
- The YAML syntax is valid.
- The file can be successfully parsed into the expected structure.

If parsing fails, validation stops and the corresponding error is captured in the report.

---

### Step 5: Required Key Validation

The validator checks whether all mandatory fields exist in the YAML file.

Examples of validations include:

```yaml
applicationName:
applicationVersion:
vendor:
description:
```

Checks performed:

- Mandatory key presence
- Missing key detection
- Empty value checks
- Expected data type validation

Example Output:

```text
PASS - applicationName present
PASS - applicationVersion present
FAIL - vendor missing
```

---

### Step 6: Value Validation

After confirming the required keys are present, the validator verifies the contents of each field.

Example:

```yaml
applicationName: inventory-service
applicationVersion: 1.0.0
```

Validation rules may include:

- Value should not be empty.
- Value format should be correct.
- Supported values only.
- Required fields should contain meaningful data.
- Field values should comply with the expected schema.

Example Output:

```text
PASS - applicationName value valid
PASS - applicationVersion value valid
FAIL - vendor value missing
```

---

### Step 7: Validation Result Processing

All validation results are collected and categorized.

The validator records:

- Passed validations
- Failed validations
- Warning messages
- Error details

Example:

```text
Validation Started...

PASS - applicationName
PASS - applicationVersion
PASS - description

FAIL - vendor

Validation Completed
```

---

## Report Generation

After validation completes, a report is automatically generated.

The report contains:

- Application Name
- Execution Timestamp
- Validation Summary
- Passed Checks
- Failed Checks
- Detailed Error Messages

Example Summary:

```text
Total Checks : 10
Passed       : 8
Failed       : 2
```

---

## Report Storage

Reports are stored inside the CTT Runner reports directory.

Example:

```text
ctt-runner/
└── reports/
    └── application-supplier/
```

---

## Report Naming Convention

Each report file is generated using the Application Name and the execution timestamp.

Format:

```text
<ApplicationName>_<Timestamp>
```

Example:

```text
inventory-service_20261015_103000.json
```

This naming convention ensures that every validation run produces a uniquely identifiable report.

---

## Go Implementation

The complete validation workflow has been implemented in Go.

Key responsibilities handled by the implementation include:

- Application package discovery
- YAML file loading
- YAML parsing
- Required key validation
- Value validation
- Error collection
- Result aggregation
- Report generation
- Report storage

---

## Sample Validation Output

```text
================================================
Application Supplier Validation
================================================

Application : inventory-service

PASS - applicationName found
PASS - applicationVersion found
PASS - description found

FAIL - vendor missing

------------------------------------------------

Total Checks : 4
Passed       : 3
Failed       : 1

Validation Completed
```

---

## Directory Structure

```text
ctt-runner/
│
├── reports/
│   └── application-supplier/
│
├── internal/
│   └── application-supplier/
│
└── main.go
```

---

## Conclusion

The Application Supplier Validation Module automates the validation of the `margo.yaml` file provided by application vendors. It ensures that all required keys and values meet the expected validation rules, generates detailed validation results, and stores reports under the `reports/application-supplier` directory for auditing and review purposes.

The solution is fully implemented in Go and integrated with the CTT Runner workflow, enabling consistent and repeatable validation of Application Supplier packages before further certification or conformance testing activities.