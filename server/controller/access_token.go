package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetAccessTokenStatus(c *gin.Context) { httpapi.PersonalAccessTokens().Status.Handle(c) }

func GenerateAccessToken(c *gin.Context) { httpapi.PersonalAccessTokens().Generate.Handle(c) }

func RevokeAccessToken(c *gin.Context) { httpapi.PersonalAccessTokens().Revoke.Handle(c) }

func GetAuditLogs(c *gin.Context) { httpapi.Audit().Handle(c) }
