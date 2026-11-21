package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleasedComposeComponents(t *testing.T) {
	for _, version := range []string{"0.33.0", "0.34.0"} {
		t.Run(version, func(t *testing.T) {
			dir := filepath.Join("testdata", "compose-registry")
			body, err := os.ReadFile(filepath.Join(dir, version+"-manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{
				"0.33.0": "sha256:16b3f61fcd038129bba439e133818bb208604e9f7a2f57132c140564bdfeafa8",
				"0.34.0": "sha256:cdf8852066bb79dc71cf6f8ff0609556c284c1948077e074e1e29aa6aaa4bb34",
			}[version]
			if sha256Digest(body) != want {
				t.Fatal("published manifest fixture changed")
			}
			var m Manifest
			if err := json.Unmarshal(body, &m); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/manifests/") {
					w.Header().Set("Content-Type", mediaTypeOCIManifest)
					w.Header().Set("Docker-Content-Digest", sha256Digest(body))
					w.Header().Set("Content-Length", fmt.Sprint(len(body)))
					w.Write(body)
					return
				}
				if strings.HasSuffix(r.URL.Path, m.Layers[0].Digest) {
					blob, err := os.ReadFile(filepath.Join(dir, version+"-"+strings.TrimPrefix(m.Layers[0].Digest, "sha256:")+".blob"))
					if err != nil {
						http.Error(w, "fixture unavailable", 500)
						return
					}
					w.Write(blob)
					return
				}
				w.WriteHeader(404)
			}))
			defer srv.Close()
			r := NewValidationReport()
			ValidateComposeRegistry(Component{Name: "iaiops", Properties: map[string]interface{}{"repository": "oci://" + strings.TrimPrefix(srv.URL, "http://") + "/compose", "revision": version}}, r)
			if (r.Status == "FAILED") != (version == "0.33.0") {
				t.Fatalf("report: %+v", r)
			}
			for _, entry := range r.Entries {
				if entry.Status == "NOT RUN" {
					t.Fatalf("unexpected skipped check: %+v", entry)
				}
			}
		})
	}
}

