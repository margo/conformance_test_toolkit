package main

// MIAF (mTLS/SPIFFE) listener for the device-supplier mock WFM — the Margo
// Management Interface as specified in workload-management-api-1.0.0-rc.3.
//
// The caller's identity comes only from its mTLS client certificate's SPIFFE
// ID. Every handler here rejects a request that is not authenticated by mTLS
// with a WFM Client X.509-SVID belonging to this WFM (spec: API Requirements
// and Security, WFM Identity Profile "Recognition by the WFM"), and every error
// is an RFC 9457 problem+json body with a registered Margo problem type.

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

const DefaultMIAFPort = ":3003"

// miafClients holds MIAF-identified clients (keyed by SPIFFE ID) separately
// from the legacy `clients` map (keyed by onboarding-issued clientId) — the
// two flows never share an identity namespace, matching the spec: MIAF has
// no clientId concept at all.
var (
	miafClients   = make(map[string]ClientData)
	miafClientsMu sync.RWMutex

	// miafKnownDeviceIDs tracks which devices have already had capabilities
	// reported, keyed by miafDeviceKey(caller, deviceId). A deviceId is not a
	// global name — it identifies a device only within the scope of one WFM
	// Client — and one client can front several deviceIds, so "create (201) or
	// update (200)" and "device not found (404)" key on both.
	miafKnownDeviceIDs   = make(map[string]bool)
	miafKnownDeviceIDsMu sync.Mutex
)

func miafDeviceKey(spiffeID, deviceID string) string {
	return spiffeID + "|" + deviceID
}

// spiffeIDFromRequest extracts the caller's SPIFFE ID from its already
// chain-validated (tls.RequireAndVerifyClientCert) client certificate. The
// spec requires exactly one URI SAN on an SVID; a cert with zero or multiple
// is rejected here rather than trusting an ambiguous identity.
func spiffeIDFromRequest(r *http.Request) (string, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return "", fmt.Errorf("no client certificate presented")
	}
	uris := r.TLS.PeerCertificates[0].URIs
	if len(uris) != 1 {
		return "", fmt.Errorf("SVID must carry exactly one URI SAN, got %d", len(uris))
	}
	if uris[0].Scheme != "spiffe" {
		return "", fmt.Errorf("URI SAN is not a spiffe:// URI: %s", uris[0].String())
	}
	return uris[0].String(), nil
}

// wfmSPIFFEID is this mock WFM's own identity, read from the URI SAN of the
// SVID it serves (MIAF_SERVER_CERT). Empty until the mTLS listener is configured.
var wfmSPIFFEID string

// recognizeWFMClient applies "Recognition by the WFM": the caller's SPIFFE ID
// must be exactly <this WFM's SPIFFE ID>/client/<wfm-client-id>.
func recognizeWFMClient(spiffeID string) error {
	if wfmSPIFFEID == "" {
		return fmt.Errorf("mTLS listener is not configured on this server")
	}
	clientID, ok := strings.CutPrefix(spiffeID, wfmSPIFFEID+"/client/")
	if !ok || clientID == "" || strings.Contains(clientID, "/") {
		return fmt.Errorf("SPIFFE ID %s is not a client of this WFM (expected %s/client/<wfm-client-id>)", spiffeID, wfmSPIFFEID)
	}
	return nil
}

// callerSPIFFEID returns the authenticated caller of a request that already
// passed requireSVID.
func callerSPIFFEID(r *http.Request) string {
	id, _ := spiffeIDFromRequest(r)
	return id
}

// requireSVID rejects any request that is not authenticated by mTLS with a
// recognized WFM Client X.509-SVID — including every request arriving on a
// listener that does not ask for a client certificate.
func requireSVID(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := spiffeIDFromRequest(r)
		if err == nil {
			err = recognizeWFMClient(id)
		}
		if err != nil {
			log.Printf("[MIAF] rejected %s %s from %s: %v", r.Method, r.URL.Path, r.RemoteAddr, err)
			respondProblem(w, r, 403, "not-authorized", "Not Authorized",
				"Management Interface requests must be authenticated by mTLS with a valid WFM Client X.509-SVID: "+err.Error())
			return
		}
		next(w, r)
	}
}

const problemTypeBase = "https://docs.margo.org/specification/problem-types#"

