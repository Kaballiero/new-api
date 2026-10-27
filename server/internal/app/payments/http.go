package payments

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/payments"
	creemadapter "github.com/QuantumNous/new-api/internal/adapter/http/payments/creem"
	epayadapter "github.com/QuantumNous/new-api/internal/adapter/http/payments/epay"
	pancakeadapter "github.com/QuantumNous/new-api/internal/adapter/http/payments/pancake"
	stripeadapter "github.com/QuantumNous/new-api/internal/adapter/http/payments/stripe"
	waffoadapter "github.com/QuantumNous/new-api/internal/adapter/http/payments/waffo"
	settings "github.com/QuantumNous/new-api/internal/adapter/settings/payments"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	settingsapp "github.com/QuantumNous/new-api/internal/app/settings"
	"github.com/QuantumNous/new-api/internal/app/shared"
	subscriptionsapp "github.com/QuantumNous/new-api/internal/app/subscriptions"
	"github.com/QuantumNous/new-api/internal/payments/admin_list_topups"
	"github.com/QuantumNous/new-api/internal/payments/complete_topup"
	"github.com/QuantumNous/new-api/internal/payments/confirm_compliance"
	"github.com/QuantumNous/new-api/internal/payments/create_creem_topup"
	"github.com/QuantumNous/new-api/internal/payments/create_epay_topup"
	"github.com/QuantumNous/new-api/internal/payments/create_pancake_pair"
	"github.com/QuantumNous/new-api/internal/payments/create_pancake_topup"
	"github.com/QuantumNous/new-api/internal/payments/create_stripe_topup"
	"github.com/QuantumNous/new-api/internal/payments/create_waffo_topup"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/get_topup_info"
	"github.com/QuantumNous/new-api/internal/payments/list_own_topups"
	"github.com/QuantumNous/new-api/internal/payments/list_pancake_catalog"
	"github.com/QuantumNous/new-api/internal/payments/quote_epay_topup"
	"github.com/QuantumNous/new-api/internal/payments/quote_pancake_topup"
	"github.com/QuantumNous/new-api/internal/payments/quote_stripe_topup"
	"github.com/QuantumNous/new-api/internal/payments/quote_waffo_topup"
	"github.com/QuantumNous/new-api/internal/payments/receive_creem_webhook"
	"github.com/QuantumNous/new-api/internal/payments/receive_epay_notify"
	"github.com/QuantumNous/new-api/internal/payments/receive_pancake_webhook"
	"github.com/QuantumNous/new-api/internal/payments/receive_stripe_webhook"
	"github.com/QuantumNous/new-api/internal/payments/receive_waffo_webhook"
	"github.com/QuantumNous/new-api/internal/payments/save_pancake_config"
	"github.com/QuantumNous/new-api/model"
)

