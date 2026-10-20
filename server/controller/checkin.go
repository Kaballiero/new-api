package controller

import (
	"github.com/gin-gonic/gin"
)

// GetCheckinStatus 获取用户签到状态和历史记录
func GetCheckinStatus(c *gin.Context) { NewAccountsHTTP().GetCheckinStatus.Handle(c) }

// DoCheckin 执行用户签到
func DoCheckin(c *gin.Context) { NewAccountsHTTP().CheckIn.Handle(c) }
