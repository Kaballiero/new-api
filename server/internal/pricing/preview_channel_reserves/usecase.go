package preview_channel_reserves

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

// referencePromptTokens are the prompt tokens of the reference request.
const referencePromptTokens = 1000

// ChannelStore reads the channel whose reserves are previewed.
type ChannelStore interface {
	BuildChannel(int) (*domain.BuildChannel, error)
	IsMissing(error) bool
}

// CatalogTariffs reads the endpoint tariffs of the published upstream catalog
// revision; nil before any.
type CatalogTariffs interface {
	Load() *domain.UpstreamCatalogTariffs
}

// ReserveSettings are the OpenRouter reserve settings of a channel by upstream
// model.
type ReserveSettings struct {
	// Sources are the configured reservation sources.
	Sources map[string]string
	// ManualUSD are the manual reserves, the legacy Sonar research reserve
	// included.
	ManualUSD map[string]float64
}

// ReferenceRequest is the reference request of an upstream model of the
// channel: Chat Completions or Embeddings with PromptTokens, or the speech or
// rerank reference of the gateway side. Only and Ignore are its provider.only
// and provider.ignore.
type ReferenceRequest struct {
	// Model is the channel model: the billing model of a configured price.
	Model         string
	UpstreamModel string
	Operation     string
	PromptTokens  int
	Only, Ignore  []string
}

// ReservePreview is the reserve the gateway admission takes for a reference
// request. AdmissionPolicy is empty when no channel reserve applies and
// ConfiguredUSD is nil without a configured_price source.
type ReservePreview struct {
	USD             float64
	Source          domain.ReserveSource
	AdmissionPolicy domain.AdmissionPolicy
	ConfiguredUSD   *float64
}

// ReservePreviewer is the gateway side of the preview: the reserve settings of
// a channel and the reserve its admission takes for a reference request,
// priced from the published upstream catalog without discovering live
// endpoints.
type ReservePreviewer interface {
	Settings(*domain.BuildChannel) (ReserveSettings, error)
	Preview(*domain.BuildChannel, ReferenceRequest) (ReservePreview, error)
}

type Usecase struct {
	deps      domain.Capabilities
	channels  ChannelStore
	catalog   CatalogTariffs
	previewer ReservePreviewer
	now       func() time.Time
	// maxAge bounds the age of the catalog data a reserve is priced from.
	maxAge func() time.Duration
	// outputTokens are the output tokens the gateway reserves without an
	// output limit.
	outputTokens int
}

func NewUsecase(deps domain.Capabilities, channels ChannelStore, catalog CatalogTariffs, previewer ReservePreviewer, now func() time.Time, maxAge func() time.Duration, outputTokens int) Usecase {
	return Usecase{deps: deps, channels: channels, catalog: catalog, previewer: previewer, now: now, maxAge: maxAge, outputTokens: outputTokens}
}

// Execute previews the reserves of the models of an OpenRouter channel and of
// the reserve settings of models outside it. The examples are priced only
// from a fresh upstream catalog on the official OpenRouter API, as the
// gateway prices them; other models are live only and never discovered.
func (u Usecase) Execute(channelID int) (Output, error) {
	channel, err := u.channels.BuildChannel(channelID)
	if u.channels.IsMissing(err) {
		return Output{}, channeldomain.ErrChannelNotFound
	}
	if err != nil {
		return Output{}, err
	}
	if channel.Type != constant.ChannelTypeOpenRouter {
		return Output{}, domain.ErrChannelNotOpenRouter
	}
	settings, err := u.previewer.Settings(channel)
	if err != nil {
		return Output{}, err
	}
	out := Output{Reference: Reference{Operation: "chat/completions", PromptTokens: referencePromptTokens, OutputTokens: u.outputTokens}, Items: []Item{}}
	var catalog map[string]domain.NativeEndpointResponse
	if published := u.catalog.Load(); published != nil {
		out.FetchedAt = &published.FetchedAt
		out.CatalogStale = !published.FreshAt(u.now(), u.maxAge())
		if !out.CatalogStale && domain.IsOpenRouterCatalogBaseURL(channel.GetBaseURL()) {
			catalog = published.Models
		}
	}
	inChannel := make(map[string]bool)
	for _, model := range channel.GetModels() {
		upstream, resolved := domain.ResolvePricingUpstreamModel(u.deps, channel, model)
		if !resolved {
			out.Items = append(out.Items, Item{Model: model, InChannel: true, ExampleStatus: ExampleStatusRefused, Providers: []Provider{}})
			continue
		}
		inChannel[upstream] = true
		out.Items = append(out.Items, u.preview(channel, settings, catalog, model, upstream, true))
	}
	keys := slices.Concat(slices.Collect(maps.Keys(settings.Sources)), slices.Collect(maps.Keys(settings.ManualUSD)))
	slices.Sort(keys)
	for _, upstream := range slices.Compact(keys) {
		if !inChannel[upstream] {
			out.Items = append(out.Items, u.preview(channel, settings, catalog, upstream, upstream, false))
		}
	}
	return out, nil
}

