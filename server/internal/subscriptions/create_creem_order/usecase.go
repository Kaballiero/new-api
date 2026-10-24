package create_creem_order

import (
	"context"
	payments "github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

type Store interface {
	PlanByID(int) (*domain.SubscriptionPlan, error)
	CountPurchases(int, int) (int64, error)
	InsertOrder(*domain.SubscriptionOrder) error
	UserByID(int) (*domain.CheckoutUser, error)
}
type Settings interface {
	Availability() payments.Availability
	CreemTestMode() bool
}

type Client interface {
	Checkout(context.Context, string, payments.CreemProduct, string, string) (string, error)
}
type Result struct{ CheckoutURL, OrderID string }
type Usecase struct {
	store                     Store
	settings                  Settings
	client                    Client
	diagnostics               domain.CheckoutDiagnostics
	now                       func() int64
	reference                 func(string) string
	currency                  func() string
	pending, method, provider string
}

func NewUsecase(store Store, settings Settings, client Client, diagnostics domain.CheckoutDiagnostics, now func() int64, reference func(string) string, currency func() string, pending, method, provider string) Usecase {
	return Usecase{store, settings, client, diagnostics, now, reference, currency, pending, method, provider}
}
func (u Usecase) Execute(ctx context.Context, userID, planID int) (Result, error) {
	plan, err := u.store.PlanByID(planID)
	if err != nil {
		return Result{}, err
	}
	if !plan.Enabled {
		return Result{}, domain.ErrPlanDisabled
	}
	if plan.CreemProductId == "" {
		return Result{}, domain.ErrCreemProductMissing
	}
	if u.settings.Availability().CreemWebhook == "" && !u.settings.CreemTestMode() {
		return Result{}, domain.ErrCreemWebhook
	}
	user, err := u.store.UserByID(userID)
	if err != nil {
		return Result{}, err
	}
	if user == nil {
		return Result{}, domain.ErrUserMissing
	}
	if plan.MaxPurchasePerUser > 0 {
		count, err := u.store.CountPurchases(userID, plan.Id)
		if err != nil {
			return Result{}, err
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			return Result{}, domain.ErrPurchaseLimit
		}
	}
	ref := u.reference(user.Username)
	order := domain.SubscriptionOrder{UserId: userID, PlanId: plan.Id, Money: plan.PriceAmount, TradeNo: ref, PaymentMethod: u.method, PaymentProvider: u.provider, CreateTime: u.now(), Status: u.pending}
	if err := u.store.InsertOrder(&order); err != nil {
		return Result{}, domain.ErrCreateOrder
	}
	product := payments.CreemProduct{ProductId: plan.CreemProductId, Name: plan.Title, Price: plan.PriceAmount, Currency: u.currency(), Quota: 0}
	link, err := u.client.Checkout(ctx, ref, product, user.Email, user.Username)
	if err != nil {
		u.diagnostics.Error(ctx, "Creem 订阅支付链接创建失败 trade_no=%s product_id=%s error=%q", ref, product.ProductId, err.Error())
		return Result{}, domain.ErrCheckout
	}
	return Result{link, ref}, nil
}
