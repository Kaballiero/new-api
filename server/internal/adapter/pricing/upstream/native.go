package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	channeladapter "github.com/QuantumNous/new-api/internal/adapter/channels"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/refresh_catalogue"
	"github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"

	"gorm.io/gorm"
)

type NativeChannel struct{ channel *model.Channel }

func NewNativeChannel(channel *model.Channel) NativeChannel { return NativeChannel{channel: channel} }
func (c NativeChannel) Value() *domain.BuildChannel {
	return (*domain.BuildChannel)(channelrecord.ToDomain((*channelrecord.Channel)(c.channel)))
}
func (c NativeChannel) Metadata(ctx context.Context, endpoint string) ([]byte, error) {
	key, err := channeladapter.SelectChannelUpstreamModelDiscoveryKey(c.channel)
	if err != nil {
		return nil, errors.New("pricing discovery authentication unavailable")
	}
	headers, err := channeladapter.BuildFetchModelsHeaders(c.channel, key)
	if err != nil {
		return nil, errors.New("pricing discovery headers unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header = headers
	settings := channelstore.NewChannelPersistence().GetSetting(c.channel)
	client, err := httpclient.GetHttpClientWithProxySettings(settings.Proxy, settings)
	if err != nil {
		return nil, errors.New("pricing discovery transport unavailable")
	}
	metadataClient := *client
	metadataClient.Timeout = 20 * time.Second
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := metadataClient.Do(request)
	if err != nil {
		return nil, errors.New("pricing metadata request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pricing metadata HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return nil, errors.New("pricing metadata exceeds body limit")
	}
	return body, nil
}
func (c NativeChannel) Native() *model.Channel { return c.channel }

type NativeStore struct{ ReadPricing func() []domain.Pricing }

func (NativeStore) Abilities() ([]domain.BuildAbility, error) {
	rows, err := channelrecord.NewStore(channelrecord.Environment{DB: func() *gorm.DB { return model.DB }}).GetAllEnableAbilityWithChannels()
	out := make([]domain.BuildAbility, len(rows))
	for i, v := range rows {
		out[i] = domain.BuildAbility{Model: v.Model, Group: v.Group, ChannelId: v.ChannelId, ChannelType: v.ChannelType}
	}
	return out, err
}
func (store NativeStore) Pricing() []domain.Pricing { return store.ReadPricing() }
func (NativeStore) Channel(id int) (refresh_catalogue.Channel, error) {
	row, err := channelstore.GetChannelById(id, true)
	if err != nil {
		return nil, err
	}
	return NewNativeChannel(row), nil
}
func NativePricedEndpoint(value openrouter.PricedEndpoint) domain.NativePricedEndpoint {
	return domain.NativePricedEndpoint(value)
}
func NativeImageEndpoint(value openrouter.ImageEndpoint) domain.NativeImageEndpoint {
	out := domain.NativeImageEndpoint{PromptOnly: value.PromptOnly, ProviderTag: value.ProviderTag}
	if value.SupportedParameters != nil {
		out.SupportedParameters = make(map[string]domain.NativeImageCapability, len(value.SupportedParameters))
		for key, v := range value.SupportedParameters {
			out.SupportedParameters[key] = domain.NativeImageCapability(v)
		}
	}
	if value.Pricing != nil {
		out.Pricing = make([]domain.NativeImagePrice, len(value.Pricing))
		for i, v := range value.Pricing {
			out.Pricing[i] = domain.NativeImagePrice{Billable: v.Billable, Unit: v.Unit, Variant: v.Variant, CostUSD: v.CostUSD}
		}
	}
	return out
}
func nativeImageEndpoint(value domain.NativeImageEndpoint) openrouter.ImageEndpoint {
	out := openrouter.ImageEndpoint{PromptOnly: value.PromptOnly, ProviderTag: value.ProviderTag}
	if value.SupportedParameters != nil {
		out.SupportedParameters = make(map[string]openrouter.ImageCapability, len(value.SupportedParameters))
		for key, v := range value.SupportedParameters {
			out.SupportedParameters[key] = openrouter.ImageCapability(v)
		}
	}
	if value.Pricing != nil {
		out.Pricing = make([]openrouter.ImagePrice, len(value.Pricing))
		for i, v := range value.Pricing {
			out.Pricing[i] = openrouter.ImagePrice{Billable: v.Billable, Unit: v.Unit, Variant: v.Variant, CostUSD: v.CostUSD}
		}
	}
	return out
}
func NewNativeNormalizer() *domain.NativeNormalizer {
	return domain.NewNativeNormalizer(domain.NativeNormalizationRuntime{Capabilities: pricingadapter.Capabilities(),
		DecodeEndpoint: func(body []byte) (domain.NativeEndpointResponse, error) {
			var value struct {
				Data struct {
					ID           string                      `json:"id"`
					Endpoints    []openrouter.PricedEndpoint `json:"endpoints"`
					Architecture struct {
						Input  []string `json:"input_modalities"`
						Output []string `json:"output_modalities"`
					} `json:"architecture"`
				} `json:"data"`
			}
			err := common.Unmarshal(body, &value)
			out := domain.NativeEndpointResponse{ID: value.Data.ID, InputModalities: value.Data.Architecture.Input, OutputModalities: value.Data.Architecture.Output}
			if value.Data.Endpoints != nil {
				out.Endpoints = make([]domain.NativePricedEndpoint, len(value.Data.Endpoints))
				for i, v := range value.Data.Endpoints {
					out.Endpoints[i] = NativePricedEndpoint(v)
				}
			}
			return out, err
		},
		DecodeImage: func(body []byte) (domain.NativeImageResponse, error) {
			var value struct {
				ID        string                     `json:"id"`
				Endpoints []openrouter.ImageEndpoint `json:"endpoints"`
			}
			err := common.Unmarshal(body, &value)
			out := domain.NativeImageResponse{ID: value.ID}
			if value.Endpoints != nil {
				out.Endpoints = make([]domain.NativeImageEndpoint, len(value.Endpoints))
				for i, v := range value.Endpoints {
					out.Endpoints[i] = NativeImageEndpoint(v)
				}
			}
			return out, err
		},
		ReadPrice: func(raw json.RawMessage) (float64, error) { return openrouter.ReadEndpointPrice(raw) }, PublishedMuseImage: func(name string) (domain.NativeImageEndpoint, bool) {
			v, ok := openrouter.PublishedMuseImageEndpoint(name)
			return NativeImageEndpoint(v), ok
		}, PublishedImage: func(name string, v domain.NativeImageEndpoint) domain.NativeImageEndpoint {
			return NativeImageEndpoint(openrouter.PublishedImageEndpoint(name, nativeImageEndpoint(v)))
		},
	})
}
