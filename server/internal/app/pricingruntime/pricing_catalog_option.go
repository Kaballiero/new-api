package pricingruntime

import (
	"context"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"sync"

	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"gorm.io/gorm"

	store "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing"

	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"

	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"

	publication "github.com/QuantumNous/new-api/internal/adapter/settings/publication"
	"github.com/QuantumNous/new-api/internal/pricing/publish_native_catalogue"

	legacy "github.com/QuantumNous/new-api/model"
)

func PublishPricingCatalogContext(ctx context.Context, entries []pricingdomain.PricingCatalogEntry) (int, error) {
	return NativePublication().Execute(ctx, entries, true)
}

var NativePublication = sync.OnceValue(func() publish_native_catalogue.Usecase {
	return publish_native_catalogue.NewUsecase(
		pricingadapter.Capabilities(),
		store.NewNative(store.NativeEnvironment{
			DB:       func() *gorm.DB { return legacy.DB },
			LockRows: locking.LockForUpdate,
			Lock:     publication.LockOptionPublication,
			Snapshot: pricingstate.NativeCatalogSnapshot,
			Publish:  publication.PublishOption,
		}),
	)
})
