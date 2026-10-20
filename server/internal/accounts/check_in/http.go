package check_in

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	usecase   Usecase
	settings  func() *domain.CheckinPolicy
	recordLog func(int, string)
}

func NewHTTP(usecase Usecase, settings func() *domain.CheckinPolicy, recordLog func(int, string)) HTTP {
	return HTTP{usecase: usecase, settings: settings, recordLog: recordLog}
}
func (h HTTP) Handle(c *gin.Context) {
	setting := h.settings()
	if !setting.Enabled {
		common.ApiErrorMsg(c, "签到功能未启用")
		return
	}
	userID := c.GetInt("id")
	record, err := h.usecase.Execute(userID)
	if err != nil {
		c.JSON(http.StatusOK, Response{Message: err.Error()})
		return
	}
	h.recordLog(userID, fmt.Sprintf("用户签到，获得额度 %s", logger.LogQuota(record.QuotaAwarded)))
	c.JSON(http.StatusOK, Response{Success: true, Message: "签到成功", Data: &Data{QuotaAwarded: record.QuotaAwarded, CheckinDate: record.CheckinDate}})
}
