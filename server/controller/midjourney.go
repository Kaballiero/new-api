package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetAllMidjourney(c *gin.Context) { httpapi.Tasks().GetAllMidjourney(c) }

func GetUserMidjourney(c *gin.Context) { httpapi.Tasks().GetUserMidjourney(c) }
