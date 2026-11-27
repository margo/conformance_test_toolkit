package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The request journal records every Management Interface request the mock WFM
// answered: who sent it, what was sent, and what was answered. The test runner
// reads it (GET /api/v1/test/requests) to judge what a real device did — a
// real device only ever calls out, so observing its requests is the only
// evidence there is.

const (
	journalMaxEntries   = 5000
	journalMaxBodyBytes = 64 * 1024
	miPathPrefix        = "/v1alpha2/margo"
)

// JournalEntry is one answered request.
type JournalEntry struct {
	Seq    int          `json:"seq"`
	Time   string       `json:"time"`
	Client string       `json:"client"` // caller's SPIFFE ID; "" when it presented no usable SVID
	Remote string       `json:"remote"` // caller's IP address
	Cert   *JournalCert `json:"cert,omitempty"`
	// ServerIdentity is "svid", or the test window this connection was served
	// in: "untrusted" (a certificate the MIS did not issue), "legacy-tls" (only
	// TLS 1.0/1.1) or "rc4" (TLS 1.2 with RC4 only) — a device must never get this far.
	ServerIdentity string      `json:"server_identity"`
	Proto          string      `json:"proto,omitempty"` // HTTP version of the request, e.g. HTTP/1.1
	TLS            *JournalTLS `json:"tls,omitempty"`
	// Expectations: the verdict of each assertions.json device_expectations rule
	// for this request — "pass", or what is wrong.
	Expectations map[string]string `json:"expectations,omitempty"`
	Method       string            `json:"method"`
	Path         string            `json:"path"` // without the /v1alpha2/margo prefix
	Status       int               `json:"status"`
	Headers      map[string]string `json:"headers"`
	// ContentEncoding: the Content-Encoding the answer was sent with (e.g. gzip).
	ContentEncoding string      `json:"content_encoding,omitempty"`
	Body            interface{} `json:"body,omitempty"`     // request body (JSON when it parses)
	Response        interface{} `json:"response,omitempty"` // response body when it is JSON (manifest, problem details)
}

// JournalCert describes the client certificate a request was sent with.
type JournalCert struct {
	SHA256   string `json:"sha256"`
	URISANs  int    `json:"uri_sans"`
	IsCA     bool   `json:"is_ca"`
	NotAfter string `json:"not_after"`
	Key      string `json:"key"`       // e.g. "ECDSA P-256", "Ed25519", "RSA 3072"
	KeyType  string `json:"key_type"`  // "ECDSA", "EdDSA", "RSA"
	KeyBits  int    `json:"key_bits"`  // key size in bits
	SigAlg   string `json:"sig_alg"`   // signature algorithm of the certificate, e.g. ECDSA-SHA256
	ChainLen int    `json:"chain_len"` // certificates the device presented (leaf first)
}

// JournalTLS describes the TLS connection a request (or a failed handshake) came over.
type JournalTLS struct {
	Version         string   `json:"version,omitempty"`          // negotiated, e.g. "TLS 1.3"
	Cipher          string   `json:"cipher,omitempty"`           // negotiated cipher suite
	OfferedVersions []string `json:"offered_versions,omitempty"` // versions in the device's ClientHello
	ServerKey       string   `json:"server_key,omitempty"`       // key of the SVID the mock WFM presented
	ServerSigAlg    string   `json:"server_sig_alg,omitempty"`   // signature algorithm of that SVID
}

// describeKey names a certificate's public key the way JournalCert reports it.
func describeKey(cert *x509.Certificate) (key, keyType string, bits int) {
	switch k := cert.PublicKey.(type) {
	case *ecdsa.PublicKey:
		return "ECDSA " + k.Curve.Params().Name, "ECDSA", k.Curve.Params().BitSize
	case ed25519.PublicKey:
		return "Ed25519", "EdDSA", 256
	case *rsa.PublicKey:
		return "RSA " + strconv.Itoa(k.N.BitLen()), "RSA", k.N.BitLen()
	}
	return cert.PublicKeyAlgorithm.String(), cert.PublicKeyAlgorithm.String(), 0
}

