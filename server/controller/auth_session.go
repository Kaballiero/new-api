package controller

import (
	"github.com/gin-gonic/gin"
)

func RefreshAuth(c *gin.Context)                      { identityHTTP().RefreshSession.Handle(c) }
func AuthLogout(c *gin.Context)                       { identityHTTP().Logout.Handle(c) }
func GetLoginSessions(c *gin.Context)                 { identityHTTP().ListSessions.Handle(c) }
func DeleteLoginSession(c *gin.Context)               { identityHTTP().RevokeSession.Handle(c) }
func RevokeOtherLoginSessions(c *gin.Context)         { identityHTTP().RevokeOtherSessions.Handle(c) }
func writeAuthSessionError(c *gin.Context, err error) { identityErrors.WriteSessionError(c, err) }
