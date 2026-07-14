package system

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Config Reload
// ---------------------------------------------------------------------------

// ConfigReloadDeps holds the dependencies for the config.reload method.
type ConfigReloadDeps struct {
	// OnReloaded is called after a successful config reload with the new config snapshot.
	// Use this to propagate config changes to Go subsystems (hooks, broadcaster, etc.).
	OnReloaded func(snapshot *config.ConfigSnapshot)
}

// ConfigReloadMethods returns the config.reload handler.
// If deps is zero-value (no OnReloaded callback), the handler still works
// but skips the post-reload propagation step.
func ConfigReloadMethods(deps ConfigReloadDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"config.reload": func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			snapshot, err := config.LoadConfigFromDefaultPath()
			if err != nil {
				return rpcerr.WrapUnavailable("config reload failed", err).Response(req.ID)
			}
			if !snapshot.Valid {
				resp := rpcutil.RespondOK(req.ID, map[string]any{
					"valid":  false,
					"issues": snapshot.Issues,
				})
				return resp
			}

			// Propagate to Go subsystems (hooks, broadcaster, etc.).
			if deps.OnReloaded != nil {
				deps.OnReloaded(snapshot)
			}

			resp := rpcutil.RespondOK(req.ID, map[string]any{
				"valid":  true,
				"path":   snapshot.Path,
				"config": snapshot.Config,
			})
			return resp
		},
	}
}

// ---------------------------------------------------------------------------
// Config Advanced
// ---------------------------------------------------------------------------

// ConfigAdvancedDeps holds the dependencies for advanced config RPC methods.
type ConfigAdvancedDeps struct {
	Broadcaster BroadcastFunc
}

// ConfigAdvancedMethods returns the config.get, config.set, config.apply,
// config.patch, config.schema, and config.schema.lookup handlers.
func ConfigAdvancedMethods(deps ConfigAdvancedDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"config.get":           configGet(),
		"config.set":           configSet(deps),
		"config.apply":         configApply(deps),
		"config.patch":         configPatch(deps),
		"config.schema":        configSchema(deps),
		"config.schema.lookup": configSchemaLookup(deps),
	}
}

// configGet handles "config.get" -- returns the current gateway configuration snapshot.
func configGet() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		snapshot, err := config.LoadConfigFromDefaultPath()
		if err != nil {
			return rpcerr.WrapUnavailable("failed to load config", err).Response(req.ID)
		}

		resp := rpcutil.RespondOK(req.ID, map[string]any{
			"path":     snapshot.Path,
			"exists":   snapshot.Exists,
			"valid":    snapshot.Valid,
			"hash":     snapshot.Hash,
			"config":   snapshot.Config,
			"issues":   snapshot.Issues,
			"warnings": snapshot.Warnings,
		})
		return resp
	}
}

func configSet(deps ConfigAdvancedDeps) rpcutil.HandlerFunc {
	type params struct {
		Raw      string `json:"raw"`
		BaseHash string `json:"baseHash"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		warnings, newHash, err := persistValidatedConfig(p.Raw, p.BaseHash)
		if err != nil {
			return nil, err
		}
		if deps.Broadcaster != nil {
			wire, _ := events.PayloadOf(map[string]any{"hash": newHash})
			deps.Broadcaster("config.changed", wire)
		}
		return map[string]any{
			"ok":       true,
			"hash":     newHash,
			"warnings": warnings,
		}, nil
	})
}

func configApply(deps ConfigAdvancedDeps) rpcutil.HandlerFunc {
	type params struct {
		Raw            string `json:"raw"`
		BaseHash       string `json:"baseHash"`
		SessionKey     string `json:"sessionKey,omitempty"`
		Note           string `json:"note,omitempty"`
		RestartDelayMs int    `json:"restartDelayMs,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		warnings, newHash, err := persistValidatedConfig(p.Raw, p.BaseHash)
		if err != nil {
			return nil, err
		}
		if deps.Broadcaster != nil {
			wire, _ := events.PayloadOf(map[string]any{
				"hash":       newHash,
				"sessionKey": p.SessionKey,
				"note":       p.Note,
			})
			deps.Broadcaster("config.applied", wire)
		}
		return map[string]any{
			"ok":       true,
			"hash":     newHash,
			"warnings": warnings,
		}, nil
	})
}

