package dropbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxUploadBytes is the largest single-request upload Dropbox accepts
// (150 MiB). Larger files require the upload_session API, which v1 omits.
const maxUploadBytes = 150 * 1024 * 1024

// listPageCap bounds list_folder pagination so a huge tree can't run forever.
const listPageCap = 50

// ListFolder lists entries under path ("" means the Dropbox root). When
// recursive is true, descendant entries are included. Pagination is followed
// via list_folder/continue up to listPageCap pages.
func (c *Client) ListFolder(ctx context.Context, path string, recursive bool, limit int) ([]Entry, error) {
	// Dropbox requires "" (not "/") for the root.
	if path == "/" {
		path = ""
	}
	if limit <= 0 || limit > 2000 {
		limit = 2000
	}

	req := map[string]any{
		"path":      path,
		"recursive": recursive,
		"limit":     limit,
	}
	body, err := c.doRPC(ctx, "/2/files/list_folder", req)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Entries []rawMetadata `json:"entries"`
		Cursor  string        `json:"cursor"`
		HasMore bool          `json:"has_more"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("list_folder 응답 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}

	entries := make([]Entry, 0, len(resp.Entries))
	for _, m := range resp.Entries {
		entries = append(entries, m.toEntry())
	}

	pages := 0
	for resp.HasMore && pages < listPageCap {
		pages++
		body, err := c.doRPC(ctx, "/2/files/list_folder/continue", map[string]any{"cursor": resp.Cursor})
		if err != nil {
			return entries, err // return what we have plus the error
		}
		resp.Entries = nil
		resp.HasMore = false
		if err := json.Unmarshal(body, &resp); err != nil {
			return entries, fmt.Errorf("list_folder/continue 응답 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
		}
		for _, m := range resp.Entries {
			entries = append(entries, m.toEntry())
		}
	}
	return entries, nil
}

// Search finds files/folders matching query. maxResults bounds the count.
func (c *Client) Search(ctx context.Context, query string, maxResults int) ([]Entry, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("검색어가 비어 있습니다") //nolint:staticcheck // ST1005 — Korean error message
	}
	if maxResults <= 0 || maxResults > 100 {
		maxResults = 20
	}

	req := map[string]any{
		"query": query,
		"options": map[string]any{
			"max_results": maxResults,
			"file_status": "active",
		},
	}
	body, err := c.doRPC(ctx, "/2/files/search_v2", req)
	if err != nil {
		return nil, err
	}

	// search_v2 nests the real metadata two levels deep:
	// matches[].metadata.metadata{file|folder}.
	var resp struct {
		Matches []struct {
			Metadata struct {
				Metadata rawMetadata `json:"metadata"`
			} `json:"metadata"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("search_v2 응답 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}

	entries := make([]Entry, 0, len(resp.Matches))
	for _, m := range resp.Matches {
		entries = append(entries, m.Metadata.Metadata.toEntry())
	}
	return entries, nil
}

// Download fetches the file at path, returning its bytes and metadata.
func (c *Client) Download(ctx context.Context, path string) ([]byte, *Entry, error) {
	arg := map[string]any{"path": path}
	resp, err := c.doContent(ctx, "/2/files/download", arg, nil)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("파일 다운로드 읽기 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}

	var meta *Entry
	if result := resp.Header.Get("Dropbox-API-Result"); result != "" {
		var m rawMetadata
		if json.Unmarshal([]byte(result), &m) == nil {
			e := m.toEntry()
			meta = &e
		}
	}
	return data, meta, nil
}

// Upload writes data to destPath. When overwrite is false, an existing file is
// auto-renamed (…(1).pdf). Files larger than 150 MiB are rejected (use the
// upload_session API — not implemented in v1).
func (c *Client) Upload(ctx context.Context, destPath string, data []byte, overwrite bool) (*Entry, error) {
	if len(data) > maxUploadBytes {
		return nil, fmt.Errorf("파일이 너무 큼 (%s) — 단일 업로드 최대 150MB, 청크 업로드는 미지원", humanSize(int64(len(data)))) //nolint:staticcheck // ST1005 — Korean error message
	}

	mode := "add"
	autorename := true
	if overwrite {
		mode = "overwrite"
		autorename = false
	}
	arg := map[string]any{
		"path":       destPath,
		"mode":       mode,
		"autorename": autorename,
		"mute":       false,
	}
	resp, err := c.doContent(ctx, "/2/files/upload", arg, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var m rawMetadata
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("upload 응답 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}
	e := m.toEntry()
	return &e, nil
}

// CreateSharedLink returns a shareable URL for the file at path. If a link
// already exists (409 shared_link_already_exists), the existing one is fetched
// via list_shared_links instead of failing.
func (c *Client) CreateSharedLink(ctx context.Context, path string) (string, error) {
	body, err := c.doRPC(ctx, "/2/sharing/create_shared_link_with_settings", map[string]any{"path": path})
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict &&
			strings.Contains(apiErr.Body, "shared_link_already_exists") {
			return c.existingSharedLink(ctx, path)
		}
		return "", err
	}

	var resp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("shared link 응답 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}
	return resp.URL, nil
}

// existingSharedLink fetches the already-created shared link for path.
func (c *Client) existingSharedLink(ctx context.Context, path string) (string, error) {
	body, err := c.doRPC(ctx, "/2/sharing/list_shared_links", map[string]any{
		"path":        path,
		"direct_only": true,
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		Links []struct {
			URL string `json:"url"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("list_shared_links 응답 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}
	if len(resp.Links) == 0 {
		return "", fmt.Errorf("기존 공유 링크를 찾을 수 없습니다") //nolint:staticcheck // ST1005 — Korean error message
	}
	return resp.Links[0].URL, nil
}

// LatestCursor returns a cursor for the current state of path WITHOUT listing
// entries. Use it to start watching "from now": subsequent ListChanges calls
// return only what changed after this point, so a first poll never floods the
// agent with the folder's entire backlog.
func (c *Client) LatestCursor(ctx context.Context, path string, recursive bool) (string, error) {
	if path == "/" {
		path = ""
	}
	body, err := c.doRPC(ctx, "/2/files/list_folder/get_latest_cursor", map[string]any{
		"path":      path,
		"recursive": recursive,
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		Cursor string `json:"cursor"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("get_latest_cursor 응답 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}
	return resp.Cursor, nil
}

// ListChanges returns file entries added or modified since cursor, following
// pagination, plus the new cursor to persist for next time. Folder and delete
// entries are filtered out — watchers only care about new/changed files.
func (c *Client) ListChanges(ctx context.Context, cursor string) ([]Entry, string, error) {
	var entries []Entry
	hasMore := true
	for pages := 0; hasMore && pages < listPageCap; pages++ {
		body, err := c.doRPC(ctx, "/2/files/list_folder/continue", map[string]any{"cursor": cursor})
		if err != nil {
			return entries, cursor, err
		}
		var resp struct {
			Entries []rawMetadata `json:"entries"`
			Cursor  string        `json:"cursor"`
			HasMore bool          `json:"has_more"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return entries, cursor, fmt.Errorf("list_folder/continue 응답 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
		}
		for _, m := range resp.Entries {
			if m.Tag == "file" {
				entries = append(entries, m.toEntry())
			}
		}
		cursor = resp.Cursor
		hasMore = resp.HasMore
	}
	return entries, cursor, nil
}
