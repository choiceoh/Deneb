package hindsight

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// apiNamespace is the fixed path segment Hindsight places between the API
// version and the bank. Self-hosted instances always use "default".
const apiNamespace = "default"

// httpTimeout is a backstop for HTTP requests. Recall additionally relies on
// the caller's context deadline (the recall preflight budget), which is much
// shorter and therefore wins for the read path.
const httpTimeout = 20 * time.Second

// Client is a minimal HTTP client for the Hindsight memory API. It implements
// only the two operations Deneb needs: recall (read) and retain (write).
type Client struct {
	baseURL   string
	bankID    string
	apiKey    string
	budget    string
	maxTokens int
	retain    bool
	http      *http.Client
}

// NewClient builds a Hindsight client from cfg. Returns nil when the
// integration is not configured (no base URL), so callers can treat a nil
// client as "feature disabled".
func NewClient(cfg Config) *Client {
	if !cfg.Enabled() {
		return nil
	}
	bank := strings.TrimSpace(cfg.BankID)
	if bank == "" {
		bank = defaultBankID
	}
	maxTokens := cfg.RecallMaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultRecallMaxTokens
	}
	return &Client{
		baseURL:   strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		bankID:    bank,
		apiKey:    strings.TrimSpace(cfg.APIKey),
		budget:    normalizeBudget(cfg.Budget),
		maxTokens: maxTokens,
		retain:    cfg.Retain,
		http:      &http.Client{Timeout: httpTimeout},
	}
}

// RetainEnabled reports whether the write path is active. Safe to call on a
// nil client (returns false), which lets call sites skip a nil check.
func (c *Client) RetainEnabled() bool { return c != nil && c.retain }

// BankID returns the configured memory bank identifier.
func (c *Client) BankID() string {
	if c == nil {
		return ""
	}
	return c.bankID
}

// Memory is a single recalled memory item.
type Memory struct {
	ID          string
	Text        string
	Type        string // "world" or "experience", may be empty
	Context     string // optional label, may be empty
	OccurredAt  string // ISO 8601, may be empty
	MentionedAt string // ISO 8601, may be empty
}

type recallRequest struct {
	Query     string `json:"query"`
	Budget    string `json:"budget,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

type recallResponse struct {
	Results []struct {
		ID            string `json:"id"`
		Text          string `json:"text"`
		Type          string `json:"type"`
		Context       string `json:"context"`
		OccurredStart string `json:"occurred_start"`
		MentionedAt   string `json:"mentioned_at"`
	} `json:"results"`
}

// Recall queries the memory bank for content relevant to query. The caller's
// context governs the deadline. Results arrive ranked by Hindsight.
func (c *Client) Recall(ctx context.Context, query string) ([]Memory, error) {
	if c == nil {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	var resp recallResponse
	err := c.postJSON(ctx, c.bankURL("/memories/recall"),
		recallRequest{Query: query, Budget: c.budget, MaxTokens: c.maxTokens}, &resp)
	if err != nil {
		return nil, err
	}
	memories := make([]Memory, 0, len(resp.Results))
	for _, r := range resp.Results {
		text := strings.TrimSpace(r.Text)
		if text == "" {
			continue
		}
		memories = append(memories, Memory{
			ID:          strings.TrimSpace(r.ID),
			Text:        text,
			Type:        strings.TrimSpace(r.Type),
			Context:     strings.TrimSpace(r.Context),
			OccurredAt:  strings.TrimSpace(r.OccurredStart),
			MentionedAt: strings.TrimSpace(r.MentionedAt),
		})
	}
	return memories, nil
}

// RetainItem is one memory to store. JSON tags match the Hindsight wire
// format so the type doubles as the request payload.
type RetainItem struct {
	Content    string            `json:"content"`
	Context    string            `json:"context,omitempty"`
	DocumentID string            `json:"document_id,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type retainRequest struct {
	Items []RetainItem `json:"items"`
	Async bool         `json:"async"`
}

// Retain stores items into the memory bank. The server processes ingestion
// asynchronously (async=true), so this returns once the request is accepted.
// A no-op when the write path is disabled or there is nothing to store.
func (c *Client) Retain(ctx context.Context, items []RetainItem) error {
	if c == nil || !c.retain {
		return nil
	}
	kept := make([]RetainItem, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.Content) == "" {
			continue
		}
		kept = append(kept, it)
	}
	if len(kept) == 0 {
		return nil
	}
	return c.postJSON(ctx, c.bankURL("/memories"), retainRequest{Items: kept, Async: true}, nil)
}

// bankURL builds a bank-scoped endpoint URL with a path-escaped bank ID.
func (c *Client) bankURL(suffix string) string {
	return c.baseURL + "/v1/" + apiNamespace + "/banks/" + url.PathEscape(c.bankID) + suffix
}

// postJSON sends body as JSON to endpoint and, when out is non-nil, decodes
// the JSON response into it.
func (c *Client) postJSON(ctx context.Context, endpoint string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("hindsight: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("hindsight: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hindsight: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("hindsight: %s returned %d: %s",
			endpoint, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("hindsight: decode response: %w", err)
	}
	return nil
}
