package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/batch"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type BatchBillingSnapshot struct {
	EvaluatedAt time.Time                   `json:"evaluated_at"`
	Client      billingexpr.BillingSnapshot `json:"client"`
	// Preserve the procurement basis of existing batches. New batches freeze
	// the same expression used for client billing, before group/FX multipliers.
	PurchaseExpression string            `json:"purchase_expression"`
	FX                 BillingFXBasis    `json:"fx"`
	PureGroupRatio     float64           `json:"pure_group_ratio"`
	Headers            map[string]string `json:"headers"`
}

func CaptureBatchBilling(modelName, userGroup, usingGroup string, headers map[string]string) (BatchBillingSnapshot, error) {
	expression, ok := billing_setting.GetBatchBillingExpr(modelName)
	if !ok {
		return BatchBillingSnapshot{}, errors.New("an explicit batch billing_expr is required for this model")
	}
	if billingexpr.UsedVars(expression)["header"] {
		return BatchBillingSnapshot{}, errors.New("Batch billing expressions using header() are not supported")
	}
	if err := billing_setting.SmokeTestExpr(expression); err != nil {
		return BatchBillingSnapshot{}, fmt.Errorf("invalid batch billing_expr: %w", err)
	}
	fx, err := CurrentBillingFXBasis()
	if err != nil {
		return BatchBillingSnapshot{}, err
	}
	group, ok := ratio_setting.GetGroupGroupRatio(userGroup, usingGroup)
	if !ok {
		group = ratio_setting.GetGroupRatio(usingGroup)
	}
	effective, err := ApplyBillingFX(group, fx.Factor)
	if err != nil {
		return BatchBillingSnapshot{}, err
	}
	snapshot := BatchBillingSnapshot{EvaluatedAt: time.Now().UTC(), Client: billingexpr.BillingSnapshot{BillingMode: billing_setting.BillingModeTieredExpr, ModelName: modelName, ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), ExprVersion: 1, GroupRatio: effective, QuotaPerUnit: common.QuotaPerUnit}, FX: fx, PureGroupRatio: group, Headers: headers, PurchaseExpression: expression}
	return snapshot, nil
}

func EstimateBatchItem(c *gin.Context, snapshot BatchBillingSnapshot, endpoint string, body []byte, maxOutput int) (int, error) {
	var request dto.Request
	info := &relaycommon.RelayInfo{OriginModelName: snapshot.Client.ModelName, RelayMode: relayconstant.Path2RelayMode(endpoint)}
	switch endpoint {
	case "/v1/chat/completions":
		request, info.RelayFormat = &dto.GeneralOpenAIRequest{}, types.RelayFormatOpenAI
	case "/v1/responses":
		request, info.RelayFormat = &dto.OpenAIResponsesRequest{}, types.RelayFormatOpenAIResponses
	case "/v1/messages":
		request, info.RelayFormat = &dto.ClaudeRequest{}, types.RelayFormatClaude
		info.RelayMode = relayconstant.RelayModeChatCompletions
	case "/v1beta/generateContent":
		request, info.RelayFormat = &dto.GeminiChatRequest{}, types.RelayFormatGemini
		info.RelayMode = relayconstant.RelayModeGemini
	case "/v1/embeddings":
		request, info.RelayFormat = &dto.EmbeddingRequest{}, types.RelayFormatEmbedding
	default:
		return 0, errors.New("unsupported Batch endpoint")
	}
	if err := common.Unmarshal(body, request); err != nil {
		return 0, err
	}
	// Metadata extraction never re-marshals the prepared provider payload.
	itemContext := c.Copy()
	common.SetContextKey(itemContext, constant.ContextKeyOriginalModel, snapshot.Client.ModelName)
	inputTokens, err := CountRequestToken(itemContext, request.GetTokenCountMeta(), info)
	if err != nil {
		return 0, err
	}
	result, err := billingexpr.ComputeTieredQuotaWithRequest(&snapshot.Client, billingexpr.TokenParams{P: float64(inputTokens), Len: float64(inputTokens), C: float64(maxOutput)}, billingexpr.RequestInput{Body: body, Headers: snapshot.Headers, EvaluatedAt: snapshot.EvaluatedAt})
	if err != nil {
		return 0, err
	}
	if result.Clamp != nil {
		return 0, result.Clamp
	}
	return result.ActualQuotaAfterGroup, nil
}

// BatchChannelConfigHash pins the routing/account configuration without copying
// usable credentials into task snapshots. A changed channel requires explicit
// reconciliation instead of polling a different provider workspace.
func BatchChannelConfigHash(channel *model.Channel) (string, error) {
	data, err := common.Marshal(struct {
		Type         int
		Base         string
		Organization *string
		Headers      *string
		Settings     dto.ChannelSettings
	}{channel.Type, channel.GetBaseURL(), channel.OpenAIOrganization, channel.HeaderOverride, channel.GetSetting()})
	return fmt.Sprintf("%x", sha256.Sum256(data)), err
}

