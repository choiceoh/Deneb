package provider

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"
)

// ManagedCredential holds a provider credential with rotation metadata.
type ManagedCredential struct {
	ProviderID  string `json:"providerId"`
	ProfileID   string `json:"profileId,omitempty"`
	APIKey      string `json:"apiKey"`
	BaseURL     string `json:"baseUrl,omitempty"`
	AuthMode    string `json:"authMode,omitempty"`
	ExpiresAt   int64  `json:"expiresAt,omitempty"`   // unix ms, 0 = no expiry
	RefreshedAt int64  `json:"refreshedAt,omitempty"` // unix ms of last refresh
}

// credKey returns the map key for a credential.
func credKey(providerID, profileID string) string {
	if profileID == "" {
		return providerID
	}
	return providerID + ":" + profileID
}

// IsExpired reports whether the credential has passed its expiry time.
func (mc *ManagedCredential) IsExpired() bool {
	if mc.ExpiresAt == 0 {
		return false
	}
	return time.Now().UnixMilli() >= mc.ExpiresAt
}

// IsExpiringSoon reports whether the credential will expire within the given duration.
func (mc *ManagedCredential) IsExpiringSoon(within time.Duration) bool {
	if mc.ExpiresAt == 0 {
		return false
	}
	return time.Now().Add(within).UnixMilli() >= mc.ExpiresAt
}

// AuthManager manages provider credentials with background key rotation.
type AuthManager struct {
	mu          sync.RWMutex
	credentials map[string]*ManagedCredential
	registry    *Registry
	logger      *slog.Logger
	stopCh      chan struct{}

	// File-state-based cache invalidation fields.
	authFilePath  string
	lastAuthMtime int64 // Unix nano of last known mtime
	lastAuthSize  int64 // File size at last read
}

// NewAuthManager creates a new auth manager.
func NewAuthManager(registry *Registry, logger *slog.Logger) *AuthManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthManager{
		credentials: make(map[string]*ManagedCredential),
		registry:    registry,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}
}

// SetAuthFilePath sets the path to the auth store file for cache invalidation.
func (am *AuthManager) SetAuthFilePath(path string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.authFilePath = path
}

// hasAuthFileChanged checks if the auth store file has been modified
// since the last read. Returns true if the file should be reloaded.
func (am *AuthManager) hasAuthFileChanged() bool {
	if am.authFilePath == "" {
		return false
	}
	info, err := os.Stat(am.authFilePath)
	if err != nil {
		return true // File missing or inaccessible → force reload
	}
	mtime := info.ModTime().UnixNano()
	size := info.Size()
	return mtime != am.lastAuthMtime || size != am.lastAuthSize
}

// markAuthFileRead updates the cached file state after a successful read.
func (am *AuthManager) markAuthFileRead() {
	if am.authFilePath == "" {
		return
	}
	info, err := os.Stat(am.authFilePath)
	if err != nil {
		return
	}
	am.lastAuthMtime = info.ModTime().UnixNano()
	am.lastAuthSize = info.Size()
}

// Store adds or updates a credential in the manager.
func (am *AuthManager) Store(cred *ManagedCredential) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.credentials[credKey(cred.ProviderID, cred.ProfileID)] = cred
}

// Resolve returns the current credential for a provider+profile.
// Returns a copy to avoid data races.
func (am *AuthManager) Resolve(providerID, profileID string) *ManagedCredential {
	am.mu.RLock()
	defer am.mu.RUnlock()

	key := credKey(NormalizeProviderIDForAuth(providerID), profileID)
	cred := am.credentials[key]
	if cred == nil {
		return nil
	}
	c := *cred
	return &c
}

// Prepare resolves runtime auth for a provider. If the provider plugin
// implements RuntimeAuthProvider, it delegates locally; otherwise it
// returns the provided API key as-is.
func (am *AuthManager) Prepare(ctx context.Context, req RuntimeAuthContext) (*PreparedAuth, error) {
	// Try local provider plugin first.
	if am.registry != nil {
		plugin := am.registry.GetByNormalizedID(req.Provider)
		if rap, ok := plugin.(RuntimeAuthProvider); ok {
			prepared, err := rap.PrepareRuntimeAuth(ctx, req)
			if err != nil {
				am.logger.Warn("local provider auth prepare failed",
					"provider", req.Provider, "error", err)
			} else if prepared != nil {
				am.storePrepared(req.Provider, req.ProfileID, prepared)
				return prepared, nil
			}
		}
	}

	// No local provider plugin found — passthrough with provided API key.
	return &PreparedAuth{
		APIKey:  req.APIKey,
		BaseURL: "",
	}, nil
}

// storePrepared updates the credential store after a successful auth preparation.
func (am *AuthManager) storePrepared(providerID, profileID string, prepared *PreparedAuth) {
	am.mu.Lock()
	defer am.mu.Unlock()

	key := credKey(NormalizeProviderIDForAuth(providerID), profileID)
	existing := am.credentials[key]
	if existing == nil {
		existing = &ManagedCredential{
			ProviderID: providerID,
			ProfileID:  profileID,
		}
	}
	existing.APIKey = prepared.APIKey
	if prepared.BaseURL != "" {
		existing.BaseURL = prepared.BaseURL
	}
	existing.ExpiresAt = prepared.ExpiresAt
	existing.RefreshedAt = time.Now().UnixMilli()
	am.credentials[key] = existing
}

// RefreshIfNeeded checks if a credential needs refresh and refreshes it locally.
func (am *AuthManager) RefreshIfNeeded(ctx context.Context, providerID, profileID string) error {
	cred := am.Resolve(providerID, profileID)
	if cred == nil || !cred.IsExpiringSoon(5*time.Minute) {
		return nil
	}

	_, err := am.Prepare(ctx, RuntimeAuthContext{
		Provider:  providerID,
		ModelID:   "", // not needed for refresh
		APIKey:    cred.APIKey,
		AuthMode:  cred.AuthMode,
		ProfileID: profileID,
	})
	return err
}

// StartRotationLoop runs a background goroutine that refreshes expiring credentials.
// Call Stop() to terminate the loop.
func (am *AuthManager) StartRotationLoop(ctx context.Context) {
	go am.rotationLoop(ctx)
}

// Stop terminates the rotation loop.
func (am *AuthManager) Stop() {
	select {
	case <-am.stopCh:
	default:
		close(am.stopCh)
	}
}

func (am *AuthManager) rotationLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-am.stopCh:
			return
		case <-ticker.C:
			am.refreshExpiring(ctx)
		}
	}
}

func (am *AuthManager) refreshExpiring(ctx context.Context) {
	am.mu.RLock()
	var expiring []ManagedCredential
	for _, cred := range am.credentials {
		if cred.IsExpiringSoon(5 * time.Minute) {
			expiring = append(expiring, *cred)
		}
	}
	am.mu.RUnlock()

	for _, cred := range expiring {
		refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := am.RefreshIfNeeded(refreshCtx, cred.ProviderID, cred.ProfileID); err != nil {
			am.logger.Warn("credential refresh failed",
				"provider", cred.ProviderID,
				"profile", cred.ProfileID,
				"error", err,
			)
		}
		cancel()
	}
}
