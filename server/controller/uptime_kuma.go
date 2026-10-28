package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetUptimeKumaStatus(c *gin.Context) { httpapi.System().Uptime.Handle(c) }
