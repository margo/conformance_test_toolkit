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

    // The revision itself is judged by the
    // deploymentProfiles.components.properties.revision rule; without a string
    // revision the component cannot be looked up, so this check fails and the
    // validation continues with the next component.
    rawRev, ok := c.Properties["revision"]

    if !ok || rawRev == nil {
        return fmt.Errorf(
            "revision missing: the repository cannot be looked up without the OCI tag",
        )
    }

    rev, ok := rawRev.(string)

    if !ok {
        return fmt.Errorf(
            "revision must be a string, got %v (%T); quote it in the YAML, e.g. revision: \"1.0.0\"",
            rawRev,
            rawRev,
        )
    }

    if rev == "" {
        return fmt.Errorf(
            "revision is empty: the repository cannot be looked up without the OCI tag",
        )
    }

    return CheckOCI(
        repository + ":" + rev,
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