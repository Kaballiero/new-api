package delete_log_files

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	usecase   Usecase
	directory func() string
}

func NewHandler(usecase Usecase, directory func() string) Handler {
	return Handler{usecase: usecase, directory: directory}
}
func (h Handler) Handle(c *gin.Context) {
	mode := c.Query("mode")
	valueStr := c.Query("value")
	if mode != "by_count" && mode != "by_days" {
		common.ApiErrorMsg(c, "invalid mode, must be by_count or by_days")
		return
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil || value < 1 {
		common.ApiErrorMsg(c, "invalid value, must be a positive integer")
		return
	}
	if h.directory() == "" {
		common.ApiErrorMsg(c, "log directory not configured")
		return
	}

	outcome, err := h.usecase.Execute(Input{Mode: mode, Value: value})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	deletedCount, freedBytes, failedFiles := outcome.DeletedCount, outcome.FreedBytes, outcome.FailedFiles

	result := gin.H{
		"deleted_count": deletedCount,
		"freed_bytes":   freedBytes,
		"failed_files":  failedFiles,
	}

	if len(failedFiles) > 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("部分文件删除失败（%d/%d）", len(failedFiles), outcome.SelectedCount),
			"data":    result,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
}
