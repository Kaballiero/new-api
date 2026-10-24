package taskruntime

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/QuantumNous/new-api/common"
	billingmath "github.com/QuantumNous/new-api/internal/adapter/billing"
	billingstore "github.com/QuantumNous/new-api/internal/adapter/gorm/billing"
	"github.com/QuantumNous/new-api/internal/billing/apply_quota_delta"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

type taskDeltaStore struct {
	billingstore.Store
	ctx   context.Context
	task  *domain.Task
	delta int
}

func (s taskDeltaStore) token(account billingdomain.Account, amount int, credit bool) error {
	if account.TokenID <= 0 || s.delta == 0 {
		return nil
	}
	token, err := model.GetTokenById(account.TokenID)
	if err != nil {
		logger.LogWarn(s.ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", account.TokenID, s.task.TaskID, err.Error()))
		return fmt.Errorf("task token is unavailable")
	}
	if token.Key == "" {
		return fmt.Errorf("task token is unavailable")
	}
	if credit {
		err = model.IncreaseTokenQuota(account.TokenID, token.Key, amount)
	} else {
		err = model.DecreaseTokenQuota(account.TokenID, token.Key, amount)
	}
	if err != nil {
		logger.LogWarn(s.ctx, fmt.Sprintf("调整令牌额度失败 (delta=%d, task=%s): %s", s.delta, s.task.TaskID, err.Error()))
	}
	return err
}
func (s taskDeltaStore) DebitToken(a billingdomain.Account, n int) error  { return s.token(a, n, false) }
func (s taskDeltaStore) CreditToken(a billingdomain.Account, n int) error { return s.token(a, n, true) }

type TaskDelta struct{}

func (TaskDelta) Apply(ctx context.Context, task *domain.Task, delta int) (domain.DeltaResult, error) {
	source := billingdomain.Wallet
	if task.PrivateData.BillingSource == billingdomain.Subscription && task.PrivateData.SubscriptionId > 0 {
		source = billingdomain.Subscription
	}
	store := taskDeltaStore{ctx: ctx, task: task, delta: delta}
	out, err := apply_quota_delta.NewUsecase(store).Execute(apply_quota_delta.Input{Account: billingdomain.Account{UserID: task.UserId, TokenID: task.PrivateData.TokenId}, Source: source, SubscriptionID: task.PrivateData.SubscriptionId, Delta: delta})
	return domain.DeltaResult{FundingApplied: out.FundingApplied, TokenApplied: out.TokenApplied}, err
}

type TaskMath struct{ billingmath.CostMath }

func (TaskMath) Float(value float64) (int, *billingdomain.Clamp) {
	n, c := common.QuotaFromFloatChecked(value)
	return n, billingmath.Clamp(c)
}
func (TaskMath) OtherMultiplier(bc *domain.TaskBillingContext) float64 {
	if bc == nil || len(bc.OtherRatios) == 0 {
		return 1
	}
	price := &types.PriceData{}
	if !price.ReplaceOtherRatios(bc.OtherRatios) {
		return 1
	}
	return price.OtherRatioMultiplier()
}
func (TaskMath) Decimal(value decimal.Decimal) (int, *billingdomain.Clamp) {
	n, c := common.QuotaFromDecimalChecked(value)
	return n, billingmath.Clamp(c)
}
func (TaskMath) Saturation(clamp *billingdomain.Clamp) (string, error) {
	c := &common.QuotaClamp{Op: clamp.Op, Kind: common.QuotaClampKind(clamp.Kind), Original: clamp.Original, Clamped: clamp.Clamped}
	b, err := common.Marshal(c.AuditMap())
	return string(b), err
}

type TaskPrices struct{}

func (TaskPrices) ModelRatio(name string) (float64, bool, string) {
	return ratio_setting.GetModelRatio(name)
}
func (TaskPrices) GroupRatio(group string) float64 { return ratio_setting.GetGroupRatio(group) }
func (TaskPrices) SpecialRatio(a, b string) (float64, bool) {
	return ratio_setting.GetGroupGroupRatio(a, b)
}
func (TaskPrices) UserGroup(id int) (string, error) {
	u, err := model.GetUserById(id, false)
	if err != nil {
		return "", err
	}
	return u.Group, nil
}
func (Diagnostics) Quota(quota int) string { return logger.LogQuota(quota) }

// MidjourneyBilling keeps the existing post-persistence debit and token order.
type MidjourneyBilling struct{ billingstore.Store }

func (MidjourneyBilling) Apply(ctx context.Context, account billingdomain.Account, source string, subscriptionID, quota int) (domain.DeltaResult, error) {
	out, err := apply_quota_delta.NewUsecase(billingstore.Store{}).Execute(apply_quota_delta.Input{Account: account, Source: source, SubscriptionID: subscriptionID, Delta: quota})
	return domain.DeltaResult{FundingApplied: out.FundingApplied, TokenApplied: out.TokenApplied}, err
}
func (MidjourneyBilling) RefundToken(ctx context.Context, task *domain.Midjourney, quota int) {
	if task.TokenId <= 0 {
		return
	}
	token, err := model.GetTokenById(task.TokenId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", task.TokenId, task.MjId, err.Error()))
		return
	}
	if token.Key == "" {
		return
	}
	if err := model.IncreaseTokenQuota(task.TokenId, token.Key, quota); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("退还 Midjourney 令牌额度失败 task %s: %s", task.MjId, err.Error()))
	}
}
