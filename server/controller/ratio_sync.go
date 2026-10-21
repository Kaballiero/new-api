package controller

import (
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/QuantumNous/new-api/internal/app"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/gin-gonic/gin"
)

func valueMap(value any) map[string]any  { return pricingdomain.ValueMap(value) }
func FetchUpstreamRatios(c *gin.Context) { app.NewFetchUpstreamRatiosHTTP().Handle(c) }
func buildDifferences(localData map[string]any, channels []struct {
	name string
	data map[string]any
}) map[string]map[string]dto.DifferenceItem {
	var values []pricingdomain.SyncChannel
	for _, ch := range channels {
		values = append(values, pricingdomain.SyncChannel{Name: ch.name, Data: ch.data})
	}
	return pricingdomain.BuildDifferences(localData, values)
}

func GetSyncableChannels(c *gin.Context) { app.NewGetSyncableChannelsHTTP().Handle(c) }
