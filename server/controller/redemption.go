package controller

import (
	"sync"

	"github.com/gin-gonic/gin"

	redemptionapp "github.com/QuantumNous/new-api/internal/app/redemption"
)

// BRIDGE: existing redemption router entrypoints; dependencies are assembled once with late database readers.
var redemptionHTTP = sync.OnceValue(redemptionapp.NewHTTP)

func GetAllRedemptions(c *gin.Context) { redemptionHTTP().List.Handle(c) }

func SearchRedemptions(c *gin.Context) { redemptionHTTP().Search.Handle(c) }

func GetRedemption(c *gin.Context) { redemptionHTTP().Get.Handle(c) }

func AddRedemption(c *gin.Context) { redemptionHTTP().Create.Handle(c) }

func DeleteRedemption(c *gin.Context) { redemptionHTTP().Delete.Handle(c) }

func UpdateRedemption(c *gin.Context) { redemptionHTTP().Update.Handle(c) }

func DeleteInvalidRedemption(c *gin.Context) { redemptionHTTP().DeleteInvalid.Handle(c) }

func DeleteRedemptionBatch(c *gin.Context) { redemptionHTTP().DeleteBatch.Handle(c) }
