package reveal_channel_key

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	usecase func(*gin.Context) Usecase
	// securityError answers storage failures like other security operations.
	securityError func(*gin.Context, error)
}

func NewHTTP(usecase func(*gin.Context) Usecase, securityError func(*gin.Context, error)) HTTP {
	return HTTP{usecase: usecase, securityError: securityError}
}
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgChannelIdFormatError)
		return
	}
	output, err := h.usecase(c).Execute(Input{ID: id})
	if errors.Is(err, domain.ErrChannelNotFound) {
		transport.WriteError(c, err)
		return
	}
	if err != nil {
		h.securityError(c, err)
		return
	}
	common.ApiSuccess(c, output)
}
