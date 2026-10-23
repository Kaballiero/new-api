package payments

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/logger"
)

// waffoObservation retains the original SDK-build-before-payload-log boundary.

// paymentDiagnostics writes request-correlated events without retaining a Gin context.
type paymentDiagnostics struct{}

func (paymentDiagnostics) Info(ctx context.Context, format string, values ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	logger.LogInfo(ctx, fmt.Sprintf(format, paymentLogValues(values)...))
}

func (paymentDiagnostics) Warn(ctx context.Context, format string, values ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	logger.LogWarn(ctx, fmt.Sprintf(format, paymentLogValues(values)...))
}

func (paymentDiagnostics) Error(ctx context.Context, format string, values ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	logger.LogError(ctx, fmt.Sprintf(format, paymentLogValues(values)...))
}

// paymentLogValues retains legacy domain error bytes in the structural move.
func paymentLogValues(values []any) []any {
	for i, value := range values {
		if err, ok := value.(error); ok {
			values[i] = paymentLogError(err)
		}
	}
	return values
}
