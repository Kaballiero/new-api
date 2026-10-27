package controller

import (
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/gin-gonic/gin"
)

func valueMap(value any) map[string]any  { return pricingdomain.ValueMap(value) }
func FetchUpstreamRatios(c *gin.Context) { pricingHTTP().FetchUpstreamRatios.Handle(c) }
func buildDifferences(localData map[string]any, channels []struct {
	name string
	data map[string]any
}) map[string]map[string]pricingdomain.RatioDifference {
	var values []pricingdomain.SyncChannel
	for _, ch := range channels {
		values = append(values, pricingdomain.SyncChannel{Name: ch.name, Data: ch.data})
	}
	return pricingdomain.BuildDifferences(localData, values)
}

func GetSyncableChannels(c *gin.Context) { pricingHTTP().ListSyncableChannels.Handle(c) }
