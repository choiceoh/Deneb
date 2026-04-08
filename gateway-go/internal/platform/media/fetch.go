// Package media provides media fetching with SSRF protection for the Go gateway.
//
// This mirrors media processing logic from src/media/ and
// src/gateway/server-methods/media/ in the TypeScript codebase.
package media

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"

	"github.com/choiceoh/deneb/gateway-go/internal/core/coremedia"
	"strings"
	"time"
)

// MediaFetchErrorCode classifies media fetch errors.
type MediaFetchErrorCode string

const (
	ErrMaxBytes    MediaFetchErrorCode = "max_bytes"
	ErrHTTPError   MediaFetchErrorCode = "http_error"
	ErrFetchFailed MediaFetchErrorCode = "fetch_failed"
)

// MediaFetchError is a structured error for media fetch failures.
type MediaFetchError struct {
	Code    MediaFetchErrorCode `json:"code"`
	Message string              `json:"message"`
	Status  int                 `json:"status,omitempty"`
	Cause   error               `json:"-"` // underlying error, excluded from JSON
}

func (e *MediaFetchError) Error() string {
	return fmt.Sprintf("media fetch error (%s): %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause, implementing the errors.Unwrap interface.
func (e *MediaFetchError) Unwrap() error { return e.Cause }

// FetchResult holds the result of a successful media fetch.
type FetchResult struct {
	Data        []byte `json:"-"`
	ContentType string `json:"contentType,omitempty"`
	FileName    string `json:"fileName,omitempty"`
	Size        int    `json:"size"`
	FinalURL    string `json:"finalUrl,omitempty"`   // URL after redirects
	StatusCode  int    `json:"statusCode,omitempty"` // HTTP status code
}

// FetchOptions configures a media fetch operation.
type FetchOptions struct {
	URL               string
	MaxBytes          int64
	MaxRedirects      int
	ReadIdleTimeoutMs int
	Headers           map[string]string
	Client            *http.Client
}

const (
	defaultMaxBytes     = 25 * 1024 * 1024 // 25 MB
	defaultMaxRedirects = 5
	defaultIdleTimeout  = 30_000 // 30s
)

// ssrfTransport is a shared SSRF-safe transport for media fetches.
// Reuses TCP connections across Fetch() calls instead of creating a new
// transport per invocation.
var ssrfTransport = &http.Transport{
	DialContext:         SSRFSafeDialer(),
	MaxIdleConns:        32,
	MaxIdleConnsPerHost: 4,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 5 * time.Second,
	ForceAttemptHTTP2:   true,
}

// Fetch downloads media from a URL with SSRF protection and size limits.
func Fetch(ctx context.Context, opts FetchOptions) (*FetchResult, error) {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultMaxBytes
	}
	if opts.MaxRedirects <= 0 {
		opts.MaxRedirects = defaultMaxRedirects
	}

	// SSRF validation.
	if err := validateURL(opts.URL); err != nil {
		return nil, &MediaFetchError{Code: ErrFetchFailed, Message: err.Error(), Cause: err}
	}

	client := opts.Client
	if client == nil {
		client = &http.Client{
			Transport: ssrfTransport,
			Timeout:   60 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= opts.MaxRedirects {
					return fmt.Errorf("too many redirects (%d)", opts.MaxRedirects)
				}
				// Validate each redirect destination.
				if err := validateURL(req.URL.String()); err != nil {
					return err
				}
				return nil
			},
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.URL, http.NoBody)
	if err != nil {
		return nil, &MediaFetchError{Code: ErrFetchFailed, Message: err.Error(), Cause: err}
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, &MediaFetchError{Code: ErrFetchFailed, Message: redactURL(err.Error(), opts.URL), Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := readBodySnippet(resp.Body, 200)
		return nil, &MediaFetchError{
			Code:    ErrHTTPError,
			Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, snippet),
			Status:  resp.StatusCode,
		}
	}

	// Check content-length upfront.
	if resp.ContentLength > opts.MaxBytes {
		return nil, &MediaFetchError{
			Code:    ErrMaxBytes,
			Message: fmt.Sprintf("content-length %d exceeds limit %d", resp.ContentLength, opts.MaxBytes),
		}
	}

	// Read with size limit.
	limited := io.LimitReader(resp.Body, opts.MaxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, &MediaFetchError{Code: ErrFetchFailed, Message: redactURL(err.Error(), opts.URL), Cause: err}
	}
	if int64(len(data)) > opts.MaxBytes {
		return nil, &MediaFetchError{
			Code:    ErrMaxBytes,
			Message: fmt.Sprintf("response body exceeds limit %d bytes", opts.MaxBytes),
		}
	}

	contentType := resp.Header.Get("Content-Type")
	fileName := parseContentDispositionFileName(resp.Header.Get("Content-Disposition"))

	// Capture final URL after redirects.
	finalURL := opts.URL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return &FetchResult{
		Data:        data,
		ContentType: contentType,
		FileName:    fileName,
		Size:        len(data),
		FinalURL:    finalURL,
		StatusCode:  resp.StatusCode,
	}, nil
}

// --- SSRF protection ---

// privateNetworks defines CIDR ranges that are blocked for SSRF.
var privateNetworks []*net.IPNet

func init() {
	cidrs := []string{
		"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12",
		"192.168.0.0/16", "169.254.0.0/16", "0.0.0.0/8",
		"::1/128", "fc00::/7", "fe80::/10",
	}
	for _, cidr := range cidrs {
		_, ipNet, _ := net.ParseCIDR(cidr)
		if ipNet != nil {
			privateNetworks = append(privateNetworks, ipNet)
		}
	}
}

func validateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty hostname")
	}

	// Check if host is a raw IP.
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("SSRF: private IP %s blocked", ip)
		}
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	for _, cidr := range privateNetworks {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// SSRFSafeDialer returns a DialContext that validates resolved IPs.
// Exported for use by cookie-jar clients that need SSRF protection.
func SSRFSafeDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		// Resolve DNS and check all addresses.
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if isPrivateIP(ip.IP) {
				return nil, fmt.Errorf("SSRF: resolved IP %s for host %s is private", ip.IP, host)
			}
		}
		// Connect to first valid address.
		if len(ips) == 0 {
			return nil, fmt.Errorf("no addresses for host %s", host)
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
}

// --- Helpers ---

func parseContentDispositionFileName(header string) string {
	if header == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	if fn, ok := params["filename"]; ok && fn != "" {
		// Return basename only (security).
		parts := strings.Split(strings.ReplaceAll(fn, "\\", "/"), "/")
		return parts[len(parts)-1]
	}
	return ""
}

func readBodySnippet(body io.Reader, maxChars int) string {
	data := make([]byte, maxChars)
	n, _ := io.ReadFull(body, data) //nolint:errcheck // partial reads expected; we only need a prefix
	s := string(data[:n])
	// Collapse whitespace.
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func redactURL(msg, rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return msg
	}
	// Redact query parameters (may contain API keys).
	if u.RawQuery != "" {
		redacted := u.Scheme + "://" + u.Host + u.Path + "?[REDACTED]"
		return strings.ReplaceAll(msg, rawURL, redacted)
	}
	return msg
}

// DetectMIME detects the MIME type from a byte buffer using magic bytes.
// Uses coremedia's 21+ format magic-byte sniffing (WEBP, AVIF, HEIC, OOXML, etc.).
func DetectMIME(data []byte) string {
	return coremedia.DetectMIME(data)
}

// Logger returns a logger suitable for media operations.
func Logger(base *slog.Logger) *slog.Logger {
	if base == nil {
		return slog.Default()
	}
	return base.With("pkg", "media")
}
