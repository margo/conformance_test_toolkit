package main

// ApplicationDescription represents the top-level structure of a Margo application description.
type ApplicationDescription struct {
    APIVersion        string                         `yaml:"apiVersion"`
    Kind              string                         `yaml:"kind"`
    ID                string                         `yaml:"id"`
    Metadata          Metadata                       `yaml:"metadata"`
    DeploymentProfile []DeploymentProfile            `yaml:"deploymentProfiles"`
    Parameters        map[string]Parameter           `yaml:"parameters"`
    Configuration     Configuration                  `yaml:"configuration"`
}

// Metadata contains the metadata information of a Margo application description.
type Metadata struct {
    Name        string  `yaml:"name"`
    Version     string  `yaml:"version"`
    Catalog     Catalog `yaml:"catalog"`
}

// Catalog represents the catalog information within the metadata of a Margo application description.
type Catalog struct {
    Organization []Organization `yaml:"organization"`
}

// Organization represents an organization within the catalog of a Margo application description.
type Organization struct {
    Name string `yaml:"name"`
}

// DeploymentProfile represents a deployment profile within a Margo application description.
type DeploymentProfile struct {
    Type       string      `yaml:"type"`
    ID         string      `yaml:"id"`
    Components []Component `yaml:"components"`
}

// Component represents a component within a deployment profile of a Margo application description.
type Component struct {
    Name       string                 `yaml:"name"`
    Properties map[string]interface{} `yaml:"properties"`
}

// Parameter represents a parameter within a Margo application description.
type Parameter struct {
    Value   interface{} `yaml:"value"`
    Targets []Target    `yaml:"targets"`
}

// Target represents the target information for a parameter within a Margo application description.
type Target struct {
    Pointer    string   `yaml:"pointer"`
    Components []string `yaml:"components"`
}

// Configuration represents the configuration section of a Margo application description.
type Configuration struct {
    Sections []Section       `yaml:"sections"`
    Schema   []SchemaDef     `yaml:"schema"`
}

// Section represents a section within the configuration of a Margo application description.
type Section struct {
    Name     string    `yaml:"name"`
    Settings []Setting `yaml:"settings"`
}

// Setting represents a setting within a section of the configuration in a Margo application description.
type Setting struct {
    Parameter   string `yaml:"parameter"`
    Name        string `yaml:"name"`
    Description string `yaml:"description"`
    Schema      string `yaml:"schema"`
}

// SchemaDef represents a schema definition within the configuration of a Margo application description.
type SchemaDef struct {
    Name       string `yaml:"name"`
    DataType   string `yaml:"dataType"`
    AllowEmpty bool   `yaml:"allowEmpty"`
}