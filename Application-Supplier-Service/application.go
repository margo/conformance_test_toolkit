package main

import (
    "encoding/json"
    "fmt"
    "os"
    "regexp"
    "strings"
    "gopkg.in/yaml.v3"
)

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

func ValidateAppDescription(
    app *ApplicationDescription,
    raw map[string]interface{},
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

func LoadMessages(
    file string,
) (
    map[string]ValidationMessage,
    error,
) {

    var messages map[string]ValidationMessage

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
    raw map[string]interface{},
    path string,
) []interface{} {

    return walk(
        raw,
        strings.Split(
            path,
            ".",
        ),
    )
}



func LoadRules() map[string]Rule {

	var rules map[string]Rule

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


func formatActual(
    value interface{},
) string {

    switch v := value.(type) {

    case map[string]interface{}:

        return fmt.Sprintf(
            "%d propertie(s)",
            len(v),
        )

    case []interface{}:

        if len(v) == 1 {

            return fmt.Sprintf(
                "%v",
                v[0],
            )
        }

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

func BuildReferences(
    raw map[string]interface{},
) map[string]map[string]bool {

    refs := map[string]map[string]bool{
        "parameters": {},
        "configuration.schema": {},
        "deploymentProfiles.components.name": {},
    }

    // parameters
    if parameters, ok :=
        raw["parameters"].(map[string]interface{}); ok {

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


func walk(
    current interface{},
    parts []string,
) []interface{} {

    if len(parts) == 0 {

        return []interface{}{
            current,
        }
    }

    switch value := current.(type) {

    case map[string]interface{}:

        if next, ok := value[parts[0]]; ok {

            return walk(
                next,
                parts[1:],
            )
        }

        var results []interface{}

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

    case []interface{}:

        var results []interface{}

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

func validateRule(
    report *ValidationReport,
    field string,
    values []interface{},
    rule Rule,
    refs map[string]map[string]bool,
    messages map[string]ValidationMessage,
    raw map[string]interface{},
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

    if len(rule.RequiredWhen) > 0 {

        shouldValidate := false

        for conditionPath, expected :=
            range rule.RequiredWhen {

            conditionValues := GetValues(
                raw,
                conditionPath,
            )

            for _, value :=
                range conditionValues {

                actual := fmt.Sprintf(
                    "%v",
                    value,
                )

                if actual == expected {

                    shouldValidate = true
                    break
                }
            }

            if shouldValidate {
                break
            }
        }

        if !shouldValidate {
            return
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


func validateValue(
	report *ValidationReport,
	field string,
	value interface{},
	rule Rule,
	refs map[string]map[string]bool,
	messages map[string]ValidationMessage,
) {

	msg := messages[field]

    actual := formatActual(
    value,
)
	if len(rule.Enum) > 0 {

		valid := false

		for _, e := range rule.Enum {

			if e == actual {
				valid = true
				break
			}
		}

		if !valid {

			fail(
				report,
				actual,
				msg.Fail.Invalid,
			)

			return
		}
	}

	if rule.Regex != "" {

		re := regexp.MustCompile(
			rule.Regex,
		)

		if !re.MatchString(actual) {

			fail(
				report,
				actual,
				msg.Fail.Invalid,
			)

			return
		}
	}

    if rule.Reference != "" {

    refMap := refs[rule.Reference]

    switch v := value.(type) {

    case []interface{}:

        for _, item := range v {

            switch nested := item.(type) {

            case []interface{}:

                for _, nestedItem := range nested {

                    actual := formatActual(
                        nestedItem,
                    )

                    if !refMap[actual] {

                        fail(
                            report,
                            actual,
                            msg.Fail.Invalid,
                        )

                        return
                    }
                }

            default:

                actual := formatActual(
                    item,
                )

                if !refMap[actual] {

                    fail(
                        report,
                        actual,
                        msg.Fail.Invalid,
                    )

                    return
                }
            }
        }

    default:

        actual := formatActual(
            value,
        )

        if !refMap[actual] {

            fail(
                report,
                actual,
                msg.Fail.Invalid,
            )

            return
        }
    }
}

	pass(
		report,
		actual,
		msg.Pass.Description,
	)
}


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
