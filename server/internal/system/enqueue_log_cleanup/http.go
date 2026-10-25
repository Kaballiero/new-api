package enqueue_log_cleanup

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/system/dto"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	usecase Usecase
	decode  func(string) any
}

func NewHandler(usecase Usecase, decode func(string) any) Handler {
	return Handler{usecase: usecase, decode: decode}
}
func (h Handler) Handle(c *gin.Context) {
	targetTimestamp, _ := strconv.ParseInt(c.Query("target_timestamp"), 10, 64)
	if targetTimestamp == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "target timestamp is required",
		})
		return
	}

	task, err := h.usecase.Execute(targetTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dto.TaskResponse(task, h.decode),
	})
}
