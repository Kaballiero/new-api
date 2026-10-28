package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func GetMissingModels(c *gin.Context) { httpapi.Catalog().ListMissingModels.Handle(c) }