// preview prices the reference request of an upstream model and, per endpoint
// tag of its catalog model, the same request restricted to that provider.
func (u Usecase) preview(channel *domain.BuildChannel, settings ReserveSettings, catalog map[string]domain.NativeEndpointResponse, model, upstream string, inChannel bool) Item {
	item := Item{Model: model, UpstreamModel: &upstream, InChannel: inChannel, ExampleStatus: ExampleStatusLiveOnly, Providers: []Provider{}}
	if source, configured := settings.Sources[upstream]; configured {
		item.ConfiguredSource = &source
	}
	if reserve, configured := settings.ManualUSD[upstream]; configured {
		item.ManualUSD = &reserve
	}
	document, documented := catalog[domain.UpstreamTariffModel(upstream, false)]
	request := ReferenceRequest{Model: model, UpstreamModel: upstream, Operation: "chat/completions", PromptTokens: referencePromptTokens}
	switch {
	case !documented:
		return item
	case slices.Equal(document.OutputModalities, []string{"embeddings"}):
		request.Operation = "embeddings"
	case slices.Contains(document.OutputModalities, "speech"):
		// Speech and rerank are priced from the measured quantities of their
		// own reference request, not from prompt tokens.
		request.Operation, request.PromptTokens = "audio/speech", 0
	case slices.Contains(document.OutputModalities, "rerank"):
		request.Operation, request.PromptTokens = "rerank", 0
	case !slices.Contains(document.OutputModalities, "text"):
		return item
	}
	item.ReferenceOperation = &request.Operation
	reserve, err := u.previewer.Preview(channel, request)
	if err != nil {
		item.ExampleStatus = ExampleStatusRefused
		if errors.Is(err, domain.ErrEndpointDiscoveryMissing) || errors.Is(err, domain.ErrEndpointPriceMissing) {
			item.ExampleStatus = ExampleStatusNeedsLiveCheck
		}
		message := err.Error()
		item.ExampleError = &message
	} else {
		item.ExampleStatus = ExampleStatusOK
		item.ExampleUSD, item.EffectiveSource, item.ConfiguredUSD = &reserve.USD, &reserve.Source, reserve.ConfiguredUSD
		if reserve.AdmissionPolicy != "" {
			item.AdmissionPolicy = &reserve.AdmissionPolicy
		}
	}
	if request.Operation == "audio/speech" {
		// The gateway refuses provider routing filters on speech, so speech
		// has no provider breakdown.
		return item
	}
	var tags []string
	for _, endpoint := range document.Endpoints {
		if !slices.Contains(tags, endpoint.Tag) {
			tags = append(tags, endpoint.Tag)
		}
	}
	for _, tag := range tags {
		// provider.only also admits the sub-tags (tag/variant) of the tag.
		request.Only, request.Ignore = []string{tag}, nil
		for _, other := range tags {
			if strings.HasPrefix(strings.ToLower(other), strings.ToLower(tag)+"/") {
				request.Ignore = append(request.Ignore, other)
			}
		}
		provider := Provider{Tag: tag, Tier: domain.EndpointServiceTier(tag)}
		if reserve, err := u.previewer.Preview(channel, request); err == nil {
			provider.USD = &reserve.USD
		}
		item.Providers = append(item.Providers, provider)
	}
	return item
}
