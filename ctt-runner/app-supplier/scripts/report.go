package main

import (
	"fmt"
	"html/template"
	"os"
	"time"
)

// ValidationEntry represents a single validation result displayed in the conformance report.
type ValidationEntry struct {
	CRID        string
	Field       string
	Status      string
	Type        string
	Rule        string
	ActualValue string
	Remarks     string
}

// ValidationReport contains application details, validation entries, and the overall validation status.
type ValidationReport struct {
	Entries            []ValidationEntry
	Status             string
	ApplicationName    string
	ApplicationVersion string
}

// reportTemplate defines the HTML template used to generate the application conformance report.
const reportTemplate = `
<!DOCTYPE html>
<html>

<head>
<meta charset="UTF-8">
<title>Application Supplier Conformance Test Report</title>

<style>

@page { size: A4 landscape; margin: 1cm; }

body {
    font-family: Arial, sans-serif;
    margin: 0;
    background: #f5f5f5;
}

.header {
    background: #333;
    color: white;
    padding: 30px;
}

.header h1 {
    margin: 0 0 30px 0;
    font-size: 42px;
    font-weight: bold;
}

.header p {
    font-size: 18px;
    margin: 10px 0;
}

.summary {
    padding: 20px;
    background: white;
}

.summary h2 {
    margin-top: 0;
}

.pass {
    color: green;
    font-weight: bold;
}

.fail {
    color: red;
    font-weight: bold;
}

table {
    width: 100%;
    border-collapse: collapse;
    background: white;
}

th,
td {
    border: 1px solid #ddd;
    padding: 12px;
    vertical-align: middle;
}

th {
    background: #f2f2f2;
    font-size: 16px;
    font-weight: bold;
    text-align: left;
}

.status-cell {
    text-align: center;
    min-width: 90px;
}

.pass-icon {
    color: green;
    font-size: 26px;
    line-height: 1;
}

.pass-text {
    color: green;
    font-size: 16px;
    font-weight: bold;
}

.fail-icon {
    color: red;
    font-size: 26px;
    line-height: 1;
}

.fail-text {
    color: red;
    font-size: 16px;
    font-weight: bold;
}

</style>

</head>

<body>

<div class="header">

    <h1>
        Application Supplier Conformance Test Report
    </h1>

    <p>
        <strong>Application:</strong>
        {{.ApplicationName}}
    </p>

    <p>
        <strong>Application Version:</strong>
        {{.ApplicationVersion}}
    </p>

    <p>
        <strong>Generated:</strong>
        {{now}}
    </p>

</div>

<div class="summary">

    <h2>Summary</h2>

    <p>
        Total Checks: {{len .Entries}}
        |
        <span class="pass">
            ✅ Passed: {{passedCount .Entries}}
        </span>
        |
        <span class="fail">
            ❌ Failed: {{failedCount .Entries}}
        </span>
    </p>

    <p>
        <strong>
            Success Rate: {{successRate .Entries}}%
        </strong>
    </p>

</div>

<table>

<tr>
    <th>CR-ID</th>
    <th>Application Description Attribute</th>
    <th>Status</th>
    <th>Type</th>
    <th>Validation Rule (Expected)</th>
    <th>Actual Value</th>
    <th>Remarks</th>
</tr>

{{range .Entries}}

<tr>

    <td>{{.CRID}}</td>
    <td>{{.Field}}</td>

    <td class="status-cell">

        {{if eq .Status "PASS"}}
            <div class="pass-icon">✅</div>
            <div class="pass-text">PASS</div>

        {{else if eq .Status "FAIL"}}
            <div class="fail-icon">❌</div>
            <div class="fail-text">FAIL</div>

        {{else}}
            {{.Status}}
        {{end}}

    </td>

    <td>{{.Type}}</td>
    <td>{{.Rule}}</td>
    <td>{{.ActualValue}}</td>
    <td>{{.Remarks}}</td>

</tr>

{{end}}

</table>

</body>
</html>
`

// NewValidationReport creates a validation report with the initial status set to PASSED.
func NewValidationReport() *ValidationReport {
	return &ValidationReport{
		Status: "PASSED",
	}
}

// Log adds a validation entry to the report and updates the overall status on failure.
func (r *ValidationReport) Log(
	validate string,
	details string,
	status string,
) {
	r.Entries = append(
		r.Entries,
		ValidationEntry{
			Field:   validate,
			Remarks: details,
			Status:  status,
		},
	)

	if status == "FAIL" {
		r.Status = "FAILED"
	}
}

// Check adds a validation check entry to the report.
func (r *ValidationReport) Check(
	crId string,
	field string,
	dataType string,
	expected string,
) {
	r.Entries = append(
		r.Entries,
		ValidationEntry{
			CRID:  crId,
			Field: field,
			Type:  dataType,
			Rule:  expected,
		},
	)
}

// Pass updates the last validation entry as passed with the actual value and remarks.
func (r *ValidationReport) Pass(
	actual string,
	details string,
) {
	if len(r.Entries) == 0 {
		return
	}

	last := &r.Entries[len(r.Entries)-1]

	last.Status = "PASS"
	last.ActualValue = actual
	last.Remarks = details
}

// Fail updates the last validation entry as failed with the actual value and remarks, and sets the overall report status to FAILED.
func (r *ValidationReport) Fail(
	actual string,
	details string,
) {
	if len(r.Entries) == 0 {
		return
	}

	last := &r.Entries[len(r.Entries)-1]

	last.Status = "FAIL"
	last.ActualValue = actual
	last.Remarks = details

	r.Status = "FAILED"
}

// GenerateHTMLReport generates an HTML report for the validation results and writes it to the specified file.
func (r ValidationReport) GenerateHTMLReport(
	file string,
) error {
	funcMap := template.FuncMap{
		"lower": func(s string) string {
			switch s {

			case "PASS":
				return "pass"

			case "FAIL":
				return "fail"

			default:
				return "validate"
			}
		},

		"now": func() string {
			return time.Now().
				UTC().
				Format(
					"2006-01-02T15:04:05Z",
				)
		},

		"passedCount": func(
			entries []ValidationEntry,
		) int {
			count := 0

			for _, e := range entries {
				if e.Status == "PASS" {
					count++
				}
			}

			return count
		},

		"failedCount": func(
			entries []ValidationEntry,
		) int {
			count := 0

			for _, e := range entries {
				if e.Status == "FAIL" {
					count++
				}
			}

			return count
		},

		"successRate": func(
			entries []ValidationEntry,
		) string {
			total := 0
			passed := 0

			for _, e := range entries {
				if e.Status == "PASS" ||
					e.Status == "FAIL" {

					total++

					if e.Status == "PASS" {
						passed++
					}
				}
			}

			if total == 0 {
				return "0.0"
			}

			return fmt.Sprintf(
				"%.1f",
				float64(passed)*100/float64(total),
			)
		},
	}

	tmpl := template.Must(
		template.New("report").
			Funcs(funcMap).
			Parse(reportTemplate),
	)

	f, err := os.Create(file)
	if err != nil {
		return err
	}

	defer f.Close()

	return tmpl.Execute(f, r)
}
