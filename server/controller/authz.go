package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetPermissionCatalog(c *gin.Context) { httpapi.Authorization().Handle(c) }
