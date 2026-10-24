package controller

import "github.com/gin-gonic/gin"

func ConfirmPaymentCompliance(c *gin.Context) { paymentsHTTP().Compliance.Handle(c) }
