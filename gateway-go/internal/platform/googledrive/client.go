// Package googledrive is a minimal read-only Google Drive client for the
// Supernote ingestion path: list a configured folder for new files and
// download their bytes. It mirrors the Gmail client's credential handling
// (internal/platform/gmail) — same ~/.deneb/credentials layout, same
// auto-refreshing OAuth token source — but reads Drive instead of mail.
//
// Scope: the stored refresh token must carry a Drive read scope
// (drive.readonly or drive.metadata.readonly + drive.readonly). Because the
// gateway never mints tokens itself (it only refreshes an operator-provisioned
// one), Drive access is an operator setup step: place drive_client.json +
// drive_token.json under ~/.deneb/credentials with a Drive-scoped consent.
package googledrive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/googleoauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

const (
	apiBase  = "https://www.googleapis.com/drive/v3"
	tokenURL = "https://oauth2.googleapis.com/token" //nolint:gosec // G101 false positive — not a credential
	// maxListResponseBytes bounds a folder-listing JSON response.
	maxListResponseBytes = 4 << 20 // 4MB
	// maxDownloadBytes bounds one downloaded file. A Supernote export (a
	// searchable PDF of handwritten pages) is small; this cap guards against
	// an unexpectedly huge file consuming memory.
	maxDownloadBytes = 32 << 20 // 32MB
)

// File is one Drive file's listing metadata.
type File struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	ModifiedTime string `json:"modifiedTime"` // RFC3339
}

// Client is a read-only Drive API client with auto-refreshing OAuth tokens.
type Client struct {
	tokens     *googleoauth.Source
	httpClient *http.Client
	// baseURL and tokenFn are seams: production leaves baseURL "" (⇒ apiBase)
	// and tokenFn nil (⇒ the token source). Tests point them at a local
	// server and a fixed token to exercise query/parse without credentials.
	baseURL string
	tokenFn func(context.Context) (string, error)
}

// CredentialsDir is the shared credentials directory (mirrors gmail).
func CredentialsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".deneb", "credentials")
}

// NewClient loads drive_client.json + drive_token.json from the default
// credentials directory. Returns an error (not a nil client) when the
// credentials are absent, so callers can degrade to "not configured".
func NewClient() (*Client, error) {
	return newClientFromDir(CredentialsDir())
}

func newClientFromDir(dir string) (*Client, error) {
	clientPath := filepath.Join(dir, "drive_client.json")
	tokenPath := filepath.Join(dir, "drive_token.json")
	loaded, err := googleoauth.Load("Drive", clientPath, tokenPath)
	if err != nil {
		return nil, err
	}
	httpClient := httputil.NewClient(60 * time.Second)
	return &Client{
		tokens:     googleoauth.NewSource("Drive", loaded, tokenPath, httpClient),
		httpClient: httpClient,
	}, nil
}

func (c *Client) validToken(ctx context.Context) (string, error) {
	if c.tokenFn != nil {
		return c.tokenFn(ctx)
	}
	return c.tokens.ValidToken(ctx, tokenURL)
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	base := apiBase
	if c.baseURL != "" {
		base = c.baseURL
	}
	return googleoauth.DoBearer(ctx, c.httpClient, c.validToken, base, method, path, body)
}

// ListFolderFiles returns non-trashed files directly in folderID modified
// strictly after modifiedAfter (RFC3339; "" lists all), oldest first. Only
// the fields needed for ingestion are requested.
func (c *Client) ListFolderFiles(ctx context.Context, folderID, modifiedAfter string) ([]File, error) {
	if folderID == "" {
		return nil, fmt.Errorf("googledrive: empty folder ID")
	}
	q := fmt.Sprintf("'%s' in parents and trashed = false", folderID)
	if modifiedAfter != "" {
		q += fmt.Sprintf(" and modifiedTime > '%s'", modifiedAfter)
	}
	params := url.Values{}
	params.Set("q", q)
	params.Set("fields", "files(id,name,mimeType,modifiedTime)")
	params.Set("orderBy", "modifiedTime")
	params.Set("pageSize", "100")
	// Support items in Shared Drives too, harmlessly no-op for My Drive.
	params.Set("supportsAllDrives", "true")
	params.Set("includeItemsFromAllDrives", "true")

	var out struct {
		Files []File `json:"files"`
	}
	err := googleoauth.JSON(ctx, c.request, http.MethodGet, "/files?"+params.Encode(), nil, &out,
		googleoauth.APIOptions{Service: "Drive", MaxResponseBytes: maxListResponseBytes})
	if err != nil {
		return nil, err
	}
	return out.Files, nil
}

// DownloadFile returns the raw bytes of fileID (bounded). alt=media streams the
// file content rather than its metadata JSON.
func (c *Client) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	if fileID == "" {
		return nil, fmt.Errorf("googledrive: empty file ID")
	}
	resp, err := c.request(ctx, http.MethodGet,
		"/files/"+url.PathEscape(fileID)+"?alt=media&supportsAllDrives=true", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return nil, fmt.Errorf("Drive download 오류 (HTTP %d): %s", resp.StatusCode, string(body))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("Drive 파일 읽기 실패: %w", err)
	}
	if int64(len(data)) > maxDownloadBytes {
		return nil, fmt.Errorf("Drive 파일이 너무 큼 (>%dB)", maxDownloadBytes)
	}
	return data, nil
}
