package pricingruntime

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/read_upstream_catalogue"
)

func LoadConfiguredCatalog(ctx context.Context) (map[string]*pricingdomain.SourceDocument, map[string]string, bool, error) {
	return cachedReadUpstreamCatalogue().Execute(ctx)
}

// RefreshUpstreamCatalogTariffs publishes the endpoint tariffs of the current
// upstream catalog head, so that the reserve estimates follow a head that
// moved without a catalogue rebuild, including a head that moved only its
// fetchedAt.
func RefreshUpstreamCatalogTariffs() {
	if _, _, _, err := LoadConfiguredCatalog(context.Background()); err != nil {
		common.SysError("failed to refresh upstream catalog tariffs: " + err.Error())
	}
}

// UpstreamCatalogDocuments keeps the decoded documents of the published
// upstream catalog revision; the catalogue rebuild, the upstream catalog sync
// and the upstream model detection share them.
var UpstreamCatalogDocuments pricingadapter.UpstreamCatalogDocumentsCache

// UpstreamCatalogTariffs keeps the endpoint tariffs of the published upstream
// catalog revision for the OpenRouter reserve estimates.
var UpstreamCatalogTariffs atomic.Pointer[pricingdomain.UpstreamCatalogTariffs]

// upstreamCatalogRevision keeps the upstream catalog revision the reader read
// last.
var upstreamCatalogRevision atomic.Pointer[read_upstream_catalogue.Revision]

var cachedReadUpstreamCatalogue = sync.OnceValue(func() read_upstream_catalogue.Usecase {
	return read_upstream_catalogue.NewUsecase(
		pricingadapter.Capabilities(),
		pricingstate.NewImmutableCatalogPersistence(),
		&UpstreamCatalogDocuments,
		&upstreamCatalogRevision,
		&UpstreamCatalogTariffs,
		func() (pricingdomain.CatalogSource, bool, error) {
			v, on, err := pricingstate.ConfiguredCatalogSource()
			return pricingdomain.CatalogSource(v), on, err
		},
	)
})
