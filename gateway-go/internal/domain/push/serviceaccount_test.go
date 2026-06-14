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
	"strings"
	"testing"
	"time"
)

// testCredentials returns a syntactically valid service-account JSON whose
// token_uri points at tokenURI, plus the RSA key used to sign so tests can
// verify the JWT signature.
func testCredentials(t *testing.T, tokenURI string) ([]byte, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	doc := map[string]string{
		"type":           "service_account",
		"project_id":     "deneb-test",
		"private_key_id": "kid-test-1",
		"private_key":    string(keyPEM),
		"client_email":   "fcm@deneb-test.iam.gserviceaccount.com",
		"token_uri":      tokenURI,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	return raw, key
}

func TestParseServiceAccount_Valid(t *testing.T) {
	raw, _ := testCredentials(t, "https://oauth2.example/token")
	sa, err := parseServiceAccount(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sa.projectID != "deneb-test" {
		t.Errorf("projectID = %q, want deneb-test", sa.projectID)
	}
	if sa.clientEmail != "fcm@deneb-test.iam.gserviceaccount.com" {
		t.Errorf("clientEmail = %q", sa.clientEmail)
	}
	if sa.tokenURI != "https://oauth2.example/token" {
		t.Errorf("tokenURI = %q", sa.tokenURI)
	}
	if sa.privateKey == nil {
		t.Error("privateKey is nil")
	}
}

func TestParseServiceAccount_DefaultsTokenURI(t *testing.T) {
	raw, _ := testCredentials(t, "") // empty token_uri
	sa, err := parseServiceAccount(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sa.tokenURI != defaultTokenURI {
		t.Errorf("tokenURI = %q, want default %q", sa.tokenURI, defaultTokenURI)
	}
}

func TestParseServiceAccount_Errors(t *testing.T) {
	cases := map[string]string{
		"missing client_email": `{"project_id":"p","private_key":"x"}`,
		"missing project_id":   `{"client_email":"a@b","private_key":"x"}`,
		"bad pem":              `{"project_id":"p","client_email":"a@b","private_key":"not-a-key"}`,
		"not json":             `{`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseServiceAccount([]byte(doc))
			if err == nil {
				t.Fatal("expected error")
			}
			// The error must never echo the private key material.
			if strings.Contains(err.Error(), "BEGIN") || strings.Contains(err.Error(), "not-a-key") {
				t.Errorf("error leaks key material: %v", err)
			}
		})
	}
}

func TestSignedAssertion_VerifiesAndCarriesClaims(t *testing.T) {
	raw, key := testCredentials(t, "https://oauth2.example/token")
	sa, err := parseServiceAccount(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	assertion, err := sa.signedAssertion(now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts, want 3", len(parts))
	}

	// Verify the RS256 signature over header.claims with the public key.
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	digest := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("signature verify failed: %v", err)
	}

	// Verify the claims.
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims jwtClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Iss != sa.clientEmail {
		t.Errorf("iss = %q, want %q", claims.Iss, sa.clientEmail)
	}
	if claims.Scope != fcmScope {
		t.Errorf("scope = %q, want %q", claims.Scope, fcmScope)
	}
	if claims.Aud != sa.tokenURI {
		t.Errorf("aud = %q, want %q", claims.Aud, sa.tokenURI)
	}
	if claims.Iat != now.Unix() {
		t.Errorf("iat = %d, want %d", claims.Iat, now.Unix())
	}
	if claims.Exp != now.Add(time.Hour).Unix() {
		t.Errorf("exp = %d, want %d", claims.Exp, now.Add(time.Hour).Unix())
	}

	// Verify the header carries the key id (so Google can pick the right key).
	headerJSON, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var header map[string]string
	_ = json.Unmarshal(headerJSON, &header)
	if header["alg"] != "RS256" || header["kid"] != "kid-test-1" {
		t.Errorf("header = %v, want RS256 + kid-test-1", header)
	}
}
