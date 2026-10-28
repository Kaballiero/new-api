package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func RequestWaffoPancakeAmount(c *gin.Context) { httpapi.Payments().QuotePancake.Handle(c) }

func RequestWaffoPancakePay(c *gin.Context) { httpapi.Payments().CreatePancake.Handle(c) }

func WaffoPancakeWebhook(c *gin.Context) { httpapi.Payments().PancakeWebhook.Handle(c) }

func ListWaffoPancakeCatalog(c *gin.Context) { httpapi.Payments().Catalog.Handle(c) }

func CreateWaffoPancakePair(c *gin.Context) { httpapi.Payments().Pair.Handle(c) }

func SaveWaffoPancake(c *gin.Context) { httpapi.Payments().SavePancake.Handle(c) }
