package middleware

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	routing "github.com/QuantumNous/new-api/gateway/routing"
	"github.com/QuantumNous/new-api/i18n"
	accountcache "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

func writeGatewayCaller(c *gin.Context, caller domain.Caller) {
	if caller.Token != nil && c.GetInt("id") == 0 {
		c.Set("id", caller.Token.UserId)
	}
	if caller.Owner != nil {
		owner := caller.Owner
		(&accountcache.UserBase{Id: owner.ID, Group: owner.Group, Email: owner.Email, Username: owner.Username, Setting: owner.Setting, Quota: owner.Quota, Status: owner.Status}).WriteContext(c)
	}
	if caller.UsingGroup != nil {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, *caller.UsingGroup)
	}
	if caller.Context != nil {
		writeGatewayTokenContext(c, caller.Context)
	}
}

func writeGatewayTokenContext(c *gin.Context, result *domain.KeyContext) {
	token := result.Token
	c.Set("id", token.UserId)
	c.Set("token_id", token.Id)
	c.Set("token_key", token.Key)
	c.Set("token_name", token.Name)
	c.Set("token_unlimited_quota", token.UnlimitedQuota)
	if !token.UnlimitedQuota {
		c.Set("token_quota", token.RemainQuota)
	}
	c.Set("token_model_limit_enabled", token.ModelLimitsEnabled)
	if token.ModelLimitsEnabled {
		c.Set("token_model_limit", result.ModelLimits)
	}
	common.SetContextKey(c, constant.ContextKeyTokenGroup, token.Group)
	common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, token.CrossGroupRetry)
	if result.HasAutoGroups {
		common.SetContextKey(c, constant.ContextKeyTokenAutoGroups, result.AutoGroups)
	}
	if result.ChannelID != nil {
		routing.GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: *result.ChannelID, Source: dto.PinSourceToken, Rank: dto.PinRankToken, RetryMode: dto.PinRetrySingleAttempt})
	}
}

func writeGatewayKeyError(c *gin.Context, err error, readOnly bool) {
	status := http.StatusUnauthorized
	message := common.TranslateMessage(c, i18n.MsgTokenInvalid)
	code := types.ErrorCode("")
	switch {
	case errors.Is(err, domain.ErrInternal):
		status = http.StatusInternalServerError
		message = common.TranslateMessage(c, i18n.MsgDatabaseError)
	case errors.Is(err, domain.ErrKeyDisabled):
		message = common.TranslateMessage(c, i18n.MsgTokenStatusUnavailable)
	case errors.Is(err, domain.ErrOwnerBanned):
		status = http.StatusForbidden
		message = common.TranslateMessage(c, i18n.MsgAuthUserBanned)
	case errors.Is(err, domain.ErrIPInvalid):
		status = http.StatusForbidden
		message = i18n.T(c, i18n.MsgGatewayIPInvalid)
	case errors.Is(err, domain.ErrIPDenied):
		status = http.StatusForbidden
		message = i18n.T(c, i18n.MsgGatewayIPDenied)
		code = types.ErrorCodeAccessDenied
	case errors.Is(err, domain.ErrGroupForbidden), errors.Is(err, domain.ErrGroupDeprecated):
		var group *domain.GroupError
		if errors.As(err, &group) {
			status = http.StatusForbidden
			key := i18n.MsgGatewayGroupForbidden
			if errors.Is(err, domain.ErrGroupDeprecated) {
				key = i18n.MsgGatewayGroupDeprecated
			}
			message = i18n.T(c, key, map[string]any{"Group": group.Group})
		}
	case errors.Is(err, domain.ErrChannelInvalid):
		status = http.StatusBadRequest
		message = i18n.T(c, i18n.MsgDistributorInvalidChannelId)
	case errors.Is(err, domain.ErrChannelForbidden):
		status = http.StatusForbidden
		message = i18n.T(c, i18n.MsgGatewayChannelForbidden)
		c.Header("specific_channel_version", "701e3ae1dc3f7975556d354e0675168d004891c8")
	}
	if readOnly {
		c.AbortWithStatusJSON(status, gin.H{"success": false, "message": message})
		return
	}
	abortWithOpenAiMessage(c, status, message, code)
}
