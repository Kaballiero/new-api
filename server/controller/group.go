package controller

import (
	"github.com/gin-gonic/gin"
)

func GetGroups(c *gin.Context) { accountsHTTP().GetGroups.List(c) }

func GetUserGroups(c *gin.Context) { accountsHTTP().GetGroups.ForUser(c) }
