package transport

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/usage/domain"

	"github.com/gin-gonic/gin"
)

func Audience(c *gin.Context) domain.Audience {
	if c.GetInt("role") >= common.RoleRootUser {
		return domain.AudienceRoot
	}
	return domain.AudienceAdmin
}

func ReadLogQuery(c *gin.Context) domain.LogQuery {
	page := common.GetPageQuery(c)
	kind, _ := strconv.Atoi(c.Query("type"))
	start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	channel, _ := strconv.Atoi(c.Query("channel"))
	return domain.LogQuery{Kind: kind, StartTimestamp: start, EndTimestamp: end,
		Username: c.Query("username"), TokenName: c.Query("token_name"), ModelName: c.Query("model_name"),
		Channel: channel, Group: c.Query("group"), RequestID: c.Query("request_id"), UpstreamRequestID: c.Query("upstream_request_id"),
		Start: page.GetStartIdx(), Count: page.GetPageSize()}
}

func WriteLogError(c *gin.Context, err error) {
	if errors.Is(err, domain.ErrTokenNotFound) {
		common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "token_not_found", i18n.MsgTokenNotExists)
		return
	}
	common.ApiErrorMsgStatusCode(c, 500, "internal_error", Message(c, err))
}

func Deprecated(c *gin.Context) {
	c.JSON(200, gin.H{"success": false, "message": Translate(c, i18n.MsgUsageDeprecated)})
}
