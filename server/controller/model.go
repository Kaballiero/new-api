package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func ListModels(c *gin.Context, modelType int) { app.CatalogModelHandlers().List.Handle(c, modelType) }

func RetrieveModel(c *gin.Context, modelType int) {
	app.CatalogModelHandlers().Retrieve.Handle(c, modelType)
}

func DashboardListModels(c *gin.Context) { app.CatalogModelHandlers().Presets.Handle(c) }

func ChannelListModels(c *gin.Context) { httpapi.Channels().ListChannelModels.Handle(c) }

func EnabledListModels(c *gin.Context) { httpapi.Channels().ListEnabledChannelModels.Handle(c) }
