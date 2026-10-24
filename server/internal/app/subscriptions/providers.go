package subscriptions

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/subscriptions"
	creem "github.com/QuantumNous/new-api/internal/adapter/http/payments/creem"
	epay "github.com/QuantumNous/new-api/internal/adapter/http/payments/epay"
	pancake "github.com/QuantumNous/new-api/internal/adapter/http/payments/pancake"
	stripe "github.com/QuantumNous/new-api/internal/adapter/http/subscriptions/stripe"
	settings "github.com/QuantumNous/new-api/internal/adapter/settings/subscriptions"
	"github.com/QuantumNous/new-api/internal/app/shared"
	payments "github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/create_creem_order"
	"github.com/QuantumNous/new-api/internal/subscriptions/create_epay_order"
	"github.com/QuantumNous/new-api/internal/subscriptions/create_pancake_order"
	"github.com/QuantumNous/new-api/internal/subscriptions/create_pancake_product"
	"github.com/QuantumNous/new-api/internal/subscriptions/create_stripe_order"
	"github.com/QuantumNous/new-api/internal/subscriptions/list_pancake_products"
	"github.com/QuantumNous/new-api/internal/subscriptions/receive_epay_callback"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/thanhpk/randstr"
	"gorm.io/gorm"
	"time"
)

type diagnostics struct{}

func (diagnostics) Error(ctx context.Context, format string, values ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	logger.LogError(ctx, fmt.Sprintf(format, values...))
}
func (diagnostics) Info(ctx context.Context, format string, values ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	logger.LogInfo(ctx, fmt.Sprintf(format, values...))
}
func (diagnostics) Warn(ctx context.Context, format string, values ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	logger.LogWarn(ctx, fmt.Sprintf(format, values...))
}

type Providers struct {
	Epay          create_epay_order.HTTP
	Stripe        create_stripe_order.HTTP
	Creem         create_creem_order.HTTP
	Pancake       create_pancake_order.HTTP
	EpayCallback  receive_epay_callback.HTTP
	CreateProduct create_pancake_product.HTTP
	Products      list_pancake_products.HTTP
}

func NewProviders() Providers {
	store := storage.NewStore(func() *gorm.DB { return model.DB })
	config := settings.Settings{}
	compliance := operation_setting.IsPaymentComplianceConfirmed
	d := diagnostics{}
	now := func() int64 { return time.Now().Unix() }
	return Providers{

		Epay: create_epay_order.NewHTTP(create_epay_order.NewUsecase(store, config, epay.Factory{}, NewOrders(), now, func(id int) string {
			return fmt.Sprintf("SUBUSR%dNO%s%d", id, common.GetRandomString(6), time.Now().Unix())
		}, payments.StatusPending, payments.PaymentProviderEpay), compliance),

		Stripe: create_stripe_order.NewHTTP(create_stripe_order.NewUsecase(store, config, stripe.Client{}, d, now, func(id int) string {
			reference := fmt.Sprintf("sub-stripe-ref-%d-%d-%s", id, time.Now().UnixMilli(), randstr.String(4))
			return "sub_ref_" + common.Sha1([]byte(reference))
		}, payments.StatusPending, payments.PaymentMethodStripe, payments.PaymentProviderStripe), compliance),

		Creem: create_creem_order.NewHTTP(create_creem_order.NewUsecase(store, config, creem.Client{}, d, now, func(username string) string {
			reference := "sub-creem-ref-" + randstr.String(6)
			return "sub_ref_" + common.Sha1([]byte(reference+time.Now().String()+username))
		}, config.Currency, payments.StatusPending, payments.PaymentMethodCreem, payments.PaymentProviderCreem), compliance, d),

		Pancake: create_pancake_order.NewHTTP(
			create_pancake_order.NewUsecase(store, config, pancake.Client{}, d, now, func(id int) string {
				return fmt.Sprintf("WAFFO_PANCAKE_SUB-%d-%d-%s", id, time.Now().UnixMilli(), randstr.String(6))
			}, payments.StatusPending, payments.StatusFailed, payments.PaymentMethodWaffoPancake, payments.PaymentProviderWaffoPancake),
			compliance,
		),

		EpayCallback: receive_epay_callback.NewHTTP(
			receive_epay_callback.NewUsecase(
				epay.Factory{},
				NewOrders(),
				&shared.Orders,
				payments.PaymentProviderEpay,
			),
			config.ReturnPath,
		),

		CreateProduct: create_pancake_product.NewHTTP(create_pancake_product.NewUsecase(config, pancake.Factory{}, d)),
		Products: list_pancake_products.NewHTTP(list_pancake_products.NewUsecase(
			config,
			pancake.Factory{},
			d,
		)),
	}
}
