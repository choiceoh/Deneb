package autoreply

import "strings"

// SkillCommandSpec describes a skill-provided command.
type SkillCommandSpec struct {
	SkillName   string `json:"skillName"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// BuildSkillCommandDefinitions converts skill command specs to chat command definitions.
func BuildSkillCommandDefinitions(specs []SkillCommandSpec) []ChatCommandDefinition {
	if len(specs) == 0 {
		return nil
	}
	defs := make([]ChatCommandDefinition, len(specs))
	for i, spec := range specs {
		defs[i] = ChatCommandDefinition{
			Key:         "skill:" + spec.SkillName,
			NativeName:  spec.Name,
			Description: spec.Description,
			TextAliases: []string{"/" + spec.Name},
			AcceptsArgs: true,
			ArgsParsing: "none",
			Scope:       ScopeBoth,
		}
	}
	return defs
}

// ResolveSkillCommand matches a command body to a skill command.
func ResolveSkillCommand(body string, skills []SkillCommandSpec) *SkillCommandSpec {
	if len(skills) == 0 || body == "" {
		return nil
	}
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "/") {
		return nil
	}

	// Extract command name.
	parts := strings.SplitN(trimmed[1:], " ", 2)
	cmdName := strings.ToLower(parts[0])

	for i := range skills {
		if strings.ToLower(skills[i].Name) == cmdName {
			return &skills[i]
		}
	}
	return nil
}
