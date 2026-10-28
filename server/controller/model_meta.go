package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/QuantumNous/new-api/internal/catalog/list_models"

	"github.com/gin-gonic/gin"
)

func GetAllModelsMeta(c *gin.Context) { httpapi.Catalog().ListModels.Handle(c) }

func SearchModelsMeta(c *gin.Context) { httpapi.Catalog().SearchModels.Handle(c) }

func GetModelMeta(c *gin.Context) { httpapi.Catalog().GetModel.Handle(c) }

func CreateModelMeta(c *gin.Context) { httpapi.Catalog().CreateModel.Handle(c) }

func UpdateModelMeta(c *gin.Context) { httpapi.Catalog().UpdateModel.Handle(c) }

func DeleteModelMeta(c *gin.Context) { httpapi.Catalog().DeleteModel.Handle(c) }

func BatchDeleteModelMeta(c *gin.Context) { httpapi.Catalog().DeleteModelsBatch.Handle(c) }

type ListModelsMetaResponse = list_models.ListModelsMetaResponse