type BatchFinancialDetails struct {
	ClientQuota    int             `json:"client_quota"`
	Usage          *dto.Usage      `json:"usage,omitempty"`
	PurchaseUSD    *float64        `json:"purchase_usd,omitempty"`
	PurchaseStatus string          `json:"purchase_status"`
	Other          *model.LogOther `json:"other,omitempty"`
}

func SettleBatchResults(job *model.BatchJob) error {
	var snapshot BatchBillingSnapshot
	if err := common.UnmarshalJsonStr(job.BillingSnapshot, &snapshot); err != nil {
		return err
	}
	// Each row is bounded; do not collect an entire batch's output in memory.
	var cursor int64
	for {
		var item model.BatchItem
		err := model.DB.Where("batch_id = ? AND id > ? AND result_hash <> '' AND billing_status <> ?", job.ID, cursor, "settled").Order("id asc").First(&item).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		cursor = item.ID
		var result batch.Result
		if err := common.UnmarshalJsonStr(item.Result, &result); err != nil {
			return err
		}
		details := BatchFinancialDetails{PurchaseStatus: "reconciliation_required"}
		quota := 0
		if result.Unbilled {
			zero := 0.0
			details.PurchaseUSD = &zero
			details.PurchaseStatus = "unbilled"
		} else {
			usage, err := batchResultUsage(job, result)
			if err != nil {
				if err := model.DB.Model(&item).Update("billing_status", "reconciliation_required").Error; err != nil {
					return err
				}
				continue
			}
			details.Usage = usage
			billingUsage := effectiveBillingUsage(usage)
			params := BuildTieredTokenParams(billingUsage, billingUsage.UsageSemantic == "anthropic", billingexpr.UsedVars(snapshot.Client.ExprString))
			computed, err := billingexpr.ComputeTieredQuotaWithRequest(&snapshot.Client, params, billingexpr.RequestInput{Body: []byte(item.UpstreamBody), Headers: snapshot.Headers, EvaluatedAt: snapshot.EvaluatedAt})
			if err != nil {
				return err
			}
			quota = computed.ActualQuotaAfterGroup
			details.Other = &model.LogOther{}
			InjectTieredBillingInfo(details.Other, &relaycommon.RelayInfo{TieredBillingSnapshot: &snapshot.Client}, &computed)
			attachQuotaSaturationToOther(details.Other, computed.Clamp)
			if computed.Clamp != nil {
				requestID := fmt.Sprintf("%x", sha256.Sum256([]byte(job.PublicID+":"+item.CustomID)))
				ctx := context.WithValue(context.Background(), common.RequestIdKey, requestID)
				logger.LogWarn(ctx, fmt.Sprintf("batch %s item %s quota saturation: %v", job.PublicID, item.CustomID, computed.Clamp))
			}
			details.PurchaseUSD = batchPurchaseUSD(job, result, usage, snapshot, []byte(item.UpstreamBody))
			if details.PurchaseUSD != nil {
				details.PurchaseStatus = "calculated"
			}
		}
		details.ClientQuota = quota
		data, err := common.Marshal(details)
		if err != nil {
			return err
		}
		if err := model.SettleBatchItem(job.ID, item.CustomID, quota, string(data)); err != nil {
			return err
		}
	}
}

func batchResultUsage(job *model.BatchJob, result batch.Result) (*dto.Usage, error) {
	if result.StatusCode < 200 || result.StatusCode >= 300 || len(result.Body) == 0 {
		return nil, errors.New("batch item requires billing reconciliation")
	}
	var envelope map[string]json.RawMessage
	if err := common.Unmarshal(result.Body, &envelope); err != nil {
		return nil, err
	}
	key := "usage"
	if job.Endpoint == "/v1beta/generateContent" {
		key = "usageMetadata"
	}
	raw := envelope[key]
	if common.GetJsonType(raw) != "object" || string(raw) == "{}" {
		return nil, errors.New("batch result has no usage facts")
	}
	var facts map[string]any
	if err := common.Unmarshal(raw, &facts); err != nil {
		return nil, err
	}
	if err := validateBatchUsage(facts); err != nil {
		return nil, err
	}
	required := []string{"prompt_tokens", "completion_tokens"}
	switch job.Endpoint {
	case "/v1/messages":
		required = []string{"input_tokens", "output_tokens"}
	case "/v1/responses":
		required = []string{"input_tokens", "output_tokens"}
	case "/v1/embeddings":
		required = []string{"prompt_tokens"}
	case "/v1beta/generateContent":
		required = []string{"promptTokenCount", "candidatesTokenCount"}
	}
	for _, key := range required {
		if _, ok := facts[key].(float64); !ok {
			return nil, fmt.Errorf("batch result has no %s usage fact", key)
		}
	}

	var usage *dto.Usage
	switch job.Endpoint {
	case "/v1/messages":
		var native dto.ClaudeUsage
		if err := common.Unmarshal(raw, &native); err != nil {
			return nil, err
		}
		usage = relayconvert.UsageFromClaudeAPIUsage(&native)
	case "/v1beta/generateContent":
		var native dto.GeminiUsageMetadata
		if err := common.Unmarshal(raw, &native); err != nil {
			return nil, err
		}
		usage = relayconvert.UsageFromGeminiMetadata(&native, 0)
	case "/v1/responses":
		var native dto.Usage
		if err := common.Unmarshal(raw, &native); err != nil {
			return nil, err
		}
		usage = relayconvert.UsageFromResponsesUsage(&native)
	default:
		var native dto.Usage
		if err := common.Unmarshal(raw, &native); err != nil {
			return nil, err
		}
		usage = relayconvert.UsageFromChatUsage(&native)
	}
	if usage == nil {
		return nil, errors.New("unrecognized batch usage")
	}
	return usage, nil
}

