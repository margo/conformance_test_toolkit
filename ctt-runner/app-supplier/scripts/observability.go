package main

import (
    "bytes"
    "errors"
    "fmt"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "sort"
    "strings"

    "gopkg.in/yaml.v3"
)

// Observability checks of the Application Supplier
// (https://docs.margo.org/specification/observability/publishing-workload-observability-data).
// They judge what the rendered workload declares (Helm chart with the
// Application Description's parameters applied, or the Compose file); what the
// workload sends at run time is not observed.

// otelExporterVars are the OpenTelemetry SDK variables that choose an exporter.
var otelExporterVars = []string{
    "OTEL_TRACES_EXPORTER",
    "OTEL_METRICS_EXPORTER",
    "OTEL_LOGS_EXPORTER",
}

// otelProtocolVars are the OpenTelemetry SDK variables that choose the OTLP protocol.
var otelProtocolVars = []string{
    "OTEL_EXPORTER_OTLP_PROTOCOL",
    "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
    "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL",
    "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
}

// otlpExporters are exporter values that keep the data on OTLP (console only
// writes to the workload's own output, none disables the signal).
var otlpExporters = map[string]bool{
    "otlp":    true,
    "none":    true,
    "console": true,
}

// otlpProtocols are the protocols of OTLP.
var otlpProtocols = map[string]bool{
    "grpc":          true,
    "http/protobuf": true,
    "http/json":     true,
}

// autoInstrumentationPrefix is the annotation prefix the OpenTelemetry Operator
// uses to inject auto-instrumentation into a pod.
const autoInstrumentationPrefix = "instrumentation.opentelemetry.io/inject-"

// workloadFacts is what a rendered component declares.
type workloadFacts struct {
    Env         map[string]string
    Annotations map[string]bool
    Kinds       map[string]bool
}

// ValidateObservability renders each component of the Application Description
// and reports MARGO-APP-OBSERVABILITY-004 and -008 per component, plus one row
// for the optional (-001, -002, -007) and the not yet covered (-003, -005, -006)
// observability requirements.
func ValidateObservability(
    app *ApplicationDescription,
    report *ValidationReport,
) {

    var exposing []string

    for _, profile := range app.DeploymentProfile {

        for _, component := range profile.Components {

            facts, err := renderComponent(
                profile.Type,
                component,
                app.Parameters,
            )

            otelVars := otelVariables(facts)

            if len(otelVars) > 0 {
                exposing = append(exposing, component.Name)
            }

            validateOTLP(report, component.Name, facts, otelVars, err)

            validateAutoInstrumentation(report, profile.Type, component.Name, facts, err)
        }
    }

    report.Check(
        "MARGO-APP-OBSERVABILITY-001, MARGO-APP-OBSERVABILITY-002, MARGO-APP-OBSERVABILITY-007",
        "Workload exposes OpenTelemetry data",
        "observability",
        "MAY / OPTIONAL (highly recommended): workload-specific data is sent to the OpenTelemetry collector",
    )

    if len(exposing) > 0 {

        report.Mark(
            "INFO",
            "yes: "+strings.Join(exposing, ", "),
            "The workload declares an OpenTelemetry configuration.",
        )

    } else {

        report.Mark(
            "INFO",
            "not declared",
            "No component declares an OpenTelemetry configuration. Exposing observability data is optional but highly recommended.",
        )
    }

    report.Check(
        "MARGO-APP-OBSERVABILITY-003, MARGO-APP-OBSERVABILITY-005, MARGO-APP-OBSERVABILITY-006",
        "Alternative observability framework, export without the collector",
        "observability",
        "Self-contained alternative framework; no export outside the device without the end user's approval",
    )

    report.Mark(
        "NOT RUN",
        "-",
        "Not covered yet: these require observing the network traffic of the running workload, or a declaration of the supplier.",
    )
}

