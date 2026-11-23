package main

import (
    "encoding/json"
    "fmt"
    "os"
    "regexp"
    "strconv"
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
    Parent       string            `json:"parent,omitempty"`

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
ValidateMatchExpressions(
    raw,
    report,
    messages,
)
ValidateRevisionPlus(
    raw,
    report,
    messages,
)

ValidateExtensions(
    raw,
    report,
    messages,
)
    return nil
}
func typeClass(v any) string {

    switch v.(type) {

    case int, int64, float64:
        return "number"

    case string:
        return "string"

    case bool:
        return "boolean"

    default:
        return "unknown"
    }
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

    if rule.Parent != "" &&
    len(GetValues(raw, rule.Parent)) == 0 {

    return
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

if rule.MinItems > 0 {

    items, ok := value.([]any)

    if !ok || len(items) < rule.MinItems {

        failInvalid(
            report,
            actual,
            msg,
        )

        return
    }
}

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


func ValidateMatchExpressions(
    raw map[string]any,
    report *ValidationReport,
    messages map[string]ValidationMessage,
) {

    expressionLists := GetValues(
        raw,
        "matchExpressions",
    )

    for _, expressionList := range expressionLists {

        expressions, ok := expressionList.([]any)
        if !ok {
            continue
        }

        validateExpressionList(
            expressions,
            report,
            messages,
        )
    }
}
func validateInOperator(
    expr map[string]any,
    report *ValidationReport,
    messages map[string]ValidationMessage,
) {

    // APPDESC-020
    msg020 := messages["matchExpressions.in-notin.values"]

    check(
        report,
        msg020.CRID,
        "matchExpressions.in-notin.values",
        msg020.Check.Datatype,
        msg020.Check.Expected,
    )

    values, ok := expr["values"]

    if !ok {

        fail(
            report,
            "(missing)",
            msg020.Fail.Missing,
        )

        return
    }

    pass(
        report,
        "present",
        msg020.Pass.Description,
    )

    arr, ok := values.([]any)
    if !ok || len(arr) == 0 {
        return
    }

    // APPDESC-021
    msg021 := messages["matchExpressions.in-notin.validType"]

    check(
        report,
        msg021.CRID,
        "matchExpressions.in-notin.validType",
        msg021.Check.Datatype,
        msg021.Check.Expected,
    )

    boolCount := 0

    for _, v := range arr {

        switch v.(type) {

        case bool:
            boolCount++

        case string,
            int,
            int64,
            float64:

        default:

            fail(
                report,
                fmt.Sprintf("%v", v),
                msg021.Fail.Invalid,
            )

            return
        }
    }

    if boolCount > 0 && len(arr) != 1 {

        fail(
            report,
            fmt.Sprintf("%v", arr),
            msg021.Fail.Invalid,
        )

        return
    }

    pass(
        report,
        formatActual(arr),
        msg021.Pass.Description,
    )

// APPDESC-022
msg022 := messages["matchExpressions.in-notin.sameType"]

check(
    report,
    msg022.CRID,
    "matchExpressions.in-notin.sameType",
    msg022.Check.Datatype,
    msg022.Check.Expected,
)

firstType := typeClass(arr[0])

for _, v := range arr[1:] {

    if typeClass(v) != firstType {

        fail(
            report,
            fmt.Sprintf("%v", arr),
            msg022.Fail.Invalid,
        )

        return
    }
}

pass(
    report,
    firstType,
    msg022.Pass.Description,
)
}
func validateExistsOperator(
    expr map[string]any,
    report *ValidationReport,
    messages map[string]ValidationMessage,
) {

    msg := messages["matchExpressions.exists.values"]

    check(
        report,
        msg.CRID,
        "matchExpressions.exists.values",
        msg.Check.Datatype,
        msg.Check.Expected,
    )

    if _, ok := expr["values"]; ok {

        fail(
            report,
            "values present",
            msg.Fail.Invalid,
        )

        return
    }

    pass(
        report,
        "(omitted)",
        msg.Pass.Description,
    )
}


func validateGtLtOperator(
    expr map[string]any,
    report *ValidationReport,
    messages map[string]ValidationMessage,
) {

    msg024 := messages["matchExpressions.gtlt.values"]

    check(
        report,
        msg024.CRID,
        "matchExpressions.gtlt.values",
        msg024.Check.Datatype,
        msg024.Check.Expected,
    )

    values, ok := expr["values"]

    if !ok {

        fail(
            report,
            "(missing)",
            msg024.Fail.Missing,
        )

        return
    }

    pass(
        report,
        "present",
        msg024.Pass.Description,
    )

    msg026 := messages["matchExpressions.gtlt.singleValue"]

    check(
        report,
        msg026.CRID,
        "matchExpressions.gtlt.singleValue",
        msg026.Check.Datatype,
        msg026.Check.Expected,
    )

    arr, ok := values.([]any)

    if !ok || len(arr) != 1 {

        fail(
            report,
            fmt.Sprintf("%v", values),
            msg026.Fail.Invalid,
        )

        return
    }

    pass(
        report,
        fmt.Sprintf("%v", arr[0]),
        msg026.Pass.Description,
    )
    // APPDESC-025
    msg025 := messages["matchExpressions.gtlt.numeric"]

    check(
        report,
        msg025.CRID,
        "matchExpressions.gtlt.numeric",
        msg025.Check.Datatype,
        msg025.Check.Expected,
    )

    switch v := arr[0].(type) {

    case int,
        int64,
        float64:

        pass(
            report,
            fmt.Sprintf("%v", v),
            msg025.Pass.Description,
        )

    case string:

        if _, err := strconv.ParseFloat(v, 64); err != nil {

            fail(
                report,
                v,
                msg025.Fail.Invalid,
            )

            return
        }

        pass(
            report,
            v,
            msg025.Pass.Description,
        )

    default:

        fail(
            report,
            fmt.Sprintf("%v", arr[0]),
            msg025.Fail.Invalid,
        )
    }
}
func validateContainsOperator(
    expr map[string]any,
    report *ValidationReport,
    messages map[string]ValidationMessage,
) {

    msg027 := messages["matchExpressions.contains.itemSelector"]

    check(
        report,
        msg027.CRID,
        "matchExpressions.contains.itemSelector",
        msg027.Check.Datatype,
        msg027.Check.Expected,
    )

    if _, ok := expr["itemSelector"]; !ok {

        fail(
            report,
            "(missing)",
            msg027.Fail.Missing,
        )

    } else {

        pass(
            report,
            "present",
            msg027.Pass.Description,
        )
    }
        // APPDESC-028
    msg028 := messages["matchExpressions.contains.matchExpressions"]

    check(
        report,
        msg028.CRID,
        "matchExpressions.contains.matchExpressions",
        msg028.Check.Datatype,
        msg028.Check.Expected,
    )

    itemSelector, ok := expr["itemSelector"].(map[string]any)

    if !ok {

        fail(
            report,
            "(missing)",
            msg028.Fail.Missing,
        )

    } else {

        matchExpr, ok :=
            itemSelector["matchExpressions"].([]any)

        if !ok || len(matchExpr) == 0 {

            fail(
                report,
                "(missing)",
                msg028.Fail.Missing,
            )

        } else {

            pass(
                report,
                fmt.Sprintf("%d item(s)", len(matchExpr)),
                msg028.Pass.Description,
            )
            validateExpressionList(
    matchExpr,
    report,
    messages,
)
        }
    }

    msg029 := messages["matchExpressions.contains.values"]

    check(
        report,
        msg029.CRID,
        "matchExpressions.contains.values",
        msg029.Check.Datatype,
        msg029.Check.Expected,
    )

    if _, ok := expr["values"]; ok {

        fail(
            report,
            "values present",
            msg029.Fail.Invalid,
        )

        return
    }

    pass(
        report,
        "(omitted)",
        msg029.Pass.Description,
    )
}



func runCustomCheck(
    report *ValidationReport,
    msg ValidationMessage,
    actual string,
    success bool,
) {

    check(
        report,
        msg.CRID,
        "",
        msg.Check.Datatype,
        msg.Check.Expected,
    )

    if success {

        pass(
            report,
            actual,
            msg.Pass.Description,
        )

        return
    }

    fail(
        report,
        actual,
        msg.Fail.Invalid,
    )
}
func ValidateRevisionPlus(
    raw map[string]any,
    report *ValidationReport,
    messages map[string]ValidationMessage,
) {
    msg034 := messages["deploymentProfiles.components.properties.revisionPlus"]

    revisions := GetValues(
        raw,
        "deploymentProfiles.components.properties.revision",
    )

    for _, r := range revisions {

        rev := fmt.Sprintf("%v", r)

        check(
            report,
            msg034.CRID,
            "revision (+)",
            msg034.Check.Datatype,
            msg034.Check.Expected,
        )

        if strings.Contains(rev, "+") {

            fail(
                report,
                rev,
                msg034.Fail.Invalid,
            )

        } else {

            pass(
                report,
                rev,
                msg034.Pass.Description,
            )
        }
    }
}

func validateExpressionList(
    expressions []any,
    report *ValidationReport,
    messages map[string]ValidationMessage,
) {

    for _, expression := range expressions {

        m, ok := expression.(map[string]any)
        if !ok {
            continue
        }

        operator, _ := m["operator"].(string)

        switch operator {

        case "In", "NotIn":
            validateInOperator(
                m,
                report,
                messages,
            )

        case "Exists", "DoesNotExist":
            validateExistsOperator(
                m,
                report,
                messages,
            )

        case "Gt", "Lt":
            validateGtLtOperator(
                m,
                report,
                messages,
            )

        case "ContainsAll", "ContainsAny":
            validateContainsOperator(
                m,
                report,
                messages,
            )
        }
    }
}

func ValidateExtensions(
    raw map[string]any,
    report *ValidationReport,
    messages map[string]ValidationMessage,
) {

    extensionRegex := regexp.MustCompile(
        `^x-[a-z][a-z0-9-]*$`,
    )

    var walkExtensions func(any)

    walkExtensions = func(node any) {

        m, ok := node.(map[string]any)
        if !ok {
            return
        }

        for key, value := range m {

            if strings.HasPrefix(
                key,
                "x-",
            ) {
                msgName := messages["extensions.name"]

check(
    report,
    msgName.CRID,
    key,
    msgName.Check.Datatype,
    msgName.Check.Expected,
)

                if !extensionRegex.MatchString(
                    key,
                ) {

                   fail(
    report,
    key,
    msgName.Fail.Invalid,
)

                } else {
                    pass(
    report,
    key,
    msgName.Pass.Description,
)

                   
                }
                msgValue := messages["extensions.value"]

                    check(
                        report,
                        msgValue.CRID,
                        key,
                        msgValue.Check.Datatype,
                        msgValue.Check.Expected,
                    )

                if _, ok := value.(map[string]any); !ok {
                    fail(
    report,
    fmt.Sprintf("%T", value),
    msgValue.Fail.Invalid,
)

                } else {

                    pass(
    report,
    "object",
    msgValue.Pass.Description,
)
                }
            }

            walkExtensions(
                value,
            )
        }
    }

    walkExtensions(
        raw,
    )
}