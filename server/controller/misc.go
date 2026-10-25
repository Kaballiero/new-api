package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/recover_password"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func TestStatus(c *gin.Context) {
	err := model.PingDB()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "数据库连接失败",
		})
		return
	}
	// 获取HTTP统计信息
	httpStats := middleware.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    "Server is running",
		"http_stats": httpStats,
	})
	return
}

func GetStatus(c *gin.Context) { settingsHTTP().GetStatus.Handle(c) }

func GetNotice(c *gin.Context) { settingsHTTP().GetNotice.Handle(c) }

func GetAbout(c *gin.Context) { settingsHTTP().GetAbout.Handle(c) }

func GetUserAgreement(c *gin.Context) { settingsHTTP().GetUserAgreement.Handle(c) }

func GetPrivacyPolicy(c *gin.Context) { settingsHTTP().GetPrivacyPolicy.Handle(c) }

func GetMidjourney(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["Midjourney"],
	})
	return
}

func GetHomePageContent(c *gin.Context) { settingsHTTP().GetHomePageContent.Handle(c) }

func SendEmailVerification(c *gin.Context) { identityHTTP().SendVerificationEmail.Handle(c) }

func SendPasswordResetEmail(c *gin.Context) { identityHTTP().RequestPasswordReset.Handle(c) }

type PasswordResetRequest = recover_password.PasswordResetRequest

func ResetPassword(c *gin.Context) { identityHTTP().RecoverPassword.Handle(c) }
