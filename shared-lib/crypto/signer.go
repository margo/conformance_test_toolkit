package crypto

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/lestrrat-go/htmsig/component"
	htmsighttp "github.com/lestrrat-go/htmsig/http"
)

type HTTPSigner interface {
	SignRequest(ctx context.Context, req *http.Request) error
	SignResponse(ctx context.Context, resp http.ResponseWriter) error
}

type HTMPayloadSigner struct {
	privateKey []byte
	signer     htmsighttp.Signer
	// configuration echoed from RequestSignerConfig
	signatureAlgo   string
	hashAlgo        string
	signatureFormat string
}

func NewSignerFromFile(filepath, signatureAlgo, hashAlgo, signatureFormat string) (HTTPSigner, error) {
	keyPath := filepath
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read request signer key from %s: %w", keyPath, err)
	}
	// derive a deterministic keyid from the private key so signatures include a stable key identifier
	keyid, err := ComputeKeyIDFromPrivateKeyPEM(string(keyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to derive keyid from private key: %w", err)
	}

	return NewSigner(string(keyBytes), keyid,
		signatureAlgo,
		hashAlgo,
		signatureFormat)
}

// NewSigner creates a signer. The signatureAlgo, hashAlgo, and format are
// currently not all mapped; for now we support defaulting to rsa/ecdsa with
// sha-256 and the default signature format. This function accepts a keyid
// parameter which will be included in the produced Signature header.
func NewSigner(privateKeyPEM string, keyid string, signatureAlgo string, hashAlgo string, signatureFormat string) (HTTPSigner, error) {
	// validate basic config values (we keep mapping to htmsig minimal for now)
	switch strings.ToLower(signatureAlgo) {
	case "", "auto", "rsa", "ecdsa":
		// allowed
	default:
		return nil, fmt.Errorf("unsupported signatureAlgo: %s", signatureAlgo)
	}

	switch strings.ToLower(hashAlgo) {
	case "", "sha256", "sha512":
		// allowed
	default:
		return nil, fmt.Errorf("unsupported hashAlgo: %s", hashAlgo)
	}

	// signatureFormat is deliberately permissive for now; accept empty or common values
	switch strings.ToLower(signatureFormat) {
	case "", "sig1", "structured", "compact", "http-signature":
		// allowed
	default:
		return nil, fmt.Errorf("unsupported signatureFormat: %s", signatureFormat)
	}

	// parse private key from PEM so htmsig picks an asymmetric signer
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode private key PEM")
	}

	var parsedKey any
	var err error
	switch block.Type {
	case "RSA PRIVATE KEY":
		parsedKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse RSA private key: %w", err)
		}
	case "EC PRIVATE KEY":
		parsedKey, err = x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse EC private key: %w", err)
		}
	default:
		parsedKey, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("unsupported or invalid private key PEM (type=%s): %w", block.Type, err)
		}
	}

	// Validate that the configured signatureAlgo matches key type
	switch signatureAlgo {
	case "", "auto":
		// accept any; choose based on key
	case "rsa":
		if _, ok := parsedKey.(*rsa.PrivateKey); !ok {
			return nil, fmt.Errorf("signatureAlgo=rsa but key is not RSA")
		}
	case "ecdsa":
		if _, ok := parsedKey.(*ecdsa.PrivateKey); !ok {
			return nil, fmt.Errorf("signatureAlgo=ecdsa but key is not ECDSA")
		}
	default:
		return nil, fmt.Errorf("unsupported signatureAlgo: %s", signatureAlgo)
	}

	// default component coverage: method, target-uri, authority
	requestSigner := htmsighttp.NewSigner(
		parsedKey,
		keyid,
		htmsighttp.WithComponents(
			component.Method(),
			component.TargetURI(),
			component.Authority(),
		))

	return &HTMPayloadSigner{
		signer:          requestSigner,
		privateKey:      []byte(privateKeyPEM),
		signatureAlgo:   signatureAlgo,
		hashAlgo:        hashAlgo,
		signatureFormat: signatureFormat,
	}, nil
}

func (s *HTMPayloadSigner) SignRequest(ctx context.Context, req *http.Request) error {
	// If body present and no Content-Digest header, compute a SHA-256 content-digest
	// Read body (non-destructively) and restore it after computing digest
	if req.Body != nil && req.Header.Get("Content-Digest") == "" {
		// Read body (non-destructively if possible)
		// Since req.Body is an io.ReadCloser, read and replace it
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return fmt.Errorf("failed to read request body for digest: %w", err)
		}
		// compute sha256
		h := sha256.Sum256(bodyBytes)
		digest := base64.StdEncoding.EncodeToString(h[:])
		// RFC9530 expects e.g., sha-256=:BASE64:
		req.Header.Set("Content-Digest", fmt.Sprintf("sha-256=:%s:", digest))
		// restore body
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	return s.signer.SignRequest(ctx, req)
}

func (s *HTMPayloadSigner) SignResponse(ctx context.Context, resp http.ResponseWriter) error {
	return fmt.Errorf("the response signer is not implemented")
}
