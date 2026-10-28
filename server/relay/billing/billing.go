// Package billing maps gateway request state to the business reservation module.
package billing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/gateway/mapping"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/counters"
	usageadapter "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	app "github.com/QuantumNous/new-api/internal/app/billing"
	"github.com/QuantumNous/new-api/internal/app/usagestorage"
	"github.com/QuantumNous/new-api/internal/billing/apply_quota_delta"
	"github.com/QuantumNous/new-api/internal/billing/create_reservation"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/billing/increase_reservation"
	"github.com/QuantumNous/new-api/internal/billing/notify_quota"
	"github.com/QuantumNous/new-api/internal/billing/settle_reservation"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

type Gateway struct {
	usecases app.Usecases
	costs    app.Costs
}

func New() *Gateway { return &Gateway{usecases: app.New(), costs: app.NewCosts()} }

// The gateway shares one billing assembly in every linked binary.
var defaultGateway = New()

type Session struct {
	gateway     *Gateway
	info        *relaycommon.RelayInfo
	reservation *domain.Reservation
	language    func() string
	mu          sync.Mutex
}

func account(info *relaycommon.RelayInfo) domain.Account {
	return domain.Account{UserID: info.UserId, TokenID: info.TokenId, TokenKey: info.TokenKey, Unlimited: info.TokenUnlimited, Playground: info.IsPlayground}
}

// Avoid converting a nil Gin pointer into a non-nil context interface.
func logContext(c *gin.Context) context.Context {
	if c == nil {
		return nil
	}
	return c
}

// Capture request language inputs before Gin reuses its context. Resolution may
// load user settings, so defer it until an error actually needs translation.
func captureErrorLanguage(c *gin.Context) func() string {
	if c == nil || c.Request == nil {
		return func() string { return i18n.DefaultLang }
	}
	snapshot := c.Copy()
	snapshot.Request = c.Request.Clone(c.Request.Context())
	return sync.OnceValue(func() string { return i18n.GetLangFromContext(snapshot) })
}

