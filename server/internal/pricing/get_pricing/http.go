package get_pricing

import (
	"github.com/QuantumNous/new-api/internal/pricing/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	id, exists := c.Get("id")
	out, err := h.usecase.Execute(Input{UserID: id, HasUser: exists})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": out.Data, "vendors": out.Vendors, "group_ratio": out.GroupRatio, "usable_group": out.UsableGroup, "supported_endpoint": out.SupportedEndpoint, "auto_groups": out.AutoGroups, "pricing_version": out.PricingVersion})
}
