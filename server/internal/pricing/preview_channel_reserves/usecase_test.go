package preview_channel_reserves

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errMissing = errors.New("record not found")

type stubChannels struct {
	channel *domain.BuildChannel
	err     error
}

func (s stubChannels) BuildChannel(int) (*domain.BuildChannel, error) { return s.channel, s.err }
func (stubChannels) IsMissing(err error) bool                         { return errors.Is(err, errMissing) }

type stubCatalog struct {
	tariffs *domain.UpstreamCatalogTariffs
}

func (s stubCatalog) Load() *domain.UpstreamCatalogTariffs { return s.tariffs }

// stubPreviewer prices a reference request by its upstream model and
// provider.only; it records the requests.
type stubPreviewer struct {
	settings ReserveSettings
	previews map[string]ReservePreview
	errs     map[string]error
	requests *[]ReferenceRequest
}

func (s stubPreviewer) Settings(*domain.BuildChannel) (ReserveSettings, error) {
	return s.settings, nil
}
func (s stubPreviewer) Preview(_ *domain.BuildChannel, request ReferenceRequest) (ReservePreview, error) {
	*s.requests = append(*s.requests, request)
	key := request.UpstreamModel + "|" + strings.Join(request.Only, ",")
	if err, failed := s.errs[key]; failed {
		return ReservePreview{}, err
	}
	return s.previews[key], nil
}

