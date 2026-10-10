package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const TaskSubmissionContextKey = "durable_paid_task_submission"

// BeginTaskReservation persists the intent before invoking the existing
// funding service. A crash anywhere after this barrier leaves an auditable
// task and cannot trigger an automatic replay or refund.
func BeginTaskReservation(c *gin.Context, platform constant.TaskPlatform, info *relaycommon.RelayInfo) (*model.Task, error) {
	task := InitTaskSubmission(c, platform, info)
	if task.PrivateData.Execution != nil && task.PrivateData.Execution.TaskPlugin != nil && task.PrivateData.Execution.TaskPlugin.Key == "openrouter-video" {
		task.PrivateData.BillingContext.SettlementMode = model.TaskSettlementOpenRouterCostV1
	}
	// Polling and private content must use the account that accepted this job,
	// even after a channel key rotation or multi-key selection.
	task.PrivateData.Key = info.ChannelMeta.ApiKey
	task.Quota = 0
	task.PrivateData.Reconciliation = &model.TaskReconciliation{
		ReservedQuota: info.PriceData.Quota, ReservationPending: true,
		SubmissionPending: true, Required: true,
	}
	if err := task.InsertWithContext(c.Request.Context()); err != nil {
		return nil, err
	}
	c.Set(TaskSubmissionContextKey, task)
	return task, nil
}

// ConfirmTaskReservation uses the existing BillingSession exactly once. Its
// pending marker remains durable until every accounting step is confirmed.
func ConfirmTaskReservation(c *gin.Context, task *model.Task, info *relaycommon.RelayInfo) error {
	task.PrivateData.BillingSource = info.BillingSource
	task.PrivateData.SubscriptionId = info.SubscriptionId
	task.Quota = info.PriceData.Quota
	if won, err := task.UpdateWithStatus(task.Status); err != nil {
		return err
	} else if !won {
		return fmt.Errorf("task reservation state changed before accounting")
	}
	if err := SettleBilling(c, info, task.Quota); err != nil {
		return err
	}
	if err := LogTaskConsumption(c, info, task); err != nil {
		return err
	}
	task.PrivateData.Reconciliation.ReservationPending = false
	won, err := task.UpdateWithStatus(task.Status)
	if err != nil {
		return err
	}
	if !won {
		return fmt.Errorf("task reservation state changed after accounting")
	}
	return nil
}

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task) error {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	// 支持任务仅按次计费
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = fmt.Sprintf("%s，按次计费", logContent)
	} else {
		var contents []string
		if otherRatios := info.PriceData.OtherRatios(); len(otherRatios) > 0 {
			for key, ra := range otherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
		}
		if snap := info.TieredBillingSnapshot; snap != nil {
			for key, value := range snap.UsageFacts {
				contents = append(contents, fmt.Sprintf("%s: %v", key, value))
			}
		}
		if len(contents) > 0 {
			logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
		}
	}
	other := model.NewLogOther()
	other.SetPublic("is_task", true)
	appendBillingFXLogInfo(other, info, info.PriceData, "submit")
	other.SetPublic("request_path", c.Request.URL.Path)
	other.SetPublic("model_price", info.PriceData.ModelPrice)
	if info.PriceData.ModelRatio > 0 {
		other.SetPublic("model_ratio", info.PriceData.ModelRatio)
	}
	other.SetPublic("group_ratio", info.PriceData.GroupRatioInfo.GroupRatio)
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other.SetPublic("user_group_ratio", info.PriceData.GroupRatioInfo.GroupSpecialRatio)
	}
	if info.IsModelMapped {
		other.SetPublic("is_model_mapped", true)
		other.SetPublic("upstream_model_name", info.UpstreamModelName)
	}
	if snap := info.TieredBillingSnapshot; snap != nil {
		other.SetPublic("billing_mode", "tiered_expr")
		other.SetPublic("expr_b64", base64.StdEncoding.EncodeToString([]byte(snap.ExprString)))
		other.SetPublic("matched_tier", snap.EstimatedTier)
		if len(snap.UsageFacts) > 0 {
			other.SetPublic("usage_facts", snap.UsageFacts)
		}
	}
	appendTaskLogInfo(task, other)
	attachQuotaSaturation(c, info, other)
	var logErr error
	if bc := task.PrivateData.BillingContext; bc != nil && bc.SettlementMode == model.TaskSettlementOpenRouterCostV1 {
		other.SetPublic("settlement_mode", bc.SettlementMode)
		other.SetPublic("reserved_quota", task.Quota)
		logErr = model.RecordTaskBillingLogWithError(model.RecordTaskBillingLogParams{
			UserId: info.UserId, LogType: model.LogTypeConsume, ChannelId: info.ChannelId, ModelName: info.OriginModelName,
			TokenId: info.TokenId, Quota: info.PriceData.Quota, Content: logContent, Group: info.UsingGroup, Other: other, NodeName: task.PrivateData.NodeName,
		})
	} else {
		model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
			ChannelId: info.ChannelId,
			ModelName: info.OriginModelName,
			TokenName: tokenName,
			Quota:     info.PriceData.Quota,
			Content:   logContent,
			TokenId:   info.TokenId,
			Group:     info.UsingGroup,
			Other:     other,
		})
	}

	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, info.PriceData.Quota)
	model.UpdateChannelUsedQuota(info.ChannelId, info.PriceData.Quota)
	return logErr
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// resolveTokenKey 通过 TokenId 运行时获取令牌 Key（用于 Redis 缓存操作）。
// 如果令牌已被删除或查询失败，返回空字符串。
func resolveTokenKey(ctx context.Context, tokenId int, taskID string) string {
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", tokenId, taskID, err.Error()))
		return ""
	}
	return token.Key
}

