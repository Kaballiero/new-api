package openrouter

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// liveVideoTariffs is the live-tariffs port of estimate_upstream_video_reserve:
// the video models OpenRouter publishes at /videos/models, read on the channel
// of the request.
type liveVideoTariffs struct {
	c    *gin.Context
	info *relaycommon.RelayInfo
}

// Discover reads the video models and returns the one with the model id.
func (live liveVideoTariffs) Discover(model string) (pricingdomain.VideoModelPricing, error) {
	req, err := http.NewRequestWithContext(live.c.Request.Context(), http.MethodGet, strings.TrimRight(live.info.ChannelBaseUrl, "/")+"/v1/videos/models", nil)
	if err != nil {
		return pricingdomain.VideoModelPricing{}, errors.New("OpenRouter reservation unavailable: invalid video discovery URL")
	}
	req.Header.Set("Authorization", "Bearer "+live.info.ApiKey)
	overrides, err := channel.ResolveHeaderOverride(live.info, live.c)
	if err != nil {
		return pricingdomain.VideoModelPricing{}, err
	}
	for key, value := range overrides {
		req.Header.Set(key, value)
	}
	client, err := httpclient.GetHttpClientWithProxySettings(live.info.ChannelSetting.Proxy, live.info.ChannelSetting)
	if err != nil {
		return pricingdomain.VideoModelPricing{}, errors.New("OpenRouter reservation unavailable: video discovery transport")
	}
	metadataClient := *client
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := metadataClient.Do(req)
	if err != nil {
		return pricingdomain.VideoModelPricing{}, errors.New("OpenRouter reservation unavailable: video discovery failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return pricingdomain.VideoModelPricing{}, fmt.Errorf("OpenRouter reservation unavailable: video discovery HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return pricingdomain.VideoModelPricing{}, errors.New("OpenRouter reservation unavailable: invalid video discovery response")
	}
	var catalog struct {
		Data []pricingdomain.VideoModelPricing `json:"data"`
	}
	if err := common.Unmarshal(data, &catalog); err != nil {
		return pricingdomain.VideoModelPricing{}, errors.New("OpenRouter reservation unavailable: invalid video catalog")
	}
	for _, entry := range catalog.Data {
		if entry.ID == model {
			return entry, nil
		}
	}
	return pricingdomain.VideoModelPricing{}, errors.New("OpenRouter reservation unavailable: model absent from fresh video catalog")
}
