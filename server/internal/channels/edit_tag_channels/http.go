package edit_tag_channels

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	var input ChannelTag
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "参数错误"})
		return
	}
	if failure := h.scenario.Execute(input); failure != nil {
		switch failure.Kind {
		case "permission":
			common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
		case "internal":
			common.ApiError(c, failure.Cause)
		default:
			c.JSON(http.StatusOK, gin.H{"success": false, "message": failure.Message})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
