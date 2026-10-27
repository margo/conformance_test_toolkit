package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

func main() {

	if len(os.Args) < 2 {

		fmt.Println(
			"Usage: validator <folder|zip|tar.gz|oci://host/repository:tag>",
		)

		os.Exit(1)
	}

	inputPath := os.Args[1]
	workDir := ""

	// packageRef is the OCI reference of the Application Package when the
	// package is validated from the Application Registry.
	packageRef := ""

	var info os.FileInfo
	var err error

	if !IsOCIReference(inputPath) {

		info, err = os.Stat(inputPath)

		if err != nil {
			panic(err)
		}
	}

	//--------------------------------------------------
	// OCI reference: pull the Application Package
	//--------------------------------------------------

	if info == nil {

		packageRef = inputPath
		workDir = "./workdir"

		os.RemoveAll(workDir)

		err = PullApplicationPackage(
			packageRef,
			workDir,
		)

		if err != nil {

			fmt.Println(
				"Application Package could not be pulled:",
				err,
			)

			os.Exit(1)
		}

	//--------------------------------------------------
	// Folder
	//--------------------------------------------------

	} else if info.IsDir() {

		workDir = inputPath

	} else if filepath.Ext(inputPath) == ".zip" {

		workDir = "./workdir"

		os.RemoveAll(workDir)

		err = ExtractZip(
			inputPath,
			workDir,
		)

		if err != nil {
			panic(err)
		}

	} else if filepath.Ext(inputPath) == ".gz" {

		workDir = "./workdir"

		os.RemoveAll(workDir)

		err = ExtractTarGz(
			inputPath,
			workDir,
		)

		if err != nil {
			panic(err)
		}

	} else {

		fmt.Println(
			"Unsupported input type",
		)

		os.Exit(1)
	}

	fmt.Println(
		"Extracted Path:",
		workDir,
	)

	//--------------------------------------------------
	// Find Application Description
	//--------------------------------------------------

	// A pulled package names its Application Description in the manifest
	// (the layer of mediaType application/vnd.margo.app.description.v1+yaml).
	appYaml := ""

	if packageRef != "" {

		appYaml =
			ApplicationDescriptionPath(
				packageRef,
				workDir,
			)
	}

	if appYaml == "" {

		appYaml, err =
			FindApplicationDescription(
				workDir,
			)
	}

	if err != nil {
		panic(err)
	}

	fmt.Println(
		"Application Description:",
		appYaml,
	)

	//--------------------------------------------------
	// Read YAML
	//--------------------------------------------------

	data, err :=
		os.ReadFile(appYaml)

	if err != nil {
		panic(err)
	}

	//--------------------------------------------------
	// Handle Template Variables
	//--------------------------------------------------
	//
	// Example:
	//
	// repository: {{HELM_REPOSITORY}}
	// revision: {{CHART_VERSION}}
	//
	//--------------------------------------------------

	content := string(data)

	placeholderRegex :=
		regexp.MustCompile(`\{\{[^}]+\}\}`)

	content =
		placeholderRegex.ReplaceAllStringFunc(
			content,
			func(s string) string {

				return "\"" + s + "\""
			},
		)

	data = []byte(content)

	//--------------------------------------------------
	// Parse YAML into ApplicationDescription
	//--------------------------------------------------

	var app ApplicationDescription
	err = yaml.Unmarshal(
		data,
		&app,
	)

	if err != nil {

		fmt.Println(
			"\nYAML PARSE FAILED",
		)

		fmt.Println(
			"File:",
			appYaml,
		)

		fmt.Println(
			"Error:",
			err,
		)

		os.Exit(1)
	}

	//--------------------------------------------------
	// Parse YAML into generic map
	//
	// Generic Application Description validation uses
	// this raw map instead of relying on Go model paths.
	//--------------------------------------------------

	raw := make(map[string]interface{})

	err = yaml.Unmarshal(
		data,
		&raw,
	)

	if err != nil {

		fmt.Println(
			"\nYAML PARSE FAILED",
		)

		fmt.Println(
			"File:",
			appYaml,
		)

		fmt.Println(
			"Error:",
			err,
		)

		os.Exit(1)
	}

	//--------------------------------------------------
	// Report Configuration
	//--------------------------------------------------

	appName := app.Metadata.Name

	if appName == "" {

		appName =
			filepath.Base(workDir)
	}

reportDir := filepath.Join(
    "..",
    "..",
    "reports",
    "application-supplier",
)

	_ = os.MkdirAll(
		reportDir,
		os.ModePerm,
	)

	reportFile :=
		filepath.Join(
			reportDir,
			fmt.Sprintf(
				"%s_%s.html",
				appName,
				time.Now().
					Format(
						"02-01-2006_15-04-05",
					),
			),
		)

	//--------------------------------------------------
	// Application Validation
	//--------------------------------------------------

	report :=
		NewValidationReport()

	report.ApplicationName =
		app.Metadata.Name

	report.ApplicationVersion =
		app.Metadata.Version

	if report.ApplicationName == "" {

		report.ApplicationName =
			app.ID
	}

	err = ValidateAppDescription(
		&app,
		raw,
		report,
	)

	if err != nil {

		report.Status =
			"FAILED"

		_ = report.GenerateHTMLReport(
			reportFile,
		)

		fmt.Println(
			"\nVALIDATION FAILED:",
		)

		fmt.Println(err)

		os.Exit(1)
	}

	fmt.Println(
		"\nApplication Description Validation PASSED",
	)

	//--------------------------------------------------
	// Deployment Profile Validation
	//--------------------------------------------------

for _, profile := range app.DeploymentProfile {

    fmt.Println(
        "\nDeployment Type:",
        profile.Type,
    )

    switch profile.Type {
		case "helm", "compose":

    for _, component :=
        range profile.Components {

        report.Check(
            "",
            fmt.Sprintf(
                "Validating %s component '%s'",
                profile.Type,
                component.Name,
            ),
            "network",
            "OCI repository reachable",
        )

        err :=
            ValidateOCIComponent(
                component,
            )

        if err != nil {

            report.Fail(
                "unreachable",
                fmt.Sprintf(
                    "OCI validation failed for component '%s' : %v",
                    component.Name,
                    err,
                ),
            )

            fmt.Println(
                "FAIL:",
                err,
            )

            continue
        }

        report.Pass(
            "reachable",
            fmt.Sprintf(
                "OCI repository reachable for component '%s'",
                component.Name,
            ),
        )

        fmt.Println(
            "PASS: OCI Repository Reachable",
        )
    }

    default:

        fmt.Printf(
            "Unsupported deployment type %s\n",
            profile.Type,
        )
    }
}

	//--------------------------------------------------
	// Package Signing
	//--------------------------------------------------

	ValidateSigning(
		&app,
		report,
	)

	//--------------------------------------------------
	// Application Registry
	//--------------------------------------------------

	if packageRef != "" {

		ValidateRegistry(
			packageRef,
			&app,
			raw,
			report,
		)

	} else {

		report.Check(
			"MARGO-APP-APPLICATIONREGISTRY-001, MARGO-APP-APPLICATIONREGISTRY-002, MARGO-APP-APPLICATIONREGISTRY-003, MARGO-APP-APPLICATIONREGISTRY-004",
			"Application Registry checks",
			"registry",
			"Application Package in the Application Registry",
		)

		report.Mark(
			"NOT RUN",
			"local package",
			"The input is a local folder or archive. Give the OCI reference of the Application Package (oci://<host>/<repository>:<metadata.version>) to run the Application Registry checks.",
		)
	}

	//--------------------------------------------------
	// Observability
	//--------------------------------------------------

	ValidateObservability(
		&app,
		report,
	)

	//--------------------------------------------------
	// Validation Completed
	//--------------------------------------------------

	fmt.Println(
		"\nConformance Validation Completed",
	)

	err =
		report.GenerateHTMLReport(
			reportFile,
		)

	reportPath, _ :=
		filepath.Abs(
			reportFile,
		)

	fmt.Println()

	fmt.Println(
		"=====================================",
	)

	fmt.Println(
		"Validation Report Generated",
	)

	fmt.Println(
		"Report Path:",
		reportPath,
	)

	fmt.Println(
		"Validation Result :",
		report.Status,
	)

	fmt.Println(
		"=====================================",
	)

	fmt.Println()

	if err != nil {

		fmt.Println(
			"failed to generate report:",
			err,
		)
	}
}