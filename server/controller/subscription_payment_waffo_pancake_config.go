package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func CreateWaffoPancakeSubscriptionProduct(c *gin.Context) {
	httpapi.SubscriptionProviders().CreateProduct.Handle(c)
}

func ListWaffoPancakeSubscriptionProductOptions(c *gin.Context) {
	httpapi.SubscriptionProviders().Products.Handle(c)
}
