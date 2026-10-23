package controller

import (
	"sync"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gin-gonic/gin"

	paymentsapp "github.com/QuantumNous/new-api/internal/app/payments"
)

var paymentsHTTP = sync.OnceValue(paymentsapp.NewHTTP)

func GetTopUpInfo(c *gin.Context) { paymentsHTTP().Info.Handle(c) }

// BRIDGE: subscription_payment_epay checkout/notify/return; remove when those callers use the payment app directly.
func GetEpayClient() *epay.Client { return paymentsapp.EpayClient() }

func RequestEpay(c *gin.Context) { paymentsHTTP().CreateEpay.Handle(c) }

// BRIDGE: subscription_payment_epay notify/return; remove when those callbacks own their admission wiring.
func LockOrder(tradeNo string)   { paymentsapp.LockOrder(tradeNo) }
func UnlockOrder(tradeNo string) { paymentsapp.UnlockOrder(tradeNo) }

func EpayNotify(c *gin.Context) { paymentsHTTP().EpayNotify.Handle(c) }

func RequestAmount(c *gin.Context) { paymentsHTTP().QuoteEpay.Handle(c) }

func GetUserTopUps(c *gin.Context) { paymentsHTTP().ListOwn.Handle(c) }

func GetAllTopUps(c *gin.Context) { paymentsHTTP().ListAll.Handle(c) }

func AdminCompleteTopUp(c *gin.Context) { paymentsHTTP().Complete.Handle(c) }
