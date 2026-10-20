package model

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"gorm.io/gorm"
)

const CatalogAPIURLEnv = "PRICING_CATALOG_API_URL"

// PricingEndpointSystemOne binds consumer rates to the existing native route.
// It is private to pricing and must not enter supported_endpoint_types.
const PricingEndpointSystemOne constant.EndpointType = "pricing_systemone"

type UpstreamCatalogRoute struct {
	Operation    string
	Mode         string
	Endpoint     constant.EndpointType
	ModelID      string
	DocumentHash string
	Document     *catalogsync.Document
	// Overrides are private projection inputs, never consumer response fields.
	ChannelID          int     `json:"-"`
	ParamOverride      *string `json:"-"`
	ServiceTierEnabled bool    `json:"-"`
}

// Catalog semantics describe the consumer operation, independently of the relay
// protocol. In particular, an image Chat request is not a text-generation offer.
func catalogConsumerProfile(document *catalogsync.Document) (pricingOperation, string) {
	if document == nil {
		return pricingOperation{}, ""
	}
	output := document.Modalities.Output
	switch document.Type {
	case "text":
		if slices.Contains(output, "text") {
			return pricingOperation{"text", "output", "tokens"}, "TEXT"
		}
	case "image":
		if slices.Contains(output, "image") {
			return pricingOperation{"image_generation", "image_output", "image"}, "IMAGE"
		}
	case "video":
		if slices.Contains(output, "video") {
			return pricingOperation{"text_to_video", "video_output", "unknown"}, "VIDEO"
		}
	case "embedding":
		if slices.Contains(output, "embeddings") {
			return pricingOperation{"embeddings", "input", "tokens"}, "EMBEDDINGS"
		}
	case "rerank":
		if slices.Contains(output, "rerank") {
			return pricingOperation{"rerank", "input", "tokens"}, "RERANK"
		}
	case "speech":
		if slices.Contains(output, "speech") {
			return pricingOperation{"speech_generation", "audio_output", "unknown"}, "OTHER"
		}
	case "transcription":
		if slices.Contains(output, "transcription") {
			return pricingOperation{"transcription", "audio_input", "unknown"}, "OTHER"
		}
	case "audio":
		if slices.Contains(output, "audio") {
			return pricingOperation{"audio_generation", "audio_output", "unknown"}, "OTHER"
		}
	case "decision":
		if slices.Contains(output, "decisions") {
			return pricingOperation{"decisions", "input", "tokens"}, "TEXT"
		}
		return pricingOperation{}, "TEXT"
	}
	return pricingOperation{}, "OTHER"
}

func catalogConsumerOperation(document *catalogsync.Document, upstream string, endpoint constant.EndpointType) pricingOperation {
	profile, _ := catalogConsumerProfile(document)
	if profile.operation == "" {
		return pricingOperation{}
	}
	batch := strings.HasSuffix(upstream, ":batch")
	if profile.operation == "decisions" {
		if endpoint == PricingEndpointSystemOne && !batch {
			return profile
		}
		return pricingOperation{}
	}
	if batch && endpoint != PricingEndpointBatch {
		return pricingOperation{}
	}
	if endpoint == PricingEndpointBatch && !batch && !slices.ContainsFunc(document.Offers, func(offer catalogsync.Offer) bool {
		return offer.Mode != nil && *offer.Mode == "batch"
	}) {
		return pricingOperation{}
	}
	switch endpoint {
	case PricingEndpointBatch:
		if profile.operation == "text" || profile.operation == "embeddings" {
			return profile
		}
	case constant.EndpointTypeEmbeddings:
		if profile.operation == "embeddings" {
			return profile
		}
	case constant.EndpointTypeJinaRerank:
		if profile.operation == "rerank" {
			return profile
		}
	case constant.EndpointTypeImageGeneration:
		if profile.operation == "image_generation" {
			return profile
		}
	case constant.EndpointTypeOpenAIVideo:
		if profile.operation == "text_to_video" {
			return profile
		}
	case constant.EndpointTypeOpenAI:
		if profile.operation != "embeddings" && profile.operation != "text_to_video" {
			return profile
		}
	case constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact, constant.EndpointTypeAnthropic, constant.EndpointTypeGemini:
		if profile.operation == "text" {
			return profile
		}
	}
	return pricingOperation{}
}

