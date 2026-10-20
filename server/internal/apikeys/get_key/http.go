package get_key

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	tokenhttp "github.com/QuantumNous/new-api/internal/adapter/apikeyhttp"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/dto"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
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
	token, failure := NewUsecase(h.reader).Execute(Input{ID: id, UserID: userId})
	if failure != nil {
		tokenhttp.WriteFailure(c, failure)
		return
	}
	common.ApiSuccess(c, dto.Masked(token, domain.MaskKey, common.SysError))
}
