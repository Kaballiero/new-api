package controller

import (
	"sync"

	"github.com/gin-gonic/gin"

	paymentsapp "github.com/QuantumNous/new-api/internal/app/payments"
)

var paymentsHTTP = sync.OnceValue(paymentsapp.NewHTTP)

func GetTopUpInfo(c *gin.Context) { paymentsHTTP().Info.Handle(c) }

func RequestEpay(c *gin.Context) { paymentsHTTP().CreateEpay.Handle(c) }

func EpayNotify(c *gin.Context) { paymentsHTTP().EpayNotify.Handle(c) }

func RequestAmount(c *gin.Context) { paymentsHTTP().QuoteEpay.Handle(c) }

func GetUserTopUps(c *gin.Context) { paymentsHTTP().ListOwn.Handle(c) }

func GetAllTopUps(c *gin.Context) { paymentsHTTP().ListAll.Handle(c) }

func AdminCompleteTopUp(c *gin.Context) { paymentsHTTP().Complete.Handle(c) }
