package test_deployment_connection

import (
	"bytes"
	"strings"

	"github.com/QuantumNous/new-api/i18n"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/gin-gonic/gin"
)

type Provider interface {
	GetMaxGPUsPerContainer() (*MaxGPUResponse, error)
}
type HTTP struct {
	settings func() domain.Settings
	provider func(string) Provider
}

func NewHTTP(settings func() domain.Settings, provider func(string) Provider) HTTP {
	return HTTP{settings: settings, provider: provider}
}
func (h HTTP) Handle(c *gin.Context) {
	var req struct {
		APIKey string `json:"api_key"`
	}

	rawBody, err := c.GetRawData()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(bytes.TrimSpace(rawBody)) > 0 {
		if err := common.Unmarshal(rawBody, &req); err != nil {
			common.ApiErrorMsg(c, i18n.T(c, i18n.MsgDeploymentManageInvalidPayload))
			return
		}
	}

	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		storedKey := strings.TrimSpace(h.settings().APIKey)
		if storedKey == "" {
			common.ApiErrorMsg(c, i18n.T(c, i18n.MsgDeploymentManageAPIKeyRequired))
			return
		}
		apiKey = storedKey
	}

	client := h.provider(apiKey)
	result, err := client.GetMaxGPUsPerContainer()
	if err != nil {
		if apiErr, ok := err.(*domain.APIError); ok {
			message := strings.TrimSpace(apiErr.Message)
			if message == "" {
				message = i18n.T(c, i18n.MsgDeploymentManageValidateKey)
			}
			common.ApiErrorMsg(c, message)
			return
		}
		common.ApiError(c, err)
		return
	}

	totalHardware := 0
	totalAvailable := 0
	if result != nil {
		totalHardware = len(result.Hardware)
		totalAvailable = result.Total
		if totalAvailable == 0 {
			for _, hw := range result.Hardware {
				totalAvailable += hw.Available
			}
		}
	}

	common.ApiSuccess(c, gin.H{
		"hardware_count":  totalHardware,
		"total_available": totalAvailable,
	})
}
