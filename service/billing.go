package service

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

type wssBillingSettler interface {
	RecordWssDirectCharge(quota int, fundingApplied, tokenApplied bool)
	SettleWss(actualQuota int) error
}

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo != nil && relayInfo.QuotaClamp != nil {
		return BillingAdmissionError(c, relayInfo, relayInfo.QuotaClamp)
	}
	if preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		if err := relayInfo.Billing.Settle(actualQuota); err != nil {
			return err
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		return PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true)
	}
	return nil
}

// SettleWssBilling keeps ordinary HTTP settlement unchanged while making the
// realtime provisional debits visible to the existing billing session.
func SettleWssBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if relayInfo.Billing != nil {
		wssSession, ok := relayInfo.Billing.(wssBillingSettler)
		if !ok {
			return SettleBilling(ctx, relayInfo, actualQuota)
		}
		return wssSession.SettleWss(actualQuota)
	}
	if !relayInfo.WssFundingSettled {
		fundingDelta := actualQuota - relayInfo.WssFundingDirect
		if relayInfo.BillingSource == BillingSourceSubscription {
			if relayInfo.SubscriptionId == 0 {
				return fmt.Errorf("subscription id is missing")
			}
			if err := model.PostConsumeUserSubscriptionDelta(relayInfo.SubscriptionId, int64(fundingDelta)); err != nil {
				return err
			}
			relayInfo.SubscriptionPostDelta += int64(fundingDelta)
		} else if fundingDelta > 0 {
			if err := model.DecreaseUserQuota(relayInfo.UserId, fundingDelta, false); err != nil {
				return err
			}
		} else if fundingDelta < 0 {
			if err := model.IncreaseUserQuota(relayInfo.UserId, -fundingDelta, false); err != nil {
				return err
			}
		}
		relayInfo.WssFundingSettled = true
	}
	if relayInfo.IsPlayground {
		return nil
	}
	if relayInfo.WssTokenSettled {
		return nil
	}
	tokenDelta := actualQuota - relayInfo.WssTokenDirect
	if tokenDelta > 0 {
		if err := model.DecreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, tokenDelta); err != nil {
			return err
		}
	} else if tokenDelta < 0 {
		if err := model.IncreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, -tokenDelta); err != nil {
			return err
		}
	}
	relayInfo.WssTokenSettled = true
	return nil
}
