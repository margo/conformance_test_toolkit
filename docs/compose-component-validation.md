# Compose component registry checks

The Application Supplier runner checks Compose components after the existing OCI
reachability check. This closes the false positive described in
https://github.com/margo/conformance_test_toolkit/issues/18 .

The check fetches the referenced component manifest, verifies its digest/size,
and reports the Margo artifact type, single layer media type, required layer
annotations, revision match and optional OCI version annotation. It retrieves
the layer by digest and checks its digest/size before reading the gzip/tar stream.
The archive must have exactly one top-level directory with a regular
`compose.yaml`. Absolute paths and `..` path segments are rejected. No content
is extracted or executed; the directory name need not match the component name.

CR-ID mapping follows `MARGO-APP-REG-021`–`025`, `027`, `029`, `035`, `036`, `038`
and `039` in the PR3 requirements document at
https://github.com/margo/conformance_documentation/blob/4d67ca2f60f513f75d9f3838d67a976f40acc826/docs/persona_based_content/app_supplier_conf_requirements.md .
That requirements revision is on the PR3 documentation branch, not its current
main branch. Normative source:
https://docs.margo.org/specification/applications/application-registry#compose-component-registry .

Limits: manifest 1 MiB, compressed layer 32 MiB, expanded stream 256 MiB,
10,000 tar entries and a 30-second network context. Exceeding a size/entry limit
is `NOT RUN`, not a claim that a larger artifact violates Margo.
This initial change does not validate Compose semantics, referenced env/config
files, excluded bind mounts/secrets, link-target safety, signing or deployment.
A structural pass is not a complete compliance claim.

Run from `ctt-runner/app-supplier/scripts`:

```sh
go test -race ./...
go vet ./...
```

Tests include a local HTTP registry boundary and unchanged public components
from `industrial-aiops/iaiops-compose`: 0.33.0 must fail (Docker Compose publish
format), while 0.34.0 passes the checks above. Their exact manifest/layer bytes,
digests, origin and license are retained under `testdata/compose-registry`.
No external registry or device is required to run the tests.