func composeTestArchive(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range names {
		data := []byte("services: {}\n")
		h := &tar.Header{Name: name, Mode: 0644, Typeflag: tar.TypeReg, Size: int64(len(data))}
		if strings.HasSuffix(name, "/") {
			h.Typeflag = tar.TypeDir
			h.Size = 0
			data = nil
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func composeTestManifest(data []byte) Manifest {
	return Manifest{SchemaVersion: 2, MediaType: mediaTypeOCIManifest, ArtifactType: composeArtifactType,
		Layers: []Descriptor{{MediaType: composeLayerType, Digest: sha256Digest(data), Size: int64(len(data)), Annotations: map[string]string{
			"org.margo.component.type": "compose", "org.margo.component.version": "1.2.3",
		}}}}
}

func TestComposeManifestConstraints(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutate   func(*Manifest)
		wantFail bool
	}{
		{"valid", func(m *Manifest) {}, false},
		{"docker-compose-publish", func(m *Manifest) {
			m.ArtifactType = "application/vnd.docker.compose.project"
			m.Layers[0].MediaType = "application/vnd.docker.compose.file+yaml"
		}, true},
		{"artifact-type", func(m *Manifest) { m.ArtifactType = "other" }, true},
		{"no-layer", func(m *Manifest) { m.Layers = nil }, true},
		{"two-layers", func(m *Manifest) { m.Layers = append(m.Layers, m.Layers[0]) }, true},
		{"wrong-layer-type", func(m *Manifest) { m.Layers[0].MediaType = "text/yaml" }, true},
		{"missing-annotations", func(m *Manifest) { m.Layers[0].Annotations = nil }, true},
		{"wrong-component-type", func(m *Manifest) { m.Layers[0].Annotations["org.margo.component.type"] = "helm" }, true},
		{"wrong-revision", func(m *Manifest) { m.Layers[0].Annotations["org.margo.component.version"] = "1.2.4" }, true},
		{"wrong-oci-version", func(m *Manifest) { m.Layers[0].Annotations["org.opencontainers.image.version"] = "1.2.4" }, true},
		{"matching-oci-version", func(m *Manifest) { m.Layers[0].Annotations["org.opencontainers.image.version"] = "1.2.3" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := composeTestManifest(nil)
			tc.mutate(&m)
			r := NewValidationReport()
			r.Check("MARGO-APP-REG-021", "manifest", "registry", composeArtifactType)
			validateComposeManifest(&m, "1.2.3", r)
			if (r.Status == "FAILED") != tc.wantFail {
				t.Fatalf("report: %+v", r)
			}
		})
	}
}

func TestComposeArchiveLayout(t *testing.T) {
	for _, tc := range []struct {
		name     string
		paths    []string
		wantFail bool
	}{
		{"valid", []string{"arbitrary-name/", "arbitrary-name/compose.yaml", "arbitrary-name/configs/example.txt"}, false},
		{"implicit-directory", []string{"arbitrary/compose.yaml"}, false},
		{"empty", nil, true},
		{"loose-compose", []string{"compose.yaml"}, true},
		{"two-roots", []string{"app/compose.yaml", "other/config.txt"}, true},
		{"missing-compose", []string{"app/readme"}, true},
		{"alternate-filename", []string{"app/docker-compose.yml"}, true},
		{"nested-compose", []string{"app/nested/compose.yaml"}, true},
		{"absolute", []string{"/app/compose.yaml"}, true},
		{"traversal", []string{"app/../compose.yaml"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateComposeArchive(composeTestArchive(t, tc.paths...))
			if (err != nil) != tc.wantFail {
				t.Fatalf("unexpected archive result: %v", err)
			}
		})
	}
	if err := validateComposeArchive([]byte("services: {}")); err == nil {
		t.Fatal("accepted plain YAML")
	}
	bad := composeTestArchive(t, "app/compose.yaml")
	bad[len(bad)-8] ^= 0xff
	if err := validateComposeArchive(bad); err == nil {
		t.Fatal("accepted corrupt gzip checksum")
	}
}

func TestComposeRegistryFetchAndIntegrity(t *testing.T) {
	for _, mode := range []string{"valid", "legacy", "corrupt-blob", "missing-blob", "resource-limit"} {
		t.Run(mode, func(t *testing.T) {
			data := composeTestArchive(t, "app/compose.yaml")
			m := composeTestManifest(data)
			if mode == "legacy" {
				m.ArtifactType = "application/vnd.docker.compose.project"
				m.Layers[0].MediaType = "application/vnd.docker.compose.file+yaml"
			}
			if mode == "resource-limit" {
				m.Layers[0].Size = maxComposeBlob + 1
			}
			body, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var blobRequests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/manifests/"):
					w.Header().Set("Content-Type", mediaTypeOCIManifest)
					w.Header().Set("Docker-Content-Digest", sha256Digest(body))
					w.Header().Set("Content-Length", fmt.Sprint(len(body)))
					w.Write(body)
				case strings.Contains(r.URL.Path, "/blobs/"):
					blobRequests++
					if mode == "missing-blob" {
						w.WriteHeader(404)
						return
					}
					copyData := append([]byte(nil), data...)
					if mode == "corrupt-blob" {
						copyData[0] ^= 0xff
					}
					w.Write(copyData)
				default:
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			r := NewValidationReport()
			ValidateComposeRegistry(Component{Name: "fixture", Properties: map[string]interface{}{"repository": "oci://" + strings.TrimPrefix(srv.URL, "http://") + "/component", "revision": "1.2.3"}}, r)
			if mode == "valid" {
				if r.Status == "FAILED" || blobRequests != 1 {
					t.Fatalf("report %+v requests %d", r, blobRequests)
				}
			} else if mode == "resource-limit" {
				if r.Entries[len(r.Entries)-1].Status != "NOT RUN" || blobRequests != 0 {
					t.Fatalf("resource limit: %+v", r)
				}
			} else if r.Status != "FAILED" {
				t.Fatalf("accepted %s: %+v", mode, r)
			}
		})
	}
}

func TestComposeRevisionSemver(t *testing.T) {
	for _, v := range []string{"1.2.3", "0.0.0", "1.2.3-rc.1+build.01"} {
		if !isComposeSemver(v) {
			t.Errorf("rejected %s", v)
		}
	}
	for _, v := range []string{"v1.2.3", "1.2", "01.2.3", "1.2.3-01", "latest", ""} {
		if isComposeSemver(v) {
			t.Errorf("accepted %s", v)
		}
	}
}
