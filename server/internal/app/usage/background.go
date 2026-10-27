package usage

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/app/usagestorage"
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
	usagestorage.Store().SaveQuotaDataCache()
}
