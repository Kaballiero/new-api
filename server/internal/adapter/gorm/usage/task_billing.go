package usage

import (
	"encoding/base64"
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	usagedomain "github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

var usageMetadata = Metadata{}

type TaskBilling struct{}

func Plugin(p *model.TaskPluginSnapshot) *usagedomain.Plugin {
	if p == nil {
		return nil
	}
	out := &usagedomain.Plugin{Key: p.Key, Name: p.Name, Version: p.Version, APIVersion: p.APIVersion, Generation: p.Generation}
	if p.Author != nil {
		out.AuthorName, out.AuthorURL = p.Author.Name, p.Author.URL
	}
	return out
}
func (TaskBilling) UserUsed(userID, delta int)       { model.UpdateUserUsedQuota(userID, delta) }
func (TaskBilling) ChannelUsed(channelID, delta int) { model.UpdateChannelUsedQuota(channelID, delta) }
func (TaskBilling) Refund(task *domain.Task, quota int, reason string) {
	t := record.FromDomain(task)
	other := taskBillingOther(t)
	other.SetPublic("task_id", t.TaskID)
	other.SetPublic("reason", reason)
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{UserId: t.UserId, LogType: model.LogTypeRefund, Content: "", ChannelId: t.ChannelId, ModelName: domain.TaskModelName(task), Quota: quota, TokenId: t.PrivateData.TokenId, Group: t.Group, Other: other})
}
func (TaskBilling) Adjustment(task *domain.Task, actualQuota, preConsumedQuota int, reason string, projection *domain.BillingProjection, clamps []*billingdomain.Clamp) {
	t := record.FromDomain(task)
	quotaDelta := actualQuota - preConsumedQuota
	var logType int
	var logQuota int
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = quotaDelta
	} else {
		logType = model.LogTypeRefund
		logQuota = -quotaDelta
	}
	other := taskBillingOtherWithProjection(t, projection)
	if projection != nil {
		other.SetPublic("model_ratio", projection.ModelRatio)
		other.SetPublic("group_ratio", projection.GroupRatio)
		if projection.HasSpecialRatio {
			other.SetPublic("user_group_ratio", projection.SpecialRatio)
		}
		if _, modern, err := domain.TaskBillingFXFactor(task.PrivateData.BillingContext, service.ModelCostFXSource); err == nil && modern {
			other.SetAdmin("billing_stage", "completion_tokens")
			applied := map[string]any{"pure_ratio": projection.GroupRatio, "effective_ratio": projection.GroupRatio * (t.PrivateData.BillingContext.BillingFX.Rate / billingdomain.FXRateDenomination)}
			if projection.HasSpecialRatio {
				applied["special_ratio"] = projection.SpecialRatio
			}
			other.SetAdmin("billing_applied_group", applied)
		}
	} else if _, modern, err := domain.TaskBillingFXFactor(task.PrivateData.BillingContext, service.ModelCostFXSource); err == nil && modern {
		switch {
		case reason == "任务用量表达式结算":
			other.SetAdmin("billing_stage", "completion_tiered")
			bc := t.PrivateData.BillingContext
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
	other.SetPublic("task_id", t.TaskID)
	other.SetPublic("pre_consumed_quota", preConsumedQuota)
	other.SetPublic("actual_quota", actualQuota)
	for _, clamp := range clamps {
		usageMetadata.Clamp(other, clamp)
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    t.UserId,
		LogType:   logType,
		Content:   reason,
		ChannelId: t.ChannelId,
		ModelName: domain.TaskModelName(task),
		Quota:     logQuota,
		TokenId:   t.PrivateData.TokenId,
		Group:     t.Group,
		Other:     other,
		NodeName:  t.PrivateData.NodeName,
	})
}
func taskBillingOther(task *model.Task) *model.LogOther {
	return taskBillingOtherWithProjection(task, nil)
}

func taskBillingOtherWithProjection(task *model.Task, projection *domain.BillingProjection) *model.LogOther {
	other := new(model.LogOther)
	if bc := task.PrivateData.BillingContext; bc != nil {
		other.SetPublic("model_price", bc.ModelPrice)
		if bc.ModelRatio > 0 {
			other.SetPublic("model_ratio", bc.ModelRatio)
		}
		if _, modern, err := domain.TaskBillingFXFactor(record.ToDomainTaskBillingContext(bc), service.ModelCostFXSource); err == nil && modern {
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
	AppendTaskIdentity(other, task)
	return other
}

func AppendTaskIdentity(other *model.LogOther, task *model.Task) {
	if task == nil || other == nil {
		return
	}
	var plugin *usagedomain.Plugin
	if task.PrivateData.Execution != nil {
		plugin = Plugin(task.PrivateData.Execution.TaskPlugin)
	}
	usageMetadata.TaskIdentity(other, usagedomain.TaskIdentity{TaskID: task.TaskID, UpstreamTaskID: task.PrivateData.UpstreamTaskID, NodeName: task.PrivateData.NodeName, Plugin: plugin})
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

func (TaskBilling) Durable(value *domain.Task, delta int) error {
	task := record.FromDomain(value)
	r := task.PrivateData.Reconciliation
	other := new(model.LogOther)
	AppendTaskIdentity(other, task)
	other.SetPublic("settlement_mode", task.PrivateData.BillingContext.SettlementMode)
	other.SetPublic("pre_consumed_quota", r.ReservedQuota)
	other.SetPublic("actual_quota", r.TargetQuota)
	bc := task.PrivateData.BillingContext
	other.SetAdmin("upstream_cost_usd", r.UpstreamCostUSD)
	if r.QuotaSaturation != "" {
		other.SetAdmin("quota_saturation", json.RawMessage(r.QuotaSaturation))
	}
	if r.UpstreamIsBYOK != nil {
		other.SetAdmin("upstream_is_byok", *r.UpstreamIsBYOK)
	}
	if len(r.UpstreamCostDetails) > 0 {
		other.SetAdmin("upstream_cost_details", json.RawMessage(r.UpstreamCostDetails))
	}
	other.SetAdmin("billing_fx", bc.BillingFX)
	other.SetAdmin("billing_submit_group", bc.SubmitGroup)
	if bc.OpenRouterCost != nil {
		other.SetAdmin("openrouter_cost_basis", bc.OpenRouterCost)
		other.SetAdmin("openrouter_order_facts", bc.OpenRouterOrderFacts)
	} else if bc.TieredSnapshot != nil {
		other.SetAdmin("effective_group_ratio", bc.TieredSnapshot.GroupRatio)
	}
	logType, quota := model.LogTypeConsume, delta
	if delta < 0 {
		logType, quota = model.LogTypeRefund, -delta
	}
	if err := model.RecordTaskBillingLogWithError(model.RecordTaskBillingLogParams{
		UserId: task.UserId, ChannelId: task.ChannelId, ModelName: domain.TaskModelName(value), TokenId: task.PrivateData.TokenId,
		LogType: logType, Quota: quota, Group: task.Group, Other: other, NodeName: task.PrivateData.NodeName,
		Content: "OpenRouter confirmed cost settlement",
	}); err != nil {
		return err
	}
	return nil
}

func (TaskBilling) RefundMidjourney(task *domain.Midjourney, quota int, reason string) {
	other := new(model.LogOther)
	other.SetPublic("task_id", task.MjId)
	other.SetPublic("reason", reason)
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{UserId: task.UserId, LogType: model.LogTypeRefund, Content: "", ChannelId: task.GetBillingChannelId(), ModelName: domain.MidjourneyModelName(task.Action), Quota: quota, TokenId: task.TokenId, Other: other})
}
