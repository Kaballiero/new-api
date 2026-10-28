package enqueue_log_cleanup

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/internal/system/dto"
	"github.com/QuantumNous/new-api/internal/system/transport"

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
			"message": transport.Message(c, i18n.MsgSystemTargetRequired),
		})
		return
	}

	task, err := h.usecase.Execute(targetTimestamp)
	if err != nil {
		if errors.Is(err, domain.ErrTargetTimestamp) {
			common.ApiErrorMsg(c, transport.Message(c, i18n.MsgSystemTargetRequired))
			return
		}
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dto.TaskResponse(task, h.decode),
	})
}
