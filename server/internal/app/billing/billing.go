// Package billing assembles request billing without importing gateway types.
package billing

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	notification "github.com/QuantumNous/new-api/internal/adapter/billing"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/billing"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/apikeystorage"
	"github.com/QuantumNous/new-api/internal/app/dbclock"
	"github.com/QuantumNous/new-api/internal/app/notifications"
	"github.com/QuantumNous/new-api/internal/app/quotaruntime"
	"github.com/QuantumNous/new-api/internal/app/subscriptionstorage"
	"github.com/QuantumNous/new-api/internal/billing/apply_quota_delta"
	"github.com/QuantumNous/new-api/internal/billing/create_reservation"
	"github.com/QuantumNous/new-api/internal/billing/increase_reservation"
	"github.com/QuantumNous/new-api/internal/billing/notify_quota"
	"github.com/QuantumNous/new-api/internal/billing/refund_reservation"
	"github.com/QuantumNous/new-api/internal/billing/settle_reservation"

	"github.com/bytedance/gopkg/util/gopool"
)

type Usecases struct {
	Create   create_reservation.Usecase
	Increase increase_reservation.Usecase
	Settle   settle_reservation.Usecase
	Refund   refund_reservation.Usecase
	Apply    apply_quota_delta.Usecase
	Notify   notify_quota.Usecase
}

var sharedStore = sync.OnceValue(func() storage.Store {
	accounts := accountstorage.SQL()
	return storage.Store{Wallet: storage.WalletCapabilities{
		Balance: func(id int) (int, error) { return accounts.GetUserQuota(id, false) },
		Debit:   func(id, amount int) error { return accounts.DecreaseUserQuota(id, amount, false) },
		Credit:  func(id, amount int) error { return accounts.IncreaseUserQuota(id, amount, false) },
	}, Reserves: quotaruntime.Reserves(), Keys: apikeystorage.Reader(), KeyQuotas: apikeystorage.Quota(),
		SubscriptionPlans: subscriptionstorage.Cache(), DatabaseTime: dbclock.Now}
})

func NewStore() storage.Store { return sharedStore() }

func New() Usecases {
	store := NewStore()
	return Usecases{
		Create:   create_reservation.NewUsecase(store, common.GetTrustQuota, common.SysLog),
		Increase: increase_reservation.NewUsecase(store, common.SysLog),
		Settle:   settle_reservation.NewUsecase(store, common.SysLog),
		Refund:   refund_reservation.NewUsecase(store, gopool.Go, time.Sleep, common.SysLog),
		Apply:    apply_quota_delta.NewUsecase(store),
		Notify:   notify_quota.NewUsecase(notification.Notification{Send: notifications.Send}, func() int { return common.QuotaRemindThreshold }, gopool.Go, common.SysError),
	}
}
