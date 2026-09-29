package main

import (
    "encoding/json"
    "fmt"
    "os"
    "regexp"
    "strings"
    "gopkg.in/yaml.v3"
)

// Rule defines the configurable constraints and report metadata for validating an application description field.
type Rule struct {
    Type         string            `json:"type"`
    Required     bool              `json:"required"`
    Enum         []string          `json:"enum,omitempty"`
    Regex        string            `json:"regex,omitempty"`
    Reference    string            `json:"reference,omitempty"`
    RequiredWhen map[string]string `json:"requiredWhen,omitempty"`
    MinItems     int               `json:"minItems,omitempty"`

    DisplayName  string            `json:"displayName,omitempty"`
    Expected     string            `json:"expected,omitempty"`
}

// ValidateAppDescription validates the application description against configured rules and messages
func ValidateAppDescription(
    app *ApplicationDescription,
    raw map[string]any,
    report *ValidationReport,
) error {

    rules := LoadRules()

    messages, err := LoadMessages(
        "validation-messages.yaml",
    )

    if err != nil {
        return err
    }

    references := BuildReferences(
        raw,
    )

    for path, rule := range rules {

        values := GetValues(
            raw,
            path,
        )

        validateRule(
            report,
            path,
            values,
            rule,
            references,
            messages,
            raw,
        )
    }

    return nil
}

// LoadMessages loads validation messages and CR-ID mappings from the YAML configuration file.
func LoadMessages(
    file string,
) (
    map[string]ValidationMessage,
    error,
) {
    messages := make(map[string]ValidationMessage)

    data, err := os.ReadFile(file)

    if err != nil {
        return nil, err
    }

    err = yaml.Unmarshal(
        data,
        &messages,
    )

    if err != nil {
        return nil, err
    }

    return messages, nil
}

func GetValues(
    raw map[string]any,
    path string,
) []any {

    return walk(
        raw,
        strings.Split(
            path,
            ".",
        ),
    )
}


// LoadRules loads application description validation rules from the JSON specification file.
func LoadRules() map[string]Rule {
    rules := make(map[string]Rule)
    

	data, err := os.ReadFile(
		"application-description-spec.json",
	)

	if err != nil {
		panic(err)
	}

	err = json.Unmarshal(
		data,
		&rules,
	)

	if err != nil {
		panic(err)
	}

	return rules
}

// formatActual converts a validation value into a readable format for the validation report.
func formatActual(
    value any,
) string {

    switch v := value.(type) {

    case map[string]any:

        return fmt.Sprintf(
            "%d properties",
            len(v),
        )

    case []any:

        return fmt.Sprintf(
            "%d item(s)",
            len(v),
        )

    default:

        return fmt.Sprintf(
            "%v",
            v,
        )
    }
}

// BuildReferences builds lookup sets used to validate references between application description fields.
func BuildReferences(
    raw map[string]any,
) map[string]map[string]bool {

    refs := map[string]map[string]bool{
        "parameters": {},
        "configuration.schema": {},
        "deploymentProfiles.components.name": {},
    }

    // parameters
    if parameters, ok :=
        raw["parameters"].(map[string]any); ok {

        for name := range parameters {

            refs["parameters"][name] = true
        }
    }

    // configuration.schema.name
    schemaNames := GetValues(
        raw,
        "configuration.schema.name",
    )

    for _, value := range schemaNames {

        refs["configuration.schema"][
            fmt.Sprintf(
                "%v",
                value,
            ),
        ] = true
    }

    // deploymentProfiles.components.name
    componentNames := GetValues(
        raw,
        "deploymentProfiles.components.name",
    )

    for _, value := range componentNames {

        refs["deploymentProfiles.components.name"][
            fmt.Sprintf(
                "%v",
                value,
            ),
        ] = true
    }

    return refs
}

// walk recursively traverses maps and slices to resolve values for a dot-separated field path.
func walk(
    current any,
    parts []string,
) []any {

    if len(parts) == 0 {

        return []any{
            current,
        }
    }

    switch value := current.(type) {

    case map[string]any:

        if next, ok := value[parts[0]]; ok {

            return walk(
                next,
                parts[1:],
            )
        }

        results := make([]any, 0)

        for _, item := range value {

            results = append(
                results,
                walk(
                    item,
                    parts,
                )...,
            )
        }

        return results

    case []any:

        results := make([]any, 0)

        for _, item := range value {

            results = append(
                results,
                walk(
                    item,
                    parts,
                )...,
            )
        }

        return results
    }

    return nil
}

