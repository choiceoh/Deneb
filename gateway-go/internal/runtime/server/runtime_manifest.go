package server

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	runtimemanifest "github.com/choiceoh/deneb/gateway-go/internal/runtime/manifest"
)

// runtimeManifestSnapshot projects the live registries into a redacted,
// content-addressed health section. It intentionally reads the chat skills
// cache: before the first agent run has discovered skills, that component is
// reported as pending instead of claiming that an on-disk catalog was loaded.
func (s *Server) runtimeManifestSnapshot() *runtimemanifest.Snapshot {
	if s == nil || s.ServerRuntime == nil || s.runtimeManifest == nil {
		return nil
	}
	input := runtimemanifest.Input{Version: s.version}

	if s.ChatManager != nil && s.chatToolRegistry != nil {
		input.ToolsLoaded = true
		definitions := s.chatToolRegistry.Definitions()
		input.Tools = make([]runtimemanifest.Tool, 0, len(definitions))
		for _, definition := range definitions {
			schema, err := json.Marshal(definition.InputSchema)
			input.Tools = append(input.Tools, runtimemanifest.Tool{
				Name:        definition.Name,
				Description: definition.Description,
				Schema:      string(schema),
				SchemaValid: err == nil,
				Hidden:      definition.Hidden,
				Deferred:    definition.Deferred,
				Profile:     definition.Profile,
				MaxOutput:   definition.MaxOutput,
			})
		}
	}

	if s.ChatManager != nil && s.modelRegistry != nil {
		input.ModelsLoaded = true
		models := s.modelRegistry.ConfiguredModels()
		input.Models = make([]runtimemanifest.Model, 0, len(models))
		for role, model := range models {
			input.Models = append(input.Models, runtimemanifest.Model{
				Role:          string(role),
				Provider:      model.ProviderID,
				Name:          model.Model,
				BaseURL:       model.BaseURL,
				APIMode:       model.APIMode,
				CredentialSet: model.APIKey != "",
			})
		}
	}

	if skills := chat.CachedSkillsSnapshot(); skills != nil {
		input.SkillsLoaded = true
		input.Skills = make([]runtimemanifest.Skill, 0, len(skills.ResolvedSkills))
		for _, skill := range skills.ResolvedSkills {
			input.Skills = append(input.Skills, runtimemanifest.Skill{
				Name:    skill.Name,
				Version: skill.Version,
				Path:    skill.FilePath,
			})
		}
	}

	snapshot := s.runtimeManifest.Build(input)
	return &snapshot
}
