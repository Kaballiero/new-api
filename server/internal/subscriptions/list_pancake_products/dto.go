package list_pancake_products

import payments "github.com/QuantumNous/new-api/internal/payments/domain"

type Product struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

func catalogProducts(catalog *payments.WaffoPancakeCatalog, storeID string) []Product {
	products := []Product{}
	for _, store := range catalog.Stores {
		if store.ID != storeID {
			continue
		}
		if store.OnetimeProducts == nil {
			return nil
		}
		products = make([]Product, len(store.OnetimeProducts))
		for i, p := range store.OnetimeProducts {
			products[i] = Product{ID: p.ID, Name: p.Name, Status: p.Status}
		}
		break
	}
	return products
}
