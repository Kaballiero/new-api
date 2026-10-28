package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func SubscriptionRequestCreemPay(c *gin.Context) { httpapi.SubscriptionProviders().Creem.Handle(c) }
