package crypto

import (
	"os"
	"strings"
	"testing"
)

func TestGetDigestOfFile(t *testing.T) {
	// Create temporary test file
	content := "Hello, World!"
	tempFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name())

	if _, err := tempFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tempFile.Close()

	// Test digest calculation
	digest, err := GetDigestOfFile(tempFile.Name())
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedDigest := "sha256:dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f"
	if digest != expectedDigest {
		t.Errorf("Expected digest %s, got %s", expectedDigest, digest)
	}

	// Test with non-existent file
	_, err = GetDigestOfFile("/non/existent/file.txt")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	// Test with empty filepath
	_, err = GetDigestOfFile("")
	if err == nil {
		t.Error("Expected error for empty filepath")
	}
}

func TestGetDigestOfContent(t *testing.T) {
	content := []byte("Hello, World!")
	digest, err := GetDigestOfContent(content)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedDigest := "sha256:dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f"
	if digest != expectedDigest {
		t.Errorf("Expected digest %s, got %s", expectedDigest, digest)
	}

	// Test with empty content
	digest, err = GetDigestOfContent([]byte{})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedEmptyDigest := "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if digest != expectedEmptyDigest {
		t.Errorf("Expected empty digest %s, got %s", expectedEmptyDigest, digest)
	}
}

func TestGetDigestOfString(t *testing.T) {
	content := "Hello, World!"
	digest, err := GetDigestOfString(content)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedDigest := "sha256:dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f"
	if digest != expectedDigest {
		t.Errorf("Expected digest %s, got %s", expectedDigest, digest)
	}
}

func TestGetDigestOfReader(t *testing.T) {
	content := "Hello, World!"
	reader := strings.NewReader(content)

	digest, err := GetDigestOfReader(reader)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedDigest := "sha256:dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f"
	if digest != expectedDigest {
		t.Errorf("Expected digest %s, got %s", expectedDigest, digest)
	}

	// Test with nil reader
	_, err = GetDigestOfReader(nil)
	if err == nil {
		t.Error("Expected error for nil reader")
	}
}

func TestVerifyFileDigest(t *testing.T) {
	// Create temporary test file
	content := "Hello, World!"
	tempFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name())

	if _, err := tempFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tempFile.Close()

	// Test with correct digest
	correctDigest := "sha256:dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f"
	isValid, err := VerifyFileDigest(tempFile.Name(), correctDigest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !isValid {
		t.Error("Expected digest to be valid")
	}

	// Test with incorrect digest
	incorrectDigest := "sha256:incorrect"
	isValid, err = VerifyFileDigest(tempFile.Name(), incorrectDigest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if isValid {
		t.Error("Expected digest to be invalid")
	}
}

func TestVerifyContentDigest(t *testing.T) {
	content := []byte("Hello, World!")
	correctDigest := "sha256:dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f"

	// Test with correct digest
	isValid, err := VerifyContentDigest(content, correctDigest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !isValid {
		t.Error("Expected digest to be valid")
	}

	// Test with incorrect digest
	incorrectDigest := "sha256:incorrect"
	isValid, err = VerifyContentDigest(content, incorrectDigest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if isValid {
		t.Error("Expected digest to be invalid")
	}
}
