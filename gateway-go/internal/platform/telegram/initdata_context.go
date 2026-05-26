package telegram

import "context"

// initDataContextKey is an unexported type to scope the context value lookup
// so callers cannot accidentally collide with another package's context keys.
type initDataContextKey struct{}

// WithInitDataContext returns a copy of ctx carrying the verified InitData.
// The HTTP middleware in front of the Mini App RPC endpoint calls this after
// VerifyInitData succeeds; downstream RPC handlers then retrieve it with
// InitDataFromContext.
func WithInitDataContext(ctx context.Context, data *InitData) context.Context {
	if data == nil {
		return ctx
	}
	return context.WithValue(ctx, initDataContextKey{}, data)
}

// InitDataFromContext returns the InitData attached by WithInitDataContext,
// or nil if the context carries none. A nil result means the caller reached
// the handler without passing the initData middleware — handlers should
// respond with an UNAUTHORIZED error in that case.
func InitDataFromContext(ctx context.Context) *InitData {
	if ctx == nil {
		return nil
	}
	data, _ := ctx.Value(initDataContextKey{}).(*InitData)
	return data
}
