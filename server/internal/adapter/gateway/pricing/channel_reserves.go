package pricing

import (
	"maps"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/preview_channel_reserves"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// ChannelReserves is the ReservePreviewer of the OpenRouter gateway. It runs
// the admission estimate (openrouter.EstimateRequestReserve) on an estimator
// without a bound request: such an estimator refuses to discover live
// endpoints, so a preview never calls OpenRouter.
type ChannelReserves struct{}

func (r ChannelReserves) Settings(channel *domain.BuildChannel) (preview_channel_reserves.ReserveSettings, error) {
	settings, err := r.channelSettings(channel)
	if err != nil {
		return preview_channel_reserves.ReserveSettings{}, err
	}
	reserves := preview_channel_reserves.ReserveSettings{Sources: make(map[string]string), ManualUSD: make(map[string]float64)}
	for model, source := range settings.OpenRouterReservationSource {
		reserves.Sources[model] = string(source)
	}
	maps.Copy(reserves.ManualUSD, settings.OpenRouterModelReserveUSD)
	// An exact model reserve takes precedence over the legacy Sonar setting.
	if reserve := settings.OpenRouterReserveUSD(openrouter.SonarResearchModel); reserve != nil {
		reserves.ManualUSD[openrouter.SonarResearchModel] = *reserve
	}
	return reserves, nil
}

// Reference speech and rerank requests: a short speech input and a short
// query over three short documents.
var (
	referenceSpeechInput     = strings.Repeat("a", 100)
	referenceRerankQuery     = "What is the capital of France?"
	referenceRerankDocuments = []string{"Paris is the capital of France.", "Berlin is the capital of Germany.", "Madrid is the capital of Spain."}
)

// Preview prices the reference request as a client request routed to the
// channel: Chat Completions or Embeddings with the measured prompt tokens and
// no output limit, OpenAI audio speech of the reference input, or Rerank of
// the reference query and documents.
func (r ChannelReserves) Preview(channel *domain.BuildChannel, request preview_channel_reserves.ReferenceRequest) (preview_channel_reserves.ReservePreview, error) {
	settings, err := r.channelSettings(channel)
	if err != nil {
		return preview_channel_reserves.ReservePreview{}, err
	}
	reference := map[string]any{"model": request.UpstreamModel}
	format := types.RelayFormatOpenAI
	switch request.Operation {
	case "embeddings":
		reference["input"] = ""
		format = types.RelayFormatEmbedding
	case "audio/speech":
		reference["input"] = referenceSpeechInput
		format = types.RelayFormatOpenAIAudio
	case "rerank":
		reference["query"], reference["documents"] = referenceRerankQuery, referenceRerankDocuments
		format = types.RelayFormatRerank
	default:
		reference["messages"] = []map[string]string{{"role": "user", "content": ""}}
	}
	if len(request.Only) > 0 {
		reference["provider"] = map[string][]string{"only": request.Only, "ignore": request.Ignore}
	}
	body, err := common.Marshal(reference)
	if err != nil {
		return preview_channel_reserves.ReservePreview{}, err
	}
	info := &relaycommon.RelayInfo{OriginModelName: request.Model, RelayFormat: format}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: channel.Type, ChannelId: channel.Id, ChannelBaseUrl: channel.GetBaseURL(),
		ChannelSetting: settings, UpstreamModelName: request.UpstreamModel}
	info.SetEstimatePromptTokens(request.PromptTokens)
	estimator := openrouter.NewRequestEstimator()
	usd, configured, err := openrouter.EstimateRequestReserve(estimator, info, body, request.Operation, nil)
	if err != nil {
		return preview_channel_reserves.ReservePreview{}, err
	}
	preview := preview_channel_reserves.ReservePreview{USD: usd, Source: estimator.ReserveSource, AdmissionPolicy: estimator.AdmissionPolicy(&settings)}
	if configured != nil {
		preview.ConfiguredUSD = &configured.USD
	}
	return preview, nil
}

// channelSettings decodes the settings of the channel the gateway routes with.
func (ChannelReserves) channelSettings(channel *domain.BuildChannel) (dto.ChannelSettings, error) {
	var settings dto.ChannelSettings
	if channel.Setting == nil || *channel.Setting == "" {
		return settings, nil
	}
	err := common.UnmarshalJsonStr(*channel.Setting, &settings)
	return settings, err
}
