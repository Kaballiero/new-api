package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func VerifyLogin(c *gin.Context) { httpapi.Identity().VerifyLoginCode.Handle(c) }

func LoginPasskeyBegin(c *gin.Context) { httpapi.Identity().BeginLoginPasskey.Handle(c) }

func LoginPasskeyFinish(c *gin.Context) { httpapi.Identity().FinishLoginPasskey.Handle(c) }
