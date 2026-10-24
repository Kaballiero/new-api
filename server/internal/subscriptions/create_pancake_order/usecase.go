package create_pancake_order

import (
	"context"
	"strings"

	"github.com/shopspring/decimal"

	payments "github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

type Diagnostics interface {
	Error(context.Context, string, ...any)
	Info(context.Context, string, ...any)
}

type Input struct{ UserID, PlanID int }

type Store interface {
	PlanByID(int) (*domain.SubscriptionPlan, error)
	CountPurchases(int, int) (int64, error)
	InsertOrder(*domain.SubscriptionOrder) error
	UserByID(int) (*domain.CheckoutUser, error)
	SaveOrder(*domain.SubscriptionOrder) error
}
type Settings interface{ Availability() payments.Availability }

type Result struct {
	Session *payments.WaffoPancakeCheckoutSession
	OrderID string
}

type Usecase struct {
	store                             Store
	settings                          Settings
	client                            payments.PancakeCheckout
	diagnostics                       Diagnostics
	now                               func() int64
	reference                         func(int) string
	pending, failed, method, provider string
}

func NewUsecase(
	store Store,
	settings Settings,
	client payments.PancakeCheckout,
	diagnostics Diagnostics,
	now func() int64,
	reference func(int) string,
	pending, failed, method, provider string,
) Usecase {
	return Usecase{store, settings, client, diagnostics, now, reference, pending, failed, method, provider}
}
func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	userID, planID := in.UserID, in.PlanID
	plan, err := u.store.PlanByID(planID)
	if err != nil {
		return Result{}, err
	}
	if !plan.Enabled {
		return Result{}, domain.ErrPlanDisabled
	}
	if strings.TrimSpace(plan.WaffoPancakeProductId) == "" {
		return Result{}, domain.ErrPancakeProductMissing
	}
	a := u.settings.Availability()
	if strings.TrimSpace(a.PancakeMerchant) == "" || strings.TrimSpace(a.PancakePrivate) == "" {
		return Result{}, domain.ErrPancakeConfig
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
	ref := u.reference(userID)
	order := domain.SubscriptionOrder{
		UserId:          userID,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         ref,
		PaymentMethod:   u.method,
		PaymentProvider: u.provider,
		CreateTime:      u.now(),
		Status:          u.pending,
	}
	if err := u.store.InsertOrder(&order); err != nil {
		u.diagnostics.Error(
			ctx,
			"Waffo Pancake 订阅订单创建失败 user_id=%d plan_id=%d trade_no=%s error=%q",
			userID,
			plan.Id,
			ref,
			err.Error(),
		)
		return Result{}, domain.ErrCreateOrder
	}
	expiry := 45 * 60
	session, err := payments.CreatePancakeCheckout(
		ctx,
		u.client,
		&payments.WaffoPancakeCreateSessionParams{
			ProductID:     plan.WaffoPancakeProductId,
			BuyerIdentity: payments.PancakeBuyerIdentity(user.ID),
			PriceSnapshot: &payments.WaffoPancakePriceSnapshot{
				Amount:      decimal.NewFromFloat(plan.PriceAmount).StringFixed(2),
				TaxCategory: "saas",
			},
			BuyerEmail:              payments.PancakeBuyerEmail(user.Email),
			ExpiresInSeconds:        &expiry,
			OrderMerchantExternalID: ref,
		},
	)
	if err != nil {
		u.diagnostics.Error(
			ctx,
			"Waffo Pancake 订阅结账会话创建失败 user_id=%d plan_id=%d trade_no=%s error=%q",
			userID,
			plan.Id,
			ref,
			err.Error(),
		)
		order.Status = u.failed
		_ = u.store.SaveOrder(&order)
		return Result{}, domain.ErrCheckout
	}
	u.diagnostics.Info(
		ctx,
		"Waffo Pancake 订阅订单创建成功 user_id=%d plan_id=%d trade_no=%s session_id=%s money=%.2f",
		userID,
		plan.Id,
		ref,
		session.SessionID,
		plan.PriceAmount,
	)
	return Result{Session: session, OrderID: ref}, nil
}
