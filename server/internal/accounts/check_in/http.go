package check_in

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	usecase   Usecase
	settings  func() *domain.CheckinPolicy
	recordLog func(userID, quota int, lang string)
}

// NewHTTP builds the check-in handler; recordLog writes the user log entry of
// a successful check-in with the awarded quota in the request language.
func NewHTTP(usecase Usecase, settings func() *domain.CheckinPolicy, recordLog func(userID, quota int, lang string)) HTTP {
	return HTTP{usecase: usecase, settings: settings, recordLog: recordLog}
}
func (h HTTP) Handle(c *gin.Context) {
	setting := h.settings()
	if !setting.Enabled {
		transport.WriteError(c, domain.ErrCheckinDisabled)
		return
	}
	userID := c.GetInt("id")
	record, err := h.usecase.Execute(userID)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	h.recordLog(userID, record.QuotaAwarded, i18n.GetLangFromContext(c))
	c.JSON(http.StatusOK, Response{Success: true, Message: common.TranslateMessage(c, i18n.MsgCheckinSuccess), Data: &Data{QuotaAwarded: record.QuotaAwarded, CheckinDate: record.CheckinDate}})
}
