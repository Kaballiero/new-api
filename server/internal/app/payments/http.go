package payments

import (
	"fmt"

	"github.com/Calcium-Ion/go-epay/epay"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/payments"

	epayadapter "github.com/QuantumNous/new-api/internal/adapter/http/payments/epay"

	settings "github.com/QuantumNous/new-api/internal/adapter/settings/payments"
	"github.com/QuantumNous/new-api/internal/payments/admin_list_topups"
	"github.com/QuantumNous/new-api/internal/payments/complete_topup"

	"github.com/QuantumNous/new-api/internal/payments/create_epay_topup"

	"github.com/QuantumNous/new-api/internal/payments/domain"

	"github.com/QuantumNous/new-api/internal/payments/get_topup_info"
	"github.com/QuantumNous/new-api/internal/payments/list_own_topups"

	"github.com/QuantumNous/new-api/internal/payments/quote_epay_topup"

	"github.com/QuantumNous/new-api/internal/payments/receive_epay_notify"

	"github.com/QuantumNous/new-api/model"
)

type HTTP struct {
	QuoteEpay  quote_epay_topup.HTTP
	CreateEpay create_epay_topup.HTTP
	EpayNotify receive_epay_notify.HTTP
	ListOwn    list_own_topups.HTTP
	ListAll    admin_list_topups.HTTP
	Complete   complete_topup.HTTP
	Info       get_topup_info.HTTP
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
	pricing := domain.Pricing{Settings: settings.Settings{}, Math: quotaMath{}, Wallet: storage.Wallet{}}

	return HTTP{

		QuoteEpay: quote_epay_topup.NewHTTP(quote_epay_topup.NewUsecase(pricing, storage.Users{})),

		CreateEpay: create_epay_topup.NewHTTP(
			create_epay_topup.NewUsecase(
				pricing,
				storage.Users{},
				store,
				settings.Settings{},
				epayadapter.Factory{},
				create_epay_topup.Timing{Now: common.GetTimestamp, TradeNo: func(userID int) string {
					return fmt.Sprintf("USR%dNO%s%d", userID, common.GetRandomString(6), common.GetTimestamp())
				}},
				paymentDiagnostics{}),
			observeEpayCheckout,
		),

		EpayNotify: receive_epay_notify.NewHTTP(
			receive_epay_notify.NewUsecase(epayadapter.Factory{}, completion, &orderAdmission, paymentDiagnostics{}),
			func() bool { return settings.Settings{}.Availability().Webhook(domain.PaymentProviderEpay) },
			observeEpay,
		),

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
func EpayClient() *epay.Client { return epayadapter.NewClient() }

// BRIDGE: subscription_payment_creem checkout; remove when it injects the Creem port directly.

// BRIDGE: subscription_payment_waffo_pancake checkout; remove when it injects the Pancake checkout port directly.

// BRIDGE: subscription_payment_waffo_pancake_config plan creation; remove when it injects the management port directly.

// BRIDGE: subscription_payment_waffo_pancake_config product dropdown; remove when its catalog dependency is injected.

// BRIDGE: subscription_payment_stripe/epay redirects; remove when subscription settings own their address dependency.
func ReturnPath(suffix string) string { return settings.Settings{}.ReturnPath(suffix) }

// BRIDGE: subscription_payment_epay checkout; remove when subscription settings own their callback dependency.
func CallbackAddress() string { return settings.Settings{}.CallbackAddress() }
