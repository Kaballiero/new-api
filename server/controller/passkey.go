package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func PasskeyRegisterBegin(c *gin.Context) { httpapi.Identity().BeginPasskeyRegistration.Handle(c) }

func PasskeyRegisterFinish(c *gin.Context) { httpapi.Identity().FinishPasskeyRegistration.Handle(c) }

func PasskeyDelete(c *gin.Context) { httpapi.Identity().DeletePasskey.Handle(c) }

func PasskeyStatus(c *gin.Context) { httpapi.Identity().GetPasskeyStatus.Handle(c) }

func PasskeyLoginBegin(c *gin.Context) { httpapi.Identity().BeginPasskeyLogin.Handle(c) }

func PasskeyLoginFinish(c *gin.Context) { httpapi.Identity().FinishPasskeyLogin.Handle(c) }

func AdminResetPasskey(c *gin.Context) { httpapi.Identity().AdminResetPasskey.Handle(c) }

func PasskeyVerifyBegin(c *gin.Context) { httpapi.Identity().BeginPasskeyVerification.Handle(c) }

func PasskeyVerifyFinish(c *gin.Context) { httpapi.Identity().FinishPasskeyVerification.Handle(c) }
