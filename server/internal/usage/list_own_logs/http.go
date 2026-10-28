package list_own_logs

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/usage/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	usecase Usecase
}

func NewHandler(usecase Usecase) Handler {
	return Handler{usecase: usecase}
}

func (h Handler) Handle(c *gin.Context) {
	page := common.GetPageQuery(c)
	query := transport.ReadLogQuery(c)
	query.UserID = c.GetInt("id")
	if value, supplied := c.GetQuery("token_id"); supplied {
		tokenID, err := strconv.Atoi(value)
		if err != nil || tokenID <= 0 {
			common.ApiErrorI18nStatusCode(c, 400, "invalid_params", i18n.MsgInvalidParams)
			return
		}
		query.TokenID = tokenID
	}
	values, _ := c.GetQueryArray("types")
	result, err := h.usecase.Execute(Input{Query: query, TypesQuery: values})
	if err != nil {
		transport.WriteLogError(c, err)
		return
	}
	page.SetTotal(int(result.Total))
	page.SetItems(result.Items)
	common.ApiSuccess(c, page)
}
