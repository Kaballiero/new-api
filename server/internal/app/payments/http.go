package payments

import (
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/payments"

	settings "github.com/QuantumNous/new-api/internal/adapter/settings/payments"
	"github.com/QuantumNous/new-api/internal/payments/admin_list_topups"
	"github.com/QuantumNous/new-api/internal/payments/complete_topup"

	"github.com/QuantumNous/new-api/internal/payments/domain"

	"github.com/QuantumNous/new-api/internal/payments/get_topup_info"
	"github.com/QuantumNous/new-api/internal/payments/list_own_topups"

	"github.com/QuantumNous/new-api/model"
)

type HTTP struct {
	ListOwn  list_own_topups.HTTP
	ListAll  admin_list_topups.HTTP
	Complete complete_topup.HTTP
	Info     get_topup_info.HTTP
}

// NewHTTP wires request-independent dependencies with late database and settings reads.
func NewHTTP() HTTP {
	store := storage.NewStore(func() *gorm.DB { return model.DB })
	completion := domain.Completion{
		Transactions: store,
		Math:         quotaMath{},
		Effects:      completionEffects{},
		Now:          common.GetTimestamp,
		QuotaPerUnit: settings.Settings{}.QuotaPerUnit,
	}

	return HTTP{

		ListOwn: list_own_topups.NewHTTP(list_own_topups.NewUsecase(store, common.GetTimestamp)),

		ListAll: admin_list_topups.NewHTTP(admin_list_topups.NewUsecase(store)),

		Complete: complete_topup.NewHTTP(complete_topup.NewUsecase(completion, &orderAdmission)),

		Info: get_topup_info.NewHTTP(get_topup_info.NewUsecase(settings.Settings{})),
	}
}

// Payment and subscription callbacks share one admission registry.
var orderAdmission OrderAdmission

// BRIDGE: controller/topup LockOrder for subscription_payment_epay; remove after subscription admission is extracted.
func LockOrder(tradeNo string) { orderAdmission.Lock(tradeNo) }

// BRIDGE: controller/topup UnlockOrder for subscription_payment_epay; remove after subscription admission is extracted.
func UnlockOrder(tradeNo string) { orderAdmission.Unlock(tradeNo) }

// BRIDGE: controller.GetEpayClient for subscription_payment_epay; remove after subscription Epay wiring is extracted.

// BRIDGE: subscription_payment_creem checkout; remove when it injects the Creem port directly.

// BRIDGE: subscription_payment_waffo_pancake checkout; remove when it injects the Pancake checkout port directly.

// BRIDGE: subscription_payment_waffo_pancake_config plan creation; remove when it injects the management port directly.

// BRIDGE: subscription_payment_waffo_pancake_config product dropdown; remove when its catalog dependency is injected.

// BRIDGE: subscription_payment_stripe/epay redirects; remove when subscription settings own their address dependency.

// BRIDGE: subscription_payment_epay checkout; remove when subscription settings own their callback dependency.
