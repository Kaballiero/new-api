package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func VideoProxy(c *gin.Context) { httpapi.Tasks().VideoProxy(c) }
