package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/QuantumNous/new-api/internal/identity/recover_password"
	"github.com/gin-gonic/gin"
)

func TestStatus(c *gin.Context) { httpapi.System().Health.Handle(c) }

func GetStatus(c *gin.Context) { httpapi.Settings().GetStatus.Handle(c) }

func GetNotice(c *gin.Context) { httpapi.Settings().GetNotice.Handle(c) }

func GetAbout(c *gin.Context) { httpapi.Settings().GetAbout.Handle(c) }

func GetUserAgreement(c *gin.Context) { httpapi.Settings().GetUserAgreement.Handle(c) }

func GetPrivacyPolicy(c *gin.Context) { httpapi.Settings().GetPrivacyPolicy.Handle(c) }

func GetHomePageContent(c *gin.Context) { httpapi.Settings().GetHomePageContent.Handle(c) }

func SendEmailVerification(c *gin.Context) { httpapi.Identity().SendVerificationEmail.Handle(c) }

func SendPasswordResetEmail(c *gin.Context) { httpapi.Identity().RequestPasswordReset.Handle(c) }

type PasswordResetRequest = recover_password.PasswordResetRequest

func ResetPassword(c *gin.Context) { httpapi.Identity().RecoverPassword.Handle(c) }
