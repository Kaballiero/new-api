package controller

import (
	"github.com/gin-gonic/gin"
)

func GetRatioConfig(c *gin.Context) { pricingHTTP().GetRatioConfig.Handle(c) }