// taskIsSubscription 判断任务是否通过订阅计费。
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

// taskAdjustFunding 调整任务的资金来源（钱包或订阅），delta > 0 表示扣费，delta < 0 表示退还。
func taskAdjustFunding(task *model.Task, delta int) error {
	if taskIsSubscription(task) {
		return model.PostConsumeUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta))
	}
	if delta > 0 {
		return model.DecreaseUserQuota(task.UserId, delta, false)
	}
	return model.IncreaseUserQuota(task.UserId, -delta, false)
}

// taskAdjustTokenQuota 调整任务的令牌额度，delta > 0 表示扣费，delta < 0 表示退还。
// 需要通过 resolveTokenKey 运行时获取 key（不从 PrivateData 中读取）。
func taskAdjustTokenQuota(ctx context.Context, task *model.Task, delta int) error {
	if task.PrivateData.TokenId <= 0 || delta == 0 {
		return nil
	}
	tokenKey := resolveTokenKey(ctx, task.PrivateData.TokenId, task.TaskID)
	if tokenKey == "" {
		return fmt.Errorf("task token is unavailable")
	}
	var err error
	if delta > 0 {
		err = model.DecreaseTokenQuota(task.PrivateData.TokenId, tokenKey, delta)
	} else {
		err = model.IncreaseTokenQuota(task.PrivateData.TokenId, tokenKey, -delta)
	}
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("调整令牌额度失败 (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
	}
	return err
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) *model.LogOther {
	return taskBillingOtherWithProjection(task, nil)
}

