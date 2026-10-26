package get_deployment_settings

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ settings func() domain.Settings }

func NewHTTP(settings func() domain.Settings) HTTP { return HTTP{settings: settings} }
func (h HTTP) Handle(c *gin.Context) {
	s := h.settings()
	enabled := s.Enabled
	hasAPIKey := strings.TrimSpace(s.APIKey) != ""

	common.ApiSuccess(c, gin.H{
		"provider":    "io.net",
		"enabled":     enabled,
		"configured":  hasAPIKey,
		"can_connect": enabled && hasAPIKey,
	})
}
