package create_epay_order

import (
	"fmt"
	payments "github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"net/url"
	"strconv"
)

type Store interface {
	PlanByID(int) (*domain.SubscriptionPlan, error)
	CountPurchases(int, int) (int64, error)
	InsertOrder(*domain.SubscriptionOrder) error
}
type Settings interface {
	CallbackAddress() string
	ContainsMethod(string) bool
}

type Input struct {
	UserID, PlanID int
	PaymentMethod  string
}
type Result struct {
	URL    string
	Params map[string]string
}
type Usecase struct {
	store             Store
	settings          Settings
	factory           payments.EpayFactory
	expiry            domain.OrderExpiry
	now               func() int64
	reference         func(int) string
	pending, provider string
}

func NewUsecase(store Store, settings Settings, factory payments.EpayFactory, expiry domain.OrderExpiry, now func() int64, reference func(int) string, pending, provider string) Usecase {
	return Usecase{store, settings, factory, expiry, now, reference, pending, provider}
}
func (u Usecase) Execute(in Input) (Result, error) {
	plan, err := u.store.PlanByID(in.PlanID)
	if err != nil {
		return Result{}, err
	}
	if !plan.Enabled {
		return Result{}, domain.ErrPlanDisabled
	}
	if plan.PriceAmount < 0.01 {
		return Result{}, domain.ErrPriceLow
	}
	if !u.settings.ContainsMethod(in.PaymentMethod) {
		return Result{}, domain.ErrPayMethod
	}
	if plan.MaxPurchasePerUser > 0 {
		count, err := u.store.CountPurchases(in.UserID, plan.Id)
		if err != nil {
			return Result{}, err
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			return Result{}, domain.ErrPurchaseLimit
		}
	}
	callback := u.settings.CallbackAddress()
	back, err := url.Parse(callback + "/api/subscription/epay/return")
	if err != nil {
		return Result{}, domain.ErrCallbackConfig
	}
	notify, err := url.Parse(callback + "/api/subscription/epay/notify")
	if err != nil {
		return Result{}, domain.ErrCallbackConfig
	}
	ref := u.reference(in.UserID)
	client := u.factory.Client()
	if client == nil {
		return Result{}, domain.ErrEpayConfig
	}
	order := domain.SubscriptionOrder{UserId: in.UserID, PlanId: plan.Id, Money: plan.PriceAmount, TradeNo: ref, PaymentMethod: in.PaymentMethod, PaymentProvider: u.provider, CreateTime: u.now(), Status: u.pending}
	if err := u.store.InsertOrder(&order); err != nil {
		return Result{}, domain.ErrCreateOrder
	}
	uri, params, err := client.Purchase(payments.EpayPurchase{Method: in.PaymentMethod, TradeNo: ref, Name: fmt.Sprintf("SUB:%s", plan.Title), Money: strconv.FormatFloat(plan.PriceAmount, 'f', 2, 64), NotifyURL: notify.String(), ReturnURL: back.String()})
	if err != nil {
		_ = u.expiry.Expire(ref, u.provider)
		return Result{}, domain.ErrCheckout
	}
	return Result{uri, params}, nil
}