// validateValue validates a single field value against enum, regex, and reference constraints.
func validateRule(
    report *ValidationReport,
    field string,
    values []any,
    rule Rule,
    refs map[string]map[string]bool,
    messages map[string]ValidationMessage,
    raw map[string]any,
) {

    msg, ok := messages[field]

    if !ok {

        msg = ValidationMessage{
            Check: CheckMessage{
                Datatype: rule.Type,
                Expected: buildExpected(rule),
            },
        }
    }

    if msg.Check.Datatype == "" {

        msg.Check.Datatype =
            rule.Type
    }

    if msg.Check.Expected == "" {

        msg.Check.Expected =
            buildExpected(rule)
    }

    expected := msg.Check.Expected

    if rule.Expected != "" {

        expected =
            rule.Expected
    }

    if rule.Required &&
        len(values) == 0 {

        displayField := field

        if rule.DisplayName != "" {

            displayField =
                rule.DisplayName
        }
        check(
    report,
    msg.CRID,
    displayField,
    msg.Check.Datatype,
    expected,
)

        fail(
            report,
            "(missing)",
            msg.Fail.Missing,
        )

        return
    }

    for _, value := range values {

        displayField := field

        if rule.DisplayName != "" {

            displayField =
                rule.DisplayName
        }

        // Make repeated rows unique
        if len(values) > 1 {

        displayField = fmt.Sprintf(
        "%s (%v)",
        displayField,
        value,
    )
}

check(
    report,
    msg.CRID,
    displayField,
    msg.Check.Datatype,
    expected,
)

        validateValue(
            report,
            field,
            value,
            rule,
            refs,
            messages,
        )
    }
}

// validateValue validates a single field value against enum, regex, and reference constraints.
func validateValue(
	report *ValidationReport,
	field string,
	value any,
	rule Rule,
	refs map[string]map[string]bool,
	messages map[string]ValidationMessage,
) {

	msg := messages[field]

	actual := formatActual(value)

	if len(rule.Enum) > 0 {

		valid := false

		for _, e := range rule.Enum {

			if e == actual {
				valid = true
				break
			}
		}

		if !valid {
			failInvalid(report, actual, msg)
			return
		}
	}

	if rule.Regex != "" {

		re := regexp.MustCompile(rule.Regex)

		if !re.MatchString(actual) {
			failInvalid(report, actual, msg)
			return
		}
	}

	if rule.Reference != "" {

		refMap := refs[rule.Reference]

		if items, ok := value.([]any); ok {

			for _, item := range items {

				if badActual, ok := firstUnreferencedItem(item, refMap); ok {
					failInvalid(report, badActual, msg)
					return
				}
			}
		} else if !refMap[actual] {
			failInvalid(report, actual, msg)
			return
		}
	}

	pass(
		report,
		actual,
		msg.Pass.Description,
	)
}

func firstUnreferencedItem(item any, refMap map[string]bool) (string, bool) {

	nested, isNested := item.([]any)
	if !isNested {

		actual := formatActual(item)
		if !refMap[actual] {
			return actual, true
		}
		return "", false
	}

	for _, nestedItem := range nested {

		actual := formatActual(nestedItem)
		if !refMap[actual] {
			return actual, true
		}
	}

	return "", false
}

// buildExpected returns the expected validation value or derives it from the configured rule.
func buildExpected(
    rule Rule,
) string {

    if rule.Expected != "" {

        return rule.Expected
    }

    if len(rule.Enum) > 0 {

        return strings.Join(
            rule.Enum,
            ", ",
        )
    }

    if rule.Regex != "" {

        return rule.Regex
    }

    if rule.Reference != "" {

        return "reference -> " +
            rule.Reference
    }

    if rule.Required {

        return "Required (non-empty)"
    }

    return ""
}


// check adds a validation check with its CR-ID, field, type, and expected value to the report.
func check(
    report *ValidationReport,
    crId string,
    field string,
    dataType string,
    expected string,
) {

    fmt.Println(
        "Validate",
        field,
        "CRID:",
        crId,
    )

    report.Check(
        crId,
        field,
        dataType,
        expected,
    )
}
// pass marks the current validation check as passed and records the actual value and details.
func pass(
    report *ValidationReport,
    actual string,
    details string,
) {

    fmt.Println("PASS", details)

    report.Pass(
        actual,
        details,
    )
}
// fail marks the current validation check as failed and records the actual value and failure details.
func fail(
    report *ValidationReport,
    actual string,
    details string,
) {

    fmt.Println("FAIL", details)

    report.Fail(
        actual,
        details,
    )
}

// failInvalid reports a validation failure when a value does not satisfy the configured rule.
func failInvalid(
    report *ValidationReport,
    actual string,
    msg ValidationMessage,
) {

    fail(
        report,
        actual,
        msg.Fail.Invalid,
    )
}
