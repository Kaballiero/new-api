package taskrecord

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	counters "github.com/QuantumNous/new-api/internal/adapter/gorm/counters"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	usageadapter "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/taskbillingruntime"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	usagestorage "github.com/QuantumNous/new-api/internal/app/usagestorage"
	"github.com/QuantumNous/new-api/internal/tasks/record_task_submission"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/taskview"
	usagegateway "github.com/QuantumNous/new-api/relay/usage"

	"github.com/gin-gonic/gin"
)

func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task) error {
	tokenName := c.GetString("token_name")
	texts := taskbillingruntime.Text()
	logContent := texts.Phrase(info.UserId, i18n.MsgTasksAction, map[string]any{"Action": info.Action})
	// Per-call tasks retain their existing submission content.
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = texts.Phrase(info.UserId, i18n.MsgTasksPerCall, map[string]any{"Content": logContent})
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
			logContent = texts.Phrase(info.UserId, i18n.MsgTasksParameters, map[string]any{"Content": logContent, "Parameters": strings.Join(contents, ", ")})
		}
	}
	other := new(usagerecord.LogOther)
	other.SetPublic("is_task", true)
	usagegateway.AppendTaskBillingFX(other, info, info.PriceData, "submit")
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
	usageadapter.AppendTaskIdentity(other, task)
	usagegateway.AppendTaskQuotaSaturation(c, info, other)
	var logErr error
	if bc := task.PrivateData.BillingContext; bc != nil && (bc.SettlementMode == record.TaskSettlementOpenRouterCostV1 || bc.SettlementMode == record.TaskSettlementOpenRouterCostV2) {
		other.SetPublic("settlement_mode", bc.SettlementMode)
		other.SetPublic("reserved_quota", task.Quota)
		if bc.OpenRouterCost != nil {
			other.SetAdmin("openrouter_cost_basis", bc.OpenRouterCost)
			other.SetAdmin("openrouter_order_facts", bc.OpenRouterOrderFacts)
		}
		logErr = usagestorage.Writer().RecordTaskBillingLogWithError(usageadapter.RecordTaskBillingLogParams{
			UserId: info.UserId, LogType: usagerecord.LogTypeConsume, ChannelId: info.ChannelId, ModelName: info.OriginModelName,
			TokenId: info.TokenId, Quota: info.PriceData.Quota, Content: logContent, Group: info.UsingGroup, Other: other, NodeName: task.PrivateData.NodeName,
		})
	} else {
		usagestorage.Writer().RecordConsumeLog(c, info.UserId, usageadapter.RecordConsumeLogParams{
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

	accountstorage.SQL().UpdateUserUsedQuotaAndRequestCount(info.UserId, info.PriceData.Quota)
	counters.UpdateChannelUsedQuota(model.DB, info.ChannelId, info.PriceData.Quota)
	return logErr
}

// InitTaskSubmission freezes billing and execution identity after the upstream
// submission succeeds, before the standard task persistence barrier.
func InitTaskSubmission(c *gin.Context, platform constant.TaskPlatform, relayInfo *relaycommon.RelayInfo) *model.Task {
	task := InitTask(platform, relayInfo)
	var specialRatio *float64
	if relayInfo.PriceData.GroupRatioInfo.HasSpecialRatio {
		ratio := relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio
		specialRatio = &ratio
	}
	task.PrivateData.BillingContext = &record.TaskBillingContext{
		ModelPrice:      relayInfo.PriceData.ModelPrice,
		GroupRatio:      relayInfo.PriceData.EffectiveGroupRatio(),
		ModelRatio:      relayInfo.PriceData.ModelRatio,
		OtherRatios:     relayInfo.PriceData.OtherRatios(),
		OriginModelName: relayInfo.OriginModelName,
		PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		TieredSnapshot:  relayInfo.TieredBillingSnapshot,
		BillingFX: &record.TaskBillingFX{
			SchemaVersion:      1,
			Source:             relayInfo.BillingFXSource,
			Rate:               relayInfo.BillingFXRate,
			PublicationVersion: relayInfo.BillingFXPublicationVersion,
			EffectiveAt:        relayInfo.BillingFXEffectiveAt,
			FetchedAt:          relayInfo.BillingFXFetchedAt,
		},
		SubmitGroup: &record.TaskBillingSubmitGroup{
			PureRatio:       relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			HasSpecialRatio: relayInfo.PriceData.GroupRatioInfo.HasSpecialRatio,
			SpecialRatio:    specialRatio,
		},
	}

	input := record_task_submission.Input{
		Task: record.ToDomain(task), Execution: record.ToDomainTaskExecutionSnapshot(taskview.TaskExecutionSnapshotFromContext(c)),
		Source: relayInfo.BillingSource, SubscriptionID: relayInfo.SubscriptionId, TokenID: relayInfo.TokenId, NodeName: common.NodeName,
		Billing: record.ToDomainTaskBillingContext(task.PrivateData.BillingContext), Quota: relayInfo.PriceData.Quota, Action: relayInfo.Action,
	}
	if relayInfo.ChannelMeta != nil {
		input.AccountKey = relayInfo.ChannelMeta.ApiKey
	}
	if relayInfo.OpenRouterCost != nil {
		input.CostBasis = record.ToDomainBasis(&relayInfo.OpenRouterCost.Basis)
		if c != nil {
			if value, ok := c.Get("openrouter_video_order_facts"); ok {
				input.OrderFacts, _ = value.(map[string]any)
			}
		}
	}
	frozen, _ := taskSubmission.Execute(context.Background(), input)
	return record.FromDomain(frozen)
}

var taskSubmission = tasksapp.NewSubmission()