func validateBatchUsage(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, value := range typed {
			if number, ok := value.(float64); ok && strings.Contains(strings.ToLower(key), "token") && math.Trunc(number) != number {
				return errors.New("fractional batch token count")
			}
			if err := validateBatchUsage(value); err != nil {
				return err
			}
		}
	case []any:
		for _, value := range typed {
			if err := validateBatchUsage(value); err != nil {
				return err
			}
		}
	case float64:
		if typed < 0 || typed > common.MaxQuota || math.IsNaN(typed) || math.IsInf(typed, 0) {
			return errors.New("invalid batch usage quantity")
		}
	}
	return nil
}

func batchPurchaseUSD(job *model.BatchJob, result batch.Result, usage *dto.Usage, snapshot BatchBillingSnapshot, body []byte) *float64 {
	if job.ChannelType == constant.ChannelTypeOpenRouter {
		var envelope struct {
			Usage struct {
				Cost *float64 `json:"cost"`
			} `json:"usage"`
		}
		if common.Unmarshal(result.Body, &envelope) == nil && envelope.Usage.Cost != nil && *envelope.Usage.Cost >= 0 && !math.IsNaN(*envelope.Usage.Cost) && !math.IsInf(*envelope.Usage.Cost, 0) {
			return envelope.Usage.Cost
		}
		return nil
	}
	if snapshot.PurchaseExpression == "" {
		return nil
	}
	// Token expressions alone do not prove ancillary tool/grounding charges.
	var request map[string]json.RawMessage
	var response map[string]json.RawMessage
	if common.Unmarshal(body, &request) != nil || common.Unmarshal(result.Body, &response) != nil {
		return nil
	}
	if len(request["tools"]) > 0 || len(response["groundingMetadata"]) > 0 {
		return nil
	}

	billingUsage := effectiveBillingUsage(usage)
	params := BuildTieredTokenParams(billingUsage, billingUsage.UsageSemantic == "anthropic", billingexpr.UsedVars(snapshot.PurchaseExpression))
	value, _, err := billingexpr.RunExprWithRequest(snapshot.PurchaseExpression, params, billingexpr.RequestInput{Body: body, Headers: snapshot.Headers, EvaluatedAt: snapshot.EvaluatedAt})
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	usd := value / 1_000_000
	return &usd
}

// ReconcileBatchPurchase runs independently of client settlement. Provider
// aggregate facts can arrive after client receipts; this never debits a wallet.
func ReconcileBatchPurchase(job *model.BatchJob) error {
	var current model.BatchJob
	if err := model.DB.First(&current, job.ID).Error; err != nil {
		return err
	}
	if current.PurchaseStatus == "settled" {
		return nil
	}
	if job.ChannelType == constant.ChannelTypeOpenRouter && current.UpstreamUsage != "" {
		var facts struct {
			Cost *float64 `json:"cost"`
		}
		if common.UnmarshalJsonStr(current.UpstreamUsage, &facts) == nil && facts.Cost != nil && *facts.Cost >= 0 && !math.IsNaN(*facts.Cost) && !math.IsInf(*facts.Cost, 0) && batch.State(current.Status).Terminal() {
			return model.DB.Model(&current).Where("purchase_status <> ?", "settled").Updates(map[string]any{"purchase_status": "settled", "purchase_usd": decimal.NewFromFloat(*facts.Cost).String(), "purchase_evidence": "provider_aggregate"}).Error
		}
	}
	var cursor int64
	total := decimal.Zero
	count := 0
	for {
		var item model.BatchItem
		err := model.DB.Select("id", "purchase_status", "financial_details").Where("batch_id = ? AND id > ?", job.ID, cursor).Order("id asc").First(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			break
		}
		if err != nil {
			return err
		}
		cursor = item.ID
		if item.PurchaseStatus != "calculated" && item.PurchaseStatus != "unbilled" {
			return nil
		}
		var facts BatchFinancialDetails
		if err := common.UnmarshalJsonStr(item.FinancialDetails, &facts); err != nil {
			return err
		}
		if facts.PurchaseUSD == nil || *facts.PurchaseUSD < 0 || math.IsNaN(*facts.PurchaseUSD) || math.IsInf(*facts.PurchaseUSD, 0) {
			return nil
		}
		total = total.Add(decimal.NewFromFloat(*facts.PurchaseUSD))
		count++
	}
	if count != current.RequestCount {
		return nil
	}
	return model.DB.Model(&current).Where("purchase_status <> ?", "settled").Updates(map[string]any{"purchase_status": "settled", "purchase_usd": total.String(), "purchase_evidence": "item_receipts"}).Error
}
