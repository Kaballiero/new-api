package save_pancake_config

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

// Options commits all selected options atomically before publishing runtime values.
type Options interface {
	Bulk(values map[string]string) error
}

// Settings reads the persisted product/store after the option commit.
type Settings interface{ PancakeIDs() (string, string) }

type Input struct{ MerchantID, PrivateKey, ReturnURL, StoreID, ProductID string }

type Result struct{ StoreID, ProductID string }

type Usecase struct {
	options  Options
	settings Settings
}

func NewUsecase(options Options, settings Settings) Usecase {
	return Usecase{options: options, settings: settings}
}

func (u Usecase) Execute(in Input) (Result, error) {
	merchant, store, product := strings.TrimSpace(in.MerchantID), strings.TrimSpace(in.StoreID), strings.TrimSpace(in.ProductID)
	if merchant == "" || store == "" || product == "" {
		return Result{}, domain.ConfigFailure{Cause: fmt.Errorf("merchant id, store id, and product id are required to save")}
	}
	values := map[string]string{
		"WaffoPancakeMerchantID": merchant,
		"WaffoPancakeReturnURL":  strings.TrimSpace(in.ReturnURL),
		"WaffoPancakeStoreID":    store,
		"WaffoPancakeProductID":  product,
	}
	if key := strings.TrimSpace(in.PrivateKey); key != "" {
		values["WaffoPancakePrivateKey"] = key
	}
	if err := u.options.Bulk(values); err != nil {
		return Result{}, domain.ConfigFailure{Cause: fmt.Errorf("persist Waffo Pancake config: %w", err)}
	}
	store, product = u.settings.PancakeIDs()
	return Result{StoreID: store, ProductID: product}, nil
}
