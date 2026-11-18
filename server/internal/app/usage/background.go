package usage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	settingsstore "github.com/QuantumNous/new-api/internal/adapter/gorm/settings"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	"github.com/QuantumNous/new-api/internal/app/channelruntime"
	"github.com/QuantumNous/new-api/internal/app/notifications"
	"github.com/QuantumNous/new-api/internal/app/usagestorage"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/report_unknown_outcome_refunds"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

func StartQuotaData() {
	go func() {
		for {
			if common.DataExportEnabled {
				common.SysLog("updating dashboard usage data")
				FlushQuotaData()
			}
			time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
		}
	}()
}

func FlushQuotaData() {
	usagestorage.Store().SaveQuotaDataCache()
}

// unknownOutcomeRefundReportTick is how often the master checks whether the
// daily summary of unknown outcome refunds is due.
const unknownOutcomeRefundReportTick = 5 * time.Minute

var unknownOutcomeRefundReportOnce sync.Once

// StartUnknownOutcomeRefundReport sends, on the master node, the daily
// summary of OpenRouter holds refunded although the upstream charge was unknown.
func StartUnknownOutcomeRefundReport() {
	unknownOutcomeRefundReportOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		report := report_unknown_outcome_refunds.NewUsecase(
			storage.UnknownOutcomeRefundLogs{LogDB: func() *gorm.DB { return model.LOG_DB }},
			settingsstore.NewStore(func() *gorm.DB { return model.DB }),
			unknownOutcomeRefundAlerts{}, time.Now,
		)
		gopool.Go(func() {
			ticker := time.NewTicker(unknownOutcomeRefundReportTick)
			defer ticker.Stop()
			for range ticker.C {
				if err := report.Execute(context.Background()); err != nil {
					logger.LogWarn(context.Background(), fmt.Sprintf("unknown outcome refund report failed: %v", err))
				}
			}
		})
	})
}

// unknownOutcomeRefundAlerts sends the summary to the Telegram chat in the
// root user's language, like the manual hold alerts.
type unknownOutcomeRefundAlerts struct{}

func (unknownOutcomeRefundAlerts) UnknownOutcomeRefunds(day string, refunds domain.UnknownOutcomeRefunds) error {
	lang := channelruntime.RootUserLanguage()
	content := i18n.Translate(lang, i18n.MsgUsageUnknownOutcomeRefunds, map[string]any{
		"Day":         day,
		"TextCount":   refunds.Text.Count,
		"TextAmount":  fmt.Sprintf("$%.6f", float64(refunds.Text.Quota)/common.QuotaPerUnit),
		"ImageCount":  refunds.Image.Count,
		"ImageAmount": fmt.Sprintf("$%.6f", float64(refunds.Image.Quota)/common.QuotaPerUnit),
	})
	return notifications.TelegramChat(i18n.Translate(lang, i18n.MsgUsageUnknownOutcomeRefundsSubject), content)
}
