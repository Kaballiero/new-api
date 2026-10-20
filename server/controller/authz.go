package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

func GetPermissionCatalog(c *gin.Context) { app.NewAuthorizationHTTP().Handle(c) }
