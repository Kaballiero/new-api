// Package usagestorage assembles the shared log writer and accounting adapters.
package usagestorage

import (
	"sync"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/counters"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	tasktext "github.com/QuantumNous/new-api/internal/adapter/taskruntime"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/apikeystorage"
	"github.com/QuantumNous/new-api/model"
)

var writerOnce = sync.OnceValue(func() storage.Writer {
	accounts := accountstorage.SQL()
	return storage.NewWriter(storage.WriterEnvironment{
		DB: func() *gorm.DB { return model.DB }, LogDB: func() *gorm.DB { return model.LOG_DB },
		GetUsernameById: accounts.GetUsernameById, GetUserSetting: accounts.GetUserSetting,
	})
})

func Writer() storage.Writer { return writerOnce() }

var storeOnce = sync.OnceValue(func() storage.Store {
	accounts := accountstorage.SQL()
	return storage.Store{
		ConsumeEnabled: func() bool { return common.LogConsumeEnabled },
		ExportEnabled:  func() bool { return common.DataExportEnabled },
		ReadToken:      apikeystorage.Persistence().Get, Writer: Writer(), UserConsumption: accounts.UpdateUserUsedQuotaAndRequestCount,
		ChannelConsumption: func(id, quota int) { counters.UpdateChannelUsedQuota(model.DB, id, quota) },
	}
})

func Store() storage.Store { return storeOnce() }

var taskBillingOnce = sync.OnceValue(func() storage.TaskBilling {
	return storage.TaskBilling{
		Writer: Writer(), AdjustmentText: (tasktext.TaskText{Language: accountstorage.Cache().GetUserLanguage}).Adjustment, UserUsedQuota: accountstorage.SQL().UpdateUserUsedQuota,
		ChannelUsedQuota: func(id, quota int) { counters.UpdateChannelUsedQuota(model.DB, id, quota) },
	}
})

func TaskBilling() storage.TaskBilling { return taskBillingOnce() }
