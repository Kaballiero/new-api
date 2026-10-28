package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/QuantumNous/new-api/internal/usage/admin_query_user_logs"

	"github.com/gin-gonic/gin"
)

type AdminLogQueryRequest = admin_query_user_logs.AdminLogQueryRequest

func GetAdminUserLogs(c *gin.Context) { httpapi.Usage().QueryUserLogs.Get(c) }

func QueryAdminLogs(c *gin.Context) { httpapi.Usage().QueryUserLogs.Post(c) }
