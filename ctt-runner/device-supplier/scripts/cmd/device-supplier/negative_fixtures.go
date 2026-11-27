package main

import (
	"bytes"
	"compress/gzip"
	"strings"
)

// Negative fixtures — a FIRST-CLASS conformance capability, not test scaffolding.
//
// To verify a "the device client MUST reject X" requirement (e.g.
// MARGO-DEV-MANAGEMENTINTERFACE-008), the mock WFM must be able to serve a
// spec-violating desired-state manifest on demand and then observe whether the
// device under test reacts correctly. This file is the catalogue of the spec
// violations the suite can simulate.
//
// Design rules:
//   - Each mode maps 1:1 to a Margo conformance requirement; the constant name
//     carries the intent so a reader sees "these are the spec violations we can
//     exercise", not "responses get mangled here".
//   - buildStateManifest() always builds the CORRECT manifest first. A fixture
//     is a pure post-processor applied over that correct manifest, so the happy
//     path stays the mock's de-facto reference for "what conformant looks like".
//   - Which fixture proves which requirement lives in the scenario files the
//     suite reads, not compiled in here — this file only provides the mechanism.
//   - Every fixture is one or two lines: walk the manifest with the shared
//     forEachDigestHolder / forEachDeployment helpers, mutate one field.

// NegativeFixture names a single spec violation to inject into a client's
// desired-state manifest. The empty value means no injection — the default.
type NegativeFixture string

const (
	// FixtureNone is the default: serve a fully spec-conformant manifest.
	FixtureNone NegativeFixture = ""

	// FixtureUnsupportedDigestAlgorithm rewrites every digest to a non-sha256
	// algorithm prefix. MARGO-DEV-MANAGEMENTINTERFACE-008.
	FixtureUnsupportedDigestAlgorithm NegativeFixture = "unsupported_digest_algorithm"

	// FixtureBundleDigestMismatch corrupts bundle.digest (URL left intact, so it
	// still resolves to the real bundle whose hash no longer matches).
	// MARGO-DEV-MANAGEMENTINTERFACE-006 / -009.
	FixtureBundleDigestMismatch NegativeFixture = "bundle_digest_mismatch"

	// FixtureDeploymentDigestMismatch corrupts the first deployments[].digest
	// the same way. MARGO-DEV-MANAGEMENTINTERFACE-019.
	FixtureDeploymentDigestMismatch NegativeFixture = "deployment_digest_mismatch"

	// FixtureNonIncreasingManifestVersion forces manifestVersion back to 1 so a
	// client that has already seen a higher version must reject it.
	// MARGO-DEV-MANAGEMENTINTERFACE-010.
	FixtureNonIncreasingManifestVersion NegativeFixture = "non_increasing_manifest_version"

	// FixtureWrongSizeBytes sets every sizeBytes to a bogus value while leaving
	// digests correct — a conformant client MUST still accept (it verifies by
	// digest, not size). MARGO-DEV-MANAGEMENTINTERFACE-025 / -026.
	FixtureWrongSizeBytes NegativeFixture = "wrong_size_bytes"
)

// knownFixtures lets the test-control endpoint reject a typo instead of
// silently serving a conformant manifest (which would make a negative test
// pass for the wrong reason).
var knownFixtures = map[NegativeFixture]bool{
	FixtureNone:                         true,
	FixtureUnsupportedDigestAlgorithm:   true,
	FixtureBundleDigestMismatch:         true,
	FixtureDeploymentDigestMismatch:     true,
	FixtureNonIncreasingManifestVersion: true,
	FixtureWrongSizeBytes:               true,
	FixtureBundleContentTampered:        true,
	FixtureDeploymentContentTampered:    true,
	FixtureContentNotFound:              true,
	FixtureGzipContentEncoding:          true,
}

// The next two fixtures also leave the manifest correct and change only how
// the content-addressed endpoints answer.
const (
	// FixtureContentNotFound: the bundle and deployment endpoints answer 404
	// for every digest while the manifest still lists the deployments. A 404
	// MUST NOT be taken as a deletion signal. MARGO-DEV-MGMT-057 / -058.
	FixtureContentNotFound NegativeFixture = "content_not_found"

	// FixtureGzipContentEncoding: the bundle and deployment endpoints apply
	// Content-Encoding: gzip when the request accepts it. Digests refer to the
	// decoded body, so a conformant client still adopts. MARGO-DEV-MGMT-044.
	FixtureGzipContentEncoding NegativeFixture = "gzip_content_encoding"
)

