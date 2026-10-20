package controller

import (
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func VerifyLogin(c *gin.Context) { NewIdentityHTTP().VerifyLoginCode.Handle(c) }

func LoginPasskeyBegin(c *gin.Context) { NewIdentityHTTP().BeginLoginPasskey.Handle(c) }

func LoginPasskeyFinish(c *gin.Context) { NewIdentityHTTP().FinishLoginPasskey.Handle(c) }

func completeVerifiedLoginResponse(c *gin.Context, bundle *service.AuthBundle, method string) {
	NewIdentityHTTP().PrimaryLogin.Verified(c, bundle, method)
}
