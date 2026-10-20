package change_group

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/gin-gonic/gin"
	"net/http"
)

const bulkUpdateUserGroupMaxBatch = accounts.MaxBulkGroupBatch

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
		switch {
		case errors.Is(err, accounts.ErrInvalidGroupChange):
			common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgInvalidParams)
		case errors.Is(err, accounts.ErrGroupBatchTooLarge):
			common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgBatchTooMany, map[string]any{"Max": bulkUpdateUserGroupMaxBatch})
		case errors.Is(err, accounts.ErrGroupDoesNotExist):
			common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgGroupNotExists, map[string]any{"Group": req.Group})
		case errors.Is(err, accounts.ErrGroupUsersNotFound):
			common.ApiErrorI18nStatus(c, http.StatusNotFound, i18n.MsgUserNotExists)
		case errors.Is(err, accounts.ErrGroupChangeForbidden):
			common.ApiErrorI18nStatus(c, http.StatusForbidden, i18n.MsgUserNoPermissionHigherLevel)
		default:
			common.ApiErrorStatus(c, http.StatusInternalServerError, err)
		}
		return
	}
	common.ApiSuccess(c, BulkUpdateUserGroupResponse{
		Updated:     result.Updated,
		UpdatedIds:  result.UpdatedIDs,
		SkippedIds:  result.SkippedIDs,
		NotFoundIds: result.NotFoundIDs,
	})
}
