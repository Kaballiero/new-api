package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetRatioConfig(c *gin.Context) { httpapi.Pricing().GetRatioConfig.Handle(c) }
