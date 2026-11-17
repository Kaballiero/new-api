package openrouter

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// liveTariffs is the live-tariffs port of estimate_upstream_reserve: the
// endpoints OpenRouter publishes for an upstream tariff model, discovered on
// the channel of the bound request once per request.
type liveTariffs struct {
	estimator *RequestEstimator
	info      *relaycommon.RelayInfo
}

// Discover returns a discovery of the request; an estimator without a bound
// request has only the discoveries it was given.
func (live liveTariffs) Discover(model string) (pricingdomain.NativeEndpointResponse, error) {
	_, cached := live.estimator.Models[model]
	if !cached && live.estimator.c == nil {
		return pricingdomain.NativeEndpointResponse{}, fmt.Errorf("OpenRouter endpoint discovery missing for %s", model)
	}
	if err := live.estimator.Discover(live.estimator.c, live.info, live.estimator.adaptor, model); err != nil {
		return pricingdomain.NativeEndpointResponse{}, err
	}
	discovered := live.estimator.Models[model]
	endpoints := make([]pricingdomain.NativePricedEndpoint, len(discovered))
	for i, endpoint := range discovered {
		endpoints[i] = pricingdomain.NativePricedEndpoint(endpoint)
	}
	return pricingdomain.NativeEndpointResponse{ID: model, Endpoints: endpoints, OutputModalities: live.estimator.OutputModalities[model]}, nil
}

// Bind sets the request and the adaptor the estimates discover the live
// endpoints of the models the upstream catalog does not price with.
func (e *RequestEstimator) Bind(c *gin.Context, adaptor channel.Adaptor) {
	e.c, e.adaptor = c, adaptor
}

// Discover reads the live endpoints of an upstream tariff model
// (pricingdomain.UpstreamTariffModel) once per request.
func (e *RequestEstimator) Discover(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, model string) error {
	if _, ok := e.Models[model]; ok {
		return nil
	}
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("invalid OpenRouter model identity")
	}
	endpoint := strings.TrimRight(info.ChannelBaseUrl, "/") + "/v1/models/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/endpoints"
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if err = adaptor.SetupRequestHeader(c, &req.Header, info); err != nil {
		return err
	}
	overrides, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return err
	}
	for key, value := range overrides {
		req.Header.Set(key, value)
	}
	client, err := httpclient.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return errors.New("OpenRouter pricing discovery transport unavailable")
	}
	metadataClient := *client
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := metadataClient.Do(req)
	if err != nil {
		return errors.New("OpenRouter pricing discovery failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenRouter pricing discovery HTTP %d", resp.StatusCode)
	}
	var result struct {
		Data struct {
			ID           string `json:"id"`
			Architecture struct {
				OutputModalities []string `json:"output_modalities"`
			} `json:"architecture"`
			Endpoints []PricedEndpoint `json:"endpoints"`
		} `json:"data"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return errors.New("OpenRouter pricing discovery exceeds limit")
	}
	if err := common.Unmarshal(data, &result); err != nil {
		return errors.New("invalid OpenRouter endpoint discovery metadata")
	}
	if len(result.Data.Endpoints) == 0 {
		return errors.New("OpenRouter model has no available endpoints")
	}
	if result.Data.ID == "" && info.RelayFormat == types.RelayFormatSystemOne {
		return errors.New("OpenRouter decisions discovery model identity is missing")
	}
	if result.Data.ID != "" && result.Data.ID != model {
		return errors.New("OpenRouter pricing discovery model mismatch")
	}
	e.Models[model] = result.Data.Endpoints
	if e.OutputModalities == nil {
		e.OutputModalities = make(map[string][]string)
	}
	e.OutputModalities[model] = result.Data.Architecture.OutputModalities
	return nil
}
