package toolreg

// PilotToolSchema returns the JSON Schema for the pilot tool.
// Exported so chat/ can register the pilot tool with the schema.
func PilotToolSchema() map[string]any { return pilotToolSchema() }
