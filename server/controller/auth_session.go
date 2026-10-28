package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func RefreshAuth(c *gin.Context) { httpapi.Identity().RefreshSession.Handle(c) }

func AuthLogout(c *gin.Context) { httpapi.Identity().Logout.Handle(c) }

func GetLoginSessions(c *gin.Context) { httpapi.Identity().ListSessions.Handle(c) }

func DeleteLoginSession(c *gin.Context) { httpapi.Identity().RevokeSession.Handle(c) }

func RevokeOtherLoginSessions(c *gin.Context) { httpapi.Identity().RevokeOtherSessions.Handle(c) }
