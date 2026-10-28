package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func GetGroups(c *gin.Context) { httpapi.Accounts().GetGroups.List(c) }

func GetUserGroups(c *gin.Context) { httpapi.Accounts().GetGroups.ForUser(c) }
