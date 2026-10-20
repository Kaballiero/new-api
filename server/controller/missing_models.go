package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

func GetMissingModels(c *gin.Context) { app.NewGetMissingModelsHTTP().Handle(c) }
