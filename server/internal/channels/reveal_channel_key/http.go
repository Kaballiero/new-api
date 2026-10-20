package reveal_channel_key

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store         Store
	audit         func(int, string)
	securityError func(*gin.Context, error)
}

func NewHTTP(store Store, audit func(int, string), securityError func(*gin.Context, error)) HTTP {
	return HTTP{store: store, audit: audit, securityError: securityError}
}
func (h HTTP) Handle(c *gin.Context) {
	scenario := NewUsecase(h.store, h.audit)
	_ = scenario
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgChannelIdFormatError)
		return
	}
	output, failure := scenario.Execute(Input{ID: id})
	if failure != nil {
		if failure.Kind == "missing" {
			common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "channel_not_found", i18n.MsgChannelNotExists)
		} else {
			h.securityError(c, failure.Cause)
		}
		return
	}
	common.ApiSuccess(c, output)
}
