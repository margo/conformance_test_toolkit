package main

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestDescribeInputSource(t *testing.T) {
	directory := t.TempDir()
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := describeInputSource("", info); got != "local directory" {
		t.Fatalf("directory source = %q", got)
	}

	archive := filepath.Join(directory, "private-path.tar.gz")
	if err := os.WriteFile(archive, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if got := describeInputSource("", info); got != "local archive" {
		t.Fatalf("archive source = %q", got)
	}

	reference := "oci://ghcr.io/example/app:1.0"
	if got := describeInputSource(reference, nil); got != reference {
		t.Fatalf("OCI source = %q", got)
	}
}

func TestRevisionFromBuildSettings(t *testing.T) {
	settings := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc123"},
		{Key: "vcs.modified", Value: "true"},
	}
	if got := revisionFromBuildSettings(settings); got != "abc123 (modified working tree)" {
		t.Fatalf("revision = %q", got)
	}
	if got := revisionFromBuildSettings(nil); got != "" {
		t.Fatalf("missing revision = %q", got)
	}
}

func TestCTTSourceRevisionPrefersLauncherRevision(t *testing.T) {
	t.Setenv("CTT_SOURCE_REVISION", "launcher-commit (modified working tree)")
	if got := cttSourceRevision(); got != "launcher-commit (modified working tree)" {
		t.Fatalf("CTT source revision = %q", got)
	}
}

func TestPinnedPackageReferenceKeepsThePulledManifest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	got, err := pinnedPackageReference("oci://localhost:5000/example/app:1.0", digest)
	if err != nil {
		t.Fatal(err)
	}
	want := "localhost:5000/example/app@" + digest
	if got != want {
		t.Fatalf("pinned reference = %q, want %q", got, want)
	}
	if _, err := pinnedPackageReference("oci://localhost:5000/example/app:1.0", "not-a-digest"); err == nil {
		t.Fatal("invalid copied digest was accepted")
	}
}

func TestHTMLReportIncludesProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	report := NewValidationReport()
	report.ApplicationName = "Test app"
	report.ApplicationVersion = "1.0"
	report.InputSource = "oci://ghcr.io/example/app:1.0<script>"
	report.ManifestDigest = "sha256:abcdef"
	report.CTTRevision = "abc123"
	if err := report.GenerateHTMLReport(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, expected := range []string{
		"Input source:", "OCI manifest digest:", "CTT source revision:",
		"sha256:abcdef", "abc123",
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("report is missing %q", expected)
		}
	}
	if strings.Contains(html, "<script>") {
		t.Error("input source was not HTML-escaped")
	}
}

func TestHTMLReportOmitsUnavailableProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	if err := NewValidationReport().GenerateHTMLReport(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "OCI manifest digest:") ||
		strings.Contains(string(data), "CTT source revision:") {
		t.Error("report displayed unavailable provenance")
	}
}
