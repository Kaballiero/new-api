package tasks

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/tasks/clear_sync_task_credentials"
	"github.com/QuantumNous/new-api/logger"

	"github.com/bytedance/gopkg/util/gopool"
)

// StartSyncTaskCredentialCleanup removes, once per master start, the account
// credentials that earlier releases stored in synchronous Task rows.
func StartSyncTaskCredentialCleanup() {
	if !common.IsMasterNode {
		return
	}
	gopool.Go(func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.LogError(context.Background(), fmt.Sprintf("synchronous task credential cleanup panicked: %v", recovered))
			}
		}()
		cleared, err := clear_sync_task_credentials.NewUsecase(NewStore()).Execute(context.Background())
		if err != nil {
			logger.LogError(context.Background(), fmt.Sprintf("synchronous task credential cleanup stopped after %d tasks: %v", cleared, err))
			return
		}
		if cleared > 0 {
			common.SysLog(fmt.Sprintf("synchronous task credential cleanup cleared %d tasks", cleared))
		}
	})
}
