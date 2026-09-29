package main

// ValidationMessage defines the CR-ID and messages used for a validation rule.
type ValidationMessage struct {
    CRID  string       `yaml:"crId"`
    Check CheckMessage `yaml:"check"`
    Pass  PassMessage  `yaml:"pass"`
    Fail  FailMessage  `yaml:"fail"`
}
// CheckMessage defines the data type and expected value displayed for a validation check.
type CheckMessage struct {
	Datatype string `yaml:"datatype"`
	Expected string `yaml:"expected"`
}

// PassMessage defines the message displayed when a validation check passes.
type PassMessage struct {
	Description string `yaml:"description"`
}

// FailMessage defines the messages displayed for various validation failures.
type FailMessage struct {
	Missing         string `yaml:"missing"`
	Invalid         string `yaml:"invalid"`
	InvalidRef      string `yaml:"invalid_reference"`
	MissingName     string `yaml:"missing_name"`
	MissingDatatype string `yaml:"missing_datatype"`
}