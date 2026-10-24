package controller

import "github.com/gin-gonic/gin"

func SubscriptionRequestCreemPay(c *gin.Context) { subscriptionProviders().Creem.Handle(c) }