func (g *Gateway) PreConsume(c *gin.Context, quota int, info *relaycommon.RelayInfo) *types.NewAPIError {
	if info != nil && info.QuotaClamp != nil {
		return BillingAdmissionError(c, info, info.QuotaClamp)
	}
	if quota < 0 {
		return types.NewErrorWithStatusCode(fmt.Errorf("pre-consume quota cannot be negative: %d", quota), types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if info == nil {
		return types.NewError(errors.New("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	tokenQuota := 0
	if c != nil {
		tokenQuota = c.GetInt("token_quota")
	}
	r, err := g.usecases.Create.Execute(create_reservation.Input{Account: account(info), RequestID: info.RequestId, ModelName: info.GetBillingModelName(), Preference: info.UserSetting.BillingPreference, Quota: quota, UserQuota: info.UserQuota, TokenQuota: tokenQuota, Force: info.ForcePreConsume})
	info.UserQuota = r.UserQuota
	if err != nil {
		return mapError(captureErrorLanguage(c)(), err)
	}
	session := &Session{gateway: g, info: info, reservation: r, language: captureErrorLanguage(c)}
	session.syncRelayInfo()
	info.Billing = session
	logger.LogInfo(logContext(c), fmt.Sprintf("billing reservation: user=%d quota=%s funding=%s trusted=%t", info.UserId, logger.FormatQuota(r.PreConsumed), r.Source, r.Trusted))
	return nil
}

func (s *Session) syncRelayInfo() {
	r, info := s.reservation, s.info
	info.FinalPreConsumedQuota, info.BillingSource = r.PreConsumed, r.Source
	if r.Source == domain.Subscription {
		sub := r.Subscription
		info.SubscriptionId = sub.ID
		info.SubscriptionPreConsumed = sub.PreConsumed + int64(r.ExtraReserved)
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = sub.AmountTotal
		info.SubscriptionAmountUsedAfterPreConsume = sub.AmountUsedAfter + int64(r.ExtraReserved)
		info.SubscriptionPlanId, info.SubscriptionPlanTitle = sub.PlanID, sub.PlanTitle
	} else {
		info.SubscriptionId, info.SubscriptionPreConsumed = 0, 0
	}
}

// Funding retains its original owner; token metadata follows the gateway's
// current request state, as the legacy session did at each mutation.
func (s *Session) refreshAccount() {
	current := account(s.info)
	current.UserID = s.reservation.Account.UserID
	s.reservation.Account = current
}

func (s *Session) Reserve(target int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshAccount()
	previous := s.reservation.PreConsumed
	err := s.gateway.usecases.Increase.Execute(increase_reservation.Input{Reservation: s.reservation, Target: target, AtomicWallet: s.info.OpenRouterCost != nil})
	if err != nil {
		return mapError(s.language(), err)
	}
	if s.reservation.PreConsumed != previous {
		s.syncRelayInfo()
	}
	return nil
}

func (s *Session) Settle(actual int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshAccount()
	previous := s.reservation.SubscriptionPostDelta
	err := s.gateway.usecases.Settle.Execute(settle_reservation.Input{Reservation: s.reservation, Actual: actual, RecordOutcome: s.info.OpenRouterCost != nil})
	s.info.SubscriptionPostDelta += s.reservation.SubscriptionPostDelta - previous
	return err
}
func (s *Session) SettleWss(actual int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshAccount()
	previous := s.reservation.SubscriptionPostDelta
	err := s.gateway.usecases.Settle.Execute(settle_reservation.Input{Reservation: s.reservation, Actual: actual, Realtime: true})
	s.info.SubscriptionPostDelta += s.reservation.SubscriptionPostDelta - previous
	return err
}
func (s *Session) RecordWssDirectCharge(quota int, fundingApplied, tokenApplied bool) {
	if quota <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if fundingApplied {
		s.reservation.WssFundingDirect += quota
	}
	if tokenApplied {
		s.reservation.WssTokenDirect += quota
	}
}
func (s *Session) Refund(c *gin.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshAccount()
	if !s.reservation.NeedsRefund() {
		return
	}
	logger.LogInfo(logContext(c), fmt.Sprintf("refunding billing reservation: user=%d token_quota=%s funding=%s", s.info.UserId, logger.FormatQuota(s.reservation.TokenConsumed), s.reservation.Source))
	_ = s.gateway.usecases.Refund.Execute(s.reservation)
}
func (s *Session) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reservation.NeedsRefund()
}
func (s *Session) GetPreConsumedQuota() int { return s.reservation.PreConsumed }
func (s *Session) OpenRouterSettlementOutcome() domain.Outcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reservation.Outcome
}

func (g *Gateway) Settle(c *gin.Context, info *relaycommon.RelayInfo, actual int) error {
	if info.Billing == nil {
		delta := actual - info.FinalPreConsumedQuota
		if delta == 0 {
			return nil
		}
		_, _, err := g.ApplyDelta(info, delta, info.FinalPreConsumedQuota, true)
		return err
	}
	preConsumed := info.Billing.GetPreConsumedQuota()
	logger.LogInfo(logContext(c), fmt.Sprintf("settling billing: delta=%s actual=%s reserved=%s", logger.FormatQuota(actual-preConsumed), logger.FormatQuota(actual), logger.FormatQuota(preConsumed)))
	if err := info.Billing.Settle(actual); err != nil {
		return err
	}
	return g.NotifyQuota(info, actual-preConsumed, preConsumed, info.BillingSource == domain.Subscription)
}
func (g *Gateway) SettleWss(c *gin.Context, info *relaycommon.RelayInfo, actual int) error {
	if info.Billing != nil {
		if session, ok := info.Billing.(wssBillingSettler); ok {
			return session.SettleWss(actual)
		}
		return g.Settle(c, info, actual)
	}
	result, err := g.usecases.Apply.Execute(apply_quota_delta.Input{Account: account(info), Source: info.BillingSource, SubscriptionID: info.SubscriptionId, Realtime: true, FundingSettled: info.WssFundingSettled, TokenSettled: info.WssTokenSettled, FundingDelta: actual - info.WssFundingDirect, TokenDelta: actual - info.WssTokenDirect})
	info.SubscriptionPostDelta += result.SubscriptionPostDelta
	if result.FundingApplied {
		info.WssFundingSettled = true
	}
	if result.TokenApplied {
		info.WssTokenSettled = true
	}
	return err
}
func (g *Gateway) ApplyDelta(info *relaycommon.RelayInfo, delta, preConsumed int, sendNotification bool) (bool, bool, error) {
	result, err := g.usecases.Apply.Execute(apply_quota_delta.Input{Account: account(info), Source: info.BillingSource, SubscriptionID: info.SubscriptionId, Delta: delta})
	info.SubscriptionPostDelta += result.SubscriptionPostDelta
	if err == nil && sendNotification {
		err = g.NotifyQuota(info, delta, preConsumed, false)
	}
	return result.FundingApplied, result.TokenApplied, err
}

func (g *Gateway) NotifyQuota(info *relaycommon.RelayInfo, delta, preConsumed int, subscription bool) error {
	return g.usecases.Notify.Execute(notify_quota.Input{Delta: delta, PreConsumed: preConsumed, Subscription: subscription, State: func() domain.NotificationState {
		s := info.UserSetting
		return domain.NotificationState{
			Recipient: domain.Recipient{UserID: info.UserId, Email: info.UserEmail, Language: s.Language, NotifyType: s.NotifyType, NotificationEmail: s.NotificationEmail, WebhookURL: s.WebhookUrl, WebhookSecret: s.WebhookSecret, BarkURL: s.BarkUrl, GotifyURL: s.GotifyUrl, GotifyToken: s.GotifyToken, GotifyPriority: s.GotifyPriority, WarningThreshold: s.QuotaWarningThreshold},
			UserQuota: info.UserQuota, SubscriptionID: info.SubscriptionId, SubscriptionTotal: info.SubscriptionAmountTotal, SubscriptionUsedAfter: info.SubscriptionAmountUsedAfterPreConsume, SubscriptionPostDelta: info.SubscriptionPostDelta,
		}
	}})
}

func mapError(language string, err error) *types.NewAPIError {
	var failure *domain.Failure
	if !errors.As(err, &failure) {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	skip := types.ErrOptionWithSkipRetry()
	noLog := types.ErrOptionWithNoRecordErrorLog()
	switch failure.Kind {
	case domain.QueryFailed:
		return types.NewError(failure.Cause, types.ErrorCodeQueryDataError, skip)
	case domain.UpdateFailed:
		return types.NewError(failure.Cause, types.ErrorCodeUpdateDataError, skip)
	case domain.WalletEmpty:
		return types.NewErrorWithStatusCode(errors.New(i18n.Translate(language, i18n.MsgBillingWalletInsufficient, map[string]any{"Remaining": logger.FormatQuota(failure.Remaining)})), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, skip, noLog)
	case domain.WalletBelowEstimate:
		return types.NewErrorWithStatusCode(errors.New(i18n.Translate(language, i18n.MsgBillingWalletBelowReserve, map[string]any{"Remaining": logger.FormatQuota(failure.Remaining), "Required": logger.FormatQuota(failure.Required)})), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, skip, noLog)
	case domain.WalletReserveRejected:
		return types.NewErrorWithStatusCode(errors.New("wallet quota insufficient"), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, skip)
	case domain.SubscriptionUnavailable:
		return types.NewErrorWithStatusCode(errors.New(i18n.Translate(language, i18n.MsgBillingSubscriptionInsufficient, map[string]any{"Reason": failure.Cause.Error()})), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, skip, noLog)
	case domain.TokenReserveFailed:
		cause := failure.Cause
		var tokenFailure *domain.Failure
		if errors.As(cause, &tokenFailure) {
			if tokenFailure.Kind == domain.TokenInsufficient {
				cause = fmt.Errorf("token quota is not enough, token remain quota: %s, need quota: %s", logger.FormatQuota(tokenFailure.Remaining), logger.FormatQuota(tokenFailure.Required))
			} else if tokenFailure.Kind == domain.NegativeQuota {
				cause = errors.New(i18n.Translate(language, i18n.MsgBillingQuotaNegative))
			}
		}
		return types.NewErrorWithStatusCode(cause, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, skip, noLog)
	default:
		return types.NewError(err, types.ErrorCodeUpdateDataError, skip)
	}
}

func PreConsumeBilling(c *gin.Context, quota int, info *relaycommon.RelayInfo) *types.NewAPIError {
	return defaultGateway.PreConsume(c, quota, info)
}
func SettleBilling(c *gin.Context, info *relaycommon.RelayInfo, quota int) error {
	return defaultGateway.Settle(c, info, quota)
}

func recordViolationFee(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, feeQuota int, apiErr *types.NewAPIError, baseAmount, groupRatio float64) {
	accountstorage.SQL().UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, feeQuota)
	counters.UpdateChannelUsedQuota(model.DB, relayInfo.ChannelId, feeQuota)

	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	tokenName := ctx.GetString("token_name")
	oai := apiErr.ToOpenAIError()

	other := new(usagerecord.LogOther)
	other.MergePublic(map[string]any{
		"violation_fee":        true,
		"violation_fee_code":   string(types.ErrorCodeViolationFeeGrokCSAM),
		"fee_quota":            feeQuota,
		"base_amount":          baseAmount,
		"group_ratio":          groupRatio,
		"status_code":          apiErr.StatusCode,
		"upstream_error_type":  oai.Type,
		"upstream_error_code":  fmt.Sprintf("%v", oai.Code),
		"violation_fee_marker": mapping.CSAMViolationMarker,
	})

	usagestorage.Writer().RecordConsumeLog(ctx, relayInfo.UserId, usageadapter.RecordConsumeLogParams{
		ChannelId:      relayInfo.ChannelId,
		ModelName:      relayInfo.OriginModelName,
		TokenName:      tokenName,
		Quota:          feeQuota,
		Content:        "Violation fee charged",
		TokenId:        relayInfo.TokenId,
		UseTimeSeconds: int(useTimeSeconds),
		IsStream:       relayInfo.IsStream,
		Group:          relayInfo.UsingGroup,
		Other:          other,
	})

}

func PostConsumeQuota(info *relaycommon.RelayInfo, delta, preConsumed int, notify bool) error {
	_, _, err := defaultGateway.ApplyDelta(info, delta, preConsumed, notify)
	return err
}
