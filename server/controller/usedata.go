package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func GetAllQuotaDates(c *gin.Context) { httpapi.Usage().AdminQuota.Handle(c) }

func GetQuotaDatesByUser(c *gin.Context) { httpapi.Usage().UserQuota.Handle(c) }

func GetUserQuotaDates(c *gin.Context) { httpapi.Usage().OwnQuota.Handle(c) }

func GetAllFlowQuotaDates(c *gin.Context) { httpapi.Usage().AdminFlow.Handle(c) }

func GetUserFlowQuotaDates(c *gin.Context) { httpapi.Usage().OwnFlow.Handle(c) }
