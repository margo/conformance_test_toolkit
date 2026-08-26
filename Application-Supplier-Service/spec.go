package main	

type SpecAttribute struct {
	Type         string            `json:"type"`
	Required     bool              `json:"required"`
	Regex        string            `json:"regex,omitempty"`
	Enum         []string          `json:"enum,omitempty"`
	Reference    string            `json:"reference,omitempty"`
	RequiredWhen map[string]string `json:"requiredWhen,omitempty"`
	MinItems     int               `json:"minItems,omitempty"`
}

type ValidationSpec map[string]SpecAttribute