func TestPreviewChannelReserves(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	fresh := &domain.UpstreamCatalogTariffs{FetchedAt: now.Add(-time.Hour), Models: map[string]domain.NativeEndpointResponse{
		"openai/gpt":                     {OutputModalities: []string{"text"}, Endpoints: []domain.NativePricedEndpoint{{Tag: "openai"}, {Tag: "openai/fast"}, {Tag: "azure"}, {Tag: "openai"}}},
		"cohere/embed":                   {OutputModalities: []string{"embeddings"}, Endpoints: []domain.NativePricedEndpoint{{Tag: "cohere"}}},
		"google/image":                   {OutputModalities: []string{"image"}, Endpoints: []domain.NativePricedEndpoint{{Tag: "google"}}},
		"perplexity/sonar-deep-research": {OutputModalities: []string{"text"}, Endpoints: []domain.NativePricedEndpoint{{Tag: "perplexity"}}},
		"anthropic/claude":               {OutputModalities: []string{"text"}, Endpoints: []domain.NativePricedEndpoint{{Tag: "anthropic"}}},
		"orphan/model":                   {OutputModalities: []string{"text"}},
		"acme/tts":                       {OutputModalities: []string{"speech"}, Endpoints: []domain.NativePricedEndpoint{{Tag: "deepinfra"}}},
		"cohere/rerank":                  {OutputModalities: []string{"rerank"}, Endpoints: []domain.NativePricedEndpoint{{Tag: "cohere"}}},
	}}
	stale := &domain.UpstreamCatalogTariffs{FetchedAt: now.Add(-25 * time.Hour), Models: fresh.Models}
	official, proxy := "https://openrouter.ai/api", "https://proxy.example/api"
	mapping := `{"alias":"openai/gpt"}`
	channel := func(base, models string) *domain.BuildChannel {
		return &domain.BuildChannel{Id: 7, Type: constant.ChannelTypeOpenRouter, BaseURL: &base, Models: models, ModelMapping: &mapping}
	}
	settings := ReserveSettings{
		Sources:   map[string]string{"openai/gpt": "upstream", "orphan/model": "configured_price"},
		ManualUSD: map[string]float64{"openai/gpt": 0.05},
	}
	previews := map[string]ReservePreview{
		"openai/gpt|":            {USD: 0.0571, Source: domain.ReserveSourceCatalog, AdmissionPolicy: domain.AdmissionPolicyModelReserveFloor},
		"openai/gpt|openai":      {USD: 0.0571},
		"openai/gpt|openai/fast": {USD: 0.09},
		"cohere/embed|":          {USD: 0.0001, Source: domain.ReserveSourceCatalog},
		"cohere/embed|cohere":    {USD: 0.0001},
		"orphan/model|":          {USD: 0.02, Source: domain.ReserveSourceConfiguredPrice, ConfiguredUSD: common.GetPointer(0.02)},
		"perplexity/sonar-deep-research|perplexity": {USD: 1},
		"acme/tts|":            {USD: 0.1, Source: domain.ReserveSourceManual, AdmissionPolicy: domain.AdmissionPolicyModelApproximateReserve},
		"cohere/rerank|":       {USD: 0.002, Source: domain.ReserveSourceCatalog},
		"cohere/rerank|cohere": {USD: 0.002},
	}
	errs := map[string]error{
		"openai/gpt|azure":                errors.New("OpenRouter request has no compatible priced endpoint"),
		"perplexity/sonar-deep-research|": fmt.Errorf("%w for perplexity/sonar-deep-research", domain.ErrEndpointDiscoveryMissing),
		"anthropic/claude|":               errors.New("OpenRouter Sonar research reserve policy is disabled"),
		"anthropic/claude|anthropic":      errors.New("OpenRouter Sonar research reserve policy is disabled"),
	}
	reference := Reference{Operation: "chat/completions", PromptTokens: 1000, OutputTokens: 8192}
	live := func(model, upstream string, inChannel bool) Item {
		return Item{Model: model, UpstreamModel: &upstream, InChannel: inChannel, ExampleStatus: ExampleStatusLiveOnly, Providers: []Provider{}}
	}
	withSettings := func(item Item, source string, manual float64) Item {
		if source != "" {
			item.ConfiguredSource = &source
		}
		if manual > 0 {
			item.ManualUSD = &manual
		}
		return item
	}
	priced := func(item Item, operation string, preview ReservePreview, providers ...Provider) Item {
		item.ReferenceOperation = &operation
		item.ExampleStatus, item.ExampleUSD, item.EffectiveSource, item.ConfiguredUSD = ExampleStatusOK, &preview.USD, &preview.Source, preview.ConfiguredUSD
		if preview.AdmissionPolicy != "" {
			item.AdmissionPolicy = &preview.AdmissionPolicy
		}
		item.Providers = append(item.Providers, providers...)
		return item
	}
	usd := func(value float64) *float64 { return &value }
	chat := "chat/completions"
	discoveryMissing := "OpenRouter endpoint discovery missing for perplexity/sonar-deep-research"
	refused := "OpenRouter Sonar research reserve policy is disabled"

	for _, tc := range []struct {
		name     string
		channels stubChannels
		catalog  *domain.UpstreamCatalogTariffs
		want     Output
		wantErr  error
		previews int
	}{
		{name: "not found", channels: stubChannels{err: errMissing}, wantErr: channeldomain.ErrChannelNotFound},
		{name: "not OpenRouter", channels: stubChannels{channel: &domain.BuildChannel{Type: constant.ChannelTypeOpenAI}}, wantErr: domain.ErrChannelNotOpenRouter},
		{
			name: "catalog missing", channels: stubChannels{channel: channel(official, "alias")},
			want: Output{Reference: reference, Items: []Item{withSettings(live("alias", "openai/gpt", true), "upstream", 0.05), withSettings(live("orphan/model", "orphan/model", false), "configured_price", 0)}},
		},
		{
			name: "stale catalog", channels: stubChannels{channel: channel(official, "alias")}, catalog: stale,
			want: Output{FetchedAt: &stale.FetchedAt, CatalogStale: true, Reference: reference, Items: []Item{withSettings(live("alias", "openai/gpt", true), "upstream", 0.05), withSettings(live("orphan/model", "orphan/model", false), "configured_price", 0)}},
		},
		{
			name: "unofficial base URL", channels: stubChannels{channel: channel(proxy, "alias")}, catalog: fresh,
			want: Output{FetchedAt: &fresh.FetchedAt, Reference: reference, Items: []Item{withSettings(live("alias", "openai/gpt", true), "upstream", 0.05), withSettings(live("orphan/model", "orphan/model", false), "configured_price", 0)}},
		},
		{
			name: "model mapping, orphan key and providers", channels: stubChannels{channel: channel(official, "alias,cohere/embed,google/image,missing/model,perplexity/sonar-deep-research,anthropic/claude")}, catalog: fresh, previews: 11,
			want: Output{FetchedAt: &fresh.FetchedAt, Reference: reference, Items: []Item{
				priced(withSettings(live("alias", "openai/gpt", true), "upstream", 0.05), chat, previews["openai/gpt|"],
					Provider{Tag: "openai", Tier: domain.ServiceTierDefault, USD: usd(0.0571)}, Provider{Tag: "openai/fast", Tier: domain.ServiceTierPriority, USD: usd(0.09)}, Provider{Tag: "azure", Tier: domain.ServiceTierDefault}),
				priced(live("cohere/embed", "cohere/embed", true), "embeddings", previews["cohere/embed|"], Provider{Tag: "cohere", Tier: domain.ServiceTierDefault, USD: usd(0.0001)}),
				live("google/image", "google/image", true),
				live("missing/model", "missing/model", true),
				func() Item {
					item := live("perplexity/sonar-deep-research", "perplexity/sonar-deep-research", true)
					item.ReferenceOperation, item.ExampleStatus, item.ExampleError = &chat, ExampleStatusNeedsLiveCheck, &discoveryMissing
					item.Providers = []Provider{{Tag: "perplexity", Tier: domain.ServiceTierDefault, USD: usd(1)}}
					return item
				}(),
				func() Item {
					item := live("anthropic/claude", "anthropic/claude", true)
					item.ReferenceOperation, item.ExampleStatus, item.ExampleError = &chat, ExampleStatusRefused, &refused
					item.Providers = []Provider{{Tag: "anthropic", Tier: domain.ServiceTierDefault}}
					return item
				}(),
				priced(withSettings(live("orphan/model", "orphan/model", false), "configured_price", 0), chat, previews["orphan/model|"]),
			}},
		},
		{
			// Speech takes no provider breakdown: the gateway refuses provider
			// filters on speech.
			name: "speech and rerank", channels: stubChannels{channel: channel(official, "acme/tts,cohere/rerank")}, catalog: fresh, previews: 8,
			want: Output{FetchedAt: &fresh.FetchedAt, Reference: reference, Items: []Item{
				priced(live("acme/tts", "acme/tts", true), "audio/speech", previews["acme/tts|"]),
				priced(live("cohere/rerank", "cohere/rerank", true), "rerank", previews["cohere/rerank|"], Provider{Tag: "cohere", Tier: domain.ServiceTierDefault, USD: usd(0.002)}),
				priced(withSettings(live("openai/gpt", "openai/gpt", false), "upstream", 0.05), chat, previews["openai/gpt|"],
					Provider{Tag: "openai", Tier: domain.ServiceTierDefault, USD: usd(0.0571)}, Provider{Tag: "openai/fast", Tier: domain.ServiceTierPriority, USD: usd(0.09)}, Provider{Tag: "azure", Tier: domain.ServiceTierDefault}),
				priced(withSettings(live("orphan/model", "orphan/model", false), "configured_price", 0), chat, previews["orphan/model|"]),
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []ReferenceRequest
			previewer := stubPreviewer{settings: settings, previews: previews, errs: errs, requests: &requests}
			usecase := NewUsecase(domain.Capabilities{Codec: domain.JSONCodec{UnmarshalJsonStr: common.UnmarshalJsonStr}}, tc.channels, stubCatalog{tariffs: tc.catalog}, previewer,
				func() time.Time { return now }, func() time.Duration { return 24 * time.Hour }, 8192)

			out, err := usecase.Execute(7)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, out)
			assert.Len(t, requests, tc.previews)
		})
	}
}

