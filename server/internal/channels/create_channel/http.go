package create_channel

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	var request AddChannelRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	if failure := h.scenario.Execute(request); failure != nil {
		switch failure.Kind {
		case "internal":
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", failure.Cause)
		case "plugin_permission":
			c.JSON(http.StatusOK, gin.H{"success": false, "message": failure.Message})
		default:
			common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, failure.Kind, failure.Message)
		}
		return
	}
	common.ApiSuccessStatus(c, http.StatusCreated, nil)
}