func taskBillingOtherWithProjection(task *model.Task, projection *taskBillingProjection) *model.LogOther {
	other := model.NewLogOther()
	if bc := task.PrivateData.BillingContext; bc != nil {
		other.SetPublic("model_price", bc.ModelPrice)
		if bc.ModelRatio > 0 {
			other.SetPublic("model_ratio", bc.ModelRatio)
		}
		if _, modern, err := taskBillingFXFactor(bc); err == nil && modern {
			if projection == nil {
				other.SetPublic("group_ratio", bc.SubmitGroup.PureRatio)
				if bc.SubmitGroup.HasSpecialRatio {
					other.SetPublic("user_group_ratio", *bc.SubmitGroup.SpecialRatio)
				}
			}
			other.SetAdmin("billing_submit_group", map[string]any{
				"pure_ratio":        bc.SubmitGroup.PureRatio,
				"has_special_ratio": bc.SubmitGroup.HasSpecialRatio,
				"special_ratio":     bc.SubmitGroup.SpecialRatio,
			})
			other.SetAdmin("billing_fx", map[string]any{
				"schema_version":      bc.BillingFX.SchemaVersion,
				"source":              bc.BillingFX.Source,
				"rate":                bc.BillingFX.Rate,
				"publication_version": bc.BillingFX.PublicationVersion,
				"effective_at":        bc.BillingFX.EffectiveAt,
				"fetched_at":          bc.BillingFX.FetchedAt,
			})
			other.SetAdmin("billing_stage", "saved_quota")
		} else if !modern {
			other.SetPublic("group_ratio", bc.GroupRatio)
		}
		if priceData := taskBillingContextPriceData(bc); priceData != nil {
			for k, v := range priceData.OtherRatios() {
				if !other.SetPublic(k, v) {
					common.SysError("task billing other ratio key rejected: " + k)
				}
			}
		}
		if snap := bc.TieredSnapshot; snap != nil {
			other.SetPublic("billing_mode", "tiered_expr")
			other.SetPublic("expr_b64", base64.StdEncoding.EncodeToString([]byte(snap.ExprString)))
			other.SetPublic("matched_tier", snap.EstimatedTier)
			if len(snap.UsageFacts) > 0 {
				other.SetPublic("usage_facts", snap.UsageFacts)
			}
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other.SetPublic("is_model_mapped", true)
		other.SetPublic("upstream_model_name", props.UpstreamModelName)
	}
	appendTaskLogInfo(task, other)
	return other
}

type taskBillingProjection struct {
	modelRatio      float64
	groupRatio      float64
	hasSpecialRatio bool
	specialRatio    float64
}

// taskBillingFXFactor validates modern durable task history. Both new members
// are required together; their joint absence is the only legacy marker.
func taskBillingFXFactor(bc *model.TaskBillingContext) (float64, bool, error) {
	if bc == nil || (!bc.HasBillingFX() && !bc.HasSubmitGroup()) {
		return 1, false, nil
	}
	if !bc.HasBillingFX() || !bc.HasSubmitGroup() || !bc.HasCompleteBillingFX() || !bc.HasCompleteSubmitGroup() || bc.BillingFX == nil || bc.SubmitGroup == nil {
		return 0, true, fmt.Errorf("task billing FX history is incomplete")
	}
	fx := bc.BillingFX
	if fx.SchemaVersion != 1 || fx.Source != modelCostFXSource || fx.Rate <= 0 || math.IsNaN(fx.Rate) || math.IsInf(fx.Rate, 0) ||
		fx.PublicationVersion < 0 || fx.EffectiveAt <= 0 || fx.FetchedAt <= 0 {
		return 0, true, fmt.Errorf("task billing FX history is invalid")
	}
	factor := fx.Rate / billingFXRateDenomination
	if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		return 0, true, fmt.Errorf("task billing FX history is invalid")
	}
	group := bc.SubmitGroup
	if group.PureRatio < 0 || math.IsNaN(group.PureRatio) || math.IsInf(group.PureRatio, 0) {
		return 0, true, fmt.Errorf("task billing submit group is invalid")
	}
	if group.HasSpecialRatio != (group.SpecialRatio != nil) {
		return 0, true, fmt.Errorf("task billing submit group is incomplete")
	}
	if group.SpecialRatio != nil && (*group.SpecialRatio < 0 || math.IsNaN(*group.SpecialRatio) ||
		math.IsInf(*group.SpecialRatio, 0) || *group.SpecialRatio != group.PureRatio) {
		return 0, true, fmt.Errorf("task billing special group is invalid")
	}
	effective := group.PureRatio
	if effective != 0 && factor != 1 {
		effective *= factor
		if effective <= 0 || math.IsNaN(effective) || math.IsInf(effective, 0) {
			return 0, true, fmt.Errorf("task billing effective group is invalid")
		}
	}
	if bc.GroupRatio != effective {
		return 0, true, fmt.Errorf("task billing effective group does not match FX history")
	}
	if snapshot := bc.TieredSnapshot; snapshot != nil &&
		(snapshot.GroupRatio != effective || snapshot.QuotaPerUnit != billingFXQuotaPerUnit) {
		return 0, true, fmt.Errorf("task billing tiered snapshot does not match FX history")
	}
	return factor, true, nil
}

