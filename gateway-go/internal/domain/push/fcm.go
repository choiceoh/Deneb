package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// defaultFCMBaseURL is the FCM HTTP v1 host. Overridable per-sender for tests.
const defaultFCMBaseURL = "https://fcm.googleapis.com"

// SendResult classifies the outcome of a single-token send so the caller can
// decide whether to prune the token, escalate to the operator, or retry later.
type SendResult struct {
	OK bool
	// Permanent is true when the device token is rejected as invalid/stale
	// (UNREGISTERED / NOT_FOUND / SENDER_ID_MISMATCH) — prune it.
	Permanent bool
	// AuthFailed is true when the credentials/access token were rejected
	// (HTTP 401/403). The whole sender is broken until the operator fixes the
	// service account; do NOT prune the device token in this case.
	AuthFailed bool
	Err        error
}

// FCMSender sends notifications via the FCM HTTP v1 API.
type FCMSender struct {
	ts        *tokenSource
	http      *http.Client
	projectID string
	baseURL   string
}

// NewFCMSender builds a sender from configured service-account credentials.
// Returns an error (without key material) when the credentials can't be loaded.
func NewFCMSender(cfg Config) (*FCMSender, error) {
	sa, err := loadServiceAccount(cfg.CredentialsFile)
	if err != nil {
		return nil, err
	}
	client := httputil.NewClient(15 * time.Second)
	return &FCMSender{
		ts:        newTokenSource(sa, client),
		http:      client,
		projectID: sa.projectID,
		baseURL:   defaultFCMBaseURL,
	}, nil
}

// ProjectID returns the Firebase project ID parsed from the credentials.
func (s *FCMSender) ProjectID() string { return s.projectID }

// Send delivers one notification to a single device token. data is an optional
// string map delivered to the app for in-foreground handling; the notification
// block is what the system tray shows when the app is fully closed.
func (s *FCMSender) Send(ctx context.Context, deviceToken, title, body string, data map[string]string) SendResult {
	accessToken, err := s.ts.accessToken(ctx)
	if err != nil {
		return SendResult{Err: err}
	}

	message := map[string]any{
		"token": deviceToken,
		"notification": map[string]any{
			"title": title,
			"body":  body,
		},
		// high priority wakes the app through Doze for a timely heads-up.
		"android": map[string]any{"priority": "high"},
	}
	if len(data) > 0 {
		message["data"] = data
	}
	payload, err := json.Marshal(map[string]any{"message": message})
	if err != nil {
		return SendResult{Err: fmt.Errorf("push: marshal message: %w", err)}
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	endpoint := fcmSendEndpoint(s.baseURL, s.projectID)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return SendResult{Err: fmt.Errorf("push: build send request: %w", err)}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return SendResult{Err: fmt.Errorf("push: send request failed: %w", err)}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	return classifyFCMResponse(resp.StatusCode, respBody)
}

// fcmSendEndpoint formats the HTTP v1 send URL for a project.
func fcmSendEndpoint(base, projectID string) string {
	return fmt.Sprintf("%s/v1/projects/%s/messages:send", strings.TrimRight(base, "/"), projectID)
}

// classifyFCMResponse maps an FCM HTTP v1 response to a SendResult. We only
// prune on clear token-death signals; an ambiguous 400 (which can also be a
// payload bug on our side) is treated as transient so a single mistake can't
// wipe every registered device.
func classifyFCMResponse(status int, body []byte) SendResult {
	if status == http.StatusOK {
		return SendResult{OK: true}
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return SendResult{AuthFailed: true, Err: fmt.Errorf("push: FCM auth rejected (HTTP %d)", status)}
	}
	code := fcmMessagingErrorCode(body)
	switch code {
	case "UNREGISTERED", "NOT_FOUND", "SENDER_ID_MISMATCH":
		return SendResult{Permanent: true, Err: fmt.Errorf("push: FCM rejected token (%s)", code)}
	}
	if status == http.StatusNotFound {
		return SendResult{Permanent: true, Err: fmt.Errorf("push: FCM rejected token (HTTP 404)")}
	}
	// 400 INVALID_ARGUMENT (ambiguous), 429, 5xx, etc. — transient, keep token.
	return SendResult{Err: fmt.Errorf("push: FCM transient error (HTTP %d, %s)", status, code)}
}

// fcmMessagingErrorCode extracts the canonical FCM error code (e.g.
// "UNREGISTERED") from a v1 error body, falling back to the generic gRPC status.
// It never returns anything token-bearing.
func fcmMessagingErrorCode(body []byte) string {
	var doc struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				Type      string `json:"@type"`
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return "unknown"
	}
	for _, d := range doc.Error.Details {
		if strings.TrimSpace(d.ErrorCode) != "" {
			return d.ErrorCode
		}
	}
	if strings.TrimSpace(doc.Error.Status) != "" {
		return doc.Error.Status
	}
	return "unknown"
}
