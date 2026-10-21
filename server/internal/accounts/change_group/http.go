package change_group

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/transport"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) *HTTP { return &HTTP{scenario: scenario} }
func (h *HTTP) Handle(c *gin.Context) {
	var req BulkUpdateUserGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgInvalidParams)
		return
	}
	result, err := h.scenario.Execute(Input{UserIDs: req.UserIds, Group: req.Group, ActorRole: c.GetInt("role")})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, BulkUpdateUserGroupResponse{
		Updated:     result.Updated,
		UpdatedIds:  result.UpdatedIDs,
		SkippedIds:  result.SkippedIDs,
		NotFoundIds: result.NotFoundIDs,
	})
}
