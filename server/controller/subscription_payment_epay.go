package controller

import "github.com/gin-gonic/gin"

func SubscriptionRequestEpay(c *gin.Context) { subscriptionProviders().Epay.Handle(c) }
func SubscriptionEpayNotify(c *gin.Context)  { subscriptionProviders().EpayCallback.Notify(c) }
func SubscriptionEpayReturn(c *gin.Context)  { subscriptionProviders().EpayCallback.Return(c) }