// hasIndependentTaskRefundEntitlement identifies failed task modes whose
// existing refund contract uses the already saved quota without repricing.
func hasIndependentTaskRefundEntitlement(task *model.Task) bool {
	if task == nil || task.Status != model.TaskStatusFailure || task.Quota == 0 {
		return false
	}
	bc := task.PrivateData.BillingContext
	return bc != nil && bc.SettlementMode == "" && (bc.PerCallBilling || bc.TieredSnapshot != nil)
}

func appendTaskLogInfo(task *model.Task, other *model.LogOther) {
	if task == nil || other == nil {
		return
	}
	if task.TaskID != "" {
		other.SetPublic("task_id", task.TaskID)
	}
	if task.PrivateData.Execution != nil {
		AppendTaskPluginAuditInfo(other, task.PrivateData.Execution.TaskPlugin)
	}
	if task.PrivateData.UpstreamTaskID == "" && task.PrivateData.NodeName == "" {
		return
	}
	if task.PrivateData.UpstreamTaskID != "" {
		other.SetRoot("upstream_task_id", task.PrivateData.UpstreamTaskID)
	}
	if task.PrivateData.NodeName != "" {
		other.SetRoot("node_name", task.PrivateData.NodeName)
	}
}

func taskBillingContextPriceData(bc *model.TaskBillingContext) *types.PriceData {
	if bc == nil || len(bc.OtherRatios) == 0 {
		return nil
	}
	priceData := &types.PriceData{}
	if !priceData.ReplaceOtherRatios(bc.OtherRatios) {
		return nil
	}
	return priceData
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，退还资金与令牌额度，并回减用户和渠道用量。
// 返回资金来源是否已成功退还；失败时保留 quota，供显式重试或人工对账。
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) bool {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.SettlementMode != "" {
		return false
	}
	reconciliation := task.PrivateData.Reconciliation
	if reconciliation != nil {
		if reconciliation.RefundPending || reconciliation.ReservationPending || reconciliation.SubmissionPending || !reconciliation.HasUpstreamCost || !reconciliation.UpstreamCostConfirmed || reconciliation.UpstreamCostUSD != 0 {
			reconciliation.Required = true
			return false
		}
	}
	quota := task.Quota
	if quota == 0 {
		return true
	}
	if reconciliation != nil {
		won, err := task.ClaimReconciledTaskRefund(quota)
		if err != nil || !won {
			return false
		}
	}

	// 1. 退还资金来源（钱包或订阅）
	if err := taskAdjustFunding(task, -quota); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("退还资金来源失败 task %s: %s", task.TaskID, err.Error()))
		return false
	}

	// 2. 退还令牌额度
	tokenErr := taskAdjustTokenQuota(ctx, task, -quota)

	// 3. 回减预扣时累计的用户和渠道用量，请求次数保持不变
	model.UpdateUserUsedQuota(task.UserId, -quota)
	model.UpdateChannelUsedQuota(task.ChannelId, -quota)

	// 4. 记录日志
	other := taskBillingOther(task)
	other.SetPublic("task_id", task.TaskID)
	other.SetPublic("reason", reason)
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		Content:   "",
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     quota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
	})

	// 5. 资金退款完成后再清除持久化标记。
	// 回写失败必须显式告警，避免漏掉潜在的重复退款风险。
	task.Quota = 0
	if reconciliation != nil {
		if tokenErr != nil {
			return false
		}
		reconciliation.RefundPending = false
		reconciliation.Required = false
		reconciliation.Finalized = true
		if _, err := task.UpdateWithStatus(task.Status); err != nil {
			logger.LogError(ctx, fmt.Sprintf("task %s refund completed but reconciliation marker is pending", task.TaskID))
			return false
		}
		return true
	}
	if err := task.UpdateQuota(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("退款成功但清除 task quota 失败 task %s: %s", task.TaskID, err.Error()))
	}
	return true
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
// clamps 可选：若计算 actualQuota 时发生额度饱和，将其记入日志 admin_info（仅管理员可见）。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string, clamps ...*common.QuotaClamp) {
	recalculateTaskQuota(ctx, task, actualQuota, reason, nil, clamps...)
}

func recalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string, projection *taskBillingProjection, clamps ...*common.QuotaClamp) {
	if actualQuota < 0 {
		return
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 预扣费准确（%s，%s）",
			task.TaskID, logger.LogQuota(actualQuota), reason))
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("任务 %s 差额结算：delta=%s（实际：%s，预扣：%s，%s）",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason,
	))

	// 调整资金来源
	if err := taskAdjustFunding(task, quotaDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算资金调整失败 task %s: %s", task.TaskID, err.Error()))
		return
	}

	// 调整令牌额度
	taskAdjustTokenQuota(ctx, task, quotaDelta)

	task.Quota = actualQuota
	if err := task.UpdateQuota(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算回写 quota 失败 task %s: %s", task.TaskID, err.Error()))
	}

	// 提交阶段已经累计过一次请求；结算阶段只调整最终用量。
	model.UpdateUserUsedQuota(task.UserId, quotaDelta)
	model.UpdateChannelUsedQuota(task.ChannelId, quotaDelta)

	var logType int
	var logQuota int
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = quotaDelta
	} else {
		logType = model.LogTypeRefund
		logQuota = -quotaDelta
	}
	other := taskBillingOtherWithProjection(task, projection)
	if projection != nil {
		other.SetPublic("model_ratio", projection.modelRatio)
		other.SetPublic("group_ratio", projection.groupRatio)
		if projection.hasSpecialRatio {
			other.SetPublic("user_group_ratio", projection.specialRatio)
		}
		if _, modern, err := taskBillingFXFactor(task.PrivateData.BillingContext); err == nil && modern {
			other.SetAdmin("billing_stage", "completion_tokens")
			applied := map[string]any{"pure_ratio": projection.groupRatio, "effective_ratio": projection.groupRatio * (task.PrivateData.BillingContext.BillingFX.Rate / billingFXRateDenomination)}
			if projection.hasSpecialRatio {
				applied["special_ratio"] = projection.specialRatio
			}
			other.SetAdmin("billing_applied_group", applied)
		}
	} else if _, modern, err := taskBillingFXFactor(task.PrivateData.BillingContext); err == nil && modern {
		switch {
		case reason == "任务用量表达式结算":
			other.SetAdmin("billing_stage", "completion_tiered")
			bc := task.PrivateData.BillingContext
			if bc.TieredSnapshot != nil {
				applied := map[string]any{"pure_ratio": bc.SubmitGroup.PureRatio, "effective_ratio": bc.TieredSnapshot.GroupRatio}
				if bc.SubmitGroup.HasSpecialRatio {
					applied["special_ratio"] = *bc.SubmitGroup.SpecialRatio
				}
				other.SetAdmin("billing_applied_group", applied)
			}
		case reason == "adaptor计费调整":
			other.SetAdmin("billing_stage", "adaptor_quota")
		}
	}
	other.SetPublic("task_id", task.TaskID)
	other.SetPublic("pre_consumed_quota", preConsumedQuota)
	other.SetPublic("actual_quota", actualQuota)
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   logType,
		Content:   reason,
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     logQuota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
		NodeName:  task.PrivateData.NodeName,
	})
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和分组倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。支持钱包和订阅计费来源。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) bool {
	if totalTokens <= 0 {
		return false
	}
	factor, modern, err := taskBillingFXFactor(task.PrivateData.BillingContext)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("任务 %s 计费 FX 历史无效: %s", task.TaskID, err.Error()))
		return false
	}

	modelName := taskModelName(task)

	// 获取模型价格和倍率
	modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
	// 只有配置了倍率(非固定价格)时才按 token 重新计费
	if !hasRatioSetting || modelRatio <= 0 {
		return false
	}

	// 获取用户和组的倍率信息
	group := task.Group
	if group == "" {
		user, err := model.GetUserById(task.UserId, false)
		if err == nil {
			group = user.Group
		}
	}
	if group == "" {
		return false
	}

	groupRatio := ratio_setting.GetGroupRatio(group)
	userGroupRatio, hasUserGroupRatio := ratio_setting.GetGroupGroupRatio(group, group)

	var finalGroupRatio float64
	if hasUserGroupRatio {
		finalGroupRatio = userGroupRatio
	} else {
		finalGroupRatio = groupRatio
	}

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if priceData := taskBillingContextPriceData(task.PrivateData.BillingContext); priceData != nil {
		otherMultiplier = priceData.OtherRatioMultiplier()
	}

	// Modern tasks retain the submit FX factor, while model and group policy is
	// intentionally selected at completion. Decimal factors avoid an unnecessary
	// binary64 overflow before the checked quota conversion. F1 must retain the
	// legacy binary64 multiplication order, including its integer boundaries.
	var actualQuota int
	var clamp *common.QuotaClamp
	if modern && factor != 1 {
		quota := decimal.NewFromInt(int64(totalTokens)).
			Mul(decimal.NewFromFloat(modelRatio)).
			Mul(decimal.NewFromFloat(finalGroupRatio)).
			Mul(decimal.NewFromFloat(otherMultiplier)).
			Mul(decimal.NewFromFloat(factor))
		quotaFloat, _ := quota.Float64()
		actualQuota, clamp = common.QuotaFromFloatChecked(quotaFloat)
	} else {
		actualQuota, clamp = common.QuotaFromFloatChecked(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)
	}

	reason := fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", totalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	// Completion fields are current pricing policy, while billing_submit_group
	// in admin_info remains immutable submission provenance.
	projection := (*taskBillingProjection)(nil)
	if modern {
		projection = &taskBillingProjection{modelRatio: modelRatio, groupRatio: finalGroupRatio, hasSpecialRatio: hasUserGroupRatio, specialRatio: userGroupRatio}
	}
	recalculateTaskQuota(ctx, task, actualQuota, reason, projection, clamp)
	return true
}

