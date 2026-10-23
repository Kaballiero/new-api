package controller

import "github.com/gin-gonic/gin"

func RequestWaffoPancakeAmount(c *gin.Context) { paymentsHTTP().QuotePancake.Handle(c) }
func RequestWaffoPancakePay(c *gin.Context)    { paymentsHTTP().CreatePancake.Handle(c) }
func WaffoPancakeWebhook(c *gin.Context)       { paymentsHTTP().PancakeWebhook.Handle(c) }
func ListWaffoPancakeCatalog(c *gin.Context)   { paymentsHTTP().Catalog.Handle(c) }
func CreateWaffoPancakePair(c *gin.Context)    { paymentsHTTP().Pair.Handle(c) }
func SaveWaffoPancake(c *gin.Context)          { paymentsHTTP().SavePancake.Handle(c) }
