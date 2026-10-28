// Package quotaruntime assembles the existing Redis-first quota reservation protocol.
package quotaruntime

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/billing"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/apikeystorage"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

var shared = sync.OnceValue(func() billing.ReserveStore {
	return billing.NewReserveStore(billing.ReserveEnvironment{
		DB: func() *gorm.DB { return model.DB }, GetUserCache: accountstorage.Cache().GetUserCache,
		GetTokenByKey: apikeystorage.Reader().Read, DecreaseTokenQuota: apikeystorage.Quota().DecreaseTokenQuota,
	})
})

func Reserves() billing.ReserveStore { return shared() }
