package controller

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/service"
)

// refreshUpstreamCatalog uses the existing system-task lifecycle. Selection and
// channel policy remain NewAPI-owned; the upstream supplies only model facts.
func refreshUpstreamCatalog(ctx context.Context, source model.CatalogSource) (pricingCatalogRefreshSummary, error) {
	summary := pricingCatalogRefreshSummary{}
	abilities, err := model.GetAllEnableAbilityWithChannels()
	if err != nil {
		return summary, err
	}
	channels := map[int]*model.Channel{}
	requested := map[string]bool{}
	for _, ability := range abilities {
		if ability.ChannelType != constant.ChannelTypeOpenRouter {
			continue
		}
		channel, exists := channels[ability.ChannelId]
		if !exists {
			channel, err = model.GetChannelById(ability.ChannelId, true)
			if err != nil {
				return summary, err
			}
			channels[ability.ChannelId] = channel
		}
		if channel.Status != common.ChannelStatusEnabled || !model.IsOpenRouterCatalogChannel(channel) {
			continue
		}
		upstream, resolved := model.ResolvePricingUpstreamModel(channel, ability.Model)
		if !resolved {
			return summary, fmt.Errorf("catalog channel model mapping is unresolved")
		}
		requested[upstream] = true
	}
	summary.Sources = len(requested)
	if len(requested) == 0 {
		return summary, nil
	}
	client, err := catalogsync.NewClient(source.BaseURL, service.GetHttpClient())
	if err != nil {
		return summary, err
	}
	result, err := service.SyncUpstreamCatalog(ctx, source, requested, client)
	if err != nil {
		return summary, err
	}
	summary.Published = result.ImportedModels
	summary.MissingModels = result.MissingModels
	summary.NewDocuments = result.Publication.NewDocuments
	summary.ReusedDocuments = result.Publication.ReusedDocuments
	summary.Revision = result.Publication.Revision
	return summary, nil
}
