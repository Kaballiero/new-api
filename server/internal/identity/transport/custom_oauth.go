package transport

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
)

// WriteCustomOAuthError preserves management and discovery error envelopes.
func WriteCustomOAuthError(c *gin.Context, err error) {
	var message string
	switch {
	case errors.Is(err, domain.ErrCustomOAuthProviderMissing):
		message = "未找到该 OAuth 提供商"
	case errors.Is(err, domain.ErrCustomOAuthSlugTaken):
		message = "该 Slug 已被使用"
	case errors.Is(err, domain.ErrCustomOAuthBuiltinConflict):
		message = "该 Slug 与内置 OAuth 提供商冲突"
	case errors.Is(err, domain.ErrCustomOAuthBindingCheck):
		message = "检查用户绑定时发生错误，请稍后重试"
	case errors.Is(err, domain.ErrCustomOAuthProviderBound):
		message = "该 OAuth 提供商还有用户绑定，无法删除。请先解除所有用户绑定。"
	case errors.Is(err, domain.ErrCustomOAuthDiscoveryMissingURL):
		message = "请先填写 Discovery URL 或 Issuer URL"
	case errors.Is(err, domain.ErrCustomOAuthDiscoveryInvalidURL):
		message = "Discovery URL 无效，仅支持 http/https"
	default:
		var discoveryError *domain.DiscoveryError
		if !errors.As(err, &discoveryError) {
			common.ApiError(c, err)
			return
		}
		switch discoveryError.Stage {
		case "request":
			message = "创建 Discovery 请求失败: "
		case "fetch":
			message = "获取 Discovery 配置失败: "
		case "decode":
			message = "解析 Discovery 配置失败: "
		}
		message += discoveryError.Cause.Error()
	}
	common.ApiErrorMsg(c, message)
}
