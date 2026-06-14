package push

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"
)

// fcmScope is the OAuth2 scope required to send via the FCM HTTP v1 API.
const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// defaultTokenURI is Google's OAuth2 token endpoint, used when the credentials
// file omits token_uri.
const defaultTokenURI = "https://oauth2.googleapis.com/token" //nolint:gosec // G101 false positive — not a credential

// serviceAccount is the subset of a Google service-account JSON needed to mint
// an OAuth2 access token for the FCM HTTP v1 API. The private key is held in
// parsed form only; it is never logged or re-serialized.
type serviceAccount struct {
	clientEmail  string
	tokenURI     string
	projectID    string
	privateKeyID string
	privateKey   *rsa.PrivateKey
}

// serviceAccountJSON mirrors the on-disk credentials file. Only the fields we
// use are typed; the rest are ignored.
type serviceAccountJSON struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenURI     string `json:"token_uri"`
}

// loadServiceAccount reads and validates a service-account JSON file. Errors are
// deliberately generic — they never contain the private key material.
func loadServiceAccount(path string) (*serviceAccount, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("push: read credentials: %w", err)
	}
	return parseServiceAccount(raw)
}

// parseServiceAccount validates the raw credentials JSON. Split out from file
// reading so it can be unit-tested with an in-memory document.
func parseServiceAccount(raw []byte) (*serviceAccount, error) {
	var doc serviceAccountJSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("push: parse credentials JSON: %w", err)
	}
	if strings.TrimSpace(doc.ClientEmail) == "" {
		return nil, fmt.Errorf("push: credentials missing client_email")
	}
	if strings.TrimSpace(doc.ProjectID) == "" {
		return nil, fmt.Errorf("push: credentials missing project_id")
	}
	key, err := parseRSAPrivateKey(doc.PrivateKey)
	if err != nil {
		return nil, err // already sanitized
	}
	tokenURI := strings.TrimSpace(doc.TokenURI)
	if tokenURI == "" {
		tokenURI = defaultTokenURI
	}
	return &serviceAccount{
		clientEmail:  strings.TrimSpace(doc.ClientEmail),
		tokenURI:     tokenURI,
		projectID:    strings.TrimSpace(doc.ProjectID),
		privateKeyID: strings.TrimSpace(doc.PrivateKeyID),
		privateKey:   key,
	}, nil
}

// parseRSAPrivateKey decodes a PEM-encoded RSA private key (PKCS#8, as Google
// emits, with a PKCS#1 fallback). Errors are generic so key material never
// reaches a log line.
func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("push: credentials private_key is not valid PEM")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("push: credentials private_key is not an RSA key")
		}
		return rsaKey, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("push: credentials private_key could not be parsed")
}

// jwtClaims is the assertion body for the JWT-bearer grant.
type jwtClaims struct {
	Iss   string `json:"iss"`
	Scope string `json:"scope"`
	Aud   string `json:"aud"`
	Iat   int64  `json:"iat"`
	Exp   int64  `json:"exp"`
}

// signedAssertion builds and RS256-signs a JWT asserting this service account
// for the FCM scope, valid for one hour from now.
func (sa *serviceAccount) signedAssertion(now time.Time) (string, error) {
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	if sa.privateKeyID != "" {
		header["kid"] = sa.privateKeyID
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("push: marshal jwt header: %w", err)
	}
	claimsJSON, err := json.Marshal(jwtClaims{
		Iss:   sa.clientEmail,
		Scope: fcmScope,
		Aud:   sa.tokenURI,
		Iat:   now.Unix(),
		Exp:   now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("push: marshal jwt claims: %w", err)
	}
	signingInput := b64url(headerJSON) + "." + b64url(claimsJSON)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, sa.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("push: sign jwt: %w", err)
	}
	return signingInput + "." + b64url(sig), nil
}

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
