package app

import (
	counters "github.com/QuantumNous/new-api/internal/adapter/gorm/counters"
	"gorm.io/gorm"

	"time"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// StartBatchUpdater schedules the shared user/token/channel counter queue.
func StartBatchUpdater() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Duration(common.BatchUpdateInterval) * time.Second)
			counters.FlushBatchUpdates(func() *gorm.DB { return model.DB })
		}
	})
}
