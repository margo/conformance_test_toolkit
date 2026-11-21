package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// SemVer 2.0: numeric identifiers have no leading zeroes; no leading v.
var composeSemver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

var errComposeLimit = errors.New("Compose validation resource limit exceeded")

func isComposeSemver(v string) bool { return composeSemver.MatchString(v) }

const (
	composeArtifactType = "application/vnd.org.margo.component.compose+json"
	composeLayerType    = "application/vnd.org.margo.component.compose.tar+gzip"
	maxComposeBlob      = 32 << 20
	maxComposeExpanded  = 256 << 20
)

// ValidateComposeRegistry checks the component itself, not just registry reachability.
// It never extracts or executes archive content. Compose semantics and referenced
// config/env/secret files are outside this structural check.
func ValidateComposeRegistry(c Component, report *ValidationReport) {
	repository, _ := c.Properties["repository"].(string)
	revision, _ := c.Properties["revision"].(string)
	ref := repository + ":" + revision
	report.Check("MARGO-APP-REG-021", "Compose component '"+c.Name+"' manifest", "registry", composeArtifactType+" at "+ref)
	repo, err := newRepository(ref)
	if err != nil {
		report.Fail(ref, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	desc, rc, err := repo.FetchReference(ctx, repo.Reference.Reference)
	if err != nil {
		report.Fail(ref, err.Error())
		return
	}
	body, err := readComposeBounded(rc, 1<<20)
	rc.Close()
	if errors.Is(err, errComposeLimit) {
		report.Mark("NOT RUN", ref, err.Error())
		return
	}
	if err != nil || int64(len(body)) != desc.Size || sha256Digest(body) != desc.Digest.String() {
		report.Fail(ref, fmt.Sprintf("Manifest content could not be verified: %v", err))
		return
	}
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		report.Fail(ref, "Invalid OCI manifest JSON: "+err.Error())
		return
	}
	layer, ok := validateComposeManifest(&manifest, revision, report)
	if !ok {
		return
	}
	report.Check("MARGO-APP-REG-025, MARGO-APP-REG-027, MARGO-APP-REG-029, MARGO-APP-REG-035, MARGO-APP-REG-036", "Compose component '"+c.Name+"' archive", "registry", "Digest-verified gzip tar with one top-level directory containing a regular compose.yaml; no absolute or traversal paths")
	dgst, err := digest.Parse(layer.Digest)
	if layer.Size > maxComposeBlob {
		report.Mark("NOT RUN", layer.Digest, "Archive exceeds the 32 MiB validation limit")
		return
	}
	if err != nil || layer.Size < 0 {
		report.Fail(layer.Digest, "Invalid layer digest or size")
		return
	}
	blob, err := repo.Blobs().Fetch(ctx, ocispec.Descriptor{MediaType: layer.MediaType, Digest: dgst, Size: layer.Size})
	if err != nil {
		report.Fail(ref, "Archive fetch failed: "+err.Error())
		return
	}
	defer blob.Close()
	data, err := readComposeBounded(blob, maxComposeBlob)
	if errors.Is(err, errComposeLimit) {
		report.Mark("NOT RUN", layer.Digest, err.Error())
		return
	}
	if err != nil || int64(len(data)) != layer.Size || sha256Digest(data) != layer.Digest {
		report.Fail(layer.Digest, fmt.Sprintf("Archive digest/size verification failed: %v", err))
		return
	}
	if err := validateComposeArchive(data); err != nil {
		if errors.Is(err, errComposeLimit) {
			report.Mark("NOT RUN", layer.Digest, err.Error())
			return
		}
		report.Fail(layer.Digest, err.Error())
		return
	}
	report.Pass(layer.Digest, "Verified archive layout and digest; Compose semantics and file-reference rules are not checked.")
}

func validateComposeManifest(m *Manifest, revision string, report *ValidationReport) (Descriptor, bool) {
	if m.SchemaVersion != 2 || m.MediaType != mediaTypeOCIManifest || m.ArtifactType != composeArtifactType {
		report.Fail(m.ArtifactType, "Expected OCI image manifest v2 with artifactType "+composeArtifactType+"; docker compose publish artifacts are incompatible.")
	} else {
		report.Pass(m.ArtifactType, "Margo Compose component artifact type.")
	}
	report.Check("MARGO-APP-REG-022", "Compose component layer", "manifest", "Exactly one layer of mediaType "+composeLayerType)
	if len(m.Layers) != 1 {
		report.Fail(fmt.Sprint(len(m.Layers)), "Expected exactly one Compose Archive layer")
		return Descriptor{}, false
	}
	layer := m.Layers[0]
	if layer.MediaType != composeLayerType {
		report.Fail(layer.MediaType, "Expected "+composeLayerType)
		return Descriptor{}, false
	}
	report.Pass(layer.MediaType, "Single Compose Archive layer.")
	a := layer.Annotations
	report.Check("MARGO-APP-REG-023, MARGO-APP-REG-038, MARGO-APP-REG-039", "Compose layer annotations", "manifest", "org.margo.component.type=compose; org.margo.component.version matches revision "+revision)
	if a["org.margo.component.type"] != "compose" || a["org.margo.component.version"] != revision || !isComposeSemver(revision) {
		report.Fail(fmt.Sprint(a), "Required Compose type/version annotations must match the SemVer revision without a leading v.")
	} else {
		report.Pass(revision, "Required layer annotations match the component revision.")
	}
	report.Check("MARGO-APP-REG-024", "Optional OCI version annotation", "manifest", "org.opencontainers.image.version equals org.margo.component.version when present")
	if v, exists := a["org.opencontainers.image.version"]; exists && v != a["org.margo.component.version"] {
		report.Fail(v, "OCI version annotation differs from Margo component version.")
	} else {
		report.Pass(a["org.opencontainers.image.version"], "Optional version annotation is absent or matches.")
	}
	return layer, true
}

func readComposeBounded(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%w: content exceeds %d bytes", errComposeLimit, limit)
	}
	return b, err
}

func validateComposeArchive(data []byte) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("not a gzip archive: %w", err)
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: gz, N: maxComposeExpanded + 1}
	tr := tar.NewReader(limited)
	root := ""
	found := false
	for count := 0; ; count++ {
		h, err := tr.Next()
		if limited.N <= 0 {
			return fmt.Errorf("%w: archive exceeds 256 MiB expanded", errComposeLimit)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid tar archive: %w", err)
		}
		if count >= 10000 {
			return fmt.Errorf("%w: archive exceeds 10000 entries", errComposeLimit)
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || path.IsAbs(name) || strings.Contains(name, "\\") {
			return fmt.Errorf("invalid archive path %q", h.Name)
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				return fmt.Errorf("traversal path %q", h.Name)
			}
		}
		name = path.Clean(name)
		parts := strings.Split(name, "/")
		if parts[0] == "." || (len(parts) == 1 && h.Typeflag != tar.TypeDir) {
			return fmt.Errorf("entry %q is not inside a top-level directory", h.Name)
		}
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root {
			return fmt.Errorf("archive has multiple top-level directories")
		}
		if name == root+"/compose.yaml" && h.Typeflag == tar.TypeReg {
			found = true
		}
	}
	// Consume the gzip trailer as well: tar EOF alone does not verify its checksum.
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return fmt.Errorf("invalid gzip stream: %w", err)
	}
	if limited.N <= 0 {
		return fmt.Errorf("%w: archive exceeds 256 MiB expanded", errComposeLimit)
	}
	if !found {
		return fmt.Errorf("single top-level directory must contain a regular compose.yaml")
	}
	return nil
}
