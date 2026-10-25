package controller

import (
	"github.com/gin-gonic/gin"
)

// BRIDGE: dashboard MJ router entrypoints; removed when router binds task handlers directly.
func GetAllMidjourney(c *gin.Context)  { taskHTTP().GetAllMidjourney(c) }
func GetUserMidjourney(c *gin.Context) { taskHTTP().GetUserMidjourney(c) }