// InitTaskSubmission freezes the same billing and execution identity for both
// ordinary submissions and attempts persisted before a paid upstream request.
func InitTaskSubmission(c *gin.Context, platform constant.TaskPlatform, relayInfo *relaycommon.RelayInfo) *model.Task {
	task := model.InitTask(platform, relayInfo)
	task.PrivateData.Execution = TaskExecutionSnapshotFromContext(c)
	task.PrivateData.BillingSource = relayInfo.BillingSource
	task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
	task.PrivateData.TokenId = relayInfo.TokenId
	task.PrivateData.NodeName = common.NodeName
	var specialRatio *float64
	if relayInfo.PriceData.GroupRatioInfo.HasSpecialRatio {
		ratio := relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio
		specialRatio = &ratio
	}
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		ModelPrice:      relayInfo.PriceData.ModelPrice,
		GroupRatio:      relayInfo.PriceData.EffectiveGroupRatio(),
		ModelRatio:      relayInfo.PriceData.ModelRatio,
		OtherRatios:     relayInfo.PriceData.OtherRatios(),
		OriginModelName: relayInfo.OriginModelName,
		PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		TieredSnapshot:  relayInfo.TieredBillingSnapshot,
		BillingFX: &model.TaskBillingFX{
			SchemaVersion:      1,
			Source:             relayInfo.BillingFXSource,
			Rate:               relayInfo.BillingFXRate,
			PublicationVersion: relayInfo.BillingFXPublicationVersion,
			EffectiveAt:        relayInfo.BillingFXEffectiveAt,
			FetchedAt:          relayInfo.BillingFXFetchedAt,
		},
		SubmitGroup: &model.TaskBillingSubmitGroup{
			PureRatio:       relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			HasSpecialRatio: relayInfo.PriceData.GroupRatioInfo.HasSpecialRatio,
			SpecialRatio:    specialRatio,
		},
	}
	task.Quota = relayInfo.PriceData.Quota
	task.Action = relayInfo.Action
	return task
}

