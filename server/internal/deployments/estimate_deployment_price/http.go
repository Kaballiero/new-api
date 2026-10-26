package estimate_deployment_price

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/QuantumNous/new-api/internal/deployments/transport"
	"github.com/gin-gonic/gin"
)

type Provider interface {
	GetPriceEstimation(request *PriceEstimationRequest) (*PriceEstimationResponse, error)
}
type HTTP struct {
	settings func() domain.Settings
	provider func(string) Provider
}

func NewHTTP(settings func() domain.Settings, provider func(string) Provider) HTTP {
	return HTTP{settings: settings, provider: provider}
}
func (h HTTP) Handle(c *gin.Context) {
	client, ok := transport.Provider(c, h.settings, h.provider)
	if !ok {
		return
	}

	var req PriceEstimationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, common.PreserveJSONTypeError(err, "github.com/QuantumNous/new-api/internal/deployments/", "ionet"))
		return
	}

	priceResp, err := client.GetPriceEstimation(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, priceResp)
}
