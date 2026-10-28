package create_pancake_product

import (
	"context"
	"fmt"
	"strings"

	payments "github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

type CatalogSettings interface {
	PancakeCredentials() payments.PancakeCredentials
	PancakeStore() string
	PancakeReturn() string
}

type Diagnostics interface {
	Error(context.Context, string, ...any)
}

type Input struct{ Name, Amount string }
type Result struct{ ProductID, ProductName, StoreID string }
type Usecase struct {
	settings    CatalogSettings
	factory     payments.PancakeFactory
	diagnostics Diagnostics
}

func NewUsecase(settings CatalogSettings, factory payments.PancakeFactory, diagnostics Diagnostics) Usecase {
	return Usecase{settings, factory, diagnostics}
}
func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	if strings.TrimSpace(in.Name) == "" {
		return Result{}, domain.ErrProductName
	}
	if strings.TrimSpace(in.Amount) == "" {
		return Result{}, domain.ErrProductAmount
	}
	creds := u.settings.PancakeCredentials()
	storeID := strings.TrimSpace(u.settings.PancakeStore())
	if creds.MerchantID == "" || creds.PrivateKey == "" || storeID == "" {
		return Result{}, domain.ErrPancakeBinding
	}
	client, err := u.factory.Management(creds)
	productID := ""
	if err == nil {
		productID, err = client.CreateProduct(
			ctx,
			payments.PancakeProduct{
				StoreID:   storeID,
				Name:      strings.TrimSpace(in.Name),
				Amount:    strings.TrimSpace(in.Amount),
				ReturnURL: u.settings.PancakeReturn(),
			},
		)
		if err != nil {
			err = fmt.Errorf("create Waffo Pancake plan product: %w", err)
		} else if err = client.Publish(ctx, productID); err != nil {
			err = fmt.Errorf("publish Waffo Pancake plan product: %w", err)
		}
	}
	if err != nil {
		u.diagnostics.Error(
			ctx,
			"Waffo Pancake plan product creation failed store_id=%q name=%q amount=%q error=%q",
			storeID,
			in.Name,
			in.Amount,
			err.Error(),
		)
		return Result{}, domain.ErrCreateProduct
	}
	return Result{productID, in.Name, storeID}, nil
}
