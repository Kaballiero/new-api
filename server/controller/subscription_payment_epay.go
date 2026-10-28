package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func SubscriptionRequestEpay(c *gin.Context) { httpapi.SubscriptionProviders().Epay.Handle(c) }

func SubscriptionEpayNotify(c *gin.Context) { httpapi.SubscriptionProviders().EpayCallback.Notify(c) }

func SubscriptionEpayReturn(c *gin.Context) { httpapi.SubscriptionProviders().EpayCallback.Return(c) }
