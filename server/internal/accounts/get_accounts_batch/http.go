package get_accounts_batch

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(reader Reader, rootRole int) *HTTP { return &HTTP{scenario: NewUsecase(reader, rootRole)} }

type queryActorRole struct{ context *gin.Context }

func (actor queryActorRole) ActorRole() int { return actor.context.GetInt("role") }

func (h *HTTP) Handle(c *gin.Context) {
	ids, valid := ParseUserBatchIDs(c.Query("ids"))
	if !valid {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	result, err := h.scenario.Execute(ids, queryActorRole{context: c})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	var users []UserBatchRow
	if result.Users != nil {
		users = make([]UserBatchRow, len(result.Users))
		for i, user := range result.Users {
			users[i] = UserBatchRow(user)
		}
	}
	common.ApiSuccess(c, GetUsersBatchResponse{Users: users, SkippedIDs: result.SkippedIDs, NotFoundIDs: result.NotFoundIDs})
}
