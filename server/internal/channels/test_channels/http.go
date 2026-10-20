package test_channels

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	output, err := h.scenario.Execute()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !output.Created {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "已有通道测试任务正在运行或等待中，不能启动本次手动任务", "data": gin.H{"task_id": output.Task.ID, "status": output.Task.Status, "type": output.Task.Type}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"task_id": output.Task.ID, "status": output.Task.Status}})
}