// validateOTLP reports MARGO-APP-OBSERVABILITY-004 for one component: when the
// workload configures OpenTelemetry, its exporters and protocol are OTLP.
func validateOTLP(
    report *ValidationReport,
    name string,
    facts workloadFacts,
    otelVars []string,
    renderErr error,
) {

    report.Check(
        "MARGO-APP-OBSERVABILITY-004",
        fmt.Sprintf("OpenTelemetry export of component '%s'", name),
        "observability",
        "Metrics, traces and logs for OpenTelemetry are sent to the collector using OTLP",
    )

    if renderErr != nil {

        report.Mark("NOT RUN", "-", fmt.Sprintf("The component could not be rendered: %v", renderErr))

        return
    }

    if len(otelVars) == 0 {

        report.Mark("N/A", "no OTEL_* variables", "The component declares no OpenTelemetry configuration.")

        return
    }

    var problems []string

    for _, v := range otelExporterVars {

        value, ok := facts.Env[v]

        if !ok {
            continue
        }

        for _, exporter := range strings.Split(value, ",") {

            if !otlpExporters[strings.TrimSpace(exporter)] {
                problems = append(problems, v+"="+value)
                break
            }
        }
    }

    for _, v := range otelProtocolVars {

        if value, ok := facts.Env[v]; ok && !otlpProtocols[value] {
            problems = append(problems, v+"="+value)
        }
    }

    if len(problems) > 0 {

        report.Fail(
            strings.Join(problems, "; "),
            "The workload exports OpenTelemetry data with an exporter or protocol other than OTLP.",
        )

        return
    }

    report.Pass(
        strings.Join(otelVars, ", "),
        "The workload's OpenTelemetry configuration uses OTLP.",
    )
}

// validateAutoInstrumentation reports MARGO-APP-OBSERVABILITY-008 for one
// component: the workload does not rely on auto-instrumentation injected by the
// OpenTelemetry Operator.
func validateAutoInstrumentation(
    report *ValidationReport,
    profileType string,
    name string,
    facts workloadFacts,
    renderErr error,
) {

    report.Check(
        "MARGO-APP-OBSERVABILITY-008",
        fmt.Sprintf("Auto-instrumentation of component '%s'", name),
        "observability",
        "SHOULD NOT: no "+autoInstrumentationPrefix+"* annotation, no Instrumentation resource",
    )

    if profileType != "helm" {

        report.Mark("N/A", profileType, "Auto-instrumentation by the OpenTelemetry Operator applies to Kubernetes workloads only.")

        return
    }

    if renderErr != nil {

        report.Mark("NOT RUN", "-", fmt.Sprintf("The component could not be rendered: %v", renderErr))

        return
    }

    var markers []string

    for key := range facts.Annotations {

        if strings.HasPrefix(key, autoInstrumentationPrefix) {
            markers = append(markers, key)
        }
    }

    if facts.Kinds["Instrumentation"] {
        markers = append(markers, "kind: Instrumentation")
    }

    if len(markers) > 0 {

        sort.Strings(markers)

        report.Mark(
            "WARN",
            strings.Join(markers, ", "),
            "The workload expects to be auto-instrumented by the OpenTelemetry Operator. Workload suppliers SHOULD NOT expect auto-instrumentation by anything outside their control.",
        )

        return
    }

    report.Pass(
        "none",
        "The workload does not request auto-instrumentation.",
    )
}

// otelVariables returns the sorted names of the OpenTelemetry variables a
// component sets (OTEL_* and the device-style *_OTEL_EXPORTER_OTLP_ENDPOINT).
func otelVariables(
    facts workloadFacts,
) []string {

    var names []string

    for name := range facts.Env {

        if strings.HasPrefix(name, "OTEL_") ||
            strings.HasSuffix(name, "_OTEL_EXPORTER_OTLP_ENDPOINT") {

            names = append(names, name)
        }
    }

    sort.Strings(names)

    return names
}

// renderComponent renders a component and collects what it declares: a Helm
// chart with `helm template` and the parameters of the Application Description
// that target it, a Compose component from its Compose Archive.
func renderComponent(
    profileType string,
    c Component,
    parameters map[string]Parameter,
) (workloadFacts, error) {

    facts := workloadFacts{
        Env:         map[string]string{},
        Annotations: map[string]bool{},
        Kinds:       map[string]bool{},
    }

    repository, _ := c.Properties["repository"].(string)
    revision, _ := c.Properties["revision"].(string)

    if !strings.HasPrefix(repository, schemeOCI) || revision == "" {
        return facts, errors.New("the component has no oci:// repository and revision")
    }

    var docs []any
    var err error

    switch profileType {

    case "helm":
        docs, err = renderHelm(repository, revision, c.Name, parameters)

    case "compose":
        docs, err = renderCompose(repository, revision)

    default:
        return facts, fmt.Errorf("unsupported deployment type %s", profileType)
    }

    if err != nil {
        return facts, err
    }

    for _, doc := range docs {

        if profileType == "compose" {
            collectComposeFacts(doc, &facts)
        } else {
            collectFacts(doc, &facts)
        }
    }

    return facts, nil
}

