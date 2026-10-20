package list_keys

import (
	"github.com/QuantumNous/new-api/common"
	tokenhttp "github.com/QuantumNous/new-api/internal/adapter/apikeyhttp"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/dto"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) HTTP { return HTTP{reader: reader} }
func (h HTTP) Handle(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	result, failure := NewUsecase(h.reader).Execute(Input{UserID: userId, Start: pageInfo.GetStartIdx(), Limit: pageInfo.GetPageSize()})
	if failure != nil {
		tokenhttp.WriteFailure(c, failure)
		return
	}
	pageInfo.SetTotal(int(result.Total))
	pageInfo.SetItems(dto.MaskedList(result.Tokens, domain.MaskKey, common.SysError))
	common.ApiSuccess(c, pageInfo)
}