// ── TLS versions each connection's ClientHello offered, by remote "ip:port" ──

var (
	offersMu sync.Mutex
	offers   = map[string][]string{}
)

func recordOffer(addr string, versions []uint16) {
	names := make([]string, 0, len(versions))
	for _, v := range versions {
		names = append(names, tls.VersionName(v))
	}
	offersMu.Lock()
	if len(offers) > 10000 {
		offers = map[string][]string{}
	}
	offers[addr] = names
	offersMu.Unlock()
}

func offerOf(addr string) []string {
	offersMu.Lock()
	defer offersMu.Unlock()
	return offers[addr]
}

// wfmCertKey / wfmCertSigAlg describe the mock WFM's own SVID (set at start).
var wfmCertKey, wfmCertSigAlg string

var (
	journalMu      sync.Mutex
	journalEntries []JournalEntry
	journalSeq     int
)

// journalWriter keeps the status and the first bytes of the response body.
type journalWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *journalWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *journalWriter) Write(b []byte) (int, error) {
	if room := journalMaxBodyBytes - w.body.Len(); room > 0 {
		if len(b) < room {
			room = len(b)
		}
		w.body.Write(b[:room])
	}
	return w.ResponseWriter.Write(b)
}

func jsonOrNil(b []byte) interface{} {
	var v interface{}
	if len(b) == 0 || json.Unmarshal(b, &v) != nil {
		return nil
	}
	return v
}

// journaled wraps a handler so its request and answer are recorded. The
// test-control routes are not recorded: they are the harness, not the device.
func journaled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, miPathPrefix)
		if !strings.HasPrefix(path, "/api/v1/") || strings.HasPrefix(path, "/api/v1/test/") {
			next.ServeHTTP(w, r)
			return
		}
		var reqBody []byte
		if r.Body != nil {
			reqBody, _ = io.ReadAll(io.LimitReader(r.Body, journalMaxBodyBytes))
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(reqBody), r.Body))
		}
		rec := &journalWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)

		client, _ := spiffeIDFromRequest(r)
		remote, _, _ := net.SplitHostPort(r.RemoteAddr)
		serverIdentity := "svid"
		if v, ok := connIdentity.Load(r.RemoteAddr); ok {
			serverIdentity = v.(string)
		}
		var cert *JournalCert
		var tlsInfo *JournalTLS
		if r.TLS != nil {
			tlsInfo = &JournalTLS{Version: tls.VersionName(r.TLS.Version), Cipher: tls.CipherSuiteName(r.TLS.CipherSuite), OfferedVersions: offerOf(r.RemoteAddr)}
			if serverIdentity == "svid" {
				tlsInfo.ServerKey, tlsInfo.ServerSigAlg = wfmCertKey, wfmCertSigAlg
			}
		}
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			leaf := r.TLS.PeerCertificates[0]
			sum := sha256.Sum256(leaf.Raw)
			key, keyType, bits := describeKey(leaf)
			cert = &JournalCert{SHA256: hex.EncodeToString(sum[:]), URISANs: len(leaf.URIs), IsCA: leaf.IsCA, NotAfter: leaf.NotAfter.UTC().Format(time.RFC3339),
				Key: key, KeyType: keyType, KeyBits: bits, SigAlg: leaf.SignatureAlgorithm.String(), ChainLen: len(r.TLS.PeerCertificates)}
		}
		var expectations map[string]string
		if obj, ok := jsonOrNil(reqBody).(map[string]interface{}); ok {
			switch {
			case r.Method == "PUT" && strings.HasPrefix(path, "/api/v1/capabilities/"):
				expectations = evaluateExpectations("PUT_capabilities", obj, nil)
			case r.Method == "POST" && strings.HasSuffix(path, "/status"):
				deploymentID := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/deployments/"), "/status")
				expectations = evaluateExpectations("POST_status", obj, servedDeploymentFacts(deploymentID))
			}
		}
		entry := JournalEntry{
			Expectations:   expectations,
			Time:           time.Now().UTC().Format(time.RFC3339),
			Client:         client,
			Remote:         remote,
			Cert:           cert,
			ServerIdentity: serverIdentity,
			Proto:          r.Proto,
			TLS:            tlsInfo,
			Method:         r.Method,
			Path:           path,
			Status:         rec.status,
			Headers: map[string]string{
				"Accept":          r.Header.Get("Accept"),
				"If-None-Match":   r.Header.Get("If-None-Match"),
				"Content-Type":    r.Header.Get("Content-Type"),
				"Accept-Encoding": r.Header.Get("Accept-Encoding"),
			},
			ContentEncoding: rec.Header().Get("Content-Encoding"),
			Body:            jsonOrNil(reqBody),
			Response:        jsonOrNil(rec.body.Bytes()),
		}
		appendJournal(entry)
	})
}

