package config

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// SchemaNode represents a single node in the config schema tree.
type SchemaNode struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description,omitempty"`
	Default     any                    `json:"default,omitempty"`
	Enum        []string               `json:"enum,omitempty"`
	Properties  map[string]*SchemaNode `json:"properties,omitempty"`
	Required    []string               `json:"required,omitempty"`
}

// GetSchema returns the full config schema tree.
func GetSchema() *SchemaNode {
	return &SchemaNode{
		Type:        "object",
		Description: "Deneb configuration schema",
		Properties: map[string]*SchemaNode{
			"gateway": {
				Type:        "object",
				Description: "Gateway server settings",
				Properties: map[string]*SchemaNode{
					"port":    {Type: "number", Description: "Gateway port", Default: DefaultGatewayPort},
					"mode":    {Type: "string", Description: "Gateway mode", Enum: []string{"local", "remote"}},
					"bind":    {Type: "string", Description: "Bind mode", Enum: []string{"auto", "lan", "loopback", "custom", "tailnet"}},
				},
			},
			"logging": {
				Type:        "object",
				Description: "Logging configuration",
				Properties: map[string]*SchemaNode{
					"level": {Type: "string", Description: "Log level", Enum: []string{"debug", "info", "warn", "error"}},
					"file":  {Type: "string", Description: "Log file path"},
				},
			},
			"session": {
				Type:        "object",
				Description: "Session configuration",
				Properties: map[string]*SchemaNode{
					"mainKey": {Type: "string", Description: "Main session key", Default: "main"},
				},
			},
			"agents": {
				Type:        "object",
				Description: "Agent runtime configuration",
				Properties: map[string]*SchemaNode{
					"maxConcurrent": {Type: "number", Description: "Maximum concurrent agents", Default: 8},
				},
			},
		},
	}
}

// LookupSchema finds a schema node by dotted path (e.g. "gateway.port").
func LookupSchema(path string) *SchemaNode {
	schema := GetSchema()
	if path == "" {
		return schema
	}

	parts := strings.Split(path, ".")
	current := schema
	for _, part := range parts {
		if current.Properties == nil {
			return nil
		}
		node, ok := current.Properties[part]
		if !ok {
			return nil
		}
		current = node
	}
	return current
}

// HashString computes a SHA-256 hex hash of a string.
func HashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
