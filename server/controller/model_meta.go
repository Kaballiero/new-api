package controller

import (
	"github.com/QuantumNous/new-api/internal/catalog/list_models"
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
