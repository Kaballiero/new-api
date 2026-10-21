package controller

import (
	"github.com/QuantumNous/new-api/internal/identity/finish_twofa_setup"

	"github.com/gin-gonic/gin"
)

type Verify2FARequest = finish_twofa_setup.Verify2FARequest

func Setup2FA(c *gin.Context) { identityHTTP().StartTwoFASetup.Handle(c) }

func Enable2FA(c *gin.Context) { identityHTTP().FinishTwoFASetup.Handle(c) }

// Disable2FA 禁用2FA
func Disable2FA(c *gin.Context) { identityHTTP().DisableTwoFA.Handle(c) }

// Get2FAStatus 获取用户2FA状态
func Get2FAStatus(c *gin.Context) { identityHTTP().GetTwoFAStatus.Handle(c) }

// RegenerateBackupCodes 重新生成备用码
func RegenerateBackupCodes(c *gin.Context) { identityHTTP().RegenerateBackupCodes.Handle(c) }

// Verify2FALogin 登录时验证2FA
func Verify2FALogin(c *gin.Context) {
	VerifyLogin(c)
}

// Admin2FAStats 管理员获取2FA统计信息
func Admin2FAStats(c *gin.Context) { identityHTTP().GetTwoFAStats.Handle(c) }

// AdminDisable2FA 管理员强制禁用用户2FA
func AdminDisable2FA(c *gin.Context) { identityHTTP().AdminDisableTwoFA.Handle(c) }
