package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func GetAllRedemptions(c *gin.Context) { httpapi.Redemption().List.Handle(c) }

func SearchRedemptions(c *gin.Context) { httpapi.Redemption().Search.Handle(c) }

func GetRedemption(c *gin.Context) { httpapi.Redemption().Get.Handle(c) }

func AddRedemption(c *gin.Context) { httpapi.Redemption().Create.Handle(c) }

func DeleteRedemption(c *gin.Context) { httpapi.Redemption().Delete.Handle(c) }

func UpdateRedemption(c *gin.Context) { httpapi.Redemption().Update.Handle(c) }

func DeleteInvalidRedemption(c *gin.Context) { httpapi.Redemption().DeleteInvalid.Handle(c) }

func DeleteRedemptionBatch(c *gin.Context) { httpapi.Redemption().DeleteBatch.Handle(c) }
