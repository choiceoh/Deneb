package system

import (
	"context"
	"encoding/json"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
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
		if p.Raw == "" {
			return nil, rpcerr.MissingParam("raw")
		}
		issues, warnings, err := config.ValidateRawConfig([]byte(p.Raw))
		if err != nil {
			return nil, rpcerr.WrapValidationFailed("config validation error", err)
		}
		if len(issues) > 0 {
			return nil, rpcerr.ValidationFailed("invalid config: " + issues[0].String())
		}
		snapshot, loadErr := config.LoadConfigFromDefaultPath()
		if loadErr == nil && snapshot != nil && p.BaseHash != "" {
			if snapshot.Hash != p.BaseHash {
				return nil, rpcerr.Conflict("config has been modified since last read (hash mismatch)")
			}
		}
		cfgPath := config.ResolveConfigPath()
		if err := os.WriteFile(cfgPath, []byte(p.Raw), 0o644); err != nil { //nolint:gosec // G306 — world-readable config is intentional
			return nil, rpcerr.WrapUnavailable("failed to write config", err)
		}
		newHash := config.HashString(p.Raw)
		if deps.Broadcaster != nil {
			deps.Broadcaster("config.changed", map[string]any{"hash": newHash})
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
		if p.Raw == "" {
			return nil, rpcerr.MissingParam("raw")
		}
		issues, warnings, err := config.ValidateRawConfig([]byte(p.Raw))
		if err != nil {
			return nil, rpcerr.WrapValidationFailed("config validation error", err)
		}
		if len(issues) > 0 {
			return nil, rpcerr.ValidationFailed("invalid config: " + issues[0].String())
		}
		snapshot, loadErr := config.LoadConfigFromDefaultPath()
		if loadErr == nil && snapshot != nil && p.BaseHash != "" {
			if snapshot.Hash != p.BaseHash {
				return nil, rpcerr.Conflict("config has been modified since last read (hash mismatch)")
			}
		}
		cfgPath := config.ResolveConfigPath()
		if err := os.WriteFile(cfgPath, []byte(p.Raw), 0o644); err != nil { //nolint:gosec // G306 — world-readable config is intentional
			return nil, rpcerr.WrapUnavailable("failed to write config", err)
		}
		newHash := config.HashString(p.Raw)
		if deps.Broadcaster != nil {
			deps.Broadcaster("config.applied", map[string]any{
				"hash":       newHash,
				"sessionKey": p.SessionKey,
				"note":       p.Note,
			})
		}
		return map[string]any{
			"ok":       true,
			"hash":     newHash,
			"warnings": warnings,
		}, nil
	})
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
		if err := os.WriteFile(cfgPath, merged, 0o644); err != nil { //nolint:gosec // G306 — world-readable config is intentional
			return nil, rpcerr.WrapUnavailable("failed to write config", err)
		}
		newHash := config.HashString(string(merged))
		if deps.Broadcaster != nil {
			deps.Broadcaster("config.patched", map[string]any{
				"hash":       newHash,
				"sessionKey": p.SessionKey,
				"note":       p.Note,
			})
		}
		return map[string]any{
			"ok":       true,
			"hash":     newHash,
			"warnings": warnings,
		}, nil
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
