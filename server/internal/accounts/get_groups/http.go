package get_groups

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) *HTTP { return &HTTP{scenario: scenario} }
func (h *HTTP) List(c *gin.Context) {
	groupNames := h.scenario.Execute()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": groupNames})
}

func (h *HTTP) ForUser(c *gin.Context) {
	userId := c.GetInt("id")
	labels := domain.GroupLabels{
		UserGroup: common.TranslateMessage(c, i18n.MsgAccountUserGroupLabel),
		AutoRatio: common.TranslateMessage(c, i18n.MsgAccountAutoGroupRatio),
	}
	usableGroups := h.scenario.ForUser(Input{UserID: userId, Labels: labels})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": usableGroups})
}
