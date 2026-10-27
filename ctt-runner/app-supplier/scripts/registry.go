package main

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "net"
    "os"
    "path"
    "path/filepath"
    "regexp"
    "strings"

    "github.com/opencontainers/go-digest"
    ocispec "github.com/opencontainers/image-spec/specs-go/v1"
    "oras.land/oras-go/v2"
    "oras.land/oras-go/v2/content/file"
    "oras.land/oras-go/v2/registry/remote"
    "oras.land/oras-go/v2/registry/remote/auth"
    "oras.land/oras-go/v2/registry/remote/credentials"
    "oras.land/oras-go/v2/registry/remote/retry"
)

// Media types and annotation keys of the Application Registry specification
// (https://docs.margo.org/specification/applications/application-registry).
const (
    artifactTypeApp       = "application/vnd.margo.app.v1+json"
    mediaTypeDescription  = "application/vnd.margo.app.description.v1+yaml"
    mediaTypeOCIManifest  = "application/vnd.oci.image.manifest.v1+json"
    mediaTypeEmptyConfig  = "application/vnd.oci.empty.v1+json"
    digestEmptyConfig     = "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"
    annotationTitle       = "org.opencontainers.image.title"
    annotationAppResource = "org.margo.app.resource"
)

// resourceMediaType matches the media type of an application resource layer,
// e.g. application/vnd.margo.app.icon.v1+png. The first group is the resource kind.
var resourceMediaType = regexp.MustCompile(
    `^application/vnd\.margo\.app\.(icon|descriptionFile|licenseFile|releaseNotes)\.v1\+[A-Za-z0-9.+-]+$`,
)

// signatureArtifactTypes are the artifact types of signatures attached to an
// OCI artifact as referrers (cosign, Sigstore bundle, Notation).
var signatureArtifactTypes = map[string]bool{
    "application/vnd.dev.cosign.artifact.sig.v1+json":  true,
    "application/vnd.dev.sigstore.bundle.v0.3+json":    true,
    "application/vnd.dev.sigstore.bundle+json;version=0.3": true,
    "application/vnd.cncf.notary.signature":            true,
}

// Descriptor is an OCI content descriptor.
type Descriptor struct {
    MediaType    string            `json:"mediaType"`
    ArtifactType string            `json:"artifactType,omitempty"`
    Digest       string            `json:"digest"`
    Size         int64             `json:"size"`
    Annotations  map[string]string `json:"annotations,omitempty"`
}

// Manifest is an OCI image manifest.
type Manifest struct {
    SchemaVersion int          `json:"schemaVersion"`
    MediaType     string       `json:"mediaType"`
    ArtifactType  string       `json:"artifactType"`
    Config        Descriptor   `json:"config"`
    Layers        []Descriptor `json:"layers"`
}

// IsOCIReference reports whether the validator input is the OCI reference of an
// Application Package (oci://host/repository:tag or host/repository:tag) rather
// than a local folder or archive.
func IsOCIReference(input string) bool {

    if strings.HasPrefix(input, schemeOCI) {
        return true
    }

    if _, err := os.Stat(input); err == nil {
        return false
    }

    return strings.Contains(input, "/") &&
        (strings.Contains(input, ":") || strings.Contains(input, "@"))
}

// PullApplicationPackage downloads the Application Package with ORAS into dir:
// every layer with a file name (title annotation) becomes a file in dir.
func PullApplicationPackage(
    ref string,
    dir string,
) error {

    repo, err := newRepository(ref)

    if err != nil {
        return err
    }

    if err := os.MkdirAll(dir, 0o755); err != nil {
        return err
    }

    store, err := file.New(dir)

    if err != nil {
        return err
    }

    defer store.Close()

    tag := repo.Reference.Reference

    _, err = oras.Copy(
        context.Background(),
        repo, tag,
        store, tag,
        oras.DefaultCopyOptions,
    )

    return err
}

