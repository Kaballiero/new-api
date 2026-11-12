package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
)

type CatalogSource struct {
	ID             string
	Kind           string
	BaseURL        string
	Revision       string
	RemoteRevision string
	Generation     int64
	CheckedAt      int64
	FetchedAt      string
}
type CatalogRevision struct {
	SourceID       string
	Hash           string
	Manifest       string
	RemoteRevision string
	CreatedAt      int64
}
type CatalogDocument struct {
	SourceID string
	Hash     string
	ModelID  string
	Payload  string
}
type CatalogModelReference struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}
type CatalogSelection struct {
	SchemaVersion        int                     `json:"schemaVersion"`
	NormalizationVersion string                  `json:"normalizationVersion"`
	Models               []CatalogModelReference `json:"models"`
}
type CatalogPublication struct {
	Revision        string
	NewDocuments    int
	ReusedDocuments int
	Changed         bool
}

func CatalogHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func ValidCatalogHash(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// UpstreamCatalogModels are the models of the upstream catalog: Listed are the
// models of its manifest and Addable the part of them a channel may add now.
type UpstreamCatalogModels struct {
	Listed  []string
	Addable []string
}

// openRouterRoutingSuffixes select how OpenRouter routes a model. The catalog
// lists the model without them.
var openRouterRoutingSuffixes = []string{":nitro", ":floor"}

// ChannelUpstreamModels are the catalog models as the upstream models of a
// channel. A channel target that routes a listed model with an OpenRouter
// routing suffix is listed too, so it is not a removal candidate.
func (models UpstreamCatalogModels) ChannelUpstreamModels(deps Capabilities, channel *BuildChannel) channeldomain.UpstreamModels {
	listed := slices.Clone(models.Listed)
	for _, model := range channeldomain.NormalizeModelNames(channel.GetModels()) {
		target, resolved := ResolvePricingUpstreamModel(deps, channel, model)
		if !resolved {
			continue
		}
		for _, suffix := range openRouterRoutingSuffixes {
			if base, routed := strings.CutSuffix(target, suffix); routed && slices.Contains(models.Listed, base) {
				listed = append(listed, target)
			}
		}
	}
	return channeldomain.UpstreamModels{Listed: listed, Addable: models.Addable}
}
