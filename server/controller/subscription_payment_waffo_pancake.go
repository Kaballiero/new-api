package controller

import "github.com/gin-gonic/gin"

func SubscriptionRequestWaffoPancakePay(c *gin.Context) { subscriptionProviders().Pancake.Handle(c) }
