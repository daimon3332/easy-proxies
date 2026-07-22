package monitor

import "context"

type contextKey string

const managerKey contextKey = "easy_proxies.monitor"

// ContextWith attaches the manager into context so downstream components can reuse it.
func ContextWith(ctx context.Context, mgr *Manager) context.Context {
	if mgr == nil {
		return ctx
	}
	return context.WithValue(ctx, managerKey, mgr)
}

// FromContext extracts a manager if present.
func FromContext(ctx context.Context) *Manager {
	mgr, _ := ctx.Value(managerKey).(*Manager)
	return mgr
}
