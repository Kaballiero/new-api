package identityhttp

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/gin-gonic/gin"
	"net/http"
)

func WritePasskeyFailure(c *gin.Context, failure domain.PasskeyFailure) bool {
	switch failure {
	case domain.PasskeyIdentityRequired:
		common.ApiErrorMsg(c, "当前认证方式不支持安全验证")
	case domain.PasskeyRecordMissing:
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "该用户尚未绑定 Passkey"})
	case domain.PasskeyRecordInvalid:
		common.ApiErrorMsg(c, "无法创建 Passkey 凭证")
	case domain.PasskeyLoginInvalid:
		common.ApiErrorMsg(c, "Passkey 登录状态异常")
	case domain.PasskeyAccountDisabled:
		common.ApiErrorMsg(c, "该用户已被禁用")
	default:
		return false
	}
	return true
}
