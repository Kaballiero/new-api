package get_key

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/dto"
	"github.com/QuantumNous/new-api/internal/apikeys/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) HTTP { return HTTP{reader: reader} }
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	token, err := NewUsecase(h.reader).Execute(Input{ID: id, UserID: userId})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, dto.Masked(token, domain.MaskKey, common.SysError))
}
