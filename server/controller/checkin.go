package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetCheckinStatus(c *gin.Context) { httpapi.Accounts().GetCheckinStatus.Handle(c) }

func DoCheckin(c *gin.Context) { httpapi.Accounts().CheckIn.Handle(c) }
