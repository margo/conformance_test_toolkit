package main

import "testing"

func TestValidateOTLPComposeInterpolation(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		status string
	}{
		{"literal OTLP", map[string]string{"OTEL_METRICS_EXPORTER": "otlp", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "http/protobuf"}, "PASS"},
		{"literal none", map[string]string{"OTEL_METRICS_EXPORTER": "none"}, "PASS"},
		{"literal unsupported", map[string]string{"OTEL_METRICS_EXPORTER": "zipkin"}, "FAIL"},
		{"allowed default", map[string]string{"OTEL_METRICS_EXPORTER": "${OTEL_METRICS_EXPORTER:-none}", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "http/protobuf"}, "PASS"},
		{"allowed protocol default", map[string]string{"OTEL_METRICS_EXPORTER": "otlp", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "${OTEL_PROTOCOL:-http/protobuf}"}, "PASS"},
		{"multiple literal exporters", map[string]string{"OTEL_METRICS_EXPORTER": "otlp,console"}, "PASS"},
		{"multiple default exporters", map[string]string{"OTEL_METRICS_EXPORTER": "${X:-otlp,console}"}, "PASS"},
		{"dash default", map[string]string{"OTEL_METRICS_EXPORTER": "${X-none}"}, "PASS"},
		{"unsupported exporter in default list", map[string]string{"OTEL_METRICS_EXPORTER": "${X:-otlp,zipkin}"}, "FAIL"},
		{"unsupported default", map[string]string{"OTEL_METRICS_EXPORTER": "${OTEL_METRICS_EXPORTER:-zipkin}"}, "FAIL"},
		{"unknown value", map[string]string{"OTEL_METRICS_EXPORTER": "${OTEL_METRICS_EXPORTER}"}, "NOT RUN"},
		{"unsupported protocol default", map[string]string{"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "${OTEL_PROTOCOL:-thrift}"}, "FAIL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := NewValidationReport()
			validateOTLP(report, "sample", workloadFacts{Env: tc.env}, []string{"OTEL_METRICS_EXPORTER"}, nil)
			if got := report.Entries[len(report.Entries)-1].Status; got != tc.status {
				t.Fatalf("status = %q, want %q", got, tc.status)
			}
		})
	}
}

func TestComposeParameterPrecedence(t *testing.T) {
	t.Setenv("EXPORTER", "zipkin") // Never use the validator host's environment.
	cases := []struct {
		name       string
		expression string
		value      any
		component  string
		pointer    string
		want       string
		status     string
	}{
		{"parameter overrides unsupported default", "${EXPORTER:-zipkin}", "none", "sample", "EXPORTER", "none", "PASS"},
		{"unsupported parameter overrides allowed default", "${EXPORTER:-otlp}", "zipkin", "sample", "EXPORTER", "zipkin", "FAIL"},
		{"parameter without default", "${EXPORTER}", "otlp,console", "sample", "EXPORTER", "otlp,console", "PASS"},
		{"other component uses default", "${EXPORTER:-none}", "zipkin", "other", "EXPORTER", "${EXPORTER:-none}", "PASS"},
		{"other pointer uses default", "${EXPORTER:-none}", "zipkin", "sample", "OTHER", "${EXPORTER:-none}", "PASS"},
		{"nil parameter uses default", "${EXPORTER:-none}", nil, "sample", "EXPORTER", "${EXPORTER:-none}", "PASS"},
		{"unknown without default", "${EXPORTER}", nil, "sample", "EXPORTER", "${EXPORTER}", "NOT RUN"},
		{"literal is not overridden", "otlp", "zipkin", "sample", "EXPORTER", "otlp", "PASS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The parameter key intentionally differs from its target variable.
			parameters := map[string]Parameter{"exporterSetting": {
				Value:   tc.value,
				Targets: []Target{{Pointer: tc.pointer, Components: []string{tc.component}}},
			}}
			facts := workloadFacts{Env: map[string]string{"OTEL_METRICS_EXPORTER": tc.expression}}
			applyComposeParameters(&facts, "sample", parameters)
			if got := facts.Env["OTEL_METRICS_EXPORTER"]; got != tc.want {
				t.Fatalf("resolved value = %q, want %q", got, tc.want)
			}
			report := NewValidationReport()
			validateOTLP(report, "sample", facts, otelVariables(facts), nil)
			if got := report.Entries[len(report.Entries)-1].Status; got != tc.status {
				t.Fatalf("status = %q, want %q", got, tc.status)
			}
		})
	}
}

func TestComposeProtocolParameter(t *testing.T) {
	facts := workloadFacts{Env: map[string]string{
		"OTEL_METRICS_EXPORTER":               "otlp",
		"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "${PROTOCOL:-thrift}",
	}}
	applyComposeParameters(&facts, "sample", map[string]Parameter{
		"protocolSetting": {Value: "http/protobuf", Targets: []Target{{Pointer: "PROTOCOL", Components: []string{"sample"}}}},
	})
	report := NewValidationReport()
	validateOTLP(report, "sample", facts, otelVariables(facts), nil)
	if got := report.Entries[len(report.Entries)-1].Status; got != "PASS" {
		t.Fatalf("status = %q, want PASS", got)
	}
}

func TestComposeInterpolationDefault(t *testing.T) {
	cases := []struct {
		value string
		want  string
		ok    bool
	}{
		{"${OTEL_METRICS_EXPORTER:-none}", "none", true},
		{"${OTEL_METRICS_EXPORTER-otlp}", "otlp", true},
		{"${OTEL_METRICS_EXPORTER}", "", false},
		{"prefix-${OTEL_METRICS_EXPORTER:-none}", "", false},
		{"${1BAD:-none}", "", false},
	}
	for _, tc := range cases {
		got, ok := composeInterpolationDefault(tc.value)
		if got != tc.want || ok != tc.ok {
			t.Errorf("composeInterpolationDefault(%q) = (%q, %v), want (%q, %v)", tc.value, got, ok, tc.want, tc.ok)
		}
	}
}