// Legacy normalization v2 kept the exact Batch identity and variant while Mode
// was nullable. Require both facts; a model-name suffix alone is not a tariff.
func CatalogOfferMode(offer catalogsync.Offer, upstream string) (string, bool) {
	if offer.Mode != nil {
		return *offer.Mode, *offer.Mode == "sync" || *offer.Mode == "batch"
	}
	var identity struct {
		ModelID string `json:"modelId"`
	}
	var variant struct {
		Variant   string `json:"variant"`
		ModelSlug string `json:"model_variant_slug"`
	}
	if len(offer.Identity) > 0 && common.Unmarshal(offer.Identity, &identity) != nil || len(offer.Variant) > 0 && common.Unmarshal(offer.Variant, &variant) != nil {
		return "", false
	}
	if variant.Variant == "batch" || strings.HasSuffix(identity.ModelID, ":batch") {
		if variant.Variant == "batch" && variant.ModelSlug == identity.ModelID && identity.ModelID == upstream && strings.HasSuffix(identity.ModelID, ":batch") {
			return "batch", true
		}
		return "", false
	}
	if strings.HasSuffix(upstream, ":batch") {
		return "", false
	}
	return "sync", true
}

func ConfiguredCatalogSource() (CatalogSource, bool, error) {
	configured := strings.TrimSpace(os.Getenv(CatalogAPIURLEnv))
	if configured == "" {
		return CatalogSource{}, false, nil
	}
	base, err := catalogsync.NormalizeBaseURL(configured)
	if err != nil {
		return CatalogSource{}, true, err
	}
	return CatalogSource{ID: CatalogHash([]byte("openrouter\n" + base)), Kind: "openrouter", BaseURL: base}, true, nil
}

// Only actual OpenRouter channels can consume the initial acquisition source.
// A compatible protocol or an identical model name does not establish identity.
func IsOpenRouterCatalogChannel(channel *Channel) bool {
	if channel == nil || channel.Type != constant.ChannelTypeOpenRouter {
		return false
	}
	base := channel.GetBaseURL()
	if base == "" {
		base = constant.GetChannelBaseURL(channel.Type)
	}
	return strings.TrimRight(base, "/") == "https://openrouter.ai/api"
}

func loadConfiguredCatalog(ctx context.Context) (map[string]*catalogsync.Document, map[string]string, bool, error) {
	source, enabled, err := ConfiguredCatalogSource()
	if err != nil || !enabled {
		return nil, nil, enabled, err
	}
	var head CatalogSource
	if err := DB.WithContext(ctx).Where("id = ?", source.ID).Take(&head).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return map[string]*catalogsync.Document{}, map[string]string{}, true, nil
		}
		return nil, nil, true, err
	}
	if head.Revision == "" {
		return map[string]*catalogsync.Document{}, map[string]string{}, true, nil
	}
	selection, err := ReadCatalogSelection(ctx, source.ID, head.Revision)
	if err != nil {
		return nil, nil, true, err
	}
	hashes := make([]string, 0, len(selection.Models))
	for _, ref := range selection.Models {
		hashes = append(hashes, ref.Hash)
	}
	stored, err := GetCatalogDocuments(ctx, source.ID, hashes)
	if err != nil {
		return nil, nil, true, err
	}
	models := make(map[string]*catalogsync.Document, len(selection.Models))
	byID := make(map[string]string, len(selection.Models))
	for _, ref := range selection.Models {
		row, exists := stored[ref.Hash]
		if !exists {
			return nil, nil, true, errors.New("published catalog document missing")
		}
		doc, err := catalogsync.DecodeDocument([]byte(row.Payload), catalogsync.Reference{ID: ref.ID, Hash: ref.Hash})
		if err != nil {
			return nil, nil, true, err
		}
		models[ref.ID] = doc
		byID[ref.ID] = ref.Hash
	}
	for id := range models {
		if _, err := catalogsync.ResolveAlias(models, id); err != nil {
			return nil, nil, true, err
		}
	}
	return models, byID, true, nil
}

func persistedCatalogRevision(ctx context.Context) (string, error) {
	source, enabled, err := ConfiguredCatalogSource()
	if err != nil || !enabled {
		return "", err
	}
	var head CatalogSource
	if err := DB.WithContext(ctx).Select("id", "revision").Where("id = ?", source.ID).Take(&head).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return source.ID, nil
		}
		return "", err
	}
	return source.ID + ":" + head.Revision, nil
}

func (p Pricing) UpstreamCatalogRoutesForGroup(group string) []UpstreamCatalogRoute {
	return append(slices.Clone(p.UpstreamCatalogRoutesByGroup["all"]), p.UpstreamCatalogRoutesByGroup[group]...)
}
