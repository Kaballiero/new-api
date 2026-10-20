package controller

import (
	"github.com/QuantumNous/new-api/internal/app"

	"github.com/gin-gonic/gin"
)

func GetRatioConfig(c *gin.Context) { app.NewGetRatioConfigHTTP().Handle(c) }
