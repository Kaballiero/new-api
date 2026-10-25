package controller

import (
	"github.com/QuantumNous/new-api/internal/app/batchruntime"
	"github.com/gin-gonic/gin"
	"sync"
)

// BRIDGE: relay and management router registrations call these handlers; remove when routers compose Batch directly.
var batchRuntime = sync.OnceValue(batchruntime.New)

func PrepareBatchRequest(c *gin.Context)  { batchRuntime().Prepare.Handle(c) }
func CreateBatch(c *gin.Context)          { batchRuntime().Create.Handle(c) }
func GetBatch(c *gin.Context)             { batchRuntime().Get.Handle(c) }
func ListBatches(c *gin.Context)          { batchRuntime().List.Handle(c) }
func GetBatchResults(c *gin.Context)      { batchRuntime().Results.Handle(c) }
func CancelBatch(c *gin.Context)          { batchRuntime().Cancel.Handle(c) }
func DeleteBatch(c *gin.Context)          { batchRuntime().Delete.Handle(c) }
func RecoverBatch(c *gin.Context)         { batchRuntime().Recovery.Handle(c) }
func ConfirmBatchPurchase(c *gin.Context) { batchRuntime().Purchase.Handle(c) }
