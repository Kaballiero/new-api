package usage

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
)

func StartQuotaData() {
	go func() {
		for {
			if common.DataExportEnabled {
				common.SysLog("updating dashboard usage data")
				FlushQuotaData()
			}
			time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
		}
	}()
}

func FlushQuotaData() {
	storage.Store{}.SaveQuotaDataCache()
}
