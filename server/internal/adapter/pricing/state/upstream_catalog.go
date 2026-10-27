package pricingstate

import (
	"gorm.io/gorm"

	store "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	legacy "github.com/QuantumNous/new-api/model"
)

func CatalogHash(data []byte) string { return domain.CatalogHash(data) }

func NewImmutableCatalogPersistence() store.Immutable {
	return store.NewImmutable(func() *gorm.DB { return legacy.DB })
}
