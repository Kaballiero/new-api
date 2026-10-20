package controller

import (
	"github.com/gin-gonic/gin"
)

func GetGroups(c *gin.Context) { NewAccountsHTTP().GetGroups.List(c) }

func GetUserGroups(c *gin.Context) { NewAccountsHTTP().GetGroups.ForUser(c) }