func appendJournal(entry JournalEntry) {
	journalMu.Lock()
	journalSeq++
	entry.Seq = journalSeq
	journalEntries = append(journalEntries, entry)
	if len(journalEntries) > journalMaxEntries {
		journalEntries = journalEntries[len(journalEntries)-journalMaxEntries:]
	}
	journalMu.Unlock()
}

// ── A TLS handshake that fails never becomes a request. It is recorded as an
// entry of its own (method "TLS", path "/handshake-error"), so the runner can
// tell "the device refused the connection" from "the device did not try". ──

var handshakeErrorLine = regexp.MustCompile(`TLS handshake error from (([^:]+|\[[^\]]+\]):\d+): (.*)`)

type handshakeErrorRecorder struct{}

func (handshakeErrorRecorder) Write(p []byte) (int, error) {
	os.Stderr.Write(p)
	if m := handshakeErrorLine.FindSubmatch(p); m != nil {
		addr := string(m[1])
		identity := "svid"
		if v, ok := connIdentity.Load(addr); ok {
			identity = v.(string)
		}
		var tlsInfo *JournalTLS
		if offered := offerOf(addr); offered != nil {
			tlsInfo = &JournalTLS{OfferedVersions: offered}
		}
		appendJournal(JournalEntry{
			Time: time.Now().UTC().Format(time.RFC3339), Remote: strings.Trim(string(m[2]), "[]"),
			Method: "TLS", Path: "/handshake-error", ServerIdentity: identity, TLS: tlsInfo,
			Headers: map[string]string{}, Body: strings.TrimSpace(string(m[3])),
		})
	}
	return len(p), nil
}

// ── Untrusted WFM identity window ──

var (
	untrustedMu    sync.Mutex
	untrustedUntil time.Time
	legacyTLSUntil time.Time // only TLS 1.0/1.1 is offered
	rc4Until       time.Time // only TLS 1.2 with RC4 cipher suites is offered
	untrustedCert  *tls.Certificate
	connIdentity   sync.Map // remote "ip:port" → test window it was served in ("untrusted", "legacy-tls", "rc4", or a svidWindows name)
)

// svidWindow presents another MIS-issued SVID: valid chain, wrong WFM identity.
type svidWindow struct {
	name     string // server_identity recorded in the journal
	key      string // test-control key
	certFile string // in the certs folder (next to MIAF_SERVER_CERT)
	keyFile  string
	until    time.Time
	cert     *tls.Certificate
}

var svidWindows = []*svidWindow{
	{name: "other-wfm", key: "otherWfmSeconds", certFile: "other-wfm-server-svid-cert.pem", keyFile: "other-wfm-server-svid-key.pem"},
	{name: "client-path", key: "clientPathSeconds", certFile: "svid-cert.pem", keyFile: "svid-key.pem"},
	{name: "expired", key: "expiredSeconds", certFile: "expired-wfm-server-svid-cert.pem", keyFile: "expired-wfm-server-svid-key.pem"},
}

// activeSVIDWindow returns the SVID window open now, if any.
func activeSVIDWindow() *svidWindow {
	untrustedMu.Lock()
	defer untrustedMu.Unlock()
	for _, w := range svidWindows {
		if time.Now().Before(w.until) && w.cert != nil {
			return w
		}
	}
	return nil
}

