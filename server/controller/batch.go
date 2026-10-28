package controller

import (
	"github.com/QuantumNous/new-api/internal/app/batchruntime"

	"github.com/gin-gonic/gin"
)

func PrepareBatchRequest(c *gin.Context) { batchruntime.Default().Prepare.Handle(c) }

func CreateBatch(c *gin.Context) { batchruntime.Default().Create.Handle(c) }

func GetBatch(c *gin.Context) { batchruntime.Default().Get.Handle(c) }

func ListBatches(c *gin.Context) { batchruntime.Default().List.Handle(c) }

func GetBatchResults(c *gin.Context) { batchruntime.Default().Results.Handle(c) }

func CancelBatch(c *gin.Context) { batchruntime.Default().Cancel.Handle(c) }

func DeleteBatch(c *gin.Context) { batchruntime.Default().Delete.Handle(c) }

func RecoverBatch(c *gin.Context) { batchruntime.Default().Recovery.Handle(c) }

func ConfirmBatchPurchase(c *gin.Context) { batchruntime.Default().Purchase.Handle(c) }
