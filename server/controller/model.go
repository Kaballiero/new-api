package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

// BRIDGE: ListModels serves existing router registrations; remove when router assembles the owning handler directly.
func ListModels(c *gin.Context, modelType int) { app.CatalogModelHandlers().List.Handle(c, modelType) }

// BRIDGE: RetrieveModel serves existing router registrations; remove when router assembles the owning handler directly.
func RetrieveModel(c *gin.Context, modelType int) {
	app.CatalogModelHandlers().Retrieve.Handle(c, modelType)
}

// BRIDGE: DashboardListModels serves existing router registrations; remove when router assembles the owning handler directly.
func DashboardListModels(c *gin.Context) { app.CatalogModelHandlers().Presets.Handle(c) }

// BRIDGE: ChannelListModels serves existing router registrations; remove when router assembles the owning handler directly.
func ChannelListModels(c *gin.Context) { channelsHTTP().ListChannelModels.Handle(c) }

// BRIDGE: EnabledListModels serves existing router registrations; remove when router assembles the owning handler directly.
func EnabledListModels(c *gin.Context) { channelsHTTP().ListEnabledChannelModels.Handle(c) }