// fixtureDeployments: deployments whose ApplicationDeployment is not the
// default one. Each template is the default template plus the one part a
// requirement is about, so the device's reaction can only come from that part.
var fixtureDeployments = map[string]string{
	// x-ctt-unknown-extensions at spec, deploymentProfile and component level:
	// the device must ignore them. MARGO-DEV-MGMT-053/054/060/061, EXT-002/005.
	"5f0c1c1e-2b7a-4c58-9d3e-1a2b3c4d5e08": "manifests/deployment-template-extensions.yaml",
	// An eligibility rule no device meets (a label nobody reports): the device
	// must not deploy it. MARGO-DEV-MGMT-051.
	"5f0c1c1e-2b7a-4c58-9d3e-1a2b3c4d5e09": "manifests/deployment-template-ineligible.yaml",
}

// acceptsGzip reports whether an Accept-Encoding header allows gzip (RFC 9110
// 12.5.3: listed, or "*", and not with q=0).
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		coding := strings.ToLower(strings.TrimSpace(fields[0]))
		if coding != "gzip" && coding != "x-gzip" && coding != "*" {
			continue
		}
		q := "1"
		for _, p := range fields[1:] {
			if kv := strings.SplitN(strings.TrimSpace(p), "=", 2); len(kv) == 2 && strings.EqualFold(kv[0], "q") {
				q = strings.TrimSpace(kv[1])
			}
		}
		if strings.Trim(q, "0.") != "" {
			return true
		}
	}
	return false
}

// gzipBytes compresses b for Content-Encoding: gzip.
func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

// The two "content tampered" fixtures leave the manifest correct and change
// the bytes served under the correct digest URL instead. Unlike the
// *_digest_mismatch fixtures they also reach a device that builds its fetch
// URL from the digest: it receives content whose sha256 is not the digest it
// asked for, and must abort.
const (
	FixtureBundleContentTampered     NegativeFixture = "bundle_content_tampered"
	FixtureDeploymentContentTampered NegativeFixture = "deployment_content_tampered"
)

// tamper returns content that no longer hashes to its advertised digest.
func tamper(content []byte) []byte {
	return append(append([]byte{}, content...), []byte("\n# tampered by the conformance test\n")...)
}

// isKnownFixture reports whether f is a fixture this build understands.
func isKnownFixture(f NegativeFixture) bool { return knownFixtures[f] }

// applyNegativeFixture returns manifest transformed by the named fixture.
// manifest is the already-correct map from buildStateManifest; it is mutated in
// place and also returned for convenience. FixtureNone is a no-op.
func applyNegativeFixture(manifest map[string]interface{}, fixture NegativeFixture) map[string]interface{} {
	switch fixture {
	case FixtureUnsupportedDigestAlgorithm:
		forEachDigestHolder(manifest, func(h map[string]interface{}) {
			if d, ok := h["digest"].(string); ok {
				h["digest"] = swapAlgo(d, "sha512")
			}
		})
	case FixtureBundleDigestMismatch:
		if b, ok := manifest["bundle"].(map[string]interface{}); ok {
			if d, ok := b["digest"].(string); ok {
				b["digest"] = corruptHex(d)
			}
		}
	case FixtureDeploymentDigestMismatch:
		forEachDeployment(manifest, func(d map[string]interface{}, i int) {
			if i != 0 {
				return
			}
			if s, ok := d["digest"].(string); ok {
				d["digest"] = corruptHex(s)
			}
		})
	case FixtureNonIncreasingManifestVersion:
		manifest["manifestVersion"] = 1
	case FixtureWrongSizeBytes:
		forEachDigestHolder(manifest, func(h map[string]interface{}) {
			h["sizeBytes"] = 1
		})
	}
	return manifest
}

// forEachDigestHolder calls fn for every manifest node carrying a digest +
// sizeBytes pair: the bundle object and each deployment ref.
func forEachDigestHolder(manifest map[string]interface{}, fn func(map[string]interface{})) {
	if b, ok := manifest["bundle"].(map[string]interface{}); ok {
		fn(b)
	}
	forEachDeployment(manifest, func(d map[string]interface{}, _ int) { fn(d) })
}

// forEachDeployment calls fn for each object in manifest.deployments.
func forEachDeployment(manifest map[string]interface{}, fn func(map[string]interface{}, int)) {
	list, ok := manifest["deployments"].([]interface{})
	if !ok {
		return
	}
	for i, item := range list {
		if d, ok := item.(map[string]interface{}); ok {
			fn(d, i)
		}
	}
}

// swapAlgo replaces the "<algo>:" prefix of a digest, keeping the hex body.
func swapAlgo(digest, algo string) string {
	if i := strings.IndexByte(digest, ':'); i >= 0 {
		return algo + digest[i:]
	}
	return algo + ":" + digest
}

// corruptHex flips the last character of a digest so it stays well-formed but
// no longer matches the artifact it points to.
func corruptHex(digest string) string {
	if digest == "" {
		return digest
	}
	repl := "0"
	if digest[len(digest)-1] == '0' {
		repl = "1"
	}
	return digest[:len(digest)-1] + repl
}
