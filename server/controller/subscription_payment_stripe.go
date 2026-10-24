package controller

import "github.com/gin-gonic/gin"

func SubscriptionRequestStripePay(c *gin.Context) { subscriptionProviders().Stripe.Handle(c) }