// ApplicationDescriptionPath returns the path of the Application Description in
// a pulled package: the file of the manifest's Application Description layer.
// It returns "" when the manifest names none, or the file is not in dir.
func ApplicationDescriptionPath(
    ref string,
    dir string,
) string {

    repo, err := newRepository(ref)

    if err != nil {
        return ""
    }

    body, err := fetchManifest(repo)

    if err != nil {
        return ""
    }

    var manifest Manifest

    if json.Unmarshal(body, &manifest) != nil {
        return ""
    }

    for _, l := range manifest.Layers {

        if l.MediaType != mediaTypeDescription {
            continue
        }

        file := filepath.Join(dir, filepath.FromSlash(layerTitle(l)))

        if _, err := os.Stat(file); err == nil {
            return file
        }
    }

    return ""
}

// newRepository returns the ORAS client of the repository of ref
// (host/repository:tag or host/repository@digest, oci:// optional). It uses the
// credentials `oras login` or `docker login` stored (~/.docker/config.json) and,
// like the ORAS CLI, plain HTTP for a registry on localhost.
func newRepository(
    ref string,
) (*remote.Repository, error) {

    repo, err := remote.NewRepository(strings.TrimPrefix(ref, schemeOCI))

    if err != nil {
        return nil, err
    }

    store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})

    if err != nil {
        return nil, err
    }

    repo.Client = &auth.Client{
        Client:     retry.DefaultClient,
        Cache:      auth.NewCache(),
        Credential: credentials.Credential(store),
    }

    repo.PlainHTTP = isLocalRegistry(repo.Reference.Registry)

    return repo, nil
}

