# Public Compose component regression inputs

Fetched 2026-11-21 from `ghcr.io/industrial-aiops/iaiops-compose` tags
`0.33.0` and `0.34.0` for margo/conformance_test_toolkit#18.
Manifests and layer blobs are unmodified; SHA256SUMS records their exact bytes.
Blob digest and size were verified against each manifest during acquisition.
The manifests have SHA-256 digests:
- 0.33.0: `sha256:16b3f61fcd038129bba439e133818bb208604e9f7a2f57132c140564bdfeafa8`
- 0.34.0: `sha256:cdf8852066bb79dc71cf6f8ff0609556c284c1948077e074e1e29aa6aaa4bb34`

The first is Docker Compose publish format and must fail the component check.
The second must pass the checks implemented here. This does not claim complete
Compose semantics, archive file-reference validation, signing or deployment.
These Industrial-AIOps artifacts are MIT-licensed; see
https://github.com/industrial-aiops/industrial-aiops/blob/v0.34.0/LICENSE .