func untrustedIdentityActive() bool {
	untrustedMu.Lock()
	defer untrustedMu.Unlock()
	return time.Now().Before(untrustedUntil)
}

func legacyTLSActive() bool {
	untrustedMu.Lock()
	defer untrustedMu.Unlock()
	return time.Now().Before(legacyTLSUntil)
}

func rc4Active() bool {
	untrustedMu.Lock()
	defer untrustedMu.Unlock()
	return time.Now().Before(rc4Until)
}

// untrustedServerCertificate is self-signed and carries the WFM's own SPIFFE
// ID: everything about it is right except that the MIS did not issue it.
func untrustedServerCertificate(spiffeID string) (tls.Certificate, error) {
	untrustedMu.Lock()
	defer untrustedMu.Unlock()
	if untrustedCert != nil {
		return *untrustedCert, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	id, err := url.Parse(spiffeID)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "ctt-untrusted-wfm"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, URIs: []*url.URL{id},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	untrustedCert = &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return *untrustedCert, nil
}

// PUT /v1alpha2/margo/api/v1/test/server-identity
// {"untrustedSeconds": N} | {"legacyTLSSeconds": N} | {"rc4Seconds": N} |
// {"otherWfmSeconds": N} | {"clientPathSeconds": N} | {"expiredSeconds": N} —
// for the next N seconds new connections get the untrusted certificate, only
// TLS 1.0/1.1, only TLS 1.2 with RC4 cipher suites, or one of the svidWindows
// SVIDs (404 when that SVID is not installed in the certs folder).
func handleTestServerIdentity(w http.ResponseWriter, r *http.Request) {
	const usage = `expected one key: untrustedSeconds, legacyTLSSeconds, rc4Seconds, otherWfmSeconds, clientPathSeconds or expiredSeconds (0..600)`
	var body map[string]int
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 1 {
		respondMalformedBody(w, r, usage)
		return
	}
	windows := map[string]*time.Time{"untrustedSeconds": &untrustedUntil, "legacyTLSSeconds": &legacyTLSUntil, "rc4Seconds": &rc4Until}
	for _, sw := range svidWindows {
		windows[sw.key] = &sw.until
	}
	for key, seconds := range body {
		target, known := windows[key]
		if !known || seconds < 0 || seconds > 600 {
			respondMalformedBody(w, r, usage)
			return
		}
		for _, sw := range svidWindows {
			if sw.key != key {
				continue
			}
			dir := filepath.Dir(os.Getenv("MIAF_SERVER_CERT"))
			pair, err := tls.LoadX509KeyPair(filepath.Join(dir, sw.certFile), filepath.Join(dir, sw.keyFile))
			if err != nil {
				respondProblem(w, r, 404, "", "Not Found", "This test needs an optional MIS-issued SVID that is not installed: "+filepath.Join(dir, sw.certFile))
				return
			}
			untrustedMu.Lock()
			sw.cert = &pair
			untrustedMu.Unlock()
		}
		untrustedMu.Lock()
		*target = time.Now().Add(time.Duration(seconds) * time.Second)
		until := *target
		untrustedMu.Unlock()
		log.Printf("[MIAF/TestControl] %s=%d: test window until %s", key, seconds, until.Format(time.RFC3339))
		respondJSON(w, 200, map[string]interface{}{key: seconds, "until": until.UTC().Format(time.RFC3339)})
	}
}

// GET /v1alpha2/margo/api/v1/test/requests?since=<seq> — entries after <seq>.
func handleTestRequests(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.Atoi(r.URL.Query().Get("since"))
	journalMu.Lock()
	out := make([]JournalEntry, 0)
	for _, e := range journalEntries {
		if e.Seq > since {
			out = append(out, e)
		}
	}
	last := journalSeq
	journalMu.Unlock()
	respondJSON(w, 200, map[string]interface{}{"requests": out, "last": last})
}
