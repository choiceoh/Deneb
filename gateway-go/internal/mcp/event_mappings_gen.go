// Hand-written constants. Previously generated from YAML.

package mcp

// eventToResourceURI maps gateway event names to affected resource URIs.
var eventToResourceURI = map[string]string{
	"session.created":   "deneb://sessions",
	"session.completed": "deneb://sessions",
	"session.failed":    "deneb://sessions",
	"session.killed":    "deneb://sessions",
	"agent.completed":   "deneb://sessions",
	"config.changed":    "deneb://config",
	"skills.changed":    "deneb://skills",
	"memory.updated":    "deneb://memory",
}

// eventRequiresSampling lists events that should trigger Claude analysis.
var eventRequiresSampling = map[string]bool{
	"session.failed":  true,
	"agent.completed": true,
	"cron.fired":      true,
}
