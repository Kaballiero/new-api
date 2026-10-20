package get_checkin_status

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

type HTTP struct {
	usecase  Usecase
	settings func() *domain.CheckinPolicy
}

func NewHTTP(usecase Usecase, settings func() *domain.CheckinPolicy) HTTP {
	return HTTP{usecase: usecase, settings: settings}
}
func (h HTTP) Handle(c *gin.Context) {
	setting := h.settings()
	if !setting.Enabled {
		common.ApiErrorMsg(c, "签到功能未启用")
		return
	}
	userID := c.GetInt("id")
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))
	stats, err := h.usecase.Execute(userID, month)
	if err != nil {
		c.JSON(http.StatusOK, ErrorResponse{Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, Response{Success: true, Data: &Data{Enabled: setting.Enabled, MinQuota: setting.MinQuota, MaxQuota: setting.MaxQuota, Stats: stats}})
}
