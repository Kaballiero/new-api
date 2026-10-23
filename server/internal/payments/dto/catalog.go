package dto

import (
	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type WaffoPancakeCatalogProduct struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// WaffoPancakeCatalogStore nests its OnetimeProducts so the UI can render a
// dependent store→product select without a second round-trip.
type WaffoPancakeCatalogStore struct {
	ID              string                       `json:"id"`
	Name            string                       `json:"name"`
	Status          string                       `json:"status"`
	ProdEnabled     bool                         `json:"prodEnabled"`
	OnetimeProducts []WaffoPancakeCatalogProduct `json:"onetimeProducts"`
}

type WaffoPancakeCatalog struct {
	Stores []WaffoPancakeCatalogStore `json:"stores"`
}

func FromCatalog(catalog *domain.WaffoPancakeCatalog) *WaffoPancakeCatalog {
	if catalog == nil {
		return nil
	}
	out := &WaffoPancakeCatalog{}
	if catalog.Stores != nil {
		out.Stores = make([]WaffoPancakeCatalogStore, len(catalog.Stores))
	}
	for i, s := range catalog.Stores {
		var products []WaffoPancakeCatalogProduct
		if s.OnetimeProducts != nil {
			products = make([]WaffoPancakeCatalogProduct, len(s.OnetimeProducts))
		}
		for j, p := range s.OnetimeProducts {
			products[j] = WaffoPancakeCatalogProduct{ID: p.ID, Name: p.Name, Status: p.Status}
		}
		out.Stores[i] = WaffoPancakeCatalogStore{ID: s.ID, Name: s.Name, Status: s.Status, ProdEnabled: s.ProdEnabled, OnetimeProducts: products}
	}
	return out
}
