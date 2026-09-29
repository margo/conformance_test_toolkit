package main	
// SpecAttribute represents the validation rules and constraints for a specific attribute in a Margo application description.
type SpecAttribute struct {
	Type         string            `json:"type"`
	Required     bool              `json:"required"`
	Regex        string            `json:"regex,omitempty"`
	Enum         []string          `json:"enum,omitempty"`
	Reference    string            `json:"reference,omitempty"`
	RequiredWhen map[string]string `json:"requiredWhen,omitempty"`
	MinItems     int               `json:"minItems,omitempty"`
}

// ValidationSpec represents the validation specification for a Margo application description.
type ValidationSpec map[string]SpecAttribute