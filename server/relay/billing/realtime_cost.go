package billing

import (
	apikeystorage "github.com/QuantumNous/new-api/internal/app/apikeystorage"

	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"

	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	adapter "github.com/QuantumNous/new-api/internal/adapter/billing"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

type wssBillingSettler interface {
	RecordWssDirectCharge(int, bool, bool)
	SettleWss(int) error
}

func noteQuotaClamp(info *relaycommon.RelayInfo, clamp *common.QuotaClamp) {
	if info != nil && info.QuotaClamp == nil {
		info.QuotaClamp = clamp
	}
}
func ValidateRealtimeUsage(u *dto.RealtimeUsage) error {
	return domain.ValidateRealtimeUsage(realtimeUsage(u))
}
func RealtimeCacheDiscountIssue(u *dto.RealtimeUsage, used map[string]bool) string {
	return domain.RealtimeCacheDiscountIssue(realtimeUsage(u), used)
}
func (g *Gateway) PreWss(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.RealtimeUsage) error {
	if err := ValidateRealtimeUsage(usage); err != nil {
		return fmt.Errorf("invalid preliminary WSS usage: %w", err)
	}
	if relayInfo.UsePrice {
		return nil
	}
	userQuota, err := accountstorage.SQL().GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return err
	}

	token, err := apikeystorage.Reader().Read(strings.TrimPrefix(relayInfo.TokenKey, "sk-"), false)
	if err != nil {
		return err
	}

	modelName := relayInfo.OriginModelName
	groupRatio := ratio_setting.GetGroupRatio(relayInfo.UsingGroup)
	modelRatio, _, _ := ratio_setting.GetModelRatio(modelName)

	var autoGroup any
	exists := false
	if ctx != nil {
		autoGroup, exists = common.GetContextKey(ctx, constant.ContextKeyAutoGroup)
	}
	if exists {
		groupRatio = ratio_setting.GetGroupRatio(autoGroup.(string))
		logger.LogDebug(logContext(ctx), "final group ratio: %f", groupRatio)
		relayInfo.UsingGroup = autoGroup.(string)
	}

	actualGroupRatio := groupRatio
	userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup)
	if ok {
		actualGroupRatio = userGroupRatio
	}
	billingFXFactor, err := CaptureBillingFX(relayInfo)
	if err != nil {
		return err
	}
	price := domain.AudioPrice{
		InputDetails:  domain.TokenDetails{TextTokens: usage.InputTokenDetails.TextTokens, AudioTokens: usage.InputTokenDetails.AudioTokens},
		OutputDetails: domain.TokenDetails{TextTokens: usage.OutputTokenDetails.TextTokens, AudioTokens: usage.OutputTokenDetails.AudioTokens},
		ModelRatio:    modelRatio, GroupRatio: actualGroupRatio, FXFactor: billingFXFactor,
		QuotaPerUnit: g.costs.AudioPrices.QuotaUnit(),
	}
	price.CompletionRatio, price.AudioRatio, price.AudioCompletionRatio = g.costs.AudioPrices.Read(modelName)
	quota, costClamp := domain.AudioQuota(price, adapter.CostMath{})
	clamp := gatewayClamp(costClamp)
	noteQuotaClamp(relayInfo, clamp)
	if clamp != nil {
		return clamp
	}

	if userQuota < quota {
		return fmt.Errorf("user quota is not enough, user quota: %s, need quota: %s", logger.FormatQuota(userQuota), logger.FormatQuota(quota))
	}

	if !token.UnlimitedQuota && token.RemainQuota < quota {
		return fmt.Errorf("token quota is not enough, token remain quota: %s, need quota: %s", logger.FormatQuota(token.RemainQuota), logger.FormatQuota(quota))
	}

	fundingApplied, tokenApplied, err := g.ApplyDelta(relayInfo, quota, 0, false)
	if billing, ok := relayInfo.Billing.(wssBillingSettler); ok {
		billing.RecordWssDirectCharge(quota, fundingApplied, tokenApplied)
	} else {
		if fundingApplied {
			relayInfo.WssFundingDirect += quota
		}
		if tokenApplied {
			relayInfo.WssTokenDirect += quota
		}
	}
	if err != nil {
		return err
	}
	logger.LogInfo(logContext(ctx), "realtime streaming consume quota success, quota: "+fmt.Sprintf("%d", quota))
	return nil
}

func PreWssConsumeQuota(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage) error {
	return defaultGateway.PreWss(c, info, usage)
}
