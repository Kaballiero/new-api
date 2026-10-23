package payments

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/adapter/wallet"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

type quotaMath struct{}

func (quotaMath) Strict(quota decimal.Decimal) (int, error) {
	return common.WalletQuotaFromDecimalStrict(quota)
}

func (quotaMath) Maximum() int { return common.MaxWalletQuota }

type completionEffects struct{}

func (completionEffects) Sync(userID, quota int, operation string) {
	wallet.Sync(userID, quota, operation)
}

func (completionEffects) Failed(operation string, err error) {
	common.SysError(operation + " failed: " + paymentLogError(err))
}

func (completionEffects) Receipt(order domain.Order, quota int, ip, provider string) {
	params := map[string]any{"Money": fmt.Sprintf("%.2f", order.Money), "Amount": order.Amount}
	key := "payments.receipt_" + provider
	switch provider {
	case "epay":
		common.SysLog(fmt.Sprintf("易支付充值成功 trade_no=%s user_id=%d quota_to_add=%d money=%.2f", order.TradeNo, order.UserID, quota, order.Money))
		params["Quota"] = logger.LogQuota(quota)
		params["Money"] = fmt.Sprintf("%f", order.Money)
	case "stripe":
		params["Quota"] = logger.FormatQuota(quota)
	case "creem":
		params["Quota"] = quota
	case "waffo":
		params["Quota"] = logger.FormatQuota(quota)
	case "waffo_pancake":
		params["Quota"] = logger.FormatQuota(quota)
		key = "payments.receipt_pancake"
	case "admin":
		params["Quota"] = logger.FormatQuota(quota)
		key = "payments.receipt_manual"
		params["Money"] = fmt.Sprintf("%f", order.Money)
	}
	content := i18n.Translate("zh-CN", key, params)
	if provider == domain.PaymentProviderWaffoPancake {
		model.RecordLog(order.UserID, model.LogTypeTopup, content)
		return
	}
	model.RecordTopupLog(order.UserID, content, ip, order.PaymentMethod, provider)
}

// paymentLogError preserves the former model error text without matching strings.
func paymentLogError(err error) string {
	switch {
	case errors.Is(err, domain.ErrOrderMissing):
		return "充值订单不存在"
	case errors.Is(err, domain.ErrOrderStatus):
		return "充值订单状态错误"
	case errors.Is(err, domain.ErrRecharge):
		return "充值失败，请稍后重试"
	case errors.Is(err, domain.ErrPaymentTradeNo):
		return "未提供支付单号"
	default:
		return err.Error()
	}
}
