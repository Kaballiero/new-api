package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

type CatalogClient struct{ Client *catalogsync.Client }

func (c CatalogClient) Manifest(ctx context.Context) (domain.SourceManifest, error) {
	value, err := c.Client.Manifest(ctx)
	return ToSourceManifest(value), err
}
func (c CatalogClient) Document(ctx context.Context, ref domain.SourceReference) ([]byte, *domain.SourceDocument, error) {
	raw, value, err := c.Client.Document(ctx, nativeSourceReference(ref))
	if value == nil {
		return raw, nil, err
	}
	result := ToSourceDocument(*value)
	return raw, &result, err
}

// PricingCatalogSnapshot and PricingCatalogEntry retain the persisted wire decoder's
// original field types and names, including encoding/json error text.
type PricingCatalogSnapshot struct {
	Version int                            `json:"version"`
	Entries map[string]PricingCatalogEntry `json:"entries"`
}
type PricingCatalogEntry struct {
	ChannelID     int                     `json:"channel_id"`
	UpstreamModel string                  `json:"upstream_model"`
	Endpoint      constant.EndpointType   `json:"endpoint"`
	Source        string                  `json:"source"`
	FetchedAt     string                  `json:"fetched_at"`
	Fingerprint   string                  `json:"fingerprint"`
	Offers        []jsplugin.CatalogOffer `json:"offers"`
}

func decodeCatalogSnapshot(raw string, result *domain.PricingCatalogSnapshot) error {
	var native PricingCatalogSnapshot
	err := common.UnmarshalJsonStr(raw, &native)
	// Keep the original domain container type facts in errors. Nested SDK types
	// already retain their original identities through the wire decoder above.
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		switch typeError.Type {
		case reflect.TypeFor[PricingCatalogSnapshot]():
			typeError.Type = reflect.TypeFor[domain.PricingCatalogSnapshot]()
		case reflect.TypeFor[map[string]PricingCatalogEntry]():
			typeError.Type = reflect.TypeFor[map[string]domain.PricingCatalogEntry]()
		case reflect.TypeFor[PricingCatalogEntry]():
			typeError.Type = reflect.TypeFor[domain.PricingCatalogEntry]()
		}
	}
	result.Version = native.Version
	if native.Entries != nil {
		result.Entries = make(map[string]domain.PricingCatalogEntry, len(native.Entries))
		for key, value := range native.Entries {
			entry := domain.PricingCatalogEntry{
				ChannelID: value.ChannelID, UpstreamModel: value.UpstreamModel, Endpoint: value.Endpoint,
				Source: value.Source, FetchedAt: value.FetchedAt, Fingerprint: value.Fingerprint,
			}
			if value.Offers != nil {
				entry.Offers = make([]domain.CatalogOffer, len(value.Offers))
				for i, offer := range value.Offers {
					entry.Offers[i] = ToTariffOffer(offer)
				}
			}
			result.Entries[key] = entry
		}
	}
	return err
}
