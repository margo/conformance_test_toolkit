package main
type ValidationMessage struct {
    CRID  string       `yaml:"crId"`
    Check CheckMessage `yaml:"check"`
    Pass  PassMessage  `yaml:"pass"`
    Fail  FailMessage  `yaml:"fail"`
}
type CheckMessage struct {
	Datatype string `yaml:"datatype"`
	Expected string `yaml:"expected"`
}

type PassMessage struct {
	Description string `yaml:"description"`
}

type FailMessage struct {
	Missing         string `yaml:"missing"`
	Invalid         string `yaml:"invalid"`
	InvalidRef      string `yaml:"invalid_reference"`
	MissingName     string `yaml:"missing_name"`
	MissingDatatype string `yaml:"missing_datatype"`
}