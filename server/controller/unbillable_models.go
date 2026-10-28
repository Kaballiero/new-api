package controller

import (
	"github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetUnbillableModels(c *gin.Context) { httpapi.Pricing().ListUnbillableModels.Handle(c) }
