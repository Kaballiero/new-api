package controller

import (
	"github.com/gin-gonic/gin"

	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
)

func VerifyLogin(c *gin.Context) { identityHTTP().VerifyLoginCode.Handle(c) }

func LoginPasskeyBegin(c *gin.Context) { identityHTTP().BeginLoginPasskey.Handle(c) }

func LoginPasskeyFinish(c *gin.Context) { identityHTTP().FinishLoginPasskey.Handle(c) }

func completeVerifiedLoginResponse(c *gin.Context, bundle *identitydto.AuthBundle, method string) {
	identityHTTP().PrimaryLogin.Verified(c, bundle, method)
}
