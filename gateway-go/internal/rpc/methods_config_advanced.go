package rpc

import (
	"context"
	"encoding/json"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ConfigAdvancedDeps holds the dependencies for advanced config RPC methods.
type ConfigAdvancedDeps struct {
	Broadcaster BroadcastFunc
}

// RegisterConfigAdvancedMethods registers config.set, config.apply, config.patch,
// config.schema, and config.schema.lookup RPC methods.
func RegisterConfigAdvancedMethods(d *Dispatcher, deps ConfigAdvancedDeps) {
	d.Register("config.set", configSet(deps))
	d.Register("config.apply", configApply(deps))
	d.Register("config.patch", configPatch(deps))
	d.Register("config.schema", configSchema(deps))
	d.Register("config.schema.lookup", configSchemaLookup(deps))
}

func configSet(deps ConfigAdvancedDeps) HandlerFunc {
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

		// Validate JSON.
		var parsed json.RawMessage
		if err := json.Unmarshal([]byte(p.Raw), &parsed); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid JSON: "+err.Error()))
		}

		// Load current config to verify baseHash (optimistic concurrency).
		snapshot, err := config.LoadConfigFromDefaultPath()
		if err == nil && snapshot != nil && p.BaseHash != "" {
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

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ok":   true,
			"hash": newHash,
		})
		return resp
	}
}

func configApply(deps ConfigAdvancedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Raw             string `json:"raw"`
			BaseHash        string `json:"baseHash"`
			SessionKey      string `json:"sessionKey,omitempty"`
			Note            string `json:"note,omitempty"`
			RestartDelayMs  int    `json:"restartDelayMs,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Raw == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "raw is required"))
		}

		// Validate JSON.
		var parsed json.RawMessage
		if err := json.Unmarshal([]byte(p.Raw), &parsed); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid JSON: "+err.Error()))
		}

		// Concurrency check.
		snapshot, err := config.LoadConfigFromDefaultPath()
		if err == nil && snapshot != nil && p.BaseHash != "" {
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

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ok":   true,
			"hash": newHash,
		})
		return resp
	}
}

func configPatch(deps ConfigAdvancedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Raw             string `json:"raw"`
			BaseHash        string `json:"baseHash"`
			SessionKey      string `json:"sessionKey,omitempty"`
			Note            string `json:"note,omitempty"`
			RestartDelayMs  int    `json:"restartDelayMs,omitempty"`
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

		// Load current config.
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
			current = make(map[string]any)
		}
		for k, v := range patch {
			current[k] = v
		}

		merged, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "failed to marshal merged config"))
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

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ok":   true,
			"hash": newHash,
		})
		return resp
	}
}

func configSchema(_ ConfigAdvancedDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		schema := config.GetSchema()
		resp, _ := protocol.NewResponseOK(req.ID, schema)
		return resp
	}
}

func configSchemaLookup(_ ConfigAdvancedDeps) HandlerFunc {
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
			resp, _ := protocol.NewResponseOK(req.ID, nil)
			return resp
		}
		resp, _ := protocol.NewResponseOK(req.ID, node)
		return resp
	}
}

