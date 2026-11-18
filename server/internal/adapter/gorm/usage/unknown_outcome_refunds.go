package usage

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/usage/domain"

	"gorm.io/gorm"
)

// UnknownOutcomeRefundLogs reads the error logs that appendRelayAdmin marks as
// a refund of an unknown OpenRouter charge, and the consume and refund logs of
// a charge at an estimate of the unknown upstream cost.
type UnknownOutcomeRefundLogs struct{ LogDB func() *gorm.DB }

// refundWithoutRetry is the error policy of a hold refunded although the
// upstream charge is unknown.
const refundWithoutRetry = "refund_without_retry"

const (
	textRefundPattern  = `%"openrouter_error_policy":"` + refundWithoutRetry + `"%`
	imageRefundPattern = `%"image_error_policy":"` + refundWithoutRetry + `"%`
	// Each estimate charge leaves one marked row: the consume log of a request
	// priced inline, or the settlement log of a task.
	estimatedChargePattern = `%"upstream_cost_estimated":true%`
)

// UnknownOutcomeRefunds counts the refunds recorded in [from, to) and sums
// their reserved quota, and counts the estimate charges and sums their
// charged quota.
func (l UnknownOutcomeRefundLogs) UnknownOutcomeRefunds(ctx context.Context, from, to int64) (domain.UnknownOutcomeRefunds, error) {
	var refunds domain.UnknownOutcomeRefunds
	// Rows are streamed instead of paged by id: ClickHouse log rows share id 0.
	rows, err := l.LogDB().WithContext(ctx).Model(&usagerecord.Log{}).Select("type", "quota", "other").
		Where("created_at >= ? AND created_at < ?", from, to).
		Where("(type = ? AND (other LIKE ? OR other LIKE ?)) OR (type IN ? AND other LIKE ?)",
			usagerecord.LogTypeError, textRefundPattern, imageRefundPattern,
			[]int{usagerecord.LogTypeConsume, usagerecord.LogTypeRefund}, estimatedChargePattern).
		Rows()
	if err != nil {
		return refunds, err
	}
	defer rows.Close()
	for rows.Next() {
		var logType, quota int
		var stored string
		if err := rows.Scan(&logType, &quota, &stored); err != nil {
			return refunds, err
		}
		var other struct {
			ActualQuota int `json:"actual_quota"`
			AdminInfo   struct {
				ReservedQuota      int      `json:"reserved_quota"`
				ErrorPolicy        string   `json:"openrouter_error_policy"`
				ImageErrorPolicy   string   `json:"image_error_policy"`
				ImageEstimatedCost *float64 `json:"image_estimated_cost_usd"`
				CostSource         string   `json:"openrouter_cost_source"`
			} `json:"admin_info"`
		}
		if err := common.UnmarshalJsonStr(stored, &other); err != nil {
			return refunds, err
		}
		admin := other.AdminInfo
		// A native image row also carries the text policy.
		switch {
		case logType != usagerecord.LogTypeError:
			refunds.EstimatedCharges.Count++
			// A request priced inline is charged its quota; a task settlement
			// logs the change from its reserve and records the charge.
			if admin.CostSource == domain.OpenRouterCostSourceEstimate {
				refunds.EstimatedCharges.Quota += quota
			} else {
				refunds.EstimatedCharges.Quota += other.ActualQuota
			}
		case admin.ImageErrorPolicy == refundWithoutRetry:
			refunds.Image.Count++
			refunds.Image.Quota += admin.ReservedQuota
		case admin.ErrorPolicy == refundWithoutRetry && admin.ImageEstimatedCost == nil:
			refunds.Text.Count++
			refunds.Text.Quota += admin.ReservedQuota
		}
	}
	return refunds, rows.Err()
}