// The provider breakdown restricts each request to one tag of the catalog
// model, ignoring its sub-tags; embeddings, speech and rerank models take
// their own reference, speech without the provider breakdown.
func TestPreviewChannelReservesReferenceRequests(t *testing.T) {
	now := time.Now()
	base := "https://openrouter.ai/api"
	catalog := &domain.UpstreamCatalogTariffs{FetchedAt: now, Models: map[string]domain.NativeEndpointResponse{
		"openai/gpt":    {OutputModalities: []string{"text"}, Endpoints: []domain.NativePricedEndpoint{{Tag: "openai"}, {Tag: "openai/fast"}}},
		"cohere/embed":  {OutputModalities: []string{"embeddings"}, Endpoints: []domain.NativePricedEndpoint{{Tag: "cohere"}}},
		"acme/tts":      {OutputModalities: []string{"speech"}, Endpoints: []domain.NativePricedEndpoint{{Tag: "deepinfra"}}},
		"cohere/rerank": {OutputModalities: []string{"rerank"}, Endpoints: []domain.NativePricedEndpoint{{Tag: "cohere"}}},
	}}
	var requests []ReferenceRequest
	previewer := stubPreviewer{requests: &requests}
	usecase := NewUsecase(domain.Capabilities{}, stubChannels{channel: &domain.BuildChannel{Type: constant.ChannelTypeOpenRouter, BaseURL: &base, Models: "openai/gpt,cohere/embed,acme/tts,cohere/rerank"}},
		stubCatalog{tariffs: catalog}, previewer, time.Now, func() time.Duration { return time.Hour }, 8192)

	_, err := usecase.Execute(1)

	require.NoError(t, err)
	chat := ReferenceRequest{Model: "openai/gpt", UpstreamModel: "openai/gpt", Operation: "chat/completions", PromptTokens: 1000}
	embeddings := ReferenceRequest{Model: "cohere/embed", UpstreamModel: "cohere/embed", Operation: "embeddings", PromptTokens: 1000}
	speech := ReferenceRequest{Model: "acme/tts", UpstreamModel: "acme/tts", Operation: "audio/speech"}
	rerank := ReferenceRequest{Model: "cohere/rerank", UpstreamModel: "cohere/rerank", Operation: "rerank"}
	only := func(request ReferenceRequest, tag string, ignore ...string) ReferenceRequest {
		request.Only, request.Ignore = []string{tag}, ignore
		return request
	}
	assert.Equal(t, []ReferenceRequest{chat, only(chat, "openai", "openai/fast"), only(chat, "openai/fast"), embeddings, only(embeddings, "cohere"), speech, rerank, only(rerank, "cohere")}, requests)
}
