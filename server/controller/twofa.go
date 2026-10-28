package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/QuantumNous/new-api/internal/identity/finish_twofa_setup"

	"github.com/gin-gonic/gin"
)

type Verify2FARequest = finish_twofa_setup.Verify2FARequest

func Setup2FA(c *gin.Context) { httpapi.Identity().StartTwoFASetup.Handle(c) }

func Enable2FA(c *gin.Context) { httpapi.Identity().FinishTwoFASetup.Handle(c) }

func Disable2FA(c *gin.Context) { httpapi.Identity().DisableTwoFA.Handle(c) }

func Get2FAStatus(c *gin.Context) { httpapi.Identity().GetTwoFAStatus.Handle(c) }

func RegenerateBackupCodes(c *gin.Context) { httpapi.Identity().RegenerateBackupCodes.Handle(c) }

func Verify2FALogin(c *gin.Context) { httpapi.Identity().VerifyLoginCode.Handle(c) }

func Admin2FAStats(c *gin.Context) { httpapi.Identity().GetTwoFAStats.Handle(c) }

func AdminDisable2FA(c *gin.Context) { httpapi.Identity().AdminDisableTwoFA.Handle(c) }
