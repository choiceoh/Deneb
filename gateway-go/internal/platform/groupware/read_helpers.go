package groupware

import (
	"context"
	"fmt"
	"strings"
)

// ReadApprovalByDocID fetches one 전자결재 document body by Amaranth doc id
// (searches pending→done→cc→total via folder=all, matching id === query).
func ReadApprovalByDocID(ctx context.Context, cfg Config, docID string) (string, error) {
	return ReadApprovalByDocIDIn(ctx, cfg, docID, "")
}

// ReadApprovalByDocIDIn is ReadApprovalByDocID with a folder hint: a caller
// that knows the doc's box (the list rows carry it) skips the 4-folder scan —
// the dominant cost of a cold detail open. Unknown/blank folder falls back to
// the full scan.
func ReadApprovalByDocIDIn(ctx context.Context, cfg Config, docID, folder string) (string, error) {
	docID = strings.TrimSpace(docID)
	if docID == "" {
		return "", fmt.Errorf("doc id required")
	}
	f := strings.ToLower(strings.TrimSpace(folder))
	switch f {
	case "pending", "done", "cc", "total":
	default:
		f = "all"
	}
	out, err := Run(ctx, cfg, Request{
		Area:   AreaApproval,
		Action: ActionRead,
		Folder: f,
		Query:  docID,
	})
	// A stale folder hint (doc moved boxes after the list snapshot) retries
	// with the full scan before giving up.
	if f != "all" && (err != nil || out == "" || strings.HasPrefix(out, "그룹웨어")) {
		out, err = Run(ctx, cfg, Request{
			Area:   AreaApproval,
			Action: ActionRead,
			Folder: "all",
			Query:  docID,
		})
	}
	if err != nil {
		return "", err
	}
	if out == "" || strings.HasPrefix(out, "그룹웨어 리더:") || strings.HasPrefix(out, "그룹웨어 읽기 실패") {
		return "", fmt.Errorf("approval %s not found", docID)
	}
	return out, nil
}

// ListERP returns a human-readable list for stock|po|receive|ship|price|sales|people|board.
func ListERP(ctx context.Context, cfg Config, area, folder, query string, limit int) (string, error) {
	area = strings.ToLower(strings.TrimSpace(area))
	switch area {
	case "stock", "po", "receive", "ship", "price", "sales", "people", "board":
	default:
		return "", fmt.Errorf("area must be stock|po|receive|ship|price|sales|people|board")
	}
	action := ActionList
	if area == "sales" && strings.TrimSpace(folder) != "" {
		action = ActionSummary
	}
	return Run(ctx, cfg, Request{
		Area:   Area(area),
		Action: action,
		Folder: folder,
		Query:  query,
		Limit:  limit,
	})
}
