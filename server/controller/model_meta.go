package controller

import (
	catalogstore "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog"
	"github.com/QuantumNous/new-api/internal/app"

	"github.com/QuantumNous/new-api/internal/catalog/list_models"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetAllModelsMeta(c *gin.Context) { app.NewGetAllModelsMetaHTTP().Handle(c) }
func SearchModelsMeta(c *gin.Context) { app.NewSearchModelsMetaHTTP().Handle(c) }
func GetModelMeta(c *gin.Context)     { app.NewGetModelMetaHTTP().Handle(c) }
func CreateModelMeta(c *gin.Context)  { app.NewCreateModelMetaHTTP().Handle(c) }
func UpdateModelMeta(c *gin.Context)  { app.NewUpdateModelMetaHTTP().Handle(c) }
func DeleteModelMeta(c *gin.Context)  { app.NewDeleteModelMetaHTTP(catalogEffects(c)).Handle(c) }
func BatchDeleteModelMeta(c *gin.Context) {
	app.NewBatchDeleteModelMetaHTTP(catalogEffects(c)).Handle(c)
}

type ListModelsMetaResponse = list_models.ListModelsMetaResponse

func enrichModels(models []*model.Model) error {
	values := catalogstore.ModelsToDomain(models)
	failure := app.NewEnrichModelsUsecase().Execute(values)
	for i, v := range values {
		if v == nil {
			continue
		}
		target := models[i]
		target.HasMetadata = v.HasMetadata
		target.ConfiguredChannelCount = v.ConfiguredChannelCount
		target.SquareState = v.SquareState
		target.BoundChannels = v.BoundChannels
		target.EnableGroups = v.EnableGroups
		target.SupportedEndpoints = v.SupportedEndpoints
		target.QuotaTypes = v.QuotaTypes
		target.MatchedModels = v.MatchedModels
		target.MatchedCount = v.MatchedCount
	}
	if failure != nil {
		return failure.Cause
	}
	return nil
}
