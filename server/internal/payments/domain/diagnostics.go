package domain

import "context"

// Diagnostics retains payment operational messages and severity at each observed branch.
type Diagnostics interface {
	Info(ctx context.Context, format string, values ...any)
	Warn(ctx context.Context, format string, values ...any)
	Error(ctx context.Context, format string, values ...any)
}
