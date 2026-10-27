package main


import (
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "gopkg.in/yaml.v3"
)

// applicationDescriptionFile is the file name of the Application Description
// in an Application Package (see the manifest example of the Application
// Registry specification).
const applicationDescriptionFile = "margo.yaml"

// FindApplicationDescription searches the given directory for the Application
// Description: the file margo.yaml or, when there is none, a YAML file with the
// top-level keys apiVersion, metadata and deploymentProfiles. The Application
// Description has no kind field, so the file cannot be found by its kind.
func FindApplicationDescription(root string) (string, error) {

    var found string
    var named string

    err := filepath.Walk(
        root,
        func(path string, info os.FileInfo, err error) error {

            if err != nil || info == nil {
                return nil
            }

            if info.IsDir() {
                return nil
            }

            ext := strings.ToLower(
                filepath.Ext(path),
            )

            if ext != ".yaml" &&
                ext != ".yml" {

                return nil
            }

            data, err := os.ReadFile(path)

            if err != nil {
                return nil
            }

            var obj map[string]any

            if err := yaml.Unmarshal(
                data,
                &obj,
            ); err != nil {

                return nil
            }

            _, hasAPIVersion := obj["apiVersion"]
            _, hasMetadata := obj["metadata"]
            _, hasProfiles := obj["deploymentProfiles"]

            if !hasAPIVersion || !hasMetadata || !hasProfiles {
                return nil
            }

            if info.Name() == applicationDescriptionFile && named == "" {
                named = path
            }

            found = path

            return nil
        },
    )

    if err != nil {
        return "", err
    }

    if named != "" {
        found = named
    }

    if found == "" {

        return "",
            fmt.Errorf(
                "ApplicationDescription not found",
            )
    }

    return found, nil
}