type HTTP struct {
	QuotePancake   quote_pancake_topup.HTTP
	CreatePancake  create_pancake_topup.HTTP
	PancakeWebhook receive_pancake_webhook.HTTP
	Catalog        list_pancake_catalog.HTTP
	Pair           create_pancake_pair.HTTP
	SavePancake    save_pancake_config.HTTP
	Compliance     confirm_compliance.HTTP

	QuoteWaffo    quote_waffo_topup.HTTP
	CreateWaffo   create_waffo_topup.HTTP
	WaffoWebhook  receive_waffo_webhook.HTTP
	CreateCreem   create_creem_topup.HTTP
	CreemWebhook  receive_creem_webhook.HTTP
	QuoteStripe   quote_stripe_topup.HTTP
	CreateStripe  create_stripe_topup.HTTP
	StripeWebhook receive_stripe_webhook.HTTP
	QuoteEpay     quote_epay_topup.HTTP
	CreateEpay    create_epay_topup.HTTP
	EpayNotify    receive_epay_notify.HTTP
	ListOwn       list_own_topups.HTTP
	ListAll       admin_list_topups.HTTP
	Complete      complete_topup.HTTP
	Info          get_topup_info.HTTP
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
	catalog := list_pancake_catalog.NewUsecase(pancakeadapter.Factory{}, settings.Settings{}, paymentDiagnostics{})
	return HTTP{
		QuotePancake: quote_pancake_topup.NewHTTP(quote_pancake_topup.NewUsecase(pricing, storage.Users{Accounts: accountstorage.SQL()})),
		CreatePancake: create_pancake_topup.NewHTTP(
			create_pancake_topup.NewUsecase(
				pricing,
				storage.Users{Accounts: accountstorage.SQL()},
				store,
				pancakeadapter.Client{},
				settings.Settings{},
				create_pancake_topup.Timing{Now: common.GetTimestamp, Reference: func(userID int) string {
					return fmt.Sprintf("WAFFO_PANCAKE-%d-%d-%s", userID, time.Now().UnixMilli(), randstr.String(6))
				}},
				paymentDiagnostics{}),
			func() bool { return settings.Settings{}.Availability().TopUp(domain.PaymentProviderWaffoPancake) },
		),
		PancakeWebhook: receive_pancake_webhook.NewHTTP(
			receive_pancake_webhook.NewUsecase(pancakeadapter.Client{}, store, subscriptionDispatch{orders: subscriptionsapp.NewOrders()}, completion, &shared.Orders, paymentDiagnostics{}),
			func() bool { return settings.Settings{}.Availability().Webhook(domain.PaymentProviderWaffoPancake) },
			func(c *gin.Context, payload []byte, signature string) {
				observeWebhook(c, "Waffo Pancake", payload, signature)
			},
		),
		Catalog:     list_pancake_catalog.NewHTTP(catalog),
		Pair:        create_pancake_pair.NewHTTP(create_pancake_pair.NewUsecase(pancakeadapter.Factory{}, settings.Settings{}, paymentDiagnostics{})),
		SavePancake: save_pancake_config.NewHTTP(save_pancake_config.NewUsecase(options{}, settings.Settings{}), paymentDiagnostics{}),
		Compliance:  confirm_compliance.NewHTTP(confirm_compliance.NewUsecase(options{}, settings.Settings{}.Terms, common.GetTimestamp, paymentDiagnostics{})),
		QuoteWaffo:  quote_waffo_topup.NewHTTP(quote_waffo_topup.NewUsecase(pricing, storage.Users{Accounts: accountstorage.SQL()})),
		CreateWaffo: create_waffo_topup.NewHTTP(
			create_waffo_topup.NewUsecase(
				pricing,
				storage.Users{Accounts: accountstorage.SQL()},
				store,
				waffoadapter.Factory{},
				settings.Settings{},
				create_waffo_topup.Timing{Now: common.GetTimestamp, Reference: func(userID int) string {
					return fmt.Sprintf("WAFFO-%d-%d-%s", userID, time.Now().UnixMilli(), randstr.String(6))
				}, RequestedAt: func() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }},
				paymentDiagnostics{}),
			settings.Settings{}.WaffoEnabled,
		),
		WaffoWebhook: receive_waffo_webhook.NewHTTP(
			receive_waffo_webhook.NewUsecase(waffoadapter.Factory{}, store, completion, &shared.Orders, waffoObservation{}, paymentDiagnostics{}),
			func() bool { return settings.Settings{}.Availability().Webhook(domain.PaymentProviderWaffo) },
		),
		CreateCreem: create_creem_topup.NewHTTP(
			create_creem_topup.NewUsecase(
				pricing,
				storage.Users{Accounts: accountstorage.SQL()},
				store,
				creemadapter.Client{},
				settings.Settings{},
				create_creem_topup.Timing{Now: common.GetTimestamp, Reference: func(userID int) string {
					return "ref_" + common.Sha1([]byte(fmt.Sprintf("creem-api-ref-%d-%d-%s", userID, time.Now().UnixMilli(), randstr.String(4))))
				}},
				paymentDiagnostics{}),
			observeCreemPay,
		),
		CreemWebhook: receive_creem_webhook.NewHTTP(
			receive_creem_webhook.NewUsecase(creemadapter.Client{}, store, subscriptionDispatch{orders: subscriptionsapp.NewOrders()}, completion, &shared.Orders, paymentDiagnostics{}),
			func() bool { return settings.Settings{}.Availability().Webhook(domain.PaymentProviderCreem) },
			func(c *gin.Context, payload []byte, signature string) { observeWebhook(c, "Creem", payload, signature) },
		),
		QuoteStripe: quote_stripe_topup.NewHTTP(quote_stripe_topup.NewUsecase(pricing, storage.Users{Accounts: accountstorage.SQL()})),
		CreateStripe: create_stripe_topup.NewHTTP(
			create_stripe_topup.NewUsecase(
				pricing,
				storage.Users{Accounts: accountstorage.SQL()},
				store,
				stripeadapter.Client{},
				common.ValidateRedirectURL,
				create_stripe_topup.Timing{Now: common.GetTimestamp, Reference: func(userID int) string {
					return "ref_" + common.Sha1([]byte(fmt.Sprintf("new-api-ref-%d-%d-%s", userID, time.Now().UnixMilli(), randstr.String(4))))
				}},
				paymentDiagnostics{}),
		),
		StripeWebhook: receive_stripe_webhook.NewHTTP(
			receive_stripe_webhook.NewUsecase(stripeadapter.Client{}, store, store, subscriptionDispatch{orders: subscriptionsapp.NewOrders()}, completion, &shared.Orders, paymentDiagnostics{}),
			func() bool { return settings.Settings{}.Availability().Webhook(domain.PaymentProviderStripe) },
			func(c *gin.Context, payload []byte, signature string) {
				observeWebhook(c, "Stripe", payload, signature)
			},
		),
		QuoteEpay: quote_epay_topup.NewHTTP(quote_epay_topup.NewUsecase(pricing, storage.Users{Accounts: accountstorage.SQL()})),
		CreateEpay: create_epay_topup.NewHTTP(
			create_epay_topup.NewUsecase(
				pricing,
				storage.Users{Accounts: accountstorage.SQL()},
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
			receive_epay_notify.NewUsecase(epayadapter.Factory{}, completion, &shared.Orders, paymentDiagnostics{}),
			func() bool { return settings.Settings{}.Availability().Webhook(domain.PaymentProviderEpay) },
			observeEpay,
		),
		ListOwn:  list_own_topups.NewHTTP(list_own_topups.NewUsecase(store, common.GetTimestamp)),
		ListAll:  admin_list_topups.NewHTTP(admin_list_topups.NewUsecase(store)),
		Complete: complete_topup.NewHTTP(complete_topup.NewUsecase(completion, &shared.Orders)),
		Info:     get_topup_info.NewHTTP(get_topup_info.NewUsecase(settings.Settings{})),
	}
}

// options connects payment workflows to the general settings owner.
type options struct{}

func (options) Set(key, value string) error { return settingsapp.Default().Write.Execute(key, value) }
func (options) Bulk(values map[string]string) error {
	return settingsapp.Default().Bulk.Execute(context.Background(), values)
}
