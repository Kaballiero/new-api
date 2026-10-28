package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetTopUpInfo(c *gin.Context) { httpapi.Payments().Info.Handle(c) }

func RequestEpay(c *gin.Context) { httpapi.Payments().CreateEpay.Handle(c) }

func EpayNotify(c *gin.Context) { httpapi.Payments().EpayNotify.Handle(c) }

func RequestAmount(c *gin.Context) { httpapi.Payments().QuoteEpay.Handle(c) }

func GetUserTopUps(c *gin.Context) { httpapi.Payments().ListOwn.Handle(c) }

func GetAllTopUps(c *gin.Context) { httpapi.Payments().ListAll.Handle(c) }

func AdminCompleteTopUp(c *gin.Context) { httpapi.Payments().Complete.Handle(c) }
