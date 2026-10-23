package create_creem_topup

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

// Products reads the current product list, preserving malformed configuration as an error.
type Products interface {
	Products() ([]domain.CreemProduct, error)
}

type Input struct {
	UserID                   int
	ProductID, PaymentMethod string
}

type Result struct{ CheckoutURL, OrderID string }

type Timing struct {
	Now       func() int64
	Reference func(userID int) string
}

type Usecase struct {
	diagnostics domain.Diagnostics
	pricing     domain.Pricing
	users       domain.Users
	orders      domain.Orders
	client      domain.Creem
	products    Products
	timing      Timing
}

func NewUsecase(pricing domain.Pricing, users domain.Users, orders domain.Orders, client domain.Creem, products Products, timing Timing, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, pricing: pricing, users: users, orders: orders, client: client, products: products, timing: timing}
}

func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	if in.PaymentMethod != domain.PaymentMethodCreem {
		return Result{}, domain.ErrChannel
	}
	if in.ProductID == "" {
		return Result{}, domain.ErrProductRequired
	}
	products, err := u.products.Products()
	if err != nil {
		u.diagnostics.Error(ctx, "Creem product configuration parsing failed user_id=%d error=%q", in.UserID, err)
		return Result{}, domain.ErrProductsConfig
	}
	var product *domain.CreemProduct
	for _, candidate := range products {
		if candidate.ProductId == in.ProductID {
			product = &candidate
			break
		}
	}
	if product == nil {
		return Result{}, domain.ErrProductMissing
	}
	if err := u.pricing.CheckCredited(in.UserID, decimal.NewFromInt(product.Quota)); err != nil {
		return Result{}, err
	}
	user, err := u.users.Find(in.UserID)
	if err != nil || user == nil {
		return Result{}, domain.ErrUser
	}
	reference := u.timing.Reference(user.ID)
	order := domain.Order{
		UserID:          in.UserID,
		Amount:          product.Quota,
		Money:           product.Price,
		TradeNo:         reference,
		PaymentMethod:   domain.PaymentMethodCreem,
		PaymentProvider: domain.PaymentProviderCreem,
		CreateTime:      u.timing.Now(),
		Status:          domain.StatusPending,
	}
	if err := u.orders.Insert(&order); err != nil {
		u.diagnostics.Error(ctx, "Creem topup order creation failed user_id=%d trade_no=%s product_id=%s error=%q", in.UserID, reference, product.ProductId, err)
		return Result{}, domain.ErrInsert
	}
	link, err := u.client.Checkout(ctx, reference, *product, user.Email, user.Username)
	if err != nil {
		u.diagnostics.Error(ctx, "Creem payment link creation failed user_id=%d trade_no=%s product_id=%s error=%q", in.UserID, reference, product.ProductId, err)
		return Result{}, domain.ErrCheckout
	}
	u.diagnostics.Info(ctx, "Creem topup order created user_id=%d trade_no=%s product_id=%s product_name=%q quota=%d money=%.2f", in.UserID, reference, product.ProductId, product.Name, product.Quota, product.Price)
	return Result{CheckoutURL: link, OrderID: reference}, nil
}
