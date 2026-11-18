package usage

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/usage/domain"

	"gorm.io/gorm"
)

// UnknownOutcomeRefundLogs reads the error logs that appendRelayAdmin marks as
// a refund of an unknown OpenRouter charge.
type UnknownOutcomeRefundLogs struct{ LogDB func() *gorm.DB }

// refundWithoutRetry is the error policy of a hold refunded although the
// upstream charge is unknown.
const refundWithoutRetry = "refund_without_retry"

const (
	textRefundPattern  = `%"openrouter_error_policy":"` + refundWithoutRetry + `"%`
	imageRefundPattern = `%"image_error_policy":"` + refundWithoutRetry + `"%`
)

// UnknownOutcomeRefunds counts the refunds recorded in [from, to) and sums
// their reserved quota.
func (l UnknownOutcomeRefundLogs) UnknownOutcomeRefunds(ctx context.Context, from, to int64) (domain.UnknownOutcomeRefunds, error) {
	var refunds domain.UnknownOutcomeRefunds
	var rows []usagerecord.Log
	err := l.LogDB().WithContext(ctx).Model(&usagerecord.Log{}).Select("id", "other").
		Where("type = ? AND created_at >= ? AND created_at < ?", usagerecord.LogTypeError, from, to).
		Where("other LIKE ? OR other LIKE ?", textRefundPattern, imageRefundPattern).
		FindInBatches(&rows, 500, func(*gorm.DB, int) error {
			for _, row := range rows {
				var other struct {
					AdminInfo struct {
						ReservedQuota      int      `json:"reserved_quota"`
						ErrorPolicy        string   `json:"openrouter_error_policy"`
						ImageErrorPolicy   string   `json:"image_error_policy"`
						ImageEstimatedCost *float64 `json:"image_estimated_cost_usd"`
					} `json:"admin_info"`
				}
				if err := common.UnmarshalJsonStr(row.Other, &other); err != nil {
					return err
				}
				admin := other.AdminInfo
				// A native image row also carries the text policy.
				switch {
				case admin.ImageErrorPolicy == refundWithoutRetry:
					refunds.Image.Count++
					refunds.Image.Quota += admin.ReservedQuota
				case admin.ErrorPolicy == refundWithoutRetry && admin.ImageEstimatedCost == nil:
					refunds.Text.Count++
					refunds.Text.Quota += admin.ReservedQuota
				}
			}
			return nil
		}).Error
	return refunds, err
}
