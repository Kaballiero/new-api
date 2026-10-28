package clear_affinity_cache

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/i18n"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	output, err := h.scenario.Execute(Input{All: strings.TrimSpace(c.Query("all")), Rule: strings.TrimSpace(c.Query("rule_name"))})
	if err != nil {
		message := err.Error()
		if errors.Is(err, ErrRuleRequired) {
			message = i18n.T(c, i18n.MsgChannelAffinityRuleRequired)
		}
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": output})
}
