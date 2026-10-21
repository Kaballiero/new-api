package controller

import (
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func VerifyLogin(c *gin.Context) { identityHTTP().VerifyLoginCode.Handle(c) }

func LoginPasskeyBegin(c *gin.Context) { identityHTTP().BeginLoginPasskey.Handle(c) }

func LoginPasskeyFinish(c *gin.Context) { identityHTTP().FinishLoginPasskey.Handle(c) }

func completeVerifiedLoginResponse(c *gin.Context, bundle *service.AuthBundle, method string) {
	identityHTTP().PrimaryLogin.Verified(c, bundle, method)
}
