package controller

import (
	"errors"

	catalogstore "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog"
	"github.com/QuantumNous/new-api/internal/app"

	"github.com/QuantumNous/new-api/internal/catalog/list_models"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetAllModelsMeta(c *gin.Context)     { catalogHTTP().ListModels.Handle(c) }
func SearchModelsMeta(c *gin.Context)     { catalogHTTP().SearchModels.Handle(c) }
func GetModelMeta(c *gin.Context)         { catalogHTTP().GetModel.Handle(c) }
func CreateModelMeta(c *gin.Context)      { catalogHTTP().CreateModel.Handle(c) }
func UpdateModelMeta(c *gin.Context)      { catalogHTTP().UpdateModel.Handle(c) }
func DeleteModelMeta(c *gin.Context)      { catalogHTTP().DeleteModel.Handle(c) }
func BatchDeleteModelMeta(c *gin.Context) { catalogHTTP().DeleteModelsBatch.Handle(c) }

type ListModelsMetaResponse = list_models.ListModelsMetaResponse

func enrichModels(models []*model.Model) error {
	values := catalogstore.ModelsToDomain(models)
	err := app.NewEnrichModelsUsecase().Execute(values)
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
	// The usecase marks failures as internal; callers get the storage cause.
	return errors.Unwrap(err)
}
