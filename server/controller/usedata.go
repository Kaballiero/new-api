package controller

import "github.com/gin-gonic/gin"

// BRIDGE: unchanged router/api-router.go entrypoints.
func GetAllQuotaDates(c *gin.Context) { usageHTTP().AdminQuota.Handle(c) }

func GetQuotaDatesByUser(c *gin.Context) { usageHTTP().UserQuota.Handle(c) }

func GetUserQuotaDates(c *gin.Context) { usageHTTP().OwnQuota.Handle(c) }

func GetAllFlowQuotaDates(c *gin.Context) { usageHTTP().AdminFlow.Handle(c) }

func GetUserFlowQuotaDates(c *gin.Context) { usageHTTP().OwnFlow.Handle(c) }
