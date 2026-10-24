package delete_subscription

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
	"github.com/gin-gonic/gin"
	"strconv"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(u Usecase) HTTP { return HTTP{u} }
func (h HTTP) Handle(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		transport.WriteError(c, domain.ErrInvalidSubscriptionID)
		return
	}
	group, err := h.usecase.Execute(id)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	if group != "" {
		common.ApiSuccess(c, gin.H{"message": i18n.Translate(i18n.GetLangFromContext(c), i18n.MsgSubscriptionsGroupDowngrade, map[string]any{"Group": group})})
		return
	}
	common.ApiSuccess(c, nil)
}
