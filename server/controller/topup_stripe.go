package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func RequestStripeAmount(c *gin.Context) { paymentsHTTP().QuoteStripe.Handle(c) }
func RequestStripePay(c *gin.Context)    { paymentsHTTP().CreateStripe.Handle(c) }
func StripeWebhook(c *gin.Context)       { paymentsHTTP().StripeWebhook.Handle(c) }
func getStripeCreditedQuota(amount int64, group string) decimal.Decimal {
	topUpGroupRatio := common.GetTopupGroupRatio(group)
	if topUpGroupRatio == 0 {
		topUpGroupRatio = 1
	}
	return decimal.NewFromInt(amount).
		Mul(decimal.NewFromFloat(topUpGroupRatio)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit))
}
