package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/usage/admin_query_user_logs"
)

// BRIDGE: existing controller consumers and OpenAPI request discovery.
type AdminLogQueryRequest = admin_query_user_logs.AdminLogQueryRequest

func GetAdminUserLogs(c *gin.Context) { usageHTTP().QueryUserLogs.Get(c) }

func QueryAdminLogs(c *gin.Context) { usageHTTP().QueryUserLogs.Post(c) }
