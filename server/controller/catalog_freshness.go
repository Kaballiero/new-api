package controller

import (
	"github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetCatalogFreshness(c *gin.Context) { httpapi.Pricing().GetCatalogFreshness.Handle(c) }
