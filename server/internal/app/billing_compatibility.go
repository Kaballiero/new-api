package app

import (
	"github.com/QuantumNous/new-api/common"
	adapter "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/get_billing_subscription"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/apikeystorage"
	"github.com/QuantumNous/new-api/internal/usage/get_billing_usage"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

type BillingCompatibilityHTTP struct {
	Subscription get_billing_subscription.HTTP
	Usage        get_billing_usage.HTTP
}

func NewBillingCompatibilityHTTP() BillingCompatibilityHTTP {
	accounts := accountstorage.SQL()
	reader := adapter.CompatibilityReader{ReadToken: apikeystorage.Persistence().GetByID, ReadQuota: accounts.GetUserQuota, ReadUsedQuota: accounts.GetUserUsedQuota}
	tokenStat := func() bool { return common.DisplayTokenStatEnabled }
	display := func() domain.QuotaDisplay {
		switch operation_setting.GetQuotaDisplayType() {
		case operation_setting.QuotaDisplayTypeCNY:
			return domain.QuotaDisplay{CNY: true, PerUnit: common.QuotaPerUnit, ExchangeRate: operation_setting.USDExchangeRate}
		case operation_setting.QuotaDisplayTypeTokens:
			return domain.QuotaDisplay{Tokens: true}
		default:
			return domain.QuotaDisplay{PerUnit: common.QuotaPerUnit}
		}
	}
	return BillingCompatibilityHTTP{
		Subscription: get_billing_subscription.NewHTTP(get_billing_subscription.NewUsecase(reader, get_billing_subscription.Rules{TokenStat: tokenStat, Display: display}), func(c *gin.Context, err error) {
			c.JSON(200, gin.H{"error": types.OpenAIError{Message: err.Error(), Type: "upstream_error"}})
		}),
		Usage: get_billing_usage.NewHTTP(get_billing_usage.NewUsecase(reader, get_billing_usage.Rules{TokenStat: tokenStat, Display: display}), func(c *gin.Context, err error) {
			c.JSON(200, gin.H{"error": types.OpenAIError{Message: err.Error(), Type: "new_api_error"}})
		}),
	}
}