func persistValidatedConfig(raw, baseHash string) ([]string, string, error) {
	if raw == "" {
		return nil, "", rpcerr.MissingParam("raw")
	}
	issues, warnings, err := config.ValidateRawConfig([]byte(raw))
	if err != nil {
		return nil, "", rpcerr.WrapValidationFailed("config validation error", err)
	}
	if len(issues) > 0 {
		return nil, "", rpcerr.ValidationFailed("invalid config: " + issues[0].String())
	}
	snapshot, loadErr := config.LoadConfigFromDefaultPath()
	if loadErr == nil && snapshot != nil && baseHash != "" && snapshot.Hash != baseHash {
		return nil, "", rpcerr.Conflict("config has been modified since last read (hash mismatch)")
	}
	if err := writeConfigBytes(config.ResolveConfigPath(), []byte(raw)); err != nil {
		return nil, "", rpcerr.WrapUnavailable("failed to write config", err)
	}
	return warnings, config.HashString(raw), nil
}

func configPatch(deps ConfigAdvancedDeps) rpcutil.HandlerFunc {
	type params struct {
		Raw            string `json:"raw"`
		BaseHash       string `json:"baseHash"`
		SessionKey     string `json:"sessionKey,omitempty"`
		Note           string `json:"note,omitempty"`
		RestartDelayMs int    `json:"restartDelayMs,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Raw == "" {
			return nil, rpcerr.MissingParam("raw")
		}
		var patch map[string]any
		if err := json.Unmarshal([]byte(p.Raw), &patch); err != nil {
			return nil, rpcerr.WrapValidationFailed("invalid JSON patch", err)
		}
		snapshot, err := config.LoadConfigFromDefaultPath()
		if err != nil {
			return nil, rpcerr.WrapUnavailable("failed to load config", err)
		}
		if p.BaseHash != "" {
			if snapshot.Hash != p.BaseHash {
				return nil, rpcerr.Conflict("config has been modified since last read (hash mismatch)")
			}
		}
		var current map[string]any
		if err := json.Unmarshal([]byte(snapshot.Raw), &current); err != nil {
			return nil, rpcerr.WrapUnavailable("failed to parse current config", err)
		}
		for k, v := range patch {
			current[k] = v
		}
		merged, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			return nil, rpcerr.Unavailable("failed to marshal merged config")
		}
		issues, warnings, valErr := config.ValidateRawConfig(merged)
		if valErr != nil {
			return nil, rpcerr.WrapValidationFailed("config validation error", valErr)
		}
		if len(issues) > 0 {
			return nil, rpcerr.ValidationFailed("merged config is invalid: " + issues[0].String())
		}
		cfgPath := config.ResolveConfigPath()
		if err := writeConfigBytes(cfgPath, merged); err != nil {
			return nil, rpcerr.WrapUnavailable("failed to write config", err)
		}
		newHash := config.HashString(string(merged))
		if deps.Broadcaster != nil {
			wire, _ := events.PayloadOf(map[string]any{
				"hash":       newHash,
				"sessionKey": p.SessionKey,
				"note":       p.Note,
			})
			deps.Broadcaster("config.patched", wire)
		}
		return map[string]any{
			"ok":       true,
			"hash":     newHash,
			"warnings": warnings,
		}, nil
	})
}

func writeConfigBytes(path string, data []byte) error {
	return atomicfile.WriteFile(path, data, &atomicfile.Options{
		Perm:    0o600,
		DirPerm: 0o700,
		Fsync:   true,
	})
}

func configSchema(_ ConfigAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		schema := config.Schema()
		resp := rpcutil.RespondOK(req.ID, schema)
		return resp
	}
}

func configSchemaLookup(_ ConfigAdvancedDeps) rpcutil.HandlerFunc {
	type params struct {
		Path string `json:"path"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Path == "" {
			return nil, rpcerr.MissingParam("path")
		}
		return config.LookupSchema(p.Path), nil
	})
}
