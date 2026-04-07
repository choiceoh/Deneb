package tools

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// SkillsSnapshotProvider returns the current cached skills snapshot.
type SkillsSnapshotProvider func() *skills.FullSkillSnapshot

// ToolSkillsList returns a tool function that lists discoverable skills.
func ToolSkillsList(getSnapshot SkillsSnapshotProvider) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Query    string `json:"query"`
			Category string `json:"category"`
		}
		if err := jsonutil.UnmarshalInto("skills_list params", input, &p); err != nil {
			return "", err
		}

		snapshot := getSnapshot()
		if snapshot == nil {
			return "No skills snapshot available.", nil
		}

		return skills.FormatSkillsListResponse(snapshot.DiscoverableSkills, p.Query, p.Category), nil
	}
}
