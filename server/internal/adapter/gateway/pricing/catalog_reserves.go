package pricing

import (
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/constant"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/estimate_upstream_reserve"
	"github.com/QuantumNous/new-api/internal/pricing/preview_channel_reserves"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// CatalogReserves is the ReserveChecker of the OpenRouter gateway. It runs the
// admission estimate (openrouter.EstimateRequestReserve) of a reference
// request on the given endpoint tariffs alone: its estimator is never bound to
// a request, so a model the tariffs do not price is refused without calling
// OpenRouter.
type CatalogReserves struct {
	// MaxAge bounds the age of the catalog data a reserve is priced from.
	MaxAge func() time.Duration
}

// Check returns the admission error of the reference request of model for
// operation (chat/completions or embeddings), nil when the tariffs price its
// reserve. The request is routed to the official OpenRouter API of a channel
// without settings, so no manual reserve or configured price covers it.
func (r CatalogReserves) Check(tariffs map[string]domain.NativeEndpointResponse, model, operation string) error {
	var catalog atomic.Pointer[domain.UpstreamCatalogTariffs]
	catalog.Store(&domain.UpstreamCatalogTariffs{FetchedAt: time.Now(), Models: tariffs})
	reserve := estimate_upstream_reserve.NewUsecase(pricingadapter.Capabilities(), &catalog, time.Now, r.MaxAge)
	info, body, err := referenceRelayRequest(preview_channel_reserves.ReferenceRequest{Model: model, UpstreamModel: model,
		Operation: preview_channel_reserves.ReferenceOperation(operation), PromptTokens: preview_channel_reserves.ReferencePromptTokens})
	if err != nil {
		return err
	}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter,
		ChannelBaseUrl: constant.ChannelBaseURLs[constant.ChannelTypeOpenRouter], UpstreamModelName: model}
	_, _, err = openrouter.EstimateRequestReserve(openrouter.NewCatalogEstimator(reserve), info, body, operation, nil)
	return err
}
