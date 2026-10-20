package delete_channel

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store   Store
	cache   ProxyCache
	refresh func()
	audit   func(int, string)
}

func NewHTTP(store Store, cache ProxyCache, refresh func(), audit func(int, string)) HTTP {
	return HTTP{store: store, cache: cache, refresh: refresh, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	scenario := NewUsecase(h.store, h.cache, h.refresh, h.audit)
	_ = scenario
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgChannelIdFormatError)
		return
	}
	failure := scenario.Execute(Input{ID: id})
	if failure != nil {
		if failure.Kind == "missing" {
			common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "channel_not_found", i18n.MsgChannelNotExists)
		} else {
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", failure.Cause)
		}
		return
	}
	common.ApiSuccess(c, nil)
}
