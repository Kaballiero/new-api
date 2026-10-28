package payments

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/setting"
)

type CreemProduct struct {
	ProductId string  `json:"productId"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	Quota     int64   `json:"quota"`
}

func (Settings) Products() ([]domain.CreemProduct, error) {
	var wire []CreemProduct
	if err := common.Unmarshal([]byte(setting.CreemProducts), &wire); err != nil {
		return nil, err
	}
	var products []domain.CreemProduct
	for _, p := range wire {
		products = append(products, domain.CreemProduct{ProductId: p.ProductId, Name: p.Name, Price: p.Price, Currency: p.Currency, Quota: p.Quota})
	}
	return products, nil
}
