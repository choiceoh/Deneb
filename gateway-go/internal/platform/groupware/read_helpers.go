package groupware

import (
	"context"
	"fmt"
	"strings"
)

// ReadApprovalByDocID fetches one 전자결재 document body by Amaranth doc id
// (searches pending→done→cc→total via folder=all, matching id === query).
func ReadApprovalByDocID(ctx context.Context, cfg Config, docID string) (string, error) {
	docID = strings.TrimSpace(docID)
	if docID == "" {
		return "", fmt.Errorf("doc id required")
	}
	out, err := Run(ctx, cfg, Request{
		Area:   AreaApproval,
		Action: ActionRead,
		Folder: "all",
		Query:  docID,
	})
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