// prepareOpenRouterCostSettlement freezes a validated target in the terminal
// status claim, before the winning poller can move any funds.
func prepareOpenRouterCostSettlement(task *model.Task) {
	r, bc := task.PrivateData.Reconciliation, task.PrivateData.BillingContext
	if bc == nil || bc.SettlementMode != model.TaskSettlementOpenRouterCostV1 || r == nil {
		return
	}
	r.Required = true
	r.Finalized = false
	if r.SettlementPending || r.MainApplied || r.ReservationPending || r.SubmissionPending ||
		!r.HasUpstreamCost || !r.UpstreamCostConfirmed || r.UpstreamCostUSD < 0 || math.IsNaN(r.UpstreamCostUSD) || math.IsInf(r.UpstreamCostUSD, 0) {
		return
	}
	if _, modern, err := taskBillingFXFactor(bc); err != nil || !modern || bc.TieredSnapshot == nil {
		return
	}
	snap := bc.TieredSnapshot
	amount := decimal.NewFromFloat(r.UpstreamCostUSD).Mul(decimal.NewFromFloat(snap.GroupRatio)).Mul(decimal.NewFromFloat(snap.QuotaPerUnit))
	target, clamp := common.QuotaFromDecimalChecked(amount)
	if clamp != nil {
		return
	}
	r.TargetQuota = target
	r.SettlementPending = true
}

func settleOpenRouterCost(task *model.Task) error {
	r := task.PrivateData.Reconciliation
	if r == nil || !r.SettlementPending || r.MainApplied || r.Finalized {
		return nil
	}
	tokenKey, err := task.ApplyTaskCostSettlement()
	if err != nil {
		return err
	}
	if err := task.SyncTaskCostSettlementCache(tokenKey); err != nil {
		return err
	}
	delta := r.TargetQuota - r.ReservedQuota
	if delta != 0 {
		other := model.NewLogOther()
		appendTaskLogInfo(task, other)
		other.SetPublic("settlement_mode", model.TaskSettlementOpenRouterCostV1)
		other.SetPublic("pre_consumed_quota", r.ReservedQuota)
		other.SetPublic("actual_quota", r.TargetQuota)
		bc := task.PrivateData.BillingContext
		other.SetAdmin("upstream_cost_usd", r.UpstreamCostUSD)
		other.SetAdmin("billing_fx", bc.BillingFX)
		other.SetAdmin("billing_submit_group", bc.SubmitGroup)
		other.SetAdmin("effective_group_ratio", bc.TieredSnapshot.GroupRatio)
		logType, quota := model.LogTypeConsume, delta
		if delta < 0 {
			logType, quota = model.LogTypeRefund, -delta
		}
		if err := model.RecordTaskBillingLogWithError(model.RecordTaskBillingLogParams{
			UserId: task.UserId, ChannelId: task.ChannelId, ModelName: taskModelName(task), TokenId: task.PrivateData.TokenId,
			LogType: logType, Quota: quota, Group: task.Group, Other: other, NodeName: task.PrivateData.NodeName,
			Content: "OpenRouter confirmed cost settlement",
		}); err != nil {
			return err
		}
	}
	return task.FinalizeTaskCostSettlement()
}
