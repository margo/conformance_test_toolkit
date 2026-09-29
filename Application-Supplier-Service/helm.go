package main

import (
    "fmt"
    "strings"
)

const ociPrefix = "oci://"

func ValidateHelmComponent(
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
        ociPrefix,
    ) {
        return fmt.Errorf(
            "unsupported repository: %s",
            repository,
        )
    }

    return CheckOCI(
        repository,
    )
}