// renderHelm runs `helm template` on the chart with the Application
// Description's parameter values for this component, and decodes the manifests.
func renderHelm(
    repository string,
    revision string,
    component string,
    parameters map[string]Parameter,
) ([]any, error) {

    args := []string{
        "template", "ctt", repository, "--version", revision,
    }

    for _, p := range parameters {

        for _, t := range p.Targets {

            for _, target := range t.Components {

                if target == component && t.Pointer != "" && p.Value != nil {
                    args = append(args, "--set", fmt.Sprintf("%s=%v", t.Pointer, p.Value))
                }
            }
        }
    }

    var stderr bytes.Buffer

    cmd := exec.Command("helm", args...)
    cmd.Stderr = &stderr

    out, err := cmd.Output()

    if err != nil {

        msg := strings.TrimSpace(stderr.String())

        if msg == "" {
            msg = err.Error() // e.g. helm is not installed
        }

        return nil, fmt.Errorf("helm template: %s", msg)
    }

    return decodeYAMLDocuments(out)
}

// renderCompose pulls the Compose Archive of a component and decodes its Compose file.
func renderCompose(
    repository string,
    revision string,
) ([]any, error) {

    dir, err := os.MkdirTemp("", "ctt-compose-")

    if err != nil {
        return nil, err
    }

    defer os.RemoveAll(dir)

    if err := PullApplicationPackage(repository+":"+revision, dir); err != nil {
        return nil, err
    }

    archives, _ := filepath.Glob(filepath.Join(dir, "*.tar.gz"))

    for _, archive := range archives {

        if err := ExtractTarGz(archive, dir); err != nil {
            return nil, err
        }
    }

    var docs []any

    err = filepath.Walk(
        dir,
        func(p string, info os.FileInfo, err error) error {

            if err != nil || info.IsDir() {
                return nil
            }

            switch strings.ToLower(info.Name()) {

            case "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml":

                data, err := os.ReadFile(p)

                if err != nil {
                    return err
                }

                decoded, err := decodeYAMLDocuments(data)

                if err != nil {
                    return err
                }

                docs = append(docs, decoded...)
            }

            return nil
        },
    )

    if err == nil && len(docs) == 0 {
        err = errors.New("no Compose file in the Compose Archive")
    }

    return docs, err
}

// decodeYAMLDocuments decodes every document of a multi-document YAML stream.
func decodeYAMLDocuments(
    data []byte,
) ([]any, error) {

    var docs []any

    decoder := yaml.NewDecoder(bytes.NewReader(data))

    for {

        var doc any

        err := decoder.Decode(&doc)

        if errors.Is(err, io.EOF) {
            return docs, nil
        }

        if err != nil {
            return nil, err
        }

        if doc != nil {
            docs = append(docs, doc)
        }
    }
}

// collectFacts walks a Kubernetes manifest and collects container env
// variables, annotation keys and resource kinds.
func collectFacts(
    node any,
    facts *workloadFacts,
) {

    switch value := node.(type) {

    case map[string]any:

        for key, child := range value {

            switch key {

            case "env":

                if items, ok := child.([]any); ok {

                    for _, item := range items {

                        if env, ok := item.(map[string]any); ok {

                            if name, ok := env["name"].(string); ok {
                                facts.Env[name] = fmt.Sprintf("%v", env["value"])
                            }
                        }
                    }
                }

            case "annotations":

                if annotations, ok := child.(map[string]any); ok {

                    for k := range annotations {
                        facts.Annotations[k] = true
                    }
                }

            case "kind":

                if kind, ok := child.(string); ok {
                    facts.Kinds[kind] = true
                }
            }

            collectFacts(child, facts)
        }

    case []any:

        for _, item := range value {
            collectFacts(item, facts)
        }
    }
}

// collectComposeFacts collects the environment variables of every service of a
// Compose file (map or KEY=VALUE list form).
func collectComposeFacts(
    node any,
    facts *workloadFacts,
) {

    doc, _ := node.(map[string]any)
    services, _ := doc["services"].(map[string]any)

    for _, s := range services {

        service, _ := s.(map[string]any)

        switch env := service["environment"].(type) {

        case map[string]any:

            for k, v := range env {
                facts.Env[k] = fmt.Sprintf("%v", v)
            }

        case []any:

            for _, item := range env {

                entry := fmt.Sprintf("%v", item)
                name, value, _ := strings.Cut(entry, "=")
                facts.Env[name] = value
            }
        }
    }
}
