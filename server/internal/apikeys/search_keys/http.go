package search_keys

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/dto"
	"github.com/QuantumNous/new-api/internal/apikeys/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	reader        Reader
	maxTokens     func() int
	log, errorLog func(string)
}

func NewHTTP(reader Reader, maxTokens func() int, log, errorLog func(string)) HTTP {
	return HTTP{reader: reader, maxTokens: maxTokens, log: log, errorLog: errorLog}
}
func (h HTTP) Handle(c *gin.Context) {
	userId := c.GetInt("id")
	keyword := c.Query("keyword")
	token := c.Query("token")

	pageInfo := common.GetPageQuery(c)

	result, err := NewUsecase(h.reader, h.maxTokens, h.log, h.errorLog).Execute(Input{UserID: userId, Start: pageInfo.GetStartIdx(), Limit: pageInfo.GetPageSize(), Keyword: keyword, Key: token})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	pageInfo.SetTotal(int(result.Total))
	pageInfo.SetItems(dto.MaskedList(result.Tokens, domain.MaskKey, common.SysError))
	common.ApiSuccess(c, pageInfo)
}