// respondProblem writes an RFC 9457 problem+json error. problemType is a
// fragment of the Margo problem-type registry, or "" for about:blank.
// "error" and errors[].rule_id are extension members (RFC 9457 §3.2) carrying
// the mock's own diagnostics.
func respondProblem(w http.ResponseWriter, r *http.Request, status int, problemType, title, detail string, errs ...ValidationError) {
	typeURI := "about:blank"
	if problemType != "" {
		typeURI = problemTypeBase + problemType
	}
	body := map[string]interface{}{
		"type":     typeURI,
		"title":    title,
		"status":   status,
		"detail":   detail,
		"instance": r.URL.Path,
		"error":    detail,
	}
	reasons := detail
	if len(errs) > 0 {
		fieldErrors := make([]map[string]string, 0, len(errs))
		for _, e := range errs {
			fieldErrors = append(fieldErrors, map[string]string{"message": e.Error, "rule_id": e.RuleID})
			reasons += " | " + e.RuleID + ": " + e.Error
		}
		body["errors"] = fieldErrors
	}
	log.Printf("[MIAF] answered %d %s — %s", status, title, reasons)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

// respondValidationProblem reports request-body validation failures: 422
// semantic-error, or 400 invalid-request where assertions.json says so.
func respondValidationProblem(w http.ResponseWriter, r *http.Request, endpointKey string, errs []ValidationError) {
	status, _ := validationErrorResponse(endpointKey, errs)
	if status == 400 {
		respondProblem(w, r, 400, "invalid-request", "Invalid Request", errs[0].Error, errs...)
		return
	}
	respondProblem(w, r, 422, "semantic-error", "Semantic Error", "Request body includes a semantic error.", errs...)
}

func respondMalformedBody(w http.ResponseWriter, r *http.Request, detail string) {
	respondProblem(w, r, 400, "invalid-request", "Invalid Request", detail)
}

// wfmClientID is the <wfm-client-id> segment of a WFM Client SPIFFE ID.
func wfmClientID(spiffeID string) string {
	return spiffeID[strings.LastIndex(spiffeID, "/")+1:]
}

// newMIAFClient is a client's starting state. Its deployments are assigned to
// deviceID — the device it is reporting capabilities for on first contact, or
// its wfm-client-id when its first request names no device. The assignment
// never changes afterwards, which keeps every served digest stable.
func newMIAFClient(spiffeID, deviceID string) ClientData {
	if deviceID == "" {
		deviceID = wfmClientID(spiffeID)
	}
	return ClientData{ID: spiffeID, OnboardedAt: time.Now(), DeviceID: deviceID}
}

// getOrCreateMIAFClient auto-provisions a client record on its first
// authenticated request: MIAF has no onboarding call, so a recognized mTLS
// connection is what admits the client. A new client starts with the default
// deployment assigned, at manifestVersion 1.
func getOrCreateMIAFClient(spiffeID, deviceID string) ClientData {
	miafClientsMu.Lock()
	defer miafClientsMu.Unlock()
	client, exists := miafClients[spiffeID]
	if !exists {
		client = newMIAFClient(spiffeID, deviceID)
		client.DeploymentsData = []string{defaultDeploymentID}
		client.ManifestVersion = 1
		miafClients[spiffeID] = client
		mu.Lock()
		ensureDefaultDeployment(spiffeID)
		mu.Unlock()
	}
	return client
}

// buildStateManifestMIAF builds the State Manifest for a client whose
// deployments are assigned to deviceID.
func buildStateManifestMIAF(deviceID string, deploymentIDs []string, manifestVersion int, baseURL string) (map[string]interface{}, string, error) {
	refs := make([]interface{}, 0, len(deploymentIDs))
	bundle := interface{}(nil)

	if len(deploymentIDs) > 0 {
		bundleBytes, err := buildBundleArchive(deviceID, deploymentIDs, baseURL)
		if err != nil {
			return nil, "", err
		}
		bundleDigest := sha256Hex(bundleBytes)
		bundle = map[string]interface{}{
			"mediaType": "application/vnd.margo.bundle.v1+tar+gzip",
			"digest":    "sha256:" + bundleDigest,
			"sizeBytes": len(bundleBytes),
			"url":       fmt.Sprintf("/api/v1/bundles/sha256:%s", bundleDigest),
		}
		for _, deploymentID := range deploymentIDs {
			yamlBytes := buildDeploymentYAML(deviceID, deploymentID, baseURL)
			deploymentDigest := sha256Hex(yamlBytes)
			refs = append(refs, map[string]interface{}{
				"deploymentId": deploymentID,
				"digest":       "sha256:" + deploymentDigest,
				"sizeBytes":    len(yamlBytes),
				"url":          fmt.Sprintf("/api/v1/deployments/%s/sha256:%s", deploymentID, deploymentDigest),
			})
		}
	}

	manifest := map[string]interface{}{
		"manifestVersion": manifestVersion,
		"bundle":          bundle,
		"deployments":     refs,
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", err
	}
	return manifest, sha256Hex(manifestBytes), nil
}

// PUT /v1alpha2/margo/api/v1/capabilities/{deviceId}
func handleMIAFPutCapabilities(w http.ResponseWriter, r *http.Request) {
	deviceID := mux.Vars(r)["deviceId"]
	spiffeID := callerSPIFFEID(r)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondMalformedBody(w, r, "Invalid request body")
		return
	}
	defer r.Body.Close()

	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		respondMalformedBody(w, r, "Invalid JSON body")
		return
	}

	// Reuses the same data-driven validation as the legacy endpoint — the
	// capabilities body shape didn't change under MIAF, only how the caller
	// is identified.
	errors := validateRequest("POST_capabilities", body)
	if len(errors) > 0 {
		respondValidationProblem(w, r, "POST_capabilities", errors)
		return
	}

	client := getOrCreateMIAFClient(spiffeID, deviceID)
	client.Capabilities = body
	miafClientsMu.Lock()
	miafClients[spiffeID] = client
	miafClientsMu.Unlock()

	miafKnownDeviceIDsMu.Lock()
	wasKnown := miafKnownDeviceIDs[miafDeviceKey(spiffeID, deviceID)]
	miafKnownDeviceIDs[miafDeviceKey(spiffeID, deviceID)] = true
	miafKnownDeviceIDsMu.Unlock()

	log.Printf("[MIAF/Capabilities] accepted for %s (deviceId=%s)", spiffeID, deviceID)
	// Status code distinguishes create (201) vs update (200) per spec (Part 5.4);
	// the body's "status" message stays constant so callers don't need to branch
	// on it — same convention as the legacy handler's single always-201 message.
	if wasKnown {
		respondJSON(w, 200, map[string]string{"status": "capabilities_received"})
	} else {
		respondJSON(w, 201, map[string]string{"status": "capabilities_received"})
	}
}

