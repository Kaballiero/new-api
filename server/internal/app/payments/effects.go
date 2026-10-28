package payments

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	usagestorage "github.com/QuantumNous/new-api/internal/app/usagestorage"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

type quotaMath struct{}

func (quotaMath) Strict(quota decimal.Decimal) (int, error) {
	return common.WalletQuotaFromDecimalStrict(quota)
}

func (quotaMath) Maximum() int { return common.MaxWalletQuota }

type completionEffects struct{}

func (completionEffects) Sync(userID, quota int, operation string) {
	accountstorage.Cache().SyncCreditUserQuotaCache(userID, quota, operation)
}

func (completionEffects) Failed(operation string, err error) {
	common.SysError(operation + " failed: " + paymentLogError(err))
}

func (completionEffects) Receipt(order domain.Order, quota int, ip, provider string) {
	params := map[string]any{"Money": fmt.Sprintf("%.2f", order.Money), "Amount": order.Amount}
	lang := paymentReceiptLanguage(order.UserID)
	key := "payments.receipt_" + provider
	switch provider {
	case "epay":
		common.SysLog(fmt.Sprintf("Epay topup succeeded trade_no=%s user_id=%d quota_to_add=%d money=%.2f", order.TradeNo, order.UserID, quota, order.Money))
		params["Quota"] = paymentQuotaText(quota, lang, true)
		params["Money"] = fmt.Sprintf("%f", order.Money)
	case "stripe":
		params["Quota"] = paymentQuotaText(quota, lang, false)
	case "creem":
		params["Quota"] = quota
	case "waffo":
		params["Quota"] = paymentQuotaText(quota, lang, false)
	case "waffo_pancake":
		params["Quota"] = paymentQuotaText(quota, lang, false)
		key = "payments.receipt_pancake"
	case "admin":
		params["Quota"] = paymentQuotaText(quota, lang, false)
		key = "payments.receipt_manual"
		params["Money"] = fmt.Sprintf("%f", order.Money)
	}
	content := i18n.Translate(lang, key, params)
	if provider == domain.PaymentProviderWaffoPancake {
		usagestorage.Writer().RecordLog(order.UserID, usagerecord.LogTypeTopup, content)
		return
	}
	usagestorage.Writer().RecordTopupLog(order.UserID, content, ip, order.PaymentMethod, provider)
}

// paymentLogError preserves the former model error text without matching strings.
func paymentLogError(err error) string {
	switch {
	case errors.Is(err, domain.ErrOrderMissing):
		return "topup order not found"
	case errors.Is(err, domain.ErrOrderStatus):
		return "invalid topup order status"
	case errors.Is(err, domain.ErrRecharge):
		return "topup failed, please try again later"
	case errors.Is(err, domain.ErrPaymentTradeNo):
		return "payment order number is required"
	default:
		return err.Error()
	}
}

// Receipt language belongs to the beneficiary; both Chinese variants retain the legacy bytes.
func paymentReceiptLanguage(userID int) string {
	lang := i18n.ParseAcceptLanguage(accountstorage.Cache().GetUserLanguage(userID))
	switch lang {
	case i18n.LangZhCN, i18n.LangZhTW:
		return i18n.LangZhCN
	case i18n.LangRu:
		return i18n.LangRu
	default:
		return i18n.DefaultLang
	}
}

// paymentQuotaText preserves the shared formatter and custom currency symbols.
func paymentQuotaText(quota int, lang string, suffix bool) string {
	chinese := lang == i18n.LangZhCN || lang == i18n.LangZhTW
	if chinese {
		if suffix {
			return logger.LogQuota(quota)
		}
		return logger.FormatQuota(quota)
	}
	if suffix && lang == i18n.DefaultLang {
		return logger.LogQuotaEnglish(quota)
	}
	amount := logger.FormatQuota(quota)
	if operation_setting.GetQuotaDisplayType() != operation_setting.QuotaDisplayTypeCustom {
		// The legacy amount-only formatter uses the fullwidth dollar for Chinese output.
		const asciiDollar = "$"
		amount = strings.ReplaceAll(amount, "\uff04", asciiDollar)
	}
	if !suffix {
		return amount
	}
	key := i18n.MsgPaymentsQuotaAmount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		key = i18n.MsgPaymentsQuotaTokens
	}
	return i18n.Translate(lang, key, map[string]any{"Amount": amount})
}
