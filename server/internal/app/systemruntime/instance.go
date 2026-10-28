package systemruntime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	runtime "github.com/QuantumNous/new-api/internal/adapter/runtime/system"
	core "github.com/QuantumNous/new-api/internal/app/system"
	"github.com/QuantumNous/new-api/internal/system/report_system_instance"
	"github.com/QuantumNous/new-api/logger"

	"github.com/bytedance/gopkg/util/gopool"
)

const systemInstanceReportInterval = 30 * time.Second

var systemInstanceReporterOnce sync.Once
var instanceReporter = sync.OnceValue(func() report_system_instance.Usecase {
	return report_system_instance.NewUsecase(runtime.InstanceFacts{}, core.Default().Store, common.GetTimestamp, runtime.Facts{}.StartTime)
})

func StartSystemInstanceReporter() {
	systemInstanceReporterOnce.Do(func() {
		gopool.Go(func() {
			reportSystemInstanceWithLog()

			ticker := time.NewTicker(systemInstanceReportInterval)
			defer ticker.Stop()
			for range ticker.C {
				reportSystemInstanceWithLog()
			}
		})
	})
}

func reportSystemInstanceWithLog() {
	if err := instanceReporter().Execute(); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system instance report failed: %v", err))
	}
}
