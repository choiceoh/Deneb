package code

// repos.go — list the operator's GitHub repositories for the new-task picker, so
// a vibe coder chooses from a dropdown instead of hand-typing owner/repo. Auth is
// the host's concern (gh auth login); a missing/unauthenticated gh surfaces as an
// error the handler degrades to an empty picker.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ListRepos returns owner/name for each repo `gh repo list` reports.
func (m *Manager) ListRepos(ctx context.Context) ([]Repo, error) {
	out, err := m.Runner.Run(ctx, "", "gh", "repo", "list", "--limit", "200", "--json", "nameWithOwner")
	if err != nil {
		return nil, fmt.Errorf("gh repo list: %w", err)
	}
	var rows []struct {
		NameWithOwner string `json:"nameWithOwner"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse repo list: %w", err)
	}
	repos := make([]Repo, 0, len(rows))
	for _, r := range rows {
		owner, name, ok := strings.Cut(r.NameWithOwner, "/")
		if ok && owner != "" && name != "" {
			repos = append(repos, Repo{Owner: owner, Name: name})
		}
	}
	return repos, nil
}
