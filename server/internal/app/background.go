package app

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	counters "github.com/QuantumNous/new-api/internal/adapter/gorm/counters"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
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