// DELETE /v1alpha2/margo/api/v1/capabilities/{deviceId}
func handleMIAFDeleteCapabilities(w http.ResponseWriter, r *http.Request) {
	key := miafDeviceKey(callerSPIFFEID(r), mux.Vars(r)["deviceId"])
	miafKnownDeviceIDsMu.Lock()
	exists := miafKnownDeviceIDs[key]
	delete(miafKnownDeviceIDs, key)
	miafKnownDeviceIDsMu.Unlock()
	if !exists {
		respondProblem(w, r, 404, "device-not-found", "Device Not Found", "No device with the given deviceId was found for the client.")
		return
	}
	w.WriteHeader(204)
}

// GET /v1alpha2/margo/api/v1/deployments
func handleMIAFGetDeployments(w http.ResponseWriter, r *http.Request) {
	spiffeID := callerSPIFFEID(r)
	if !acceptsManifest(r.Header.Get("Accept")) {
		respondProblem(w, r, 406, "server-cannot-generate-response", "Server Cannot Generate Response",
			"Supported manifest format: application/vnd.margo.manifest.v1+json")
		return
	}

	client := getOrCreateMIAFClient(spiffeID, "")
	manifestVersion := client.ManifestVersion
	if manifestVersion == 0 {
		manifestVersion = 1
	}
	manifest, _, err := buildStateManifestMIAF(client.DeviceID, client.DeploymentsData, manifestVersion, requestBaseURL(r))
	if err != nil {
		respondProblem(w, r, 500, "", "Internal Server Error", "Failed to build deployment manifest")
		return
	}

	if client.NegativeFixture != FixtureNone {
		manifest = applyNegativeFixture(manifest, client.NegativeFixture)
	}

	// The ETag is the digest ("sha256:<hex>") of the exact bytes sent as the body.
	body, err := json.Marshal(manifest)
	if err != nil {
		respondProblem(w, r, 500, "", "Internal Server Error", "Failed to serialize deployment manifest")
		return
	}
	etag := "sha256:" + sha256Hex(body)

	if normalizeETag(r.Header.Get("If-None-Match")) == etag {
		w.Header().Set("ETag", quoteETag(etag))
		w.Header().Set("Cache-Control", "private")
		w.WriteHeader(304)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.margo.manifest.v1+json")
	w.Header().Set("ETag", quoteETag(etag))
	w.Header().Set("Cache-Control", "private")
	w.WriteHeader(200)
	w.Write(body)
}

// GET /v1alpha2/margo/api/v1/bundles/{digest}
func handleMIAFGetBundle(w http.ResponseWriter, r *http.Request) {
	spiffeID := callerSPIFFEID(r)
	digest := mux.Vars(r)["digest"]
	client := getOrCreateMIAFClient(spiffeID, "")

	bundleBytes, err := buildBundleArchive(client.DeviceID, client.DeploymentsData, requestBaseURL(r))
	if err != nil {
		respondProblem(w, r, 500, "", "Internal Server Error", "Failed to build deployment bundle")
		return
	}
	expectedDigest := sha256Hex(bundleBytes)
	if strings.TrimPrefix(digest, "sha256:") != expectedDigest {
		respondProblem(w, r, 404, "invalid-bundle", "Invalid Bundle", fmt.Sprintf("Bundle not found for digest: %s", digest))
		return
	}
	if normalizeETag(r.Header.Get("If-None-Match")) == digest {
		w.Header().Set("ETag", quoteETag(digest))
		w.WriteHeader(304)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.margo.bundle.v1+tar+gzip")
	w.Header().Set("ETag", quoteETag(digest))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(200)
	w.Write(bundleBytes)
}

// GET /v1alpha2/margo/api/v1/deployments/{deploymentId}/{digest}
func handleMIAFGetDeploymentManifest(w http.ResponseWriter, r *http.Request) {
	spiffeID := callerSPIFFEID(r)
	vars := mux.Vars(r)
	deploymentID, digest := vars["deploymentId"], vars["digest"]
	client := getOrCreateMIAFClient(spiffeID, "")

	found := false
	for _, id := range client.DeploymentsData {
		if id == deploymentID {
			found = true
			break
		}
	}
	if !found {
		respondProblem(w, r, 404, "deployment-not-found", "Deployment Not Found", fmt.Sprintf("Deployment not found: %s", deploymentID))
		return
	}

	yamlBytes := buildDeploymentYAML(client.DeviceID, deploymentID, requestBaseURL(r))
	expectedDigest := sha256Hex(yamlBytes)
	if strings.TrimPrefix(digest, "sha256:") != expectedDigest {
		respondProblem(w, r, 404, "deployment-not-found", "Deployment Not Found", fmt.Sprintf("Deployment not found for digest: %s", digest))
		return
	}
	if normalizeETag(r.Header.Get("If-None-Match")) == digest {
		w.Header().Set("ETag", quoteETag(digest))
		w.WriteHeader(304)
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("ETag", quoteETag(digest))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Vary", "Accept-Encoding")
	w.WriteHeader(200)
	w.Write(yamlBytes)
}

// POST /v1alpha2/margo/api/v1/deployments/{deploymentId}/status
func handleMIAFPostStatus(w http.ResponseWriter, r *http.Request) {
	spiffeID := callerSPIFFEID(r)
	deploymentID := mux.Vars(r)["deploymentId"]

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondMalformedBody(w, r, "Invalid request body")
		return
	}
	defer r.Body.Close()

	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		respondMalformedBody(w, r, "Invalid JSON body")
		return
	}

	if bodyDeploymentID, ok := getFieldValue(body, "deploymentId"); ok {
		if s, ok := bodyDeploymentID.(string); !ok || s != deploymentID {
			respondProblem(w, r, 422, "semantic-error", "Semantic Error", "Request body includes a semantic error.",
				ValidationError{RuleID: "status-path-001", Error: "deploymentId in body must match deploymentId in path"})
			return
		}
	}

	// rc.3: adoptedManifestVersion is required on every status report.
	if _, ok := getFieldValue(body, "adoptedManifestVersion"); !ok {
		respondProblem(w, r, 422, "semantic-error", "Semantic Error", "Request body includes a semantic error.",
			ValidationError{RuleID: "status-miaf-001", Error: "adoptedManifestVersion is required"})
		return
	}

	errors := validateRequest("POST_status", body)
	if len(errors) > 0 {
		respondValidationProblem(w, r, "POST_status", errors)
		return
	}

	mu.Lock()
	key := deploymentKey(spiffeID, deploymentID)
	deployment, exists := deployments[key]
	if !exists {
		deployment = DeploymentData{ID: deploymentID, ClientID: spiffeID}
	}
	deployment.StatusHistory = append(deployment.StatusHistory, body)
	deployments[key] = deployment
	mu.Unlock()

	log.Printf("[MIAF/Status] update for deployment %s from %s", deploymentID, spiffeID)
	respondJSON(w, 200, map[string]string{"acknowledgement": "received"})
}

// PUT /v1alpha2/margo/api/v1/test/deployments — MIAF equivalent of
// handleTestSetDeployments (main.go), keyed by SPIFFE ID instead of clientId.
// NOT part of the Margo spec: lets the suite's own self-tests (device-mi008,
// -mi006, -mi019, -mi010, -mi025/026) arm a negative fixture or script a
// desired-state timeline against a real device-agent under mTLS, exactly the
// way the legacy test-control endpoint does for RFC 9421 clients.
func handleMIAFTestSetDeployments(w http.ResponseWriter, r *http.Request) {
	spiffeID := callerSPIFFEID(r)

	var body struct {
		DeploymentIDs        []string        `json:"deploymentIds"`
		NegativeFixture      NegativeFixture `json:"negativeFixture,omitempty"`
		ResetManifestVersion bool            `json:"resetManifestVersion,omitempty"`
	}
	if decErr := json.NewDecoder(r.Body).Decode(&body); decErr != nil {
		respondMalformedBody(w, r, `Invalid JSON body: expected {"deploymentIds": [...], "negativeFixture"?: "<name>", "resetManifestVersion"?: true}`)
		return
	}
	if !isKnownFixture(body.NegativeFixture) {
		respondMalformedBody(w, r, fmt.Sprintf("Unknown negativeFixture: %q (see negative_fixtures.go)", body.NegativeFixture))
		return
	}
	newIDs := body.DeploymentIDs
	if newIDs == nil {
		newIDs = []string{}
	}

	miafClientsMu.Lock()
	client := miafClients[spiffeID]
	if client.ID == "" {
		client = newMIAFClient(spiffeID, "")
	}
	if client.ManifestVersion == 0 || body.ResetManifestVersion {
		// resetManifestVersion is not part of the Margo spec — it exists only
		// so a scenario that (like the pre-MIAF flow's own dedicated onboarded
		// client) needs a known, isolated starting point can establish one
		// itself, regardless of what earlier scenarios did to this suite's
		// single shared identity under MIAF (which has no per-run onboarding
		// to naturally reset state at).
		// Always set to 2: 1 (base) + 1 (for the deployment change), so callers
		// get a deterministic starting version regardless of prior state.
		client.ManifestVersion = 2
		client.DeploymentsData = newIDs
		for _, id := range newIDs {
			ensureDeployment(spiffeID, id)
		}
	} else if !stringSetsEqual(client.DeploymentsData, newIDs) {
		client.ManifestVersion++
		client.DeploymentsData = newIDs
		for _, id := range newIDs {
			ensureDeployment(spiffeID, id)
		}
	}
	client.NegativeFixture = body.NegativeFixture
	miafClients[spiffeID] = client
	version := client.ManifestVersion
	miafClientsMu.Unlock()

	log.Printf("[MIAF/TestControl] %s desired state set to %v (manifestVersion=%d, negativeFixture=%q)", spiffeID, newIDs, version, body.NegativeFixture)

	respondJSON(w, 200, map[string]interface{}{
		"deployments":     newIDs,
		"manifestVersion": version,
		"negativeFixture": body.NegativeFixture,
	})
}

// loadWFMSPIFFEID reads this WFM's own identity from the SVID it serves: the
// certificate must carry exactly one URI SAN of the form
// spiffe://<trust-domain>/margo/wfm/<wfm-id>.
func loadWFMSPIFFEID(certFile, keyFile string) (string, error) {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return "", err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return "", err
	}
	if len(leaf.URIs) != 1 || leaf.URIs[0].Scheme != "spiffe" {
		return "", fmt.Errorf("%s is not an X.509-SVID: it must carry exactly one spiffe:// URI SAN", certFile)
	}
	id := leaf.URIs[0].String()
	parts := strings.Split(strings.TrimPrefix(leaf.URIs[0].Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "margo" || parts[1] != "wfm" || parts[2] == "" {
		return "", fmt.Errorf("%s carries %s, which is not a WFM identity (expected spiffe://<trust-domain>/margo/wfm/<wfm-id>)", certFile, id)
	}
	return id, nil
}

// startMIAFServer registers the Management Interface routes and starts the
// mTLS listener (port 3003) that serves them. The router is shared with the
// plain-TLS port, where requireSVID turns every one of these routes into a 403.
func startMIAFServer(router *mux.Router) {
	router.HandleFunc("/v1alpha2/margo/api/v1/capabilities/{deviceId}", requireSVID(handleMIAFPutCapabilities)).Methods("PUT")
	router.HandleFunc("/v1alpha2/margo/api/v1/capabilities/{deviceId}", requireSVID(handleMIAFDeleteCapabilities)).Methods("DELETE")
	router.HandleFunc("/v1alpha2/margo/api/v1/deployments", requireSVID(handleMIAFGetDeployments)).Methods("GET")
	router.HandleFunc("/v1alpha2/margo/api/v1/bundles/{digest}", requireSVID(handleMIAFGetBundle)).Methods("GET")
	router.HandleFunc("/v1alpha2/margo/api/v1/deployments/{deploymentId}/{digest}", requireSVID(handleMIAFGetDeploymentManifest)).Methods("GET")
	router.HandleFunc("/v1alpha2/margo/api/v1/deployments/{deploymentId}/status", requireSVID(handleMIAFPostStatus)).Methods("POST")
	router.HandleFunc("/v1alpha2/margo/api/v1/test/deployments", requireSVID(handleMIAFTestSetDeployments)).Methods("PUT")

	certFile := os.Getenv("MIAF_SERVER_CERT")
	keyFile := os.Getenv("MIAF_SERVER_KEY")
	caFile := os.Getenv("MIAF_TRUST_CA")
	if certFile == "" || keyFile == "" || caFile == "" {
		log.Printf("[MIAF] mTLS listener disabled — set MIAF_SERVER_CERT, MIAF_SERVER_KEY and MIAF_TRUST_CA. Management Interface requests will be rejected until it is enabled.")
		return
	}

	id, err := loadWFMSPIFFEID(certFile, keyFile)
	if err != nil {
		log.Fatalf("[MIAF] cannot use the WFM SVID: %v", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		log.Fatalf("[MIAF] cannot read trust CA %s: %v", caFile, err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		log.Fatalf("[MIAF] no valid certificates in trust CA %s", caFile)
	}
	wfmSPIFFEID = id

	port := os.Getenv("MIAF_PORT")
	if port == "" {
		port = DefaultMIAFPort
	}

	tlsConfig := &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  caPool,
		MinVersion: tls.VersionTLS12, // TLS 1.3 default per spec; negotiated automatically, 1.2 allowed as fallback
		// Recognition by the WFM: reject the connection itself when the chain-valid
		// SVID is not one of this WFM's clients.
		VerifyPeerCertificate: func(_ [][]byte, chains [][]*x509.Certificate) error {
			if len(chains) == 0 || len(chains[0]) == 0 {
				return fmt.Errorf("no verified client certificate")
			}
			uris := chains[0][0].URIs
			if len(uris) != 1 || uris[0].Scheme != "spiffe" {
				return fmt.Errorf("client certificate must carry exactly one spiffe:// URI SAN")
			}
			return recognizeWFMClient(uris[0].String())
		},
	}
	server := &http.Server{Addr: port, Handler: router, TLSConfig: tlsConfig}

	go func() {
		log.Printf("🔐 MIAF (mTLS) listener on https://localhost%s as %s", port, wfmSPIFFEID)
		if err := server.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[MIAF] listener stopped: %v", err)
		}
	}()
}
