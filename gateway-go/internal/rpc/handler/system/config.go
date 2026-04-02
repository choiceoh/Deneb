package system

import (
	"context"
	"encoding/json"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
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
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrUnavailable, "config reload failed: "+err.Error()))
			}
			if !snapshot.Valid {
				resp := protocol.MustResponseOK(req.ID, map[string]any{
					"valid":  false,
					"issues": snapshot.Issues,
				})
				return resp
			}

			// Propagate to Go subsystems (hooks, broadcaster, etc.).
			if deps.OnReloaded != nil {
				deps.OnReloaded(snapshot)
			}

			resp := protocol.MustResponseOK(req.ID, map[string]any{
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
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "failed to load config: "+err.Error()))
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
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
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Raw      string `json:"raw"`
			BaseHash string `json:"baseHash"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Raw == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "raw is required"))
		}

		// Validate JSON syntax and config structure.
		issues, warnings, err := config.ValidateRawConfig([]byte(p.Raw))
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "config validation error: "+err.Error()))
		}
		if len(issues) > 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid config: "+issues[0].String()))
		}

		// Load current config to verify baseHash (optimistic concurrency).
		snapshot, loadErr := config.LoadConfigFromDefaultPath()
		if loadErr == nil && snapshot != nil && p.BaseHash != "" {
			if snapshot.Hash != p.BaseHash {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrConflict, "config has been modified since last read (hash mismatch)"))
			}
		}

		// Write config to the default path.
		cfgPath := config.ResolveConfigPath()
		if err := os.WriteFile(cfgPath, []byte(p.Raw), 0644); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "failed to write config: "+err.Error()))
		}

		newHash := config.HashString(p.Raw)

		if deps.Broadcaster != nil {
			deps.Broadcaster("config.changed", map[string]any{"hash": newHash})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"ok":       true,
			"hash":     newHash,
			"warnings": warnings,
		})
		return resp
	}
}

func configApply(deps ConfigAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Raw            string `json:"raw"`
			BaseHash       string `json:"baseHash"`
			SessionKey     string `json:"sessionKey,omitempty"`
			Note           string `json:"note,omitempty"`
			RestartDelayMs int    `json:"restartDelayMs,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Raw == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "raw is required"))
		}

		// Validate JSON syntax and config structure.
		issues, warnings, err := config.ValidateRawConfig([]byte(p.Raw))
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "config validation error: "+err.Error()))
		}
		if len(issues) > 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid config: "+issues[0].String()))
		}

		// Concurrency check.
		snapshot, loadErr := config.LoadConfigFromDefaultPath()
		if loadErr == nil && snapshot != nil && p.BaseHash != "" {
			if snapshot.Hash != p.BaseHash {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrConflict, "config has been modified since last read (hash mismatch)"))
			}
		}

		cfgPath := config.ResolveConfigPath()
		if err := os.WriteFile(cfgPath, []byte(p.Raw), 0644); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "failed to write config: "+err.Error()))
		}

		newHash := config.HashString(p.Raw)

		if deps.Broadcaster != nil {
			deps.Broadcaster("config.applied", map[string]any{
				"hash":       newHash,
				"sessionKey": p.SessionKey,
				"note":       p.Note,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"ok":       true,
			"hash":     newHash,
			"warnings": warnings,
		})
		return resp
	}
}

func configPatch(deps ConfigAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Raw            string `json:"raw"`
			BaseHash       string `json:"baseHash"`
			SessionKey     string `json:"sessionKey,omitempty"`
			Note           string `json:"note,omitempty"`
			RestartDelayMs int    `json:"restartDelayMs,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Raw == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "raw is required"))
		}

		// Parse the patch.
		var patch map[string]any
		if err := json.Unmarshal([]byte(p.Raw), &patch); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid JSON patch: "+err.Error()))
		}

		// Load current config for concurrency check.
		snapshot, err := config.LoadConfigFromDefaultPath()
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "failed to load config: "+err.Error()))
		}

		// Concurrency check.
		if p.BaseHash != "" {
			if snapshot.Hash != p.BaseHash {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrConflict, "config has been modified since last read (hash mismatch)"))
			}
		}

		// Merge patch into current config.
		var current map[string]any
		if err := json.Unmarshal([]byte(snapshot.Raw), &current); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "failed to parse current config: "+err.Error()))
		}
		for k, v := range patch {
			current[k] = v
		}

		merged, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "failed to marshal merged config"))
		}

		// Validate merged config before writing.
		issues, warnings, valErr := config.ValidateRawConfig(merged)
		if valErr != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "config validation error: "+valErr.Error()))
		}
		if len(issues) > 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "merged config is invalid: "+issues[0].String()))
		}

		cfgPath := config.ResolveConfigPath()
		if err := os.WriteFile(cfgPath, merged, 0644); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "failed to write config: "+err.Error()))
		}

		newHash := config.HashString(string(merged))

		if deps.Broadcaster != nil {
			deps.Broadcaster("config.patched", map[string]any{
				"hash":       newHash,
				"sessionKey": p.SessionKey,
				"note":       p.Note,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"ok":       true,
			"hash":     newHash,
			"warnings": warnings,
		})
		return resp
	}
}

func configSchema(_ ConfigAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		schema := config.GetSchema()
		resp := protocol.MustResponseOK(req.ID, schema)
		return resp
	}
}

func configSchemaLookup(_ ConfigAdvancedDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Path == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "path is required"))
		}

		node := config.LookupSchema(p.Path)
		if node == nil {
			resp := protocol.MustResponseOK(req.ID, nil)
			return resp
		}
		resp := protocol.MustResponseOK(req.ID, node)
		return resp
	}
}
