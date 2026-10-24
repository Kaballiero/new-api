package taskruntime

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/logger"
)

// Diagnostics preserves task polling's existing operational emitters.
type Diagnostics struct{}

func (Diagnostics) SysLog(message string)                     { common.SysLog(message) }
func (Diagnostics) Info(ctx context.Context, message string)  { logger.LogInfo(ctx, message) }
func (Diagnostics) Warn(ctx context.Context, message string)  { logger.LogWarn(ctx, message) }
func (Diagnostics) Error(ctx context.Context, message string) { logger.LogError(ctx, message) }
func (Diagnostics) Debug(ctx context.Context, format string, args ...any) {
	logger.LogDebug(ctx, format, args...)
}
func (Diagnostics) Json(ctx context.Context, message string, value any) {
	if task, ok := value.(*domain.Task); ok {
		value = record.FromDomain(task)
	}
	logger.LogJson(ctx, message, value)
}

func (Diagnostics) SettlementRepair(ctx context.Context, taskID string) {
	logger.LogWarn(ctx, fmt.Sprintf("OpenRouter synchronous task %s requires settlement repair", taskID))
}
