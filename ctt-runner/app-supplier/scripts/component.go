package main

import (
    "fmt"
    "os/exec"
    "strings"
)

// schemeOCI defines the supported OCI repository scheme.
const (
    schemeOCI   = "oci://"
)

// ValidateOCIComponent validates the component repository and verifies OCI repository reachability.
func ValidateOCIComponent(
    c Component,
) error {

    repo, ok := c.Properties["repository"]

    // A missing or non-string revision is reported by the
    // deploymentProfiles.components.properties.revision rule, not here.
    rev, _ := c.Properties["revision"].(string)

    if !ok {
        return fmt.Errorf(
            "repository missing",
        )
    }

    repository, ok := repo.(string)

    if !ok || repository == "" {
        return fmt.Errorf(
            "repository must be a non-empty string",
        )
    }

    if !strings.HasPrefix(
        repository,
        schemeOCI,
    ) {
        return fmt.Errorf(
            "unsupported repository: %s",
            repository,
        )
    }

    location := repository

    if rev != "" {
        location += ":" + rev
    }

    return CheckOCI(
        location,
    )
}
// CheckOCI verifies that the OCI repository is reachable using ORAS.
func CheckOCI(location string) error {
    location = strings.TrimPrefix(location, "oci://")
    cmd := exec.Command(
        "oras",
        "manifest",
        "fetch",
        location,
    )

    out, err := cmd.CombinedOutput()

    if err != nil {

        return fmt.Errorf(
            "oci unreachable: %s",
            string(out),
        )
    }

    return nil
}