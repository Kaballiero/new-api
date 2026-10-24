package controller

import "github.com/gin-gonic/gin"

func CreateWaffoPancakeSubscriptionProduct(c *gin.Context) {
	subscriptionProviders().CreateProduct.Handle(c)
}
func ListWaffoPancakeSubscriptionProductOptions(c *gin.Context) {
	subscriptionProviders().Products.Handle(c)
}
