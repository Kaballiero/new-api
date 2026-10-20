package list_models

import (
	"github.com/QuantumNous/new-api/common"
	response "github.com/QuantumNous/new-api/internal/adapter/http/catalog"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	page := common.GetPageQuery(c)
	out, f := h.scenario.Execute(Input{Keyword: "", Vendor: "", Status: c.Query("status"), SyncOfficial: c.Query("sync_official"), SquareState: c.Query("square_state"), Page: page.GetPage(), PageSize: page.GetPageSize(), IncludeChannelModels: c.Query("include_channel_models") == "true"})
	if f != nil {
		response.Failure(c, f)
		return
	}
	common.ApiSuccess(c, out)
}