// isLocalRegistry reports whether a registry (host or host:port) is on localhost.
func isLocalRegistry(
    registry string,
) bool {

    host, _, err := net.SplitHostPort(registry)

    if err != nil {
        host = registry
    }

    return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// fetchManifest returns the manifest the reference of repo points to, as the
// registry sent it.
func fetchManifest(
    repo *remote.Repository,
) ([]byte, error) {

    _, rc, err := repo.FetchReference(context.Background(), repo.Reference.Reference)

    if err != nil {
        return nil, err
    }

    defer rc.Close()

    return io.ReadAll(rc)
}

// resolveDigest returns the digest the registry reports for the manifest a
// reference (tag or digest) of repo points to.
func resolveDigest(
    repo *remote.Repository,
    reference string,
) (string, error) {

    desc, err := repo.Resolve(context.Background(), reference)

    if err != nil {
        return "", err
    }

    return desc.Digest.String(), nil
}

// listTags returns the tags of repo.
func listTags(
    repo *remote.Repository,
) ([]string, error) {

    var tags []string

    err := repo.Tags(
        context.Background(),
        "",
        func(page []string) error {
            tags = append(tags, page...)
            return nil
        },
    )

    return tags, err
}

// sha256Digest returns the OCI digest string of data.
func sha256Digest(
    data []byte,
) string {

    sum := sha256.Sum256(data)

    return "sha256:" + hex.EncodeToString(sum[:])
}

// ValidateRegistry checks the Application Package in the Application Registry:
// MARGO-APP-APPLICATIONREGISTRY-001 (parts are blobs), -002 (tags are
// metadata.version), -003 (manifest pushed per the OCI Distribution
// Specification) and -004 (Margo-specific manifest constraints).
func ValidateRegistry(
    ref string,
    app *ApplicationDescription,
    raw map[string]any,
    report *ValidationReport,
) {

    ref = strings.TrimPrefix(ref, schemeOCI)

    //--------------------------------------------------
    // MARGO-APP-APPLICATIONREGISTRY-003
    //--------------------------------------------------

    report.Check(
        "MARGO-APP-APPLICATIONREGISTRY-003",
        "Application Package Manifest (pull by reference)",
        "registry",
        "GET /v2/<name>/manifests/<reference> returns the manifest; its sha256 is the digest the registry reports",
    )

    repo, err := newRepository(ref)

    var body []byte

    if err == nil {
        body, err = fetchManifest(repo)
    }

    if err != nil {

        report.Fail(
            "not retrievable",
            fmt.Sprintf("The manifest of %s could not be retrieved: %v", ref, err),
        )

        report.Check(
            "MARGO-APP-APPLICATIONREGISTRY-001, MARGO-APP-APPLICATIONREGISTRY-002, MARGO-APP-APPLICATIONREGISTRY-004",
            "Application Registry checks",
            "registry",
            "Manifest retrievable",
        )

        report.Mark(
            "NOT RUN",
            "-",
            "Not run because the manifest could not be retrieved.",
        )

        return
    }

    digest, err := resolveDigest(repo, repo.Reference.Reference)

    if err != nil || digest != sha256Digest(body) {

        report.Fail(
            sha256Digest(body),
            fmt.Sprintf("The registry reports digest %q for %s, but the manifest content has a different sha256.", digest, ref),
        )

    } else {

        report.Pass(
            digest,
            "The manifest is retrievable by reference and is content-addressed.",
        )
    }

    var manifest Manifest

    report.Check(
        "MARGO-APP-APPLICATIONREGISTRY-003",
        "Application Package Manifest (OCI image manifest)",
        "manifest",
        "schemaVersion 2, mediaType "+mediaTypeOCIManifest,
    )

    if err := json.Unmarshal(body, &manifest); err != nil {

        report.Fail(
            "not JSON",
            fmt.Sprintf("The manifest is not valid JSON: %v", err),
        )

        return
    }

    if manifest.SchemaVersion != 2 ||
        manifest.MediaType != mediaTypeOCIManifest {

        report.Fail(
            fmt.Sprintf("schemaVersion %d, mediaType %q", manifest.SchemaVersion, manifest.MediaType),
            "The manifest is not an OCI image manifest.",
        )

    } else {

        report.Pass(
            fmt.Sprintf("schemaVersion 2, %s", manifest.MediaType),
            "The manifest is an OCI image manifest.",
        )
    }

    //--------------------------------------------------
    // MARGO-APP-APPLICATIONREGISTRY-004
    //--------------------------------------------------

    validateManifestConstraints(
        &manifest,
        raw,
        report,
    )

    //--------------------------------------------------
    // MARGO-APP-APPLICATIONREGISTRY-001
    //--------------------------------------------------

    validateBlobs(
        repo,
        &manifest,
        report,
    )

    //--------------------------------------------------
    // MARGO-APP-APPLICATIONREGISTRY-002
    //--------------------------------------------------

    validateTags(
        repo,
        digest,
        app.Metadata.Version,
        report,
    )
}

// validateManifestConstraints checks the Margo-specific constraints of the
// Application Package Manifest (MARGO-APP-APPLICATIONREGISTRY-004).
func validateManifestConstraints(
    manifest *Manifest,
    raw map[string]any,
    report *ValidationReport,
) {

    const crID = "MARGO-APP-APPLICATIONREGISTRY-004"

    // artifactType
    report.Check(crID, "manifest.artifactType", "manifest", artifactTypeApp)

    if manifest.ArtifactType == artifactTypeApp {
        report.Pass(manifest.ArtifactType, "The manifest is marked as a Margo Application Package.")
    } else {
        report.Fail(orMissing(manifest.ArtifactType), "The artifactType of the manifest MUST be "+artifactTypeApp+".")
    }

    // empty config
    report.Check(crID, "manifest.config", "manifest", "Empty descriptor ("+mediaTypeEmptyConfig+", size 2)")

    if manifest.Config.MediaType == mediaTypeEmptyConfig &&
        manifest.Config.Digest == digestEmptyConfig &&
        manifest.Config.Size == 2 {

        report.Pass(manifest.Config.MediaType, "The config is the OCI empty descriptor.")

    } else {

        report.Fail(
            fmt.Sprintf("%s, %s, size %d", orMissing(manifest.Config.MediaType), manifest.Config.Digest, manifest.Config.Size),
            "The config MUST be the empty descriptor defined by the OCI Image Specification.",
        )
    }

    // exactly one Application Description layer
    report.Check(crID, "manifest.layers (Application Description)", "manifest", "Exactly one layer of mediaType "+mediaTypeDescription)

    descriptions := 0

    for _, l := range manifest.Layers {

        if l.MediaType == mediaTypeDescription {
            descriptions++
        }
    }

    if descriptions == 1 {
        report.Pass("1 layer", "The Application Description is one layer of the manifest.")
    } else {
        report.Fail(fmt.Sprintf("%d layers", descriptions), "The Application Description MUST be referred to in one element of the layers array.")
    }

    // resource layer media types
    report.Check(crID, "manifest.layers (resource media types)", "manifest", "application/vnd.margo.app.<icon|descriptionFile|licenseFile|releaseNotes>.v1+<format>")

    var badTypes []string

    for _, l := range manifest.Layers {

        if l.MediaType != mediaTypeDescription &&
            !resourceMediaType.MatchString(l.MediaType) {

            badTypes = append(badTypes, fmt.Sprintf("%s (%s)", layerTitle(l), l.MediaType))
        }
    }

    if len(badTypes) == 0 {
        report.Pass(fmt.Sprintf("%d layer(s)", len(manifest.Layers)), "Every resource layer carries a Margo-specific media type.")
    } else {
        report.Fail(strings.Join(badTypes, "; "), "Each resource layer MUST be marked with the Margo media type of its kind of resource.")
    }

    // every resource of the Application Description is a layer of its kind
    report.Check(crID, "manifest.layers (resources of the Application Description)", "manifest", "Each file in metadata.catalog.application is a layer with the media type of its kind")

    resources := catalogResources(raw)

    var missing []string

    for kind, file := range resources {

        found := false

        for _, l := range manifest.Layers {

            m := resourceMediaType.FindStringSubmatch(l.MediaType)

            if m != nil && m[1] == kind && layerTitle(l) == file {
                found = true
                break
            }
        }

        if !found {
            missing = append(missing, fmt.Sprintf("%s %s", kind, file))
        }
    }

    switch {

    case len(resources) == 0:
        report.Mark("N/A", "no resources", "The Application Description refers to no resource files.")

    case len(missing) == 0:
        report.Pass(fmt.Sprintf("%d resource(s)", len(resources)), "Each resource of the Application Description is a layer of the manifest.")

    default:
        report.Fail(strings.Join(missing, "; "), "Each Application Package part (file) MUST be listed as an element of the layers array, with the media type of its kind.")
    }

    // org.margo.app.resource annotation
    report.Check(crID, "manifest.layers (annotation "+annotationAppResource+")", "manifest", "Resource layers are annotated with "+annotationAppResource)

    var unannotated []string

    for _, l := range manifest.Layers {

        if resourceMediaType.MatchString(l.MediaType) {

            if _, ok := l.Annotations[annotationAppResource]; !ok {
                unannotated = append(unannotated, layerTitle(l))
            }
        }
    }

    if len(unannotated) == 0 {

        report.Pass("annotated", "Every resource layer carries the "+annotationAppResource+" annotation.")

    } else {

        // The specification's table makes the annotation a MUST, but its own
        // manifest example does not use it and no value is defined: a warning
        // until the specification is consistent.
        report.Mark(
            "WARN",
            strings.Join(unannotated, "; "),
            "Not annotated with "+annotationAppResource+". The table of Margo-specific annotation keys requires it, but the manifest example of the specification does not use it and defines no value, so this is reported as a warning.",
        )
    }
}

// validateBlobs downloads every blob of the manifest by digest and compares its
// digest and size with the descriptor (MARGO-APP-APPLICATIONREGISTRY-001).
func validateBlobs(
    repo *remote.Repository,
    manifest *Manifest,
    report *ValidationReport,
) {

    report.Check(
        "MARGO-APP-APPLICATIONREGISTRY-001",
        "Application Package parts (blobs)",
        "registry",
        "GET /v2/<name>/blobs/<digest> returns each layer and the config; digest and size match the descriptor",
    )

    blobs := append([]Descriptor{manifest.Config}, manifest.Layers...)

    var problems []string

    for _, b := range blobs {

        data, err := fetchBlob(repo, b)

        switch {

        case err != nil:
            problems = append(problems, fmt.Sprintf("%s: not retrievable (%v)", layerTitle(b), err))

        case sha256Digest(data) != b.Digest:
            problems = append(problems, fmt.Sprintf("%s: content digest %s", layerTitle(b), sha256Digest(data)))

        case int64(len(data)) != b.Size:
            problems = append(problems, fmt.Sprintf("%s: %d bytes, descriptor says %d", layerTitle(b), len(data), b.Size))
        }
    }

    if len(problems) == 0 {

        report.Pass(
            fmt.Sprintf("%d of %d blobs", len(blobs), len(blobs)),
            "Every part of the Application Package is a blob retrievable by its digest.",
        )

        return
    }

    report.Fail(
        strings.Join(problems, "; "),
        "All parts of the Application Package MUST be pushed as blobs according to the OCI Distribution Specification.",
    )
}

// fetchBlob downloads the blob of a descriptor by its digest.
func fetchBlob(
    repo *remote.Repository,
    d Descriptor,
) ([]byte, error) {

    dgst, err := digest.Parse(d.Digest)

    if err != nil {
        return nil, err
    }

    rc, err := repo.Blobs().Fetch(
        context.Background(),
        ocispec.Descriptor{MediaType: d.MediaType, Digest: dgst, Size: d.Size},
    )

    if err != nil {
        return nil, err
    }

    defer rc.Close()

    return io.ReadAll(rc)
}

// validateTags checks that every tag of the Application Package's manifest is
// the metadata.version of its Application Description
// (MARGO-APP-APPLICATIONREGISTRY-002).
func validateTags(
    repo *remote.Repository,
    digest string,
    version string,
    report *ValidationReport,
) {

    report.Check(
        "MARGO-APP-APPLICATIONREGISTRY-002",
        "Tags of the Application Package",
        "registry",
        "Each tag of this manifest is metadata.version ("+orMissing(version)+")",
    )

    name := repo.Reference.Registry + "/" + repo.Reference.Repository

    allTags, err := listTags(repo)

    if err != nil {

        report.Fail("tags not listed", fmt.Sprintf("The tags of %s could not be listed: %v", name, err))

        return
    }

    var tags []string
    var wrong []string

    for _, tag := range allTags {

        tagDigest, err := resolveDigest(repo, tag)

        if err != nil || tagDigest != digest {
            continue
        }

        tags = append(tags, tag)

        if tag != version {
            wrong = append(wrong, tag)
        }
    }

    switch {

    case len(tags) == 0:
        report.Fail("no tag", "No tag of "+name+" points to this manifest; it MUST be tagged with metadata.version.")

    case len(wrong) > 0:
        report.Fail(
            strings.Join(tags, ", "),
            fmt.Sprintf("Tag(s) %s are not metadata.version %q. Each tag MUST be the value of metadata.version of the Application Description.", strings.Join(wrong, ", "), version),
        )

    default:
        report.Pass(strings.Join(tags, ", "), "The tag of the Application Package is its metadata.version.")
    }
}

// ValidateSigning reports the package signing requirements:
// MARGO-APP-APPLICATIONDESCRIPTION-004 (PGP, removed from the specification) and
// MARGO-APP-APPLICATIONDESCRIPTION-008 (signed Compose package, RECOMMENDED).
func ValidateSigning(
    app *ApplicationDescription,
    report *ValidationReport,
) {

    report.Check(
        "MARGO-APP-APPLICATIONDESCRIPTION-004",
        "Package signature (PGP)",
        "signature",
        "When signing the package, PGP MUST be used",
    )

    report.Mark(
        "N/A",
        "-",
        "Not applicable: the Compose fields packageLocation and keyLocation (the public key of a PGP-signed package) were removed from the Application Description by margo/specification #200 \"Compose the OCI way\" (2026-08-21). Packages are now signed as OCI artifacts (e.g. cosign); see MARGO-APP-APPLICATIONDESCRIPTION-008.",
    )

    composeComponents := 0

    for _, profile := range app.DeploymentProfile {

        if profile.Type != "compose" {
            continue
        }

        for _, component := range profile.Components {

            composeComponents++

            report.Check(
                "MARGO-APP-APPLICATIONDESCRIPTION-008",
                fmt.Sprintf("Signature of Compose component '%s'", component.Name),
                "signature",
                "RECOMMENDED: a signature (cosign, Sigstore bundle or Notation) is attached to the Compose package in the registry",
            )

            ref := componentReference(component)

            if ref == "" {
                report.Mark("NOT RUN", "-", "The component has no repository and revision to look up.")
                continue
            }

            signature, err := findSignature(ref)

            switch {

            case err != nil:
                report.Mark("NOT RUN", "-", fmt.Sprintf("The registry could not be queried for signatures of %s: %v", ref, err))

            case signature == "":
                report.Mark("WARN", "no signature", "No signature is attached to "+ref+". It is highly recommended to digitally sign the Compose package.")

            default:
                report.Pass(signature, "A signature is attached to "+ref+".")
            }
        }
    }

    if composeComponents == 0 {

        report.Check(
            "MARGO-APP-APPLICATIONDESCRIPTION-008",
            "Signature of the Compose package",
            "signature",
            "RECOMMENDED: the Compose package is digitally signed",
        )

        report.Mark("N/A", "no compose profile", "The Application Description has no Compose deployment profile.")
    }
}

// findSignature returns a description of the signature attached to ref (an OCI
// 1.1 referrer, or a cosign signature tag sha256-<hex>.sig), or "" when there
// is none.
func findSignature(
    ref string,
) (string, error) {

    repo, err := newRepository(ref)

    if err != nil {
        return "", err
    }

    subject, err := repo.Resolve(context.Background(), repo.Reference.Reference)

    if err != nil {
        return "", err
    }

    // OCI 1.1 referrers (the Referrers API, or the referrers tag schema of a
    // registry without it).
    var signature string

    err = repo.Referrers(
        context.Background(),
        subject,
        "",
        func(referrers []ocispec.Descriptor) error {

            for _, r := range referrers {

                if signatureArtifactTypes[r.ArtifactType] && signature == "" {
                    signature = r.ArtifactType
                }
            }

            return nil
        },
    )

    if err != nil {
        return "", err
    }

    if signature != "" {
        return signature, nil
    }

    tags, err := listTags(repo)

    if err != nil {
        return "", err
    }

    cosignTag := strings.Replace(subject.Digest.String(), ":", "-", 1) + ".sig"

    for _, tag := range tags {

        if tag == cosignTag {
            return "cosign signature tag " + cosignTag, nil
        }
    }

    return "", nil
}

// componentReference returns the OCI reference (host/repository:revision) of a
// component, or "" when it has no oci:// repository.
func componentReference(
    c Component,
) string {

    repository, _ := c.Properties["repository"].(string)
    revision, _ := c.Properties["revision"].(string)

    if !strings.HasPrefix(repository, schemeOCI) {
        return ""
    }

    ref := strings.TrimPrefix(repository, schemeOCI)

    if revision != "" {
        ref += ":" + revision
    }

    return ref
}

// catalogResources returns the resource files of metadata.catalog.application,
// keyed by their kind (icon, descriptionFile, releaseNotes, licenseFile).
func catalogResources(
    raw map[string]any,
) map[string]string {

    resources := map[string]string{}

    for _, kind := range []string{"icon", "descriptionFile", "releaseNotes", "licenseFile"} {

        for _, value := range GetValues(raw, "metadata.catalog.application."+kind) {

            if file, ok := value.(string); ok && file != "" {
                resources[kind] = path.Clean(strings.TrimPrefix(file, "./"))
            }
        }
    }

    return resources
}

// layerTitle returns the file name of a layer (its title annotation), or its digest.
func layerTitle(
    d Descriptor,
) string {

    if title := d.Annotations[annotationTitle]; title != "" {
        return title
    }

    return d.Digest
}

// orMissing returns s, or "(missing)" when s is empty.
func orMissing(
    s string,
) string {

    if s == "" {
        return "(missing)"
    }

    return s
}
