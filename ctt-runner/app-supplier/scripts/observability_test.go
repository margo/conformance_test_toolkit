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
		{"allowed default", map[string]string{"OTEL_METRICS_EXPORTER": "${OTEL_METRICS_EXPORTER:-none}", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "http/protobuf"}, "NOT RUN"},
		{"allowed protocol default", map[string]string{"OTEL_METRICS_EXPORTER": "otlp", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "${OTEL_PROTOCOL:-http/protobuf}"}, "NOT RUN"},
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
