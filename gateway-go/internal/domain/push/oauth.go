package push

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// tokenRefreshSkew refreshes the cached access token this long before its
// stated expiry, so an in-flight send never races the boundary.
const tokenRefreshSkew = time.Minute

// tokenSource mints and caches OAuth2 access tokens for the FCM scope using the
// JWT-bearer grant (a self-signed service-account assertion exchanged for a
// bearer token). Tokens are valid ~1h. Mirrors the hand-rolled token exchange
// in internal/platform/gmail/client.go (no x/oauth2 dependency).
type tokenSource struct {
	sa   *serviceAccount
	http *http.Client
	now  func() time.Time // injectable clock for tests

	mu     sync.Mutex
	token  string
	expiry time.Time
}

func newTokenSource(sa *serviceAccount, client *http.Client) *tokenSource {
	return &tokenSource{sa: sa, http: client, now: time.Now}
}

// accessToken returns a valid access token, minting a fresh one when the cache
// is empty or near expiry. Never logs or returns the token in an error.
func (ts *tokenSource) accessToken(ctx context.Context) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	now := ts.now()
	if ts.token != "" && now.Before(ts.expiry.Add(-tokenRefreshSkew)) {
		return ts.token, nil
	}

	assertion, err := ts.sa.signedAssertion(now)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, ts.sa.tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("push: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := ts.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("push: token request failed: %w", err)
	}
	defer resp.Body.Close()
	// Drain a bounded amount so the connection can be reused; never log the body
	// (it carries the access token on success).
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("push: token endpoint returned HTTP %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("push: parse token response: %w", err)
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return "", fmt.Errorf("push: token response missing access_token")
	}
	ts.token = out.AccessToken
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	ts.expiry = now.Add(ttl)
	return ts.token, nil